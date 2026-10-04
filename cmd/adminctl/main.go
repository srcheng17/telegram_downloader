// adminctl is an offline maintenance tool, never exposed through HTTP.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ryancheng/telegram-downloader/internal/app/adminauth"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	pgadmin "github.com/ryancheng/telegram-downloader/internal/store/postgres/adminauth"
	pgsources "github.com/ryancheng/telegram-downloader/internal/store/postgres/sourcesettings"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// Errors are deliberately fixed messages: driver errors can contain a DSN or
// credentials and must never be printed by an operator-facing command.
func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	if len(args) != 1 || (args[0] != "reset-password" && args[0] != "rotate-key") {
		return errors.New("usage: adminctl reset-password|rotate-key; secrets must use private environment variables or files, never command arguments")
	}
	load := func(name string) (string, error) { return credentials.LoadSecret(getenv(name), getenv(name+"_FILE")) }
	var password string
	var oldVault, newVault *credentials.Vault
	if args[0] == "reset-password" {
		var err error
		password, err = load("ADMIN_RESET_PASSWORD")
		if err != nil || !adminauth.ValidPassword(password) {
			return errors.New("invalid reset password configuration: use an owner-only file containing 12 or more characters and at most 72 UTF-8 bytes")
		}
	}
	if args[0] == "rotate-key" {
		old, err := load("SOURCE_SETTINGS_MASTER_KEY")
		if err != nil {
			return errors.New("cannot read current master key")
		}
		next, err := load("SOURCE_SETTINGS_NEW_MASTER_KEY")
		if err != nil {
			return errors.New("cannot read new master key")
		}
		if old == next {
			return errors.New("new master key must differ from current key")
		}
		oldVault, err = credentials.NewVaultFromEncoded(old)
		if err != nil {
			return errors.New("current master key is invalid")
		}
		newVault, err = credentials.NewVaultFromEncoded(next)
		if err != nil {
			return errors.New("new master key is invalid")
		}
	}
	dsn := getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return errors.New("cannot configure maintenance database connection")
	}
	defer pool.Close()
	if pool.Ping(ctx) != nil {
		return errors.New("cannot connect to maintenance database")
	}
	if args[0] == "reset-password" {
		service := adminauth.NewService(pgadmin.New(pool), adminauth.Options{})
		if err = service.ResetPassword(ctx, password); err != nil {
			return errors.New("password reset failed; check database readiness or concurrent maintenance")
		}
		fmt.Fprintln(out, "管理员密码已更新，全部旧会话已撤销。")
		return nil
	}
	repo := pgsources.New(pool)
	if err = repo.RotateCredentials(ctx, oldVault, newVault); err != nil {
		return errors.New("credential rotation failed or commit status is uncertain; preserve both keys, keep the API stopped, and verify database state before switching keys")
	}
	if err = repo.VerifyCredentials(ctx, newVault); err != nil {
		return errors.New("rotation committed but readback failed; keep the API stopped and preserve both keys for recovery")
	}
	fmt.Fprintln(out, "凭据已原子轮换并通过新密钥读回。请切换主密钥文件后启动 API，并保留旧密钥备份。")
	return nil
}
