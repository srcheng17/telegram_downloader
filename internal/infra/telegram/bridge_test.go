package telegram

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	app "github.com/ryancheng/telegram-downloader/internal/app/telegram"
)

const identityFixture = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func privateTestRoot(t *testing.T) string {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	return root
}
func bridgeFixture(t *testing.T, script string) (*Bridge, app.Lease) {
	t.Helper()
	root := privateTestRoot(t)
	helper := filepath.Join(root, "helper")
	if e := os.WriteFile(helper, []byte("#!/bin/sh\nread -r request\n"+script+"\n"), 0700); e != nil {
		t.Fatal(e)
	}
	b, e := NewBridge(BridgeOptions{HelperPath: helper, TDLPath: helper, PrivateRoot: root})
	if e != nil {
		t.Fatal(e)
	}
	f, e := lockFile(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { f.Close() })
	return b, app.Lease{File: f}
}
func errorCodeFixture(err error) string {
	var e *app.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
func TestBridgeRequiresBoundedStructuredSuccess(t *testing.T) {
	for _, tc := range []struct{ name, script, want string }{{"explicit_success", `printf '%s\n' '{"type":"authorized","identity":"` + identityFixture + `"}'`, ""}, {"exit_zero", "exit 0", "authorization_unconfirmed"}, {"garbage", `printf '%s\n' 'private-value-from-child'`, "invalid_bridge_output"}, {"unknown_field", `printf '%s\n' '{"type":"authorized","identity":"` + identityFixture + `","secret":"private-value"}'`, "invalid_bridge_output"}, {"overlong", `head -c 40000 /dev/zero | tr '\000' a`, "invalid_bridge_output"}, {"event_flood", `i=0; while [ "$i" -lt 40 ]; do printf '%s\n' '{"type":"password_required"}'; i=$((i+1)); done`, "invalid_bridge_output"}, {"explicit_failure", `printf '%s\n' '{"type":"error","code":"auth_required"}'; exit 1`, "auth_required"}} {
		t.Run(tc.name, func(t *testing.T) {
			b, lease := bridgeFixture(t, tc.script)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			id, e := b.Verify(ctx, lease, "candidate_"+strings.Repeat("a", 32))
			if tc.want == "" {
				if e != nil || id != identityFixture {
					t.Fatal("structured success failed", e)
				}
			} else if errorCodeFixture(e) != tc.want {
				t.Fatalf("code %v want %s", e, tc.want)
			}
			if e != nil && strings.Contains(e.Error(), "private-value") {
				t.Fatal("child output leaked")
			}
		})
	}
}
func TestBridgePasswordGateAndCancellationWait(t *testing.T) {
	b, lease := bridgeFixture(t, `printf '%s\n' '{"type":"password_required"}'; read -r private_input; exec sleep 30`)
	ctx, cancel := context.WithCancel(context.Background())
	p, e := b.Login(ctx, lease, "candidate_"+strings.Repeat("a", 32))
	if e != nil {
		t.Fatal(e)
	}
	select {
	case ev := <-p.Events():
		if ev.Type != "password_required" {
			t.Fatal("wrong event")
		}
	case <-time.After(time.Second):
		t.Fatal("missing event")
	}
	if e = p.Password("private-value"); e != nil {
		t.Fatal(e)
	}
	if e = p.Password("duplicate"); !errors.Is(e, app.ErrConflict) {
		t.Fatal("duplicate password accepted")
	}
	cancel()
	done := make(chan error, 1)
	go func() {
		for range p.Events() {
		}
		done <- p.Wait()
	}()
	select {
	case e = <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not reap child")
	}
	if busy, e := (&Coordinator{root: b.options.PrivateRoot}).Busy(context.Background()); e != nil || !busy {
		t.Fatal("bridge unlocked parent's inherited lock")
	}
}
func TestDiscardOnlyCandidateStorage(t *testing.T) {
	b, lease := bridgeFixture(t, "exit 0")
	ns := "candidate_" + strings.Repeat("a", 32)
	path := filepath.Join(b.options.PrivateRoot, "storage", ns)
	if e := os.WriteFile(path, []byte("synthetic session"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := b.Discard(context.Background(), lease, "../outside"); e == nil {
		t.Fatal("path traversal accepted")
	}
	if e := b.Discard(context.Background(), lease, ns); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(path); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("failed candidate remains")
	}
}
