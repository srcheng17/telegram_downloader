package metadatasearch

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/ryancheng/telegram-downloader/internal/app/sourcesettings"
	"github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

type Service struct {
	settings Settings
	schema   Schema
	provider Provider
}

func NewService(settings Settings, schema Schema, provider Provider) *Service {
	return &Service{settings, schema, provider}
}
func (s *Service) Providers(ctx context.Context) ([]Source, error) {
	views, err := s.settings.Sources(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]Source, 0, len(views))
	for _, v := range views {
		result = append(result, Source{v, s.provider.Info(v.ProviderID)})
	}
	return result, nil
}
func safeError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	if errors.Is(err, context.Canceled) {
		return &Error{Code: "cancelled"}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: "timeout"}
	}
	return &Error{Code: "unavailable"}
}
func (s *Service) unchanged(ctx context.Context, original sourcesettings.PublicSourceConfig) bool {
	current, err := s.settings.PublicSourceConfig(ctx, original.ProviderID)
	return err == nil && current.Enabled && current.ConfigVersion == original.ConfigVersion
}
func (s *Service) Search(ctx context.Context, input SearchInput) (SearchResult, error) {
	keyword := strings.TrimSpace(input.Keyword)
	if keyword == "" || len(keyword) > MaxKeywordBytes || !utf8.ValidString(keyword) || strings.ContainsFunc(keyword, unicode.IsControl) || input.QueryRevision > metadata.MaxRevision || len(input.ProviderIDs) < 1 || len(input.ProviderIDs) > 3 {
		return SearchResult{}, ErrInvalid
	}
	views, err := s.settings.Sources(ctx)
	if err != nil {
		return SearchResult{}, err
	}
	selected := map[string]bool{}
	for _, id := range input.ProviderIDs {
		if selected[id] {
			return SearchResult{}, ErrInvalid
		}
		selected[id] = true
	}
	configs := []sourcesettings.PublicSourceConfig{}
	for _, v := range views {
		if selected[v.ProviderID] {
			if !v.Enabled {
				return SearchResult{}, ErrInvalid
			}
			configs = append(configs, v.PublicSourceConfig)
		}
	}
	if len(configs) != len(selected) {
		return SearchResult{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	result := SearchResult{QueryRevision: input.QueryRevision, Sources: make([]SourceResult, len(configs))}
	var wg sync.WaitGroup
	for i, config := range configs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entry := SourceResult{ProviderID: config.ProviderID, ConfigVersion: config.ConfigVersion, Records: []Record{}, ProviderInfo: s.provider.Info(config.ProviderID)}
			secret, e := s.settings.ResolveCredentials(ctx, config.ProviderID)
			if e == nil && !s.unchanged(ctx, config) {
				e = ErrConflict
			}
			var records []Record
			if e == nil {
				records, e = s.provider.Search(ctx, config, secret, keyword)
			}
			if e == nil && !s.unchanged(ctx, config) {
				e = ErrConflict
			}
			if e != nil {
				entry.Error = safeError(e)
			} else {
				if len(records) > MaxResults {
					records = records[:MaxResults]
				}
				entry.Records = records
				if len(records) == 0 {
					entry.Error = &Error{Code: "no_results"}
				}
			}
			result.Sources[i] = entry
		}()
	}
	wg.Wait()
	return result, nil
}

var recordID = regexp.MustCompile(`^[1-9][0-9]{0,17}$`)

