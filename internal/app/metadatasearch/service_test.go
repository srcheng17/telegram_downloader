package metadatasearch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ryancheng/telegram-downloader/internal/app/sourcesettings"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

type testSettings struct {
	mu          sync.Mutex
	configs     []sourcesettings.PublicSourceConfig
	credentials []string
}

func (s *testSettings) Sources(context.Context) ([]sourcesettings.SourceView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := []sourcesettings.SourceView{}
	for _, config := range s.configs {
		rows = append(rows, sourcesettings.SourceView{PublicSourceConfig: config})
	}
	return rows, nil
}
func (s *testSettings) PublicSourceConfig(_ context.Context, id string) (sourcesettings.PublicSourceConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, config := range s.configs {
		if config.ProviderID == id {
			return config, nil
		}
	}
	return sourcesettings.PublicSourceConfig{}, sourcesettings.ErrNotFound
}
func (s *testSettings) ResolveCredentials(_ context.Context, id string) (credentials.Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.credentials = append(s.credentials, id)
	return credentials.NewSecret(id + "-private"), nil
}

type testSchema struct{ registry metadata.Registry }

func (s *testSchema) Schema(context.Context) (metadata.Registry, error) { return s.registry, nil }

type testProvider struct {
	search  func(context.Context, sourcesettings.PublicSourceConfig, credentials.Secret, string) ([]Record, error)
	resolve func(context.Context, sourcesettings.PublicSourceConfig, credentials.Secret, string) (Record, error)
}

