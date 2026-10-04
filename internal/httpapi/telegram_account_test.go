package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	app "github.com/ryancheng/telegram-downloader/internal/app/telegram"
)

type telegramAccountStub struct {
	snapshot app.Snapshot
	password func(string) error
	reads    atomic.Int64
}

func (s *telegramAccountStub) Account(context.Context) (app.Account, error) {
	return app.Account{State: "auth_required"}, nil
}
func (s *telegramAccountStub) VerifyAccount(context.Context) error { return nil }
func (s *telegramAccountStub) StartLogin(context.Context) (app.Snapshot, error) {
	return s.snapshot, nil
}
func (s *telegramAccountStub) Attempt(context.Context, string) (app.Snapshot, error) {
	s.reads.Add(1)
	return s.snapshot, nil
}
func (s *telegramAccountStub) Password(_ context.Context, _ string, p string) error {
	return s.password(p)
}
func (s *telegramAccountStub) Cancel(context.Context, string) (app.Snapshot, error) {
	return s.snapshot, nil
}
func (s *telegramAccountStub) SnapshotForTask(context.Context, string) (app.Input, error) {
	return app.Input{}, nil
}
func telegramRouter(service TelegramAccountService) http.Handler {
	r := chi.NewRouter()
	NewTelegram(service).RegisterRoutes(r)
	return r
}
func TestTelegramDisabledRoutesReturnUnavailable(t *testing.T) {
	r := telegramRouter(nil)
	for _, tc := range []struct{ method, path string }{{"GET", "/api/telegram/account"}, {"POST", "/api/telegram/login-attempts"}, {"GET", "/api/telegram/login-attempts/test/events"}} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != 503 || !strings.Contains(w.Body.String(), "telegram_unavailable") {
			t.Fatalf("response %d %s", w.Code, w.Body)
		}
	}
}
func TestTelegramPasswordStrictInputNeverEchoes(t *testing.T) {
	s := &telegramAccountStub{password: func(p string) error {
		if p != "private-test-password" {
			t.Fatal("password not forwarded")
		}
		return errors.New("private-test-password internal detail")
	}}
	r := telegramRouter(s)
	for _, body := range []string{`{"password":"private-test-password"}`, `{"password":"private-test-password","unknown":true}`, `{"password":"private-test-password"}{}`} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/api/telegram/login-attempts/test/password", strings.NewReader(body)))
		if w.Code < 400 || strings.Contains(w.Body.String(), "private-test-password") || strings.Contains(w.Body.String(), "internal detail") {
			t.Fatalf("unsafe response %d", w.Code)
		}
	}
}
func TestTelegramSSEImmediateCurrentSnapshotIgnoresReplayID(t *testing.T) {
	s := &telegramAccountStub{snapshot: app.Snapshot{ID: strings.Repeat("a", 32), Seq: 9, Revision: 9, State: "connected", ExpiresAt: time.Now().Add(time.Minute)}}
	server := httptest.NewServer(telegramRouter(s))
	defer server.Close()
	request, _ := http.NewRequest("GET", server.URL+"/api/telegram/login-attempts/"+s.snapshot.ID+"/events", nil)
	request.Header.Set("Last-Event-ID", "1")
	response, e := server.Client().Do(request)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	data, e := io.ReadAll(response.Body)
	if e != nil {
		t.Fatal(e)
	}
	if response.StatusCode != 200 || response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Accel-Buffering") != "no" || !strings.Contains(string(data), "id: 9\n") {
		t.Fatal("SSE headers/current event mismatch")
	}
	var snapshot app.Snapshot
	line := strings.Split(string(data), "\n")[1]
	if e = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &snapshot); e != nil || snapshot.State != "connected" {
		t.Fatal("current snapshot missing", e)
	}
	if s.reads.Load() != 1 {
		t.Fatal("terminal SSE kept polling")
	}
}
func TestTelegramSSEDisconnectStopsPolling(t *testing.T) {
	s := &telegramAccountStub{snapshot: app.Snapshot{ID: strings.Repeat("a", 32), Seq: 1, Revision: 1, State: "waiting_qr", ExpiresAt: time.Now().Add(time.Minute)}}
	server := httptest.NewServer(telegramRouter(s))
	defer server.Close()
	response, e := server.Client().Get(server.URL + "/api/telegram/login-attempts/" + s.snapshot.ID + "/events")
	if e != nil {
		t.Fatal(e)
	}
	scanner := bufio.NewScanner(response.Body)
	if !scanner.Scan() || scanner.Text() != "id: 1" {
		t.Fatal("initial SSE not flushed")
	}
	response.Body.Close()
	time.Sleep(600 * time.Millisecond)
	reads := s.reads.Load()
	time.Sleep(600 * time.Millisecond)
	if s.reads.Load() != reads {
		t.Fatal("client disconnect left polling alive")
	}
}