func ValidRecordID(id string) bool { return recordID.MatchString(id) }
func (s *Service) Resolve(ctx context.Context, input ResolveInput) (ResolveResult, error) {
	if !ValidRecordID(input.RecordID) || input.SchemaVersion != metadata.SchemaVersion || input.QueryRevision > metadata.MaxRevision || input.BaseDocumentRevision > metadata.MaxRevision || input.ConfigVersion < 0 || input.ConfigVersion > int64(metadata.MaxRevision) || input.ConfigRevision != nil && *input.ConfigRevision > metadata.MaxRevision || input.FieldRevisions == nil {
		return ResolveResult{}, ErrInvalid
	}
	registry, err := s.schema.Schema(ctx)
	if err != nil {
		return ResolveResult{}, err
	}
	if input.DefinitionsVersion != registry.DefinitionsVersion {
		return ResolveResult{}, ErrConflict
	}
	if len(input.FieldRevisions) > len(registry.Definitions) {
		return ResolveResult{}, ErrInvalid
	}
	for key, rev := range input.FieldRevisions {
		if _, ok := registry.Definitions[key]; !ok || rev > input.BaseDocumentRevision {
			return ResolveResult{}, ErrInvalid
		}
	}
	info := s.provider.Info(input.ProviderID)
	for target, source := range input.CustomMappings {
		def, ok := registry.Definitions[target]
		if !ok || !strings.HasPrefix(target, "custom.user.") || !def.Enabled || !slices.Contains(def.Extractable, "provider") {
			return ResolveResult{}, ErrInvalid
		}
		if !slices.ContainsFunc(info.CustomFields, func(m Mapping) bool { return m.Key == source && m.Type == def.Type }) {
			return ResolveResult{}, ErrInvalid
		}
	}
	config, err := s.settings.PublicSourceConfig(ctx, input.ProviderID)
	if err != nil {
		return ResolveResult{}, ErrInvalid
	}
	if !config.Enabled || config.ConfigVersion != input.ConfigVersion {
		return ResolveResult{}, ErrConflict
	}
	secret, err := s.settings.ResolveCredentials(ctx, input.ProviderID)
	if err != nil {
		return ResolveResult{}, safeError(err)
	}
	if !s.unchanged(ctx, config) {
		return ResolveResult{}, ErrConflict
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	record, err := s.provider.Resolve(ctx, config, secret, input.RecordID)
	if err != nil {
		return ResolveResult{}, safeError(err)
	}
	if !s.unchanged(ctx, config) {
		return ResolveResult{}, ErrConflict
	}
	current, err := s.schema.Schema(ctx)
	if err != nil {
		return ResolveResult{}, err
	}
	if current.DefinitionsVersion != registry.DefinitionsVersion {
		return ResolveResult{}, ErrConflict
	}
	candidate := metadata.MetadataCandidate{CandidateID: uuid.NewString(), RequestID: uuid.NewString(), Origin: "provider", SchemaVersion: metadata.SchemaVersion, DefinitionsVersion: registry.DefinitionsVersion, BaseDocumentRevision: input.BaseDocumentRevision, InputRevision: input.QueryRevision, ConfigRevision: input.ConfigRevision, FieldRevisions: map[string]uint64{}, Fields: map[string]metadata.CandidateField{}}
	add := func(key string, field Field) {
		def, ok := registry.Definitions[key]
		if !ok || !def.Enabled || !slices.Contains(def.Extractable, "provider") {
			return
		}
		candidate.Fields[key] = metadata.CandidateField{State: "value", Value: field.Value, Provenance: []metadata.Provenance{{Kind: "provider", SourceID: config.ProviderID, RecordID: record.ID, PublicURL: record.PublicURL, RetrievedAt: record.RetrievedAt, SourceField: field.Path, Attribution: info.Attribution.Label + " · " + info.Attribution.License, LicenseURL: info.Attribution.LicenseURL}}}
		candidate.FieldRevisions[key] = input.FieldRevisions[key]
	}
	for key, field := range record.Fields {
		add(key, field)
	}
	for target, source := range input.CustomMappings {
		if field, ok := record.Extra[source]; ok {
			add(target, field)
		}
	}
	if metadata.ValidateCandidate(candidate, registry) != nil {
		return ResolveResult{}, &Error{Code: "invalid_response"}
	}
	return ResolveResult{candidate, record, config.ConfigVersion, info}, nil
}
