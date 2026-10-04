package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ryancheng/telegram-downloader/internal/app/adminauth"
)

const AdminCookieName = "td_admin_session"

type AdminAuthOptions struct {
	PublicOrigin          string
	AllowInsecureLoopback bool
	PublicStaticPaths     []string
}
type AdminAuth struct {
	service *adminauth.Service
	origin  string
	secure  bool
	public  map[string]bool
}

func NewAdminAuth(service *adminauth.Service, opts AdminAuthOptions) (*AdminAuth, error) {
	u, err := url.Parse(opts.PublicOrigin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("invalid public origin")
	}
	secure := u.Scheme == "https"
	host := u.Hostname()
	ip := net.ParseIP(host)
	loopback := host == "localhost" || (ip != nil && ip.IsLoopback())
	if !secure && !(u.Scheme == "http" && opts.AllowInsecureLoopback && loopback) {
		return nil, errors.New("HTTPS public origin required")
	}
	if service == nil {
		return nil, errors.New("authentication service required")
	}
	a := &AdminAuth{service: service, origin: u.Scheme + "://" + u.Host, secure: secure, public: map[string]bool{}}
	for _, path := range opts.PublicStaticPaths {
		if strings.HasPrefix(path, "/static/") && !strings.Contains(path, "..") && !strings.ContainsAny(path, "%\\") {
			a.public[path] = true
		}
	}
	return a, nil
}
func (a *AdminAuth) RegisterRoutes(r chi.Router) {
	r.Get("/api/auth/session", a.session)
	r.Post("/api/auth/login", a.login)
	r.Post("/api/auth/logout", a.logout)
	r.Put("/api/auth/password", a.password)
}
func cookieToken(r *http.Request) string {
	c, err := r.Cookie(AdminCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}
func peerAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
func (a *AdminAuth) setCookie(w http.ResponseWriter, session adminauth.IssuedSession) {
	http.SetCookie(w, &http.Cookie{Name: AdminCookieName, Value: session.Token, Path: "/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteStrictMode, Expires: session.ExpiresAt})
}
func (a *AdminAuth) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: AdminCookieName, Value: "", Path: "/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}
func (a *AdminAuth) anonymous(r *http.Request) bool {
	if r.Method == http.MethodGet {
		switch r.URL.Path {
		case "/auth/login", "/api/auth/session", "/healthz", "/readyz":
			return true
		}
		return a.public[r.URL.Path]
	}
	return r.Method == http.MethodPost && r.URL.Path == "/api/auth/login"
}
func (a *AdminAuth) sameOrigin(r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		return origin == a.origin
	}
	ref, err := url.Parse(r.Referer())
	return err == nil && ref.User == nil && ref.Scheme+"://"+ref.Host == a.origin
}
func authError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	if strings.HasPrefix(r.URL.Path, "/v2/") {
		writeJSON(w, status, map[string]string{"error": message})
		return
	}
	writeAPIErrorResponse(w, status, code, message, nil)
}
func unauthorized(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/auth/login")
		authError(w, r, 401, "unauthenticated", "请先登录")
		return
	}
	if r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/v2/") && strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
		return
	}
	authError(w, r, 401, "unauthenticated", "请先登录")
}
func (a *AdminAuth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		public := a.anonymous(r)
		safe := r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions
		var session adminauth.Session
		if !public || !safe {
			var err error
			session, err = a.service.Validate(r.Context(), cookieToken(r))
			if err != nil || (!public && session.Preauth) {
				unauthorized(w, r)
				return
			}
		}
		if !safe {
			if !a.sameOrigin(r) {
				authError(w, r, 403, "csrf_rejected", "请求来源无效，请刷新后重试")
				return
			}
			token := r.Header.Get("X-CSRF-Token")
			if token == "" && strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
				r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
				if r.ParseForm() == nil {
					token = r.PostForm.Get("csrf_token")
				}
			}
			if !adminauth.ValidCSRF(session, token) {
				authError(w, r, 403, "csrf_rejected", "会话验证失效，请刷新后重试")
				return
			}
		}
		if !public && (strings.Contains(r.Header.Get("Accept"), "text/event-stream") || strings.HasSuffix(r.URL.Path, "/events")) {
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			r = r.WithContext(ctx)
			token := cookieToken(r)
			go func() {
				ticker := time.NewTicker(30 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						check, err := a.service.CheckSession(ctx, token)
						if err != nil || check.Preauth {
							cancel()
							return
						}
					}
				}
			}()
		}
		next.ServeHTTP(w, r)
	})
}
func sessionPayload(s adminauth.Session) map[string]any {
	return map[string]any{"authenticated": !s.Preauth, "csrf_token": s.CSRFToken, "expires_at": s.ExpiresAt}
}
func (a *AdminAuth) session(w http.ResponseWriter, r *http.Request) {
	s, err := a.service.Validate(r.Context(), cookieToken(r))
	if err == nil {
		writeJSON(w, 200, sessionPayload(s))
		return
	}
	// Storage failures fail closed instead of issuing substitute sessions.
	if !errors.Is(err, adminauth.ErrUnauthenticated) {
		authError(w, r, 503, "unavailable", "登录服务暂不可用")
		return
	}
	issued, err := a.service.Preauth(r.Context(), peerAddress(r))
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	a.setCookie(w, issued)
	writeJSON(w, 200, sessionPayload(issued.Session))
}
func strictJSON(w http.ResponseWriter, r *http.Request, dest any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("invalid JSON")
	}
	return nil
}
func (a *AdminAuth) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Password string `json:"password"`
	}
	if strictJSON(w, r, &input) != nil {
		authError(w, r, 422, "invalid_input", "登录信息无效")
		return
	}
	issued, err := a.service.Login(r.Context(), cookieToken(r), input.Password, peerAddress(r))
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	a.setCookie(w, issued)
	writeJSON(w, 200, sessionPayload(issued.Session))
}
func (a *AdminAuth) logout(w http.ResponseWriter, r *http.Request) {
	if err := a.service.Logout(r.Context(), cookieToken(r)); err != nil {
		a.writeError(w, r, err)
		return
	}
	a.clearCookie(w)
	writeJSON(w, 200, map[string]bool{"authenticated": false})
}
func (a *AdminAuth) password(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Current string `json:"current_password"`
		Next    string `json:"new_password"`
	}
	if strictJSON(w, r, &input) != nil {
		authError(w, r, 422, "invalid_input", "密码格式无效")
		return
	}
	if err := a.service.ChangePassword(r.Context(), cookieToken(r), input.Current, input.Next, peerAddress(r)); err != nil {
		a.writeError(w, r, err)
		return
	}
	a.clearCookie(w)
	writeJSON(w, 200, map[string]bool{"authenticated": false})
}
func (a *AdminAuth) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, adminauth.ErrRateLimited):
		w.Header().Set("Retry-After", "60")
		authError(w, r, 429, "rate_limited", "请求过于频繁，请稍后重试")
	case errors.Is(err, adminauth.ErrUnauthenticated):
		authError(w, r, 401, "unauthenticated", "登录失败，请检查密码或刷新页面")
	case errors.Is(err, adminauth.ErrInvalidPassword):
		authError(w, r, 422, "invalid_password", "密码至少需要12个字符，且不能超过72字节")
	default:
		authError(w, r, 503, "unavailable", "登录服务暂不可用")
	}
}
