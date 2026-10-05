package credentials

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestVaultIsolationAndTamper(t *testing.T) {
	v, _ := NewVault(bytes.Repeat([]byte{1}, 32))
	secret := NewSecret("synthetic-key-never-echo")
	e, err := v.Encrypt("bangumi", 3, secret)
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.Decrypt("bangumi", 3, e)
	if err != nil || got.Value() != secret.Value() {
		t.Fatal("roundtrip failed")
	}
	for _, tc := range []struct {
		id      string
		version int64
	}{{"mangabaka", 3}, {"bangumi", 4}} {
		if _, err = v.Decrypt(tc.id, tc.version, e); err == nil {
			t.Fatal("AAD isolation missing")
		}
	}
	other, _ := NewVault(bytes.Repeat([]byte{2}, 32))
	if _, err = other.Decrypt("bangumi", 3, e); err == nil {
		t.Fatal("wrong key accepted")
	}
	e.Ciphertext[0] ^= 1
	if _, err = v.Decrypt("bangumi", 3, e); err == nil {
		t.Fatal("tampering accepted")
	}
	if _, err = json.Marshal(secret); err == nil {
		t.Fatal("secret JSON accepted")
	}
	if fmt.Sprintf("%v %#v", secret, secret) != "[redacted] [redacted]" {
		t.Fatal("secret formatting leaked")
	}
}
func TestLoadSecretPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("private-value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadSecret("", path); err != nil || got != "private-value" {
		t.Fatal("private file rejected")
	}
	if _, err := LoadSecret("also-set", path); err == nil {
		t.Fatal("mutually exclusive settings accepted")
	}
	link := path + "-link"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSecret("", link); err == nil {
		t.Fatal("symlink accepted")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSecret("", path); err == nil {
		t.Fatal("public secret file accepted")
	}
}
