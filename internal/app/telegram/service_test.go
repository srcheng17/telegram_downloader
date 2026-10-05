package telegram

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testIdentity = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type memoryStore struct {
	mu      sync.Mutex
	account AccountRecord
	records map[string]AttemptRecord
	latest  string
}

func newMemoryStore() *memoryStore { return &memoryStore{records: map[string]AttemptRecord{}} }
func (s *memoryStore) Account(context.Context) (AccountRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.account, nil
}
func (s *memoryStore) Attempt(_ context.Context, id string) (AttemptRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	if !ok {
		return r, ErrNotFound
	}
	return r, nil
}
func (s *memoryStore) LatestAttempt(ctx context.Context) (AttemptRecord, error) {
	s.mu.Lock()
	id := s.latest
	s.mu.Unlock()
	return s.Attempt(ctx, id)
}
func (s *memoryStore) CreateAttempt(_ context.Context, r AttemptRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[r.ID] = r
	s.latest = r.ID
	return nil
}
func (s *memoryStore) UpdateAttempt(_ context.Context, r AttemptRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.QR != "" || r.QRExpiresAt != nil {
		panic("ephemeral material persisted")
	}
	if _, ok := s.records[r.ID]; !ok {
		return ErrNotFound
	}
	s.records[r.ID] = r
	return nil
}
func (s *memoryStore) Promote(_ context.Context, r AttemptRecord, id string, at time.Time) (AccountRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.ExpectedRevision != s.account.Revision {
		return AccountRecord{}, ErrConflict
	}
	s.account = AccountRecord{Revision: s.account.Revision + 1, Namespace: r.Namespace, Identity: id, VerifiedAt: at}
	r.State = "connected"
	r.Seq++
	r.Revision++
	s.records[r.ID] = r
	return s.account, nil
}

type fakeCoordinator struct{ locked atomic.Bool }

