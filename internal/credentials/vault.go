// Package credentials keeps recoverable secrets separate from public configuration.
package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
)

var ErrSecret = errors.New("secret unavailable or invalid")

// Secret deliberately refuses JSON serialization and redacts formatting.
type Secret struct{ value string }

func NewSecret(value string) Secret           { return Secret{value: value} }
func (s Secret) Value() string                { return s.value }
func (s Secret) String() string               { return "[redacted]" }
func (s Secret) GoString() string             { return "[redacted]" }
func (s Secret) MarshalJSON() ([]byte, error) { return nil, ErrSecret }

type Envelope struct {
	Ciphertext []byte `json:"-"`
	Nonce      []byte `json:"-"`
	KeyID      string `json:"-"`
}
type Vault struct {
	aead cipher.AEAD
	id   string
}

func NewVault(key []byte) (*Vault, error) {
	if len(key) != 32 {
		return nil, ErrSecret
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrSecret
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrSecret
	}
	digest := sha256.Sum256(key)
	return &Vault{aead: aead, id: hex.EncodeToString(digest[:8])}, nil
}
func NewVaultFromEncoded(key string) (*Vault, error) {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return nil, ErrSecret
	}
	return NewVault(raw)
}
func aad(provider string, version int64) []byte {
	return []byte(fmt.Sprintf("source-credential:v1:%s:%d", provider, version))
}
func (v *Vault) Encrypt(provider string, version int64, secret Secret) (Envelope, error) {
	if v == nil {
		return Envelope{}, ErrSecret
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, ErrSecret
	}
	return Envelope{Ciphertext: v.aead.Seal(nil, nonce, []byte(secret.value), aad(provider, version)), Nonce: nonce, KeyID: v.id}, nil
}
func (v *Vault) Decrypt(provider string, version int64, e Envelope) (Secret, error) {
	if v == nil || e.KeyID != v.id || len(e.Nonce) != v.aead.NonceSize() {
		return Secret{}, ErrSecret
	}
	b, err := v.aead.Open(nil, e.Nonce, e.Ciphertext, aad(provider, version))
	if err != nil {
		return Secret{}, ErrSecret
	}
	return NewSecret(string(b)), nil
}

// LoadSecret accepts exactly one private environment value or owner-only file.
// O_NOFOLLOW and fstat validate the opened descriptor, avoiding symlink races.
func LoadSecret(value, path string) (string, error) {
	if value != "" && path != "" {
		return "", ErrSecret
	}
	if path == "" {
		return value, nil
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", ErrSecret
	}
	file := os.NewFile(uintptr(fd), "secret")
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", ErrSecret
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return "", ErrSecret
	}
	data, err := io.ReadAll(io.LimitReader(file, 8193))
	if err != nil || len(data) > 8192 {
		return "", ErrSecret
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r"), nil
}
