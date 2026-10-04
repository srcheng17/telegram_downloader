package sourcesettings

import (
	"context"
	app "github.com/ryancheng/telegram-downloader/internal/app/sourcesettings"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
)

// RotateCredentials is an offline maintenance operation, never an HTTP endpoint.
// The operator must stop configuration writes and keep both external keys until
// commit and readback succeed. Any decryption/update error rolls back every row.
func (s *Store) RotateCredentials(ctx context.Context, oldVault, newVault *credentials.Vault) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT provider_id,ciphertext,nonce,key_id,credential_version FROM source_settings WHERE ciphertext IS NOT NULL AND octet_length(ciphertext)>0 ORDER BY provider_id FOR UPDATE`)
	if err != nil {
		return err
	}
	records := []app.Record{}
	for rows.Next() {
		var r app.Record
		if err = rows.Scan(&r.ProviderID, &r.Envelope.Ciphertext, &r.Envelope.Nonce, &r.Envelope.KeyID, &r.CredentialVersion); err != nil {
			rows.Close()
			return err
		}
		records = append(records, r)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, r := range records {
		secret, err := oldVault.Decrypt(r.ProviderID, r.CredentialVersion, r.Envelope)
		if err != nil {
			return err
		}
		version := r.CredentialVersion + 1
		envelope, err := newVault.Encrypt(r.ProviderID, version, secret)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE source_settings SET ciphertext=$2,nonce=$3,key_id=$4,credential_version=$5,config_version=config_version+1,updated_at=now() WHERE provider_id=$1`, r.ProviderID, envelope.Ciphertext, envelope.Nonce, envelope.KeyID, version); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// VerifyCredentials performs readback without returning or logging plaintext.
func (s *Store) VerifyCredentials(ctx context.Context, vault *credentials.Vault) error {
	rows, err := s.pool.Query(ctx, `SELECT provider_id,ciphertext,nonce,key_id,credential_version FROM source_settings WHERE ciphertext IS NOT NULL AND octet_length(ciphertext)>0`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var r app.Record
		if err = rows.Scan(&r.ProviderID, &r.Envelope.Ciphertext, &r.Envelope.Nonce, &r.Envelope.KeyID, &r.CredentialVersion); err != nil {
			return err
		}
		if _, err = vault.Decrypt(r.ProviderID, r.CredentialVersion, r.Envelope); err != nil {
			return err
		}
	}
	return rows.Err()
}
