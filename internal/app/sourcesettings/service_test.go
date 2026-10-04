package sourcesettings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"github.com/ryancheng/telegram-downloader/internal/modelapi"
	"testing"
)

type memoryRepo struct{ records map[string]Record }

func (r *memoryRepo) Get(_ context.Context, id string) (Record, error) {
	v, ok := r.records[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return v, nil
}
func (r *memoryRepo) Save(_ context.Context, v Record, expected int64) (Record, error) {
	if r.records[v.ProviderID].ConfigVersion != expected {
		return Record{}, ErrConflict
	}
	v.ConfigVersion = expected + 1
	r.records[v.ProviderID] = v
	return v, nil
}

type testCatalog struct{}

func (testCatalog) Descriptors() []Descriptor {
	return []Descriptor{{ID: "anonymous", AuthModes: []string{"none"}}, {ID: "bangumi", AuthModes: []string{"none", "bearer"}, Filters: map[string][]string{"rating": {"all", "safe"}}, FieldPreferences: map[string][]string{"title": {"original"}}}}
}
func (testCatalog) Test(context.Context, string, PublicSourceConfig, credentials.Secret) (ProbeResult, error) {
	return ProbeResult{Status: "unavailable", Scope: "adapter not installed"}, nil
}
func pointer[T any](value T) *T { return &value }
func newTestService() (*Service, *memoryRepo) {
	repo := &memoryRepo{records: map[string]Record{}}
	vault, _ := credentials.NewVault(bytes.Repeat([]byte{8}, 32))
	return NewService(repo, vault, testCatalog{}, nil, nil), repo
}
func sourceUpdate() SourceUpdate {
	return SourceUpdate{ExpectedVersion: pointer(int64(0)), Enabled: pointer(true), Priority: pointer(100), Filters: map[string]string{}, FieldPreferences: map[string]string{}, Credential: CredentialChange{Action: "keep"}}
}
func TestSourceCredentialsOptionsCASAndPublicProjection(t *testing.T) {
	s, _ := newTestService()
	ctx := context.Background()
	update := sourceUpdate()
	update.Credential = CredentialChange{Action: "replace", Value: "synthetic-secret"}
	if _, err := s.SaveSource(ctx, "anonymous", update); !errors.Is(err, ErrInvalid) {
		t.Fatal("anonymous credential accepted")
	}
	p, err := s.SaveSource(ctx, "bangumi", update)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(p)
	if bytes.Contains(raw, []byte("synthetic-secret")) || !p.CredentialConfigured {
		t.Fatal("public projection leaked or lost credentials")
	}
	secret, err := s.ResolveCredentials(ctx, "bangumi")
	if err != nil || secret.Value() != "synthetic-secret" {
		t.Fatal("credential resolution failed")
	}
	if _, err = s.SaveSource(ctx, "bangumi", update); !errors.Is(err, ErrConflict) {
		t.Fatal("stale save accepted")
	}
	update.ExpectedVersion = pointer(int64(1))
	update.Credential = CredentialChange{Action: "keep"}
	update.Filters = map[string]string{"url": "http://malicious"}
	if _, err = s.SaveSource(ctx, "bangumi", update); !errors.Is(err, ErrInvalid) {
		t.Fatal("unknown option accepted")
	}
	update.Filters = map[string]string{"rating": "safe"}
	update.FieldPreferences = map[string]string{"title": "original"}
	p, err = s.SaveSource(ctx, "bangumi", update)
	if err != nil || p.Filters["rating"] != "safe" {
		t.Fatal("allowed options lost")
	}
	update.ExpectedVersion = pointer(p.ConfigVersion)
	update.Credential = CredentialChange{Action: "clear"}
	p, err = s.SaveSource(ctx, "bangumi", update)
	if err != nil || p.CredentialConfigured {
		t.Fatal("clear failed")
	}
	secret, err = s.ResolveCredentials(ctx, "bangumi")
	if err != nil || secret.Value() != "" {
		t.Fatal("clear fallback detected")
	}
	sources, err := s.Sources(ctx)
	if err != nil || sources[0].ProviderID != "anonymous" {
		t.Fatal("stable priority sorting failed")
	}
	test, err := s.TestSource(ctx, "bangumi", p.ConfigVersion)
	if err != nil || test.Status != "unavailable" {
		t.Fatal("uninstalled adapter reported success")
	}
}
func TestAITargetCannotKeepOldCredential(t *testing.T) {
	s, _ := newTestService()
	ctx := context.Background()
	update := AIUpdate{ExpectedVersion: pointer(int64(0)), Enabled: pointer(true), BaseURL: pointer("http://localhost:8000/v1"), ModelID: pointer("manual-model"), Credential: CredentialChange{Action: "replace", Value: "synthetic-secret"}}
	p, err := s.SaveAI(ctx, update)
	if err != nil {
		t.Fatal(err)
	}
	update.ExpectedVersion = pointer(p.ConfigVersion)
	update.BaseURL = pointer("http://localhost:8001/v1")
	update.Credential = CredentialChange{Action: "keep"}
	if _, err = s.SaveAI(ctx, update); !errors.Is(err, ErrInvalid) {
		t.Fatal("old credential forwarded to new target")
	}
	update.Credential = CredentialChange{Action: "clear"}
	p, err = s.SaveAI(ctx, update)
	if err != nil || p.CredentialConfigured {
		t.Fatal("explicit clear failed")
	}
}

type lateModelClient struct{ repo *memoryRepo }

func (c lateModelClient) Models(context.Context, string, credentials.Secret) (modelapi.ModelsResult, error) {
	r := c.repo.records[AIProviderID]
	r.ConfigVersion++
	c.repo.records[AIProviderID] = r
	return modelapi.ModelsResult{Models: []modelapi.Model{{ID: "old-model"}}}, nil
}
func (lateModelClient) Infer(context.Context, string, credentials.Secret, string, string, json.RawMessage) (json.RawMessage, error) {
	return nil, nil
}
func TestLateModelDiscoveryIsConflict(t *testing.T) {
	s, repo := newTestService()
	s.models = lateModelClient{repo}
	p, err := s.SaveAI(context.Background(), AIUpdate{ExpectedVersion: pointer(int64(0)), Enabled: pointer(false), BaseURL: pointer("http://localhost:8000"), ModelID: pointer("manual"), Credential: CredentialChange{Action: "keep"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Models(context.Background(), p.ConfigVersion); !errors.Is(err, ErrConflict) {
		t.Fatal("late discovery accepted")
	}
}