func (c *fakeCoordinator) Busy(context.Context) (bool, error) { return c.locked.Load(), nil }
func (c *fakeCoordinator) Do(ctx context.Context, wait bool, fn func(context.Context, Lease) error) error {
	for !c.locked.CompareAndSwap(false, true) {
		if !wait {
			return ErrBusy
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
	defer c.locked.Store(false)
	return fn(ctx, Lease{})
}

type fakeProcess struct {
	events   chan BridgeEvent
	done     chan struct{}
	waited   atomic.Bool
	password chan string
}

func (p *fakeProcess) Events() <-chan BridgeEvent { return p.events }
func (p *fakeProcess) Wait() error                { <-p.done; p.waited.Store(true); return nil }
func (p *fakeProcess) Password(s string) error    { p.password <- s; return nil }
func loginProcess(ctx context.Context, events []BridgeEvent, hold bool) *fakeProcess {
	p := &fakeProcess{events: make(chan BridgeEvent, 8), done: make(chan struct{}), password: make(chan string, 1)}
	go func() {
		defer close(p.done)
		defer close(p.events)
		for _, e := range events {
			p.events <- e
		}
		if hold {
			<-ctx.Done()
		}
	}()
	return p
}

type fakeBridge struct {
	login    func(context.Context) (LoginProcess, error)
	verify   func(context.Context, string) (string, error)
	inspect  func() (BridgeEvent, error)
	download func(string) error
	discard  func(string) error
}

func (b *fakeBridge) Login(ctx context.Context, _ Lease, _ string) (LoginProcess, error) {
	return b.login(ctx)
}
func (b *fakeBridge) Verify(ctx context.Context, _ Lease, ns string) (string, error) {
	if b.verify != nil {
		return b.verify(ctx, ns)
	}
	return testIdentity, nil
}
func (b *fakeBridge) Inspect(context.Context, Lease, string, string, int64) (BridgeEvent, error) {
	return b.inspect()
}
func (b *fakeBridge) Download(_ context.Context, _ Lease, _, _, dir string, _ int64) error {
	return b.download(dir)
}
func (b *fakeBridge) Discard(_ context.Context, _ Lease, ns string) error {
	if b.discard != nil {
		return b.discard(ns)
	}
	return nil
}
func testService(t *testing.T, s *memoryStore, b *fakeBridge) *Service {
	t.Helper()
	v, e := NewService(s, &fakeCoordinator{}, b, Options{})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(v.Close)
	return v
}
func waitAttempt(t *testing.T, s *Service, id string) Snapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		v, e := s.Attempt(context.Background(), id)
		if e != nil {
			t.Fatal(e)
		}
		if terminal(v.State) {
			return v
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("attempt did not terminate")
	return Snapshot{}
}

func TestLoginRequiresExplicitAuthorizationAndFreshProcess(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		authorized, mismatch bool
		want                 string
	}{{"success", true, false, "connected"}, {"exit_zero_without_auth", false, false, "failed"}, {"identity_mismatch", true, true, "failed"}} {
		t.Run(tc.name, func(t *testing.T) {
			s := newMemoryStore()
			s.account = AccountRecord{Revision: 7, Namespace: "old", Identity: testIdentity}
			var p *fakeProcess
			var calls atomic.Int64
			var discarded atomic.Bool
			b := &fakeBridge{login: func(ctx context.Context) (LoginProcess, error) {
				expires := time.Now().Add(time.Minute)
				events := []BridgeEvent{{Type: "qr", QR: "data:image/png;base64,eA==", ExpiresAt: &expires}}
				if tc.authorized {
					events = append(events, BridgeEvent{Type: "authorized", Identity: testIdentity})
				}
				p = loginProcess(ctx, events, false)
				return p, nil
			}, verify: func(context.Context, string) (string, error) {
				calls.Add(1)
				if !p.waited.Load() {
					t.Error("fresh verify ran before process exit")
				}
				if tc.mismatch {
					return strings.Repeat("b", 64), nil
				}
				return testIdentity, nil
			}, discard: func(string) error {
				if p != nil && !p.waited.Load() {
					t.Error("discard before process wait")
				}
				discarded.Store(true)
				return nil
			}}
			service := testService(t, s, b)
			snap, err := service.StartLogin(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			result := waitAttempt(t, service, snap.ID)
			service.Close()
			if result.State != tc.want {
				t.Fatalf("state %s code %s", result.State, result.Code)
			}
			account, _ := s.Account(context.Background())
			if tc.want == "connected" {
				if account.Revision != 8 || calls.Load() != 1 || discarded.Load() {
					t.Fatal("promotion contract failed")
				}
			} else if account.Revision != 7 || account.Namespace != "old" || !discarded.Load() {
				t.Fatal("failed reconnect changed active account or leaked candidate")
			}
		})
	}
}
func TestStartupFailurePersistsTerminalAttempt(t *testing.T) {
	s := newMemoryStore()
	var discarded atomic.Bool
	b := &fakeBridge{login: func(context.Context) (LoginProcess, error) { return nil, Failure("bridge_failed") }, discard: func(string) error { discarded.Store(true); return nil }}
	service := testService(t, s, b)
	if _, err := service.StartLogin(context.Background()); err == nil {
		t.Fatal("expected startup failure")
	}
	service.Close()
	r, e := s.LatestAttempt(context.Background())
	if e != nil || r.State != "failed" || !discarded.Load() {
		t.Fatal("startup left an open attempt")
	}
}
func TestPasswordCancelAndCloseWaitForChild(t *testing.T) {
	s := newMemoryStore()
	var p *fakeProcess
	b := &fakeBridge{login: func(ctx context.Context) (LoginProcess, error) {
		p = loginProcess(ctx, []BridgeEvent{{Type: "password_required"}}, true)
		return p, nil
	}}
	service := testService(t, s, b)
	snap, err := service.StartLogin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		v, _ := service.Attempt(context.Background(), snap.ID)
		if v.State == "password_required" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err := service.Password(context.Background(), snap.ID, "private-test-value"); err != nil {
		t.Fatal(err)
	}
	if <-p.password != "private-test-value" {
		t.Fatal("password was not delivered")
	}
	if _, err := service.StartLogin(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatal("concurrent login accepted")
	}
	result, err := service.Cancel(context.Background(), snap.ID)
	if err != nil || result.State != "cancelled" || !p.waited.Load() {
		t.Fatalf("cancel did not wait: %#v %v", result, err)
	}
	if err = service.Password(context.Background(), snap.ID, "x"); !errors.Is(err, ErrConflict) {
		t.Fatal("terminal password accepted")
	}
	service.Close()
	if _, err = service.StartLogin(context.Background()); err == nil {
		t.Fatal("closed service accepted login")
	}
}
func TestCloseWaitsDuringProcessStartup(t *testing.T) {
	started := make(chan struct{})
	s := newMemoryStore()
	b := &fakeBridge{login: func(ctx context.Context) (LoginProcess, error) { close(started); <-ctx.Done(); return nil, ctx.Err() }}
	service := testService(t, s, b)
	returned := make(chan struct{})
	go func() { defer close(returned); _, _ = service.StartLogin(context.Background()) }()
	<-started
	service.Close()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("startup survived close")
	}
	r, _ := s.LatestAttempt(context.Background())
	if r.State != "cancelled" {
		t.Fatalf("state=%s", r.State)
	}
}
func TestAccountReadsCacheFailureUntilExplicitVerify(t *testing.T) {
	s := newMemoryStore()
	s.account = AccountRecord{Revision: 1, Namespace: "stored", Identity: testIdentity}
	var calls atomic.Int64
	var valid atomic.Bool
	b := &fakeBridge{verify: func(context.Context, string) (string, error) {
		calls.Add(1)
		if !valid.Load() {
			return "", Failure("auth_required")
		}
		return testIdentity, nil
	}}
	service := testService(t, s, b)
	for i := 0; i < 3; i++ {
		a, e := service.Account(context.Background())
		if e != nil || a.State != "auth_required" {
			t.Fatal(a, e)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("account polling reopened failed authorization")
	}
	valid.Store(true)
	if e := service.VerifyAccount(context.Background()); e != nil {
		t.Fatal(e)
	}
	a, _ := service.Account(context.Background())
	if a.State != "connected" || calls.Load() != 2 {
		t.Fatal("explicit verification failed")
	}
}
func TestDownloadValidationAndLargeSource(t *testing.T) {
	for _, tc := range []struct {
		name, extension  string
		declared, actual int64
		stale            bool
		want             string
	}{{"over_browser_limit", ".zip", 65 << 20, 65 << 20, false, ""}, {"size_mismatch", ".rar", 17, 18, false, "download_incomplete"}, {"above_cap", ".zip", MaximumSourceBytes + 1, 0, false, "source_too_large"}, {"unsupported", "/escape", 1, 1, false, "unsupported_media"}, {"account_changed", ".7z", 1, 1, true, "account_changed"}} {
		t.Run(tc.name, func(t *testing.T) {
			s := newMemoryStore()
			s.account = AccountRecord{Revision: 1, Namespace: "stored", Identity: testIdentity}
			b := &fakeBridge{inspect: func() (BridgeEvent, error) {
				return BridgeEvent{Identity: testIdentity, Size: tc.declared, Extension: tc.extension}, nil
			}, download: func(dir string) error {
				f, e := os.Create(filepath.Join(dir, "source"))
				if e != nil {
					return e
				}
				defer f.Close()
				return f.Truncate(tc.actual)
			}}
			service := testService(t, s, b)
			in := Input{MessageURL: "https://t.me/test_channel/12", AccountRevision: 1, AccountIdentity: testIdentity}
			if tc.stale {
				in.AccountRevision++
			}
			dir := filepath.Join(t.TempDir(), "download")
			source, e := service.Download(context.Background(), in, dir)
			if tc.want != "" {
				if codeOf(e) != tc.want {
					t.Fatalf("error %v want %s", e, tc.want)
				}
				if _, e = os.Stat(dir); !errors.Is(e, os.ErrNotExist) {
					t.Fatal("failed private output survived")
				}
				return
			}
			if e != nil || source.Size != tc.actual || len(source.SHA256) != 64 || !strings.HasSuffix(source.Path, ".zip") {
				t.Fatalf("source %#v error %v", source, e)
			}
		})
	}
}
func TestParseMessageURLRejectsBatchAndNormalizes(t *testing.T) {
	valid := map[string]string{"https://t.me/Some_Channel/3870": "https://t.me/some_channel/3870", "https://t.me/c/12345/87": "https://t.me/c/12345/87"}
	for raw, want := range valid {
		got, e := ParseMessageURL(raw)
		if e != nil || got != want {
			t.Fatalf("canonical %q %v", got, e)
		}
	}
	for _, raw := range []string{"http://t.me/channel/1", "https://t.me/channel", "https://t.me/channel/0", "https://t.me/channel/1?single", "https://evil.example/channel/1", "https://t.me/channel/1\nhttps://t.me/channel/2", "https://user@t.me/channel/1"} {
		if _, e := ParseMessageURL(raw); e == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestForumLinksShareMessageIdentityAndRejectInt32Overflow(t *testing.T) {
	for raw, want := range map[string]string{"https://t.me/example_channel/12/34": "https://t.me/example_channel/34", "https://t.me/c/12345/12/34": "https://t.me/c/12345/34"} {
		got, err := ParseMessageURL(raw)
		if err != nil || got != want {
			t.Fatalf("forum identity %q %v", got, err)
		}
	}
	for _, raw := range []string{"https://t.me/example_channel/4294967297", "https://t.me/c/12345/4294967297", "https://t.me/example_channel/4294967297/1"} {
		if _, err := ParseMessageURL(raw); err == nil {
			t.Fatal("message ID could truncate to a different Telegram message")
		}
	}
}
func TestAccountReportsConfiguredSourceLimit(t *testing.T) {
	service, err := NewService(newMemoryStore(), &fakeCoordinator{}, &fakeBridge{}, Options{MaxSourceBytes: 17 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	account, err := service.Account(context.Background())
	if err != nil || account.MaxSourceBytes != 17<<20 {
		t.Fatal("effective cap not exposed", err)
	}
	if _, err = NewService(newMemoryStore(), &fakeCoordinator{}, &fakeBridge{}, Options{MaxSourceBytes: MaximumSourceBytes + 1}); err == nil {
		t.Fatal("cap above hard maximum accepted")
	}
}
