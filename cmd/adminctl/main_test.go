package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	appauth "github.com/ryancheng/telegram-downloader/internal/app/adminauth"
	appsource "github.com/ryancheng/telegram-downloader/internal/app/sourcesettings"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	pgadmin "github.com/ryancheng/telegram-downloader/internal/store/postgres/adminauth"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres/migrations"
	pgsources "github.com/ryancheng/telegram-downloader/internal/store/postgres/sourcesettings"
)

func TestAdminctlRejectsArgumentsAndRedactsDatabaseErrors(t *testing.T) {
	var output bytes.Buffer
	env := func(key string) string {
		switch key {
		case "ADMIN_RESET_PASSWORD":
			return "synthetic-password-long"
		case "DATABASE_URL":
			return "postgresql://user:synthetic-secret@%%invalid"
		}
		return ""
	}
	for _, args := range [][]string{{"reset-password", "synthetic-password-long"}, {"reset-password"}} {
		err := run(context.Background(), args, env, &output)
		if err == nil || strings.Contains(err.Error(), "synthetic-") || strings.Contains(output.String(), "synthetic-") {
			t.Fatal("invalid invocation accepted or secret disclosed")
		}
	}
}
func TestAdminctlMaintenancePostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated TEST_DATABASE_URL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = migrations.Run(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `TRUNCATE admin_account,admin_sessions,source_settings`); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `TRUNCATE admin_account,admin_sessions,source_settings`)
	service := appauth.NewService(pgadmin.New(pool), appauth.Options{BcryptCost: 4})
	if err = service.Bootstrap(ctx, "initial-synthetic-password"); err != nil {
		t.Fatal(err)
	}
	pre, _ := service.Preauth(ctx, "local")
	session, err := service.Login(ctx, pre.Token, "initial-synthetic-password", "local")
	if err != nil {
		t.Fatal(err)
	}
	oldBytes := bytes.Repeat([]byte{3}, 32)
	newBytes := bytes.Repeat([]byte{4}, 32)
	old, _ := credentials.NewVault(oldBytes)
	next, _ := credentials.NewVault(newBytes)
	envelope, _ := old.Encrypt("maintenance-test", 1, credentials.NewSecret("synthetic-source-credential"))
	repo := pgsources.New(pool)
	if _, err = repo.Save(ctx, appsource.Record{ProviderID: "maintenance-test", Priority: 100, Config: json.RawMessage(`{}`), AuthMode: "bearer", Envelope: envelope, CredentialVersion: 1}, 0); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"DATABASE_URL": dsn, "ADMIN_RESET_PASSWORD": "reset-synthetic-password", "SOURCE_SETTINGS_MASTER_KEY": base64.StdEncoding.EncodeToString(oldBytes), "SOURCE_SETTINGS_NEW_MASTER_KEY": base64.StdEncoding.EncodeToString(newBytes)}
	env := func(key string) string { return values[key] }
	var output bytes.Buffer
	if err = run(ctx, []string{"reset-password"}, env, &output); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Validate(ctx, session.Token); err == nil {
		t.Fatal("reset did not revoke existing session")
	}
	pre, _ = service.Preauth(ctx, "local")
	if _, err = service.Login(ctx, pre.Token, values["ADMIN_RESET_PASSWORD"], "local"); err != nil {
		t.Fatal("reset password cannot login")
	}
	if err = run(ctx, []string{"rotate-key"}, env, &output); err != nil {
		t.Fatal(err)
	}
	if err = repo.VerifyCredentials(ctx, next); err != nil {
		t.Fatal(err)
	}
	if err = repo.VerifyCredentials(ctx, old); err == nil {
		t.Fatal("old key remains accepted")
	}
	for _, secret := range []string{values["ADMIN_RESET_PASSWORD"], values["SOURCE_SETTINGS_MASTER_KEY"], values["SOURCE_SETTINGS_NEW_MASTER_KEY"], "synthetic-source-credential"} {
		if strings.Contains(output.String(), secret) {
			t.Fatal("maintenance output leaked secret")
		}
	}
}
