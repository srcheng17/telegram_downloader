package httpapi

import (
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/ryancheng/telegram-downloader/internal/app/adminauth"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type authTestRepository struct {
	mu       sync.Mutex
	account  adminauth.Account
	sessions map[string]adminauth.Session
}

func (r *authTestRepository) Account(context.Context) (adminauth.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.account.PasswordHash == "" {
		return r.account, adminauth.ErrNotFound
	}
	return r.account, nil
}
func (r *authTestRepository) Bootstrap(_ context.Context, hash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.account.PasswordHash == "" {
		r.account = adminauth.Account{PasswordHash: hash, CredentialVersion: 1}
	}
	return nil
}
func (r *authTestRepository) CreateSession(_ context.Context, s adminauth.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[s.TokenHash] = s
	return nil
}
func (r *authTestRepository) Session(_ context.Context, key string, _ time.Time, _ bool) (adminauth.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[key]
	if !ok || (!s.Preauth && s.CredentialVersion != r.account.CredentialVersion) {
		return s, adminauth.ErrUnauthenticated
	}
	return s, nil
}
func (r *authTestRepository) RotateSession(_ context.Context, key string, s adminauth.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sessions[key]; !ok {
		return adminauth.ErrUnauthenticated
	}
	delete(r.sessions, key)
	r.sessions[s.TokenHash] = s
	return nil
}
func (r *authTestRepository) DeleteSession(_ context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, key)
	return nil
}
func (r *authTestRepository) ChangePassword(_ context.Context, version int64, hash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if version != r.account.CredentialVersion {
		return adminauth.ErrUnauthenticated
	}
	r.account.CredentialVersion++
	r.account.PasswordHash = hash
	r.sessions = map[string]adminauth.Session{}
	return nil
}
func TestAdminAuthMiddlewareMatrix(t *testing.T) {
	repo := &authTestRepository{sessions: map[string]adminauth.Session{}}
	now := time.Now()
	service := adminauth.NewService(repo, adminauth.Options{BcryptCost: 4, Now: func() time.Time { return now }})
	if err := service.Bootstrap(context.Background(), "synthetic-long-password"); err != nil {
		t.Fatal(err)
	}
	auth, err := NewAdminAuth(service, AdminAuthOptions{PublicOrigin: "https://workspace.test", PublicStaticPaths: []string{"/static/dist/app.bundle.js"}})
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	auth.RegisterRoutes(r)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	r.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	r.Handle("/*", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	handler := auth.Middleware(r)
	request := func(method, path, body string, cookie *http.Cookie, csrf, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "https://workspace.test"+path, strings.NewReader(body))
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	for _, path := range []string{"/", "/settings", "/logs", "/api/tasks", "/api/new-future-route", "/api/telegram/account", "/api/telegram/events", "/v2/tasks", "/downloads/private.cbz", "/static/private.json", "/healthz/private"} {
		if got := request("GET", path, "", nil, "", ""); got.Code != 401 {
			t.Fatalf("anonymous %s: %d", path, got.Code)
		}
	}
	for _, path := range []string{"/auth/login", "/healthz", "/readyz", "/static/dist/app.bundle.js"} {
		if got := request("GET", path, "", nil, "", ""); got.Code != 200 {
			t.Fatalf("public %s: %d", path, got.Code)
		}
	}
	pre := request("GET", "/api/auth/session", "", nil, "", "")
	if pre.Code != 200 {
		t.Fatalf("preauth %d", pre.Code)
	}
	var state struct {
		CSRF          string `json:"csrf_token"`
		Authenticated bool   `json:"authenticated"`
	}
	if json.Unmarshal(pre.Body.Bytes(), &state) != nil {
		t.Fatal("bad session response")
	}
	preCookie := pre.Result().Cookies()[0]
	if !preCookie.HttpOnly || !preCookie.Secure || preCookie.SameSite != http.SameSiteStrictMode || preCookie.Domain != "" {
		t.Fatal("cookie policy")
	}
	for _, tc := range []struct{ csrf, origin string }{{"", "https://workspace.test"}, {state.CSRF, "https://evil.test"}, {state.CSRF, ""}} {
		if got := request("POST", "/api/auth/login", `{"password":"synthetic-long-password"}`, preCookie, tc.csrf, tc.origin); got.Code != 403 {
			t.Fatal("login CSRF accepted")
		}
	}
	logged := request("POST", "/api/auth/login", `{"password":"synthetic-long-password"}`, preCookie, state.CSRF, "https://workspace.test")
	if logged.Code != 200 {
		t.Fatalf("login %d", logged.Code)
	}
	cookie := logged.Result().Cookies()[0]
	if cookie.Value == preCookie.Value {
		t.Fatal("fixation")
	}
	_ = json.Unmarshal(logged.Body.Bytes(), &state)
	if got := request("GET", "/api/tasks", "", preCookie, "", ""); got.Code != 401 {
		t.Fatal("preauth remains authorized")
	}
	if got := request("GET", "/v2/tasks", "", cookie, "", ""); got.Code != 200 {
		t.Fatal("business route denied")
	}
	if got := request("POST", "/api/tasks", "{}", cookie, state.CSRF, "https://workspace.test"); got.Code != 200 {
		t.Fatal("valid CSRF denied")
	}
	forged := httptest.NewRequest("POST", "https://workspace.test/api/tasks", strings.NewReader("{}"))
	forged.AddCookie(cookie)
	forged.Header.Set("X-CSRF-Token", state.CSRF)
	forged.Header.Set("Origin", "https://evil.test")
	forged.Header.Set("X-Forwarded-Host", "evil.test")
	forged.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, forged)
	if w.Code != 403 {
		t.Fatal("forwarded origin bypass")
	}
	now = now.Add(adminauth.AbsoluteTTL)
	if got := request("GET", "/api/tasks", "", cookie, "", ""); got.Code != 401 {
		t.Fatal("absolute expiry missing")
	}
	if got := request("GET", "/v2/tasks", "", nil, "", ""); strings.Contains(got.Body.String(), `"code"`) {
		t.Fatal("v2 error envelope changed")
	}
}
func TestAdminAuthRequiresTLSExceptExplicitLoopback(t *testing.T) {
	service := adminauth.NewService(&authTestRepository{}, adminauth.Options{BcryptCost: 4})
	for _, origin := range []string{"http://workspace.test", "http://localhost"} {
		if _, err := NewAdminAuth(service, AdminAuthOptions{PublicOrigin: origin}); err == nil {
			t.Fatal("insecure origin allowed")
		}
	}
	if _, err := NewAdminAuth(service, AdminAuthOptions{PublicOrigin: "http://127.0.0.1:8080", AllowInsecureLoopback: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAdminAuth(service, AdminAuthOptions{PublicOrigin: "http://workspace.test", AllowInsecureLoopback: true}); err == nil {
		t.Fatal("non-loopback dev bypass")
	}
}
func TestAdminAuthPasswordBounds(t *testing.T) {
	for _, password := range []string{"short", strings.Repeat("x", 73), strings.Repeat("界", 25)} {
		if adminauth.ValidPassword(password) {
			t.Fatal("invalid password accepted")
		}
	}
	if !adminauth.ValidPassword(strings.Repeat("界", 12)) {
		t.Fatal("unicode password rejected")
	}
}
