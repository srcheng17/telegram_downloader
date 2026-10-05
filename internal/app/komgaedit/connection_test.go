package komgaedit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ryancheng/telegram-downloader/internal/credentials"
)

type memoryConnectionRepo struct{ record ConnectionRecord }

func (r *memoryConnectionRepo) Get(context.Context) (ConnectionRecord, error) {
	if r.record.ConfigVersion == 0 {
		return ConnectionRecord{}, ErrNotFound
	}
	return r.record, nil
}
func (r *memoryConnectionRepo) Save(_ context.Context, record ConnectionRecord, expected int64) (ConnectionRecord, error) {
	if r.record.ConfigVersion != expected {
		return ConnectionRecord{}, ErrConflict
	}
	record.ConfigVersion = expected + 1
	r.record = record
	return record, nil
}

func TestConnectionRequiresSecretReplacementWhenTargetChanges(t *testing.T) {
	vault, err := credentials.NewVault(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	repo := &memoryConnectionRepo{}
	service := NewConnectionService(repo, vault, true)
	zero, one := int64(0), int64(1)
	first := "http://host.docker.internal:25600"
	view, err := service.Save(context.Background(), ConnectionUpdate{ExpectedVersion: &zero, BaseURL: &first, Credential: CredentialChange{Action: "replace", Value: "private-api-key"}})
	if err != nil || view.ConfigVersion != 1 || !view.CredentialConfigured {
		t.Fatalf("initial save: %#v, %v", view, err)
	}
	encoded, err := json.Marshal(view)
	if err != nil || string(encoded) == "" || strings.Contains(string(encoded), "private-api-key") {
		t.Fatalf("secret exposed in public view: %q, %v", encoded, err)
	}
	second := "https://komga.example.invalid"
	if _, err := service.Save(context.Background(), ConnectionUpdate{ExpectedVersion: &one, BaseURL: &second, Credential: CredentialChange{Action: "keep"}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("target changed with retained key: %v", err)
	}
	url, key, err := service.Resolve(context.Background())
	if err != nil || url != first || key.Value() != "private-api-key" {
		t.Fatalf("configured connection lost: %q, %v", url, err)
	}
	if _, err := service.Save(context.Background(), ConnectionUpdate{ExpectedVersion: &zero, BaseURL: &first, Credential: CredentialChange{Action: "keep"}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale settings accepted: %v", err)
	}
}

func TestKomgaURLRequiresHTTPSOrExplicitLocalHTTP(t *testing.T) {
	for _, raw := range []string{"http://komga.example.invalid", "https://user:pass@komga.example.invalid", "https://komga.example.invalid/?key=secret", "https://komga.example.invalid/a/../b"} {
		if _, err := normalizeKomgaURL(raw, true); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted unsafe target %q: %v", raw, err)
		}
	}
	if _, err := normalizeKomgaURL("http://127.0.0.1:25600", false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("accepted local HTTP without opt-in: %v", err)
	}
	if _, err := normalizeKomgaURL("http://host.docker.internal:25600", true); err != nil {
		t.Fatalf("rejected explicit Docker host gateway: %v", err)
	}
}
