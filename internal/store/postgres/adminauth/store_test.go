package adminauth_test

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/ryancheng/telegram-downloader/internal/app/adminauth"
	store "github.com/ryancheng/telegram-downloader/internal/store/postgres/adminauth"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres/migrations"
	"os"
	"sync"
	"testing"
	"time"
)

func TestAdminLifecyclePostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = migrations.Run(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `TRUNCATE admin_account,admin_sessions`); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `TRUNCATE admin_account,admin_sessions`)
	now := time.Now().UTC()
	repo := store.New(pool)
	service := app.NewService(repo, app.Options{BcryptCost: 4, Now: func() time.Time { return now }})
	if err = service.Bootstrap(ctx, ""); !errors.Is(err, app.ErrBootstrapRequired) {
		t.Fatal("missing bootstrap accepted")
	}
	var wg sync.WaitGroup
	failures := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- service.Bootstrap(ctx, "initial-password-long") }()
	}
	wg.Wait()
	close(failures)
	for err = range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	first, _ := repo.Account(ctx)
	if first.PasswordHash == "initial-password-long" {
		t.Fatal("plaintext stored")
	}
	if err = service.Bootstrap(ctx, "replacement-ignored"); err != nil {
		t.Fatal(err)
	}
	second, _ := repo.Account(ctx)
	if first.PasswordHash != second.PasswordHash {
		t.Fatal("restart reset password")
	}
	pre, err := service.Preauth(ctx, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	logged, err := service.Login(ctx, pre.Token, "initial-password-long", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if pre.Token == logged.Token || pre.CSRFToken == logged.CSRFToken {
		t.Fatal("session fixation")
	}
	if _, err = service.Validate(ctx, pre.Token); !errors.Is(err, app.ErrUnauthenticated) {
		t.Fatal("preauth reusable")
	}
	now = now.Add(app.IdleTTL / 2)
	passive, err := service.CheckSession(ctx, logged.Token)
	if err != nil || !passive.LastSeen.Equal(logged.LastSeen.Truncate(time.Microsecond)) {
		t.Fatal("passive stream extended idle deadline")
	}
	now = now.Add(app.IdleTTL / 2)
	if _, err = service.Validate(ctx, logged.Token); !errors.Is(err, app.ErrUnauthenticated) {
		t.Fatal("idle expiry missing")
	}
	pre, _ = service.Preauth(ctx, "127.0.0.1")
	logged, err = service.Login(ctx, pre.Token, "initial-password-long", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if err = service.ChangePassword(ctx, logged.Token, "initial-password-long", "changed-password-long", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Validate(ctx, logged.Token); !errors.Is(err, app.ErrUnauthenticated) {
		t.Fatal("old session not revoked")
	}
	pre, _ = service.Preauth(ctx, "127.0.0.2")
	logged, err = service.Login(ctx, pre.Token, "changed-password-long", "127.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Logout(ctx, logged.Token); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Validate(ctx, logged.Token); !errors.Is(err, app.ErrUnauthenticated) {
		t.Fatal("logout not revoked")
	}
	pre, _ = service.Preauth(ctx, "127.0.0.3")
	for i := 0; i < 6; i++ {
		_, err = service.Login(ctx, pre.Token, "wrong-password", "127.0.0.3")
	}
	if !errors.Is(err, app.ErrRateLimited) {
		t.Fatal("login not rate limited")
	}
}
