package sourcesettings_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/ryancheng/telegram-downloader/internal/app/sourcesettings"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres/migrations"
	store "github.com/ryancheng/telegram-downloader/internal/store/postgres/sourcesettings"
	"os"
	"sync"
	"testing"
)

func TestSourceCASAndEncryptionPostgres(t *testing.T) {
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
	if _, err = pool.Exec(ctx, `DELETE FROM source_settings WHERE provider_id='test-source'`); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM source_settings WHERE provider_id='test-source'`)
	vault, _ := credentials.NewVault(bytes.Repeat([]byte{7}, 32))
	envelope, _ := vault.Encrypt("test-source", 1, credentials.NewSecret("synthetic-private-value"))
	record := app.Record{ProviderID: "test-source", Priority: 100, Config: json.RawMessage(`{"filters":{}}`), AuthMode: "bearer", Envelope: envelope, CredentialVersion: 1}
	repo := store.New(pool)
	record, err = repo.Save(ctx, record, 0)
	if err != nil {
		t.Fatal(err)
	}
	read, err := repo.Get(ctx, record.ProviderID)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := vault.Decrypt(read.ProviderID, read.CredentialVersion, read.Envelope)
	if err != nil || secret.Value() != "synthetic-private-value" {
		t.Fatal("encrypted readback failed")
	}
	if bytes.Contains(read.Envelope.Ciphertext, []byte(secret.Value())) {
		t.Fatal("plaintext stored")
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := repo.Save(ctx, record, 1); results <- err }()
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for e := range results {
		if e == nil {
			wins++
		} else if errors.Is(e, app.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal("CAS did not serialize writers")
	}
}

func TestCredentialRotationRollbackPostgres(t *testing.T) {
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
	ids := []string{"rotation-a", "rotation-b"}
	_, err = pool.Exec(ctx, `DELETE FROM source_settings WHERE provider_id=ANY($1)`, ids)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM source_settings WHERE provider_id=ANY($1)`, ids)
	old, _ := credentials.NewVault(bytes.Repeat([]byte{1}, 32))
	next, _ := credentials.NewVault(bytes.Repeat([]byte{2}, 32))
	repo := store.New(pool)
	for _, id := range ids {
		vault := old
		if id == "rotation-b" {
			vault = next
		}
		envelope, _ := vault.Encrypt(id, 1, credentials.NewSecret("rotation-synthetic"))
		_, err = repo.Save(ctx, app.Record{ProviderID: id, Priority: 100, Config: json.RawMessage(`{}`), AuthMode: "bearer", Envelope: envelope, CredentialVersion: 1}, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = repo.RotateCredentials(ctx, old, next); !errors.Is(err, credentials.ErrSecret) {
		t.Fatal("mixed-key rotation should fail")
	}
	first, err := repo.Get(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if first.ConfigVersion != 1 {
		t.Fatal("partial rotation committed")
	}
	if _, err = old.Decrypt(first.ProviderID, first.CredentialVersion, first.Envelope); err != nil {
		t.Fatal("rollback lost old key access")
	}
	envelope, _ := old.Encrypt(ids[1], 1, credentials.NewSecret("rotation-synthetic"))
	_, err = pool.Exec(ctx, `UPDATE source_settings SET ciphertext=$2,nonce=$3,key_id=$4 WHERE provider_id=$1`, ids[1], envelope.Ciphertext, envelope.Nonce, envelope.KeyID)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.RotateCredentials(ctx, old, next); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		r, err := repo.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if r.ConfigVersion != 2 || r.CredentialVersion != 2 {
			t.Fatal("rotation version unchanged")
		}
		if _, err = next.Decrypt(id, r.CredentialVersion, r.Envelope); err != nil {
			t.Fatal("new key cannot read rotated credential")
		}
	}
}
