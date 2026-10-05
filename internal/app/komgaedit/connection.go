package komgaedit

import (
	"context"
	"errors"
	"net"
	"net/url"
	"path"
	"strings"
	"unicode"

	"github.com/ryancheng/telegram-downloader/internal/credentials"
)

var (
	ErrInvalid  = errors.New("invalid Komga input")
	ErrConflict = errors.New("Komga configuration changed")
	ErrNotFound = errors.New("Komga object not found")
	ErrDisabled = errors.New("Komga connection not configured")
	ErrUnsafe   = errors.New("Komga path or archive is unsafe")
	ErrUpstream = errors.New("Komga unavailable")
)

const komgaCredentialID = "__komga_connection__"

type ConnectionRecord struct {
	BaseURL           string
	Envelope          credentials.Envelope
	ConfigVersion     int64
	CredentialVersion int64
}

type ConnectionRepository interface {
	Get(context.Context) (ConnectionRecord, error)
	Save(context.Context, ConnectionRecord, int64) (ConnectionRecord, error)
}

type ConnectionView struct {
	BaseURL              string `json:"base_url"`
	CredentialConfigured bool   `json:"credential_configured"`
	ConfigVersion        int64  `json:"config_version"`
}

type CredentialChange struct {
	Action string `json:"action"`
	Value  string `json:"value,omitempty"`
}

type ConnectionUpdate struct {
	ExpectedVersion *int64           `json:"expected_version"`
	BaseURL         *string          `json:"base_url"`
	Credential      CredentialChange `json:"credential"`
}

type ConnectionService struct {
	repo                  ConnectionRepository
	vault                 *credentials.Vault
	allowInsecureLoopback bool
}

func NewConnectionService(repo ConnectionRepository, vault *credentials.Vault, allowInsecureLoopback bool) *ConnectionService {
	return &ConnectionService{repo: repo, vault: vault, allowInsecureLoopback: allowInsecureLoopback}
}

func (s *ConnectionService) Get(ctx context.Context) (ConnectionView, error) {
	record, err := s.record(ctx)
	if err != nil {
		return ConnectionView{}, err
	}
	return connectionView(record), nil
}

func (s *ConnectionService) Save(ctx context.Context, input ConnectionUpdate) (ConnectionView, error) {
	if s == nil || s.repo == nil || s.vault == nil || input.ExpectedVersion == nil || input.BaseURL == nil {
		return ConnectionView{}, ErrInvalid
	}
	baseURL, err := normalizeKomgaURL(*input.BaseURL, s.allowInsecureLoopback)
	if err != nil {
		return ConnectionView{}, err
	}
	record, err := s.record(ctx)
	if err != nil {
		return ConnectionView{}, err
	}
	if record.ConfigVersion != *input.ExpectedVersion {
		return ConnectionView{}, ErrConflict
	}
	if baseURL != record.BaseURL && len(record.Envelope.Ciphertext) > 0 && input.Credential.Action == "keep" {
		return ConnectionView{}, ErrInvalid
	}
	switch input.Credential.Action {
	case "keep":
		if input.Credential.Value != "" {
			return ConnectionView{}, ErrInvalid
		}
		if len(record.Envelope.Ciphertext) > 0 {
			if _, err := s.vault.Decrypt(komgaCredentialID, record.CredentialVersion, record.Envelope); err != nil {
				return ConnectionView{}, err
			}
		}
	case "replace":
		value := input.Credential.Value
		if len(value) == 0 || len(value) > 4096 || strings.TrimSpace(value) != value || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return ConnectionView{}, ErrInvalid
		}
		record.CredentialVersion++
		record.Envelope, err = s.vault.Encrypt(komgaCredentialID, record.CredentialVersion, credentials.NewSecret(value))
		if err != nil {
			return ConnectionView{}, err
		}
	case "clear":
		if input.Credential.Value != "" {
			return ConnectionView{}, ErrInvalid
		}
		record.CredentialVersion++
		record.Envelope = credentials.Envelope{}
	default:
		return ConnectionView{}, ErrInvalid
	}
	record.BaseURL = baseURL
	if record.BaseURL != "" && len(record.Envelope.Ciphertext) == 0 {
		return ConnectionView{}, ErrInvalid
	}
	record, err = s.repo.Save(ctx, record, *input.ExpectedVersion)
	if err != nil {
		return ConnectionView{}, err
	}
	return connectionView(record), nil
}

func (s *ConnectionService) Resolve(ctx context.Context) (string, credentials.Secret, error) {
	record, err := s.record(ctx)
	if err != nil {
		return "", credentials.Secret{}, err
	}
	if record.BaseURL == "" || len(record.Envelope.Ciphertext) == 0 {
		return "", credentials.Secret{}, ErrDisabled
	}
	secret, err := s.vault.Decrypt(komgaCredentialID, record.CredentialVersion, record.Envelope)
	return record.BaseURL, secret, err
}

func (s *ConnectionService) record(ctx context.Context) (ConnectionRecord, error) {
	if s == nil || s.repo == nil {
		return ConnectionRecord{}, ErrDisabled
	}
	record, err := s.repo.Get(ctx)
	if errors.Is(err, ErrNotFound) {
		return ConnectionRecord{}, nil
	}
	return record, err
}

func connectionView(record ConnectionRecord) ConnectionView {
	return ConnectionView{BaseURL: record.BaseURL, CredentialConfigured: len(record.Envelope.Ciphertext) > 0, ConfigVersion: record.ConfigVersion}
}

func normalizeKomgaURL(raw string, allowInsecureLoopback bool) (string, error) {
	if raw == "" {
		return "", nil
	}
	if strings.TrimSpace(raw) != raw || len(raw) > 2048 {
		return "", ErrInvalid
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", ErrInvalid
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	local := strings.EqualFold(host, "localhost") || strings.EqualFold(host, "host.docker.internal") || (ip != nil && ip.IsLoopback())
	if u.Scheme != "https" && !(u.Scheme == "http" && allowInsecureLoopback && local) {
		return "", ErrInvalid
	}
	if u.RawPath != "" || strings.ContainsAny(u.Path, "\\\x00\r\n") || (u.Path != "" && (!strings.HasPrefix(u.Path, "/") || path.Clean(u.Path) != u.Path)) || strings.Contains(u.Path, "..") {
		return "", ErrInvalid
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}