func (p testProvider) Info(string) ProviderInfo {
	return ProviderInfo{Attribution: Attribution{Label: "Example Source", License: "Test terms", LicenseURL: "https://example.com/terms"}, CustomFields: []Mapping{{Key: "source.type", Type: "string"}}}
}
func (p testProvider) Search(ctx context.Context, c sourcesettings.PublicSourceConfig, s credentials.Secret, q string) ([]Record, error) {
	return p.search(ctx, c, s, q)
}
func (p testProvider) Resolve(ctx context.Context, c sourcesettings.PublicSourceConfig, s credentials.Secret, id string) (Record, error) {
	return p.resolve(ctx, c, s, id)
}
func settingsFixture() *testSettings {
	return &testSettings{configs: []sourcesettings.PublicSourceConfig{{ProviderID: "mangabaka", Enabled: true, ConfigVersion: 3}, {ProviderID: "mangaupdates", Enabled: true, ConfigVersion: 4}, {ProviderID: "bangumi", Enabled: true, ConfigVersion: 5}}}
}
func sourceRecord() Record {
	return Record{ID: "123", Title: "Example", PublicURL: "https://example.com/123", RetrievedAt: "2026-10-05T00:00:00Z", Fields: map[string]Field{"title": {json.RawMessage(`"Example"`), "title"}, "publisher": {json.RawMessage(`"Publisher"`), "publisher"}}, Extra: map[string]Field{"source.type": {json.RawMessage(`"Doujinshi"`), "type"}}}
}
func TestOnlySelectedEnabledSourcesAndPartialFailure(t *testing.T) {
	settings := settingsFixture()
	var mu sync.Mutex
	called := []string{}
	provider := testProvider{search: func(_ context.Context, c sourcesettings.PublicSourceConfig, s credentials.Secret, q string) ([]Record, error) {
		mu.Lock()
		called = append(called, c.ProviderID)
		mu.Unlock()
		if s.Value() != c.ProviderID+"-private" || q != "Neutral" {
			t.Error("query or source token mixed")
		}
		switch c.ProviderID {
		case "mangaupdates":
			return nil, &Error{Code: "rate_limited", RetryAfter: 60}
		case "bangumi":
			return nil, context.DeadlineExceeded
		}
		return []Record{sourceRecord()}, nil
	}}
	service := NewService(settings, &testSchema{metadata.StandardRegistry()}, provider)
	result, err := service.Search(context.Background(), SearchInput{Keyword: " Neutral ", ProviderIDs: []string{"mangabaka", "mangaupdates", "bangumi"}, QueryRevision: 9})
	if err != nil {
		t.Fatal(err)
	}
	if result.QueryRevision != 9 || len(result.Sources[0].Records) != 1 || result.Sources[1].Error.Code != "rate_limited" || result.Sources[2].Error.Code != "timeout" {
		t.Fatalf("partial results lost: %+v", result)
	}
	called = nil
	settings.credentials = nil
	_, err = service.Search(context.Background(), SearchInput{Keyword: "Neutral", ProviderIDs: []string{"mangabaka"}})
	if err != nil || len(called) != 1 || called[0] != "mangabaka" || len(settings.credentials) != 1 {
		t.Fatal("unselected source contacted")
	}
	settings.configs[0].Enabled = false
	called = nil
	_, err = service.Search(context.Background(), SearchInput{Keyword: "Neutral", ProviderIDs: []string{"mangabaka"}})
	if !errors.Is(err, ErrInvalid) || len(called) != 0 {
		t.Fatal("disabled source contacted")
	}
	for _, ids := range [][]string{{"unknown"}, {"bangumi", "bangumi"}, {}} {
		if _, err = service.Search(context.Background(), SearchInput{Keyword: "Neutral", ProviderIDs: ids}); err == nil {
			t.Fatal("bad source selection accepted")
		}
	}
	if _, err = service.Search(context.Background(), SearchInput{Keyword: strings.Repeat("a", 513), ProviderIDs: []string{"bangumi"}}); err == nil {
		t.Fatal("oversized keyword accepted")
	}
}
func TestResolveCustomMappingAndProvenanceRoundTrip(t *testing.T) {
	registry, err := metadata.NewRegistry(metadata.StandardRegistry(), []metadata.FieldDefinition{{Key: "custom.user.category", Label: "来源分类", Type: "string", MaxBytes: 100, Editable: true, Enabled: true, Extractable: []string{"provider"}, ExportStatus: "internal_only"}})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(settingsFixture(), &testSchema{registry}, testProvider{resolve: func(context.Context, sourcesettings.PublicSourceConfig, credentials.Secret, string) (Record, error) {
		return sourceRecord(), nil
	}})
	input := ResolveInput{ProviderID: "mangabaka", RecordID: "123", QueryRevision: 2, BaseDocumentRevision: 0, FieldRevisions: map[string]uint64{}, SchemaVersion: 1, DefinitionsVersion: registry.DefinitionsVersion, ConfigVersion: 3, CustomMappings: map[string]string{"custom.user.category": "source.type"}}
	result, err := service.Resolve(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidate.Fields) != 3 || string(result.Candidate.Fields["custom.user.category"].Value) != `"Doujinshi"` {
		t.Fatal("extended fields absent")
	}
	doc, _, err := metadata.ApplyCandidate(metadata.EmptyDocument(registry), registry, 0, result.Candidate, []string{"title", "publisher", "custom.user.category"}, false, metadata.MergeContext{InputRevision: 2, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(doc)
	var restored metadata.Document
	if metadata.DecodeJSON(raw, &restored) != nil {
		t.Fatal("provenance round trip decode failed")
	}
	if _, _, err = metadata.Validate(restored, registry); err != nil {
		t.Fatal(err)
	}
	field := restored.Fields["custom.user.category"]
	p := field.Provenance[0]
	if field.ManualLocked || p.SourceField != "type" || p.RetrievedAt == "" || p.AdoptedAt == "" || p.Attribution == "" || p.LicenseURL == "" {
		t.Fatal("adoption provenance lost or manually locked")
	}
	input.CustomMappings["custom.user.missing"] = "source.type"
	if _, err = service.Resolve(context.Background(), input); !errors.Is(err, ErrInvalid) {
		t.Fatal("implicit custom definition accepted")
	}
	delete(input.CustomMappings, "custom.user.missing")
	input.CustomMappings["custom.user.category"] = "arbitrary.payload"
	if _, err = service.Resolve(context.Background(), input); !errors.Is(err, ErrInvalid) {
		t.Fatal("arbitrary source mapping accepted")
	}
}
func TestChangedConfigAndDefinitionsInvalidateLateResolve(t *testing.T) {
	for _, change := range []string{"config", "definitions"} {
		t.Run(change, func(t *testing.T) {
			settings := settingsFixture()
			schema := &testSchema{metadata.StandardRegistry()}
			service := NewService(settings, schema, testProvider{resolve: func(context.Context, sourcesettings.PublicSourceConfig, credentials.Secret, string) (Record, error) {
				if change == "config" {
					settings.mu.Lock()
					settings.configs[0].ConfigVersion++
					settings.mu.Unlock()
				} else {
					schema.registry.DefinitionsVersion = "changed"
				}
				return sourceRecord(), nil
			}})
			input := ResolveInput{ProviderID: "mangabaka", RecordID: "123", FieldRevisions: map[string]uint64{}, SchemaVersion: 1, DefinitionsVersion: metadata.StandardDefinitionsVersion, ConfigVersion: 3}
			if _, err := service.Resolve(context.Background(), input); !errors.Is(err, ErrConflict) {
				t.Fatal("late response survived config change")
			}
		})
	}
}
func TestProviderErrorNeverLeaksRawFailure(t *testing.T) {
	service := NewService(settingsFixture(), &testSchema{metadata.StandardRegistry()}, testProvider{search: func(context.Context, sourcesettings.PublicSourceConfig, credentials.Secret, string) ([]Record, error) {
		return nil, errors.New("Authorization: synthetic-private-token")
	}})
	result, err := service.Search(context.Background(), SearchInput{Keyword: "Neutral", ProviderIDs: []string{"bangumi"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "private") || result.Sources[0].Error.Code != "unavailable" {
		t.Fatal("private error leaked")
	}
}
