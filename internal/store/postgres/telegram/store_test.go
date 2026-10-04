package telegram

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/ryancheng/telegram-downloader/internal/app/telegram"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres/migrations"
)

func openTelegramStore(t *testing.T) (context.Context, *Store) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	if e = migrations.Run(ctx, pool); e != nil {
		t.Fatal(e)
	}
	reset := func() {
		if _, e := pool.Exec(ctx, `DELETE FROM telegram_login_attempts; UPDATE telegram_account SET revision=0,namespace='',identity='',verified_at=NULL WHERE singleton`); e != nil {
			t.Fatal(e)
		}
	}
	reset()
	t.Cleanup(reset)
	return ctx, NewStore(pool)
}
func attemptFixture(id string) app.AttemptRecord {
	return app.AttemptRecord{Snapshot: app.Snapshot{ID: id, Seq: 1, Revision: 1, State: "waiting_qr", ExpiresAt: time.Now().Add(time.Minute)}, Owner: "owner", Namespace: "candidate_" + id}
}
func TestStorePromotionCASAndEphemeralMaterial(t *testing.T) {
	ctx, s := openTelegramStore(t)
	a := attemptFixture(strings.Repeat("a", 32))
	a.QR = "sensitive-not-persisted"
	expires := time.Now().Add(time.Second)
	a.QRExpiresAt = &expires
	if e := s.CreateAttempt(ctx, a); e != nil {
		t.Fatal(e)
	}
	persisted, e := s.Attempt(ctx, a.ID)
	if e != nil || persisted.QR != "" || persisted.QRExpiresAt != nil {
		t.Fatal("ephemeral QR persisted")
	}
	wrongOwner := a
	wrongOwner.Owner = "stale"
	wrongOwner.Seq++
	wrongOwner.Revision++
	if e = s.UpdateAttempt(ctx, wrongOwner); !errors.Is(e, app.ErrConflict) {
		t.Fatal("stale owner update", e)
	}
	a.State = "verifying"
	a.Seq++
	a.Revision++
	if e = s.UpdateAttempt(ctx, a); e != nil {
		t.Fatal(e)
	}
	if e = s.UpdateAttempt(ctx, a); !errors.Is(e, app.ErrConflict) {
		t.Fatal("out-of-order sequence accepted", e)
	}
	stale := a
	stale.ExpectedRevision = 9
	if _, e = s.Promote(ctx, stale, strings.Repeat("b", 64), time.Now()); !errors.Is(e, app.ErrConflict) {
		t.Fatal("stale account promotion accepted", e)
	}
	unchanged, _ := s.Account(ctx)
	if unchanged.Revision != 0 {
		t.Fatal("failed promotion mutated account")
	}
	active, e := s.Promote(ctx, a, strings.Repeat("b", 64), time.Now())
	if e != nil || active.Revision != 1 || active.Namespace != a.Namespace {
		t.Fatal("promotion failed", e)
	}
	connected, e := s.Attempt(ctx, a.ID)
	if e != nil || connected.State != "connected" || connected.Seq != 3 {
		t.Fatal("attempt not atomically connected", e)
	}
	a.Seq++
	a.Revision++
	a.State = "failed"
	if e = s.UpdateAttempt(ctx, a); !errors.Is(e, app.ErrConflict) {
		t.Fatal("terminal attempt regressed", e)
	}
	var forbidden int
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name IN ('telegram_account','telegram_login_attempts') AND column_name IN ('qr','password','phone','api_hash','session','token')`).Scan(&forbidden); e != nil || forbidden != 0 {
		t.Fatal("ephemeral persistence columns present", e)
	}
}
func TestStoreReconnectPreservesActiveAndSupersedesOrphans(t *testing.T) {
	ctx, s := openTelegramStore(t)
	first := attemptFixture(strings.Repeat("a", 32))
	first.State = "verifying"
	if e := s.CreateAttempt(ctx, first); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Promote(ctx, first, strings.Repeat("a", 64), time.Now()); e != nil {
		t.Fatal(e)
	}
	orphan := attemptFixture(strings.Repeat("b", 32))
	orphan.ExpectedRevision = 1
	if e := s.CreateAttempt(ctx, orphan); e != nil {
		t.Fatal(e)
	}
	replacement := attemptFixture(strings.Repeat("c", 32))
	replacement.ExpectedRevision = 1
	if e := s.CreateAttempt(ctx, replacement); e != nil {
		t.Fatal(e)
	}
	old, e := s.Attempt(ctx, orphan.ID)
	if e != nil || old.State != "failed" || old.Code != "interrupted" {
		t.Fatal("orphan not retired", e)
	}
	replacement.State = "verifying"
	replacement.Seq++
	replacement.Revision++
	if e = s.UpdateAttempt(ctx, replacement); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE telegram_login_attempts SET expires_at=now()-interval '1 second' WHERE id=$1`, replacement.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Promote(ctx, replacement, strings.Repeat("d", 64), time.Now()); !errors.Is(e, app.ErrConflict) {
		t.Fatal("expired reconnect promoted", e)
	}
	account, e := s.Account(ctx)
	if e != nil || account.Revision != 1 || account.Namespace != first.Namespace {
		t.Fatal("failed reconnect changed active account", e)
	}
}
