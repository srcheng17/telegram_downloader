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

func TestAIProtocolDefaultsAndPreservesVersionedSettings(t *testing.T) {
	s, repo := newTestService()
	ctx := context.Background()
	p, err := s.AI(ctx)
	if err != nil || p.Protocol != modelapi.ProtocolLlamaCPPNative {
		t.Fatalf("empty settings protocol = %q, error = %v", p.Protocol, err)
	}
	// Previously saved records do not contain a protocol field.
	repo.records[AIProviderID] = Record{ProviderID: AIProviderID, ConfigVersion: 4, Config: json.RawMessage(`{"base_url":"http://localhost:8000/v1","model_id":"manual-model"}`)}
	p, err = s.AI(ctx)
	if err != nil || p.Protocol != modelapi.ProtocolLlamaCPPNative {
		t.Fatalf("legacy settings protocol = %q, error = %v", p.Protocol, err)
	}
	update := AIUpdate{ExpectedVersion: pointer(int64(4)), Enabled: pointer(true), BaseURL: pointer(p.BaseURL), ModelID: pointer(p.ModelID), Protocol: pointer(modelapi.ProtocolLlamaCPPChat), Credential: CredentialChange{Action: "replace", Value: "synthetic-secret"}}
	p, err = s.SaveAI(ctx, update)
	if err != nil || p.Protocol != modelapi.ProtocolLlamaCPPChat || p.ConfigVersion != 5 {
		t.Fatalf("chat settings = %+v, error = %v", p, err)
	}
	before := repo.records[AIProviderID]
	update.Protocol = nil
	update.Credential = CredentialChange{Action: "keep"}
	if _, err = s.SaveAI(ctx, update); !errors.Is(err, ErrConflict) {
		t.Fatal("stale protocol update accepted")
	}
	update.ExpectedVersion = pointer(p.ConfigVersion)
	update.Enabled = pointer(false)
	p, err = s.SaveAI(ctx, update)
	if err != nil || p.Protocol != modelapi.ProtocolLlamaCPPChat || p.Enabled || p.ConfigVersion != 6 {
		t.Fatalf("legacy client changed protocol: %+v, error = %v", p, err)
	}
	after := repo.records[AIProviderID]
	if after.CredentialVersion != before.CredentialVersion || !bytes.Equal(after.Envelope.Ciphertext, before.Envelope.Ciphertext) {
		t.Fatal("protocol update changed saved credential")
	}
	readback, err := s.AI(ctx)
	if err != nil || readback != p || !bytes.Contains(after.Config, []byte(`"protocol":"llama_cpp_chat"`)) {
		t.Fatal("protocol did not survive saved settings readback")
	}
	update.ExpectedVersion = pointer(p.ConfigVersion)
	update.Protocol = pointer(modelapi.ProtocolLlamaCPPNative)
	p, err = s.SaveAI(ctx, update)
	if err != nil || p.Protocol != modelapi.ProtocolLlamaCPPNative {
		t.Fatal("explicit native switch failed")
	}
}

func TestAIRejectsUnsupportedProtocolWithoutChangingSettings(t *testing.T) {
	for _, protocol := range []string{"", "openai", "llama_cpp_chat ", "LLAMA_CPP_CHAT"} {
		t.Run(protocol, func(t *testing.T) {
			s, repo := newTestService()
			_, err := s.SaveAI(context.Background(), AIUpdate{ExpectedVersion: pointer(int64(0)), Enabled: pointer(false), BaseURL: pointer("http://localhost:8000"), ModelID: pointer("manual"), Protocol: pointer(protocol), Credential: CredentialChange{Action: "replace", Value: "synthetic-secret"}})
			if !errors.Is(err, ErrInvalid) || len(repo.records) != 0 {
				t.Fatal("invalid protocol changed settings")
			}
		})
	}
}

func TestAIUpdateProtocolStrictJSON(t *testing.T) {
	for _, raw := range []string{`{"protocol":null}`, `{"Protocol":null}`, `{"protocol":42}`, `{"protocol":"llama_cpp_chat","protocol":"llama_cpp_native"}`, `{"protocol":"llama_cpp_chat","unknown":true}`, `{"credential":{"action":"keep","unknown":true}}`} {
		var update AIUpdate
		if json.Unmarshal([]byte(raw), &update) == nil {
			t.Fatalf("accepted invalid update %s", raw)
		}
	}
	for _, raw := range []string{`{}`, `{"protocol":"llama_cpp_chat"}`} {
		var update AIUpdate
		if err := json.Unmarshal([]byte(raw), &update); err != nil {
			t.Fatalf("valid protocol update rejected: %v", err)
		}
		if (update.Protocol == nil) != (raw == `{}`) {
			t.Fatal("omitted protocol did not remain optional")
		}
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
