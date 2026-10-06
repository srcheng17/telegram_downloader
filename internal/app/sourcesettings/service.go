package sourcesettings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"github.com/ryancheng/telegram-downloader/internal/modelapi"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
)

type Service struct {
	repo     Repository
	vault    *credentials.Vault
	catalog  Catalog
	models   ModelClient
	contract InferenceContract
}

func NewService(repo Repository, vault *credentials.Vault, catalog Catalog, models ModelClient, contract InferenceContract) *Service {
	return &Service{repo, vault, catalog, models, contract}
}
func (s *Service) descriptor(id string) (Descriptor, error) {
	if s.catalog != nil {
		for _, d := range s.catalog.Descriptors() {
			if d.ID == id && id != AIProviderID {
				return d, nil
			}
		}
	}
	return Descriptor{}, ErrNotFound
}
func (s *Service) record(ctx context.Context, id string) (Record, error) {
	r, err := s.repo.Get(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return Record{ProviderID: id, Priority: 100, AuthMode: "none", Config: json.RawMessage(`{}`)}, nil
	}
	return r, err
}
func sourcePublic(r Record) PublicSourceConfig {
	p := PublicSourceConfig{ProviderID: r.ProviderID, Enabled: r.Enabled, Priority: r.Priority, AuthMode: r.AuthMode, CredentialConfigured: len(r.Envelope.Ciphertext) > 0, ConfigVersion: r.ConfigVersion, Filters: map[string]string{}, FieldPreferences: map[string]string{}}
	var options struct {
		Filters          map[string]string `json:"filters"`
		FieldPreferences map[string]string `json:"field_preferences"`
	}
	if json.Unmarshal(r.Config, &options) == nil {
		if options.Filters != nil {
			p.Filters = options.Filters
		}
		if options.FieldPreferences != nil {
			p.FieldPreferences = options.FieldPreferences
		}
	}
	return p
}
func (s *Service) PublicSourceConfig(ctx context.Context, id string) (PublicSourceConfig, error) {
	if _, err := s.descriptor(id); err != nil {
		return PublicSourceConfig{}, err
	}
	r, err := s.record(ctx, id)
	return sourcePublic(r), err
}
func (s *Service) Sources(ctx context.Context) ([]SourceView, error) {
	views := []SourceView{}
	if s.catalog == nil {
		return views, nil
	}
	for _, d := range s.catalog.Descriptors() {
		p, err := s.PublicSourceConfig(ctx, d.ID)
		if err != nil {
			return nil, err
		}
		views = append(views, SourceView{p, d})
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].Priority == views[j].Priority {
			return views[i].ProviderID < views[j].ProviderID
		}
		return views[i].Priority < views[j].Priority
	})
	return views, nil
}
func member(value string, choices []string) bool {
	for _, x := range choices {
		if x == value {
			return true
		}
	}
	return false
}
func validateOptions(values map[string]string, allowed map[string][]string) bool {
	if values == nil {
		return false
	}
	for k, v := range values {
		if !member(v, allowed[k]) {
			return false
		}
	}
	return true
}
func (s *Service) changeCredential(r *Record, change CredentialChange, allowBearer bool) error {
	switch change.Action {
	case "keep":
		if change.Value != "" {
			return ErrInvalid
		}
		if len(r.Envelope.Ciphertext) > 0 {
			if _, err := s.vault.Decrypt(r.ProviderID, r.CredentialVersion, r.Envelope); err != nil {
				return err
			}
		}
	case "clear":
		if change.Value != "" {
			return ErrInvalid
		}
		r.CredentialVersion++
		r.Envelope = credentials.Envelope{}
		r.AuthMode = "none"
	case "replace":
		if !allowBearer || len(change.Value) == 0 || len(change.Value) > 4096 || strings.TrimSpace(change.Value) != change.Value || strings.IndexFunc(change.Value, unicode.IsControl) >= 0 {
			return ErrInvalid
		}
		r.CredentialVersion++
		e, err := s.vault.Encrypt(r.ProviderID, r.CredentialVersion, credentials.NewSecret(change.Value))
		if err != nil {
			return err
		}
		r.Envelope = e
		r.AuthMode = "bearer"
	default:
		return ErrInvalid
	}
	return nil
}
func (s *Service) SaveSource(ctx context.Context, id string, input SourceUpdate) (PublicSourceConfig, error) {
	d, err := s.descriptor(id)
	if err != nil {
		return PublicSourceConfig{}, err
	}
	if input.ExpectedVersion == nil || input.Enabled == nil || input.Priority == nil || *input.Priority < 0 || *input.Priority > 1000 || !validateOptions(input.Filters, d.Filters) || !validateOptions(input.FieldPreferences, d.FieldPreferences) {
		return PublicSourceConfig{}, ErrInvalid
	}
	r, err := s.record(ctx, id)
	if err != nil {
		return PublicSourceConfig{}, err
	}
	if r.ConfigVersion != *input.ExpectedVersion {
		return PublicSourceConfig{}, ErrConflict
	}
	if err = s.changeCredential(&r, input.Credential, member("bearer", d.AuthModes)); err != nil {
		return PublicSourceConfig{}, err
	}
	r.Enabled = *input.Enabled
	r.Priority = *input.Priority
	r.Config, _ = json.Marshal(map[string]any{"filters": input.Filters, "field_preferences": input.FieldPreferences})
	r, err = s.repo.Save(ctx, r, *input.ExpectedVersion)
	return sourcePublic(r), err
}
func (s *Service) ResolveCredentials(ctx context.Context, id string) (credentials.Secret, error) {
	if id != AIProviderID {
		if _, err := s.descriptor(id); err != nil {
			return credentials.Secret{}, err
		}
	}
	r, err := s.record(ctx, id)
	if err != nil {
		return credentials.Secret{}, err
	}
	return s.decrypt(r)
}
func (s *Service) decrypt(r Record) (credentials.Secret, error) {
	if len(r.Envelope.Ciphertext) == 0 {
		return credentials.NewSecret(""), nil
	}
	return s.vault.Decrypt(r.ProviderID, r.CredentialVersion, r.Envelope)
}
func (s *Service) TestSource(ctx context.Context, id string, version int64) (SourceTestResult, error) {
	if _, err := s.descriptor(id); err != nil {
		return SourceTestResult{}, err
	}
	r, err := s.record(ctx, id)
	if err != nil {
		return SourceTestResult{}, err
	}
	if version != r.ConfigVersion {
		return SourceTestResult{}, ErrConflict
	}
	secret, err := s.decrypt(r)
	if err != nil {
		return SourceTestResult{}, err
	}
	probe, err := s.catalog.Test(ctx, id, sourcePublic(r), secret)
	if err != nil {
		return SourceTestResult{}, err
	}
	current, err := s.record(ctx, id)
	if err != nil {
		return SourceTestResult{}, err
	}
	if current.ConfigVersion != version {
		return SourceTestResult{}, ErrConflict
	}
	return SourceTestResult{ProviderID: id, ConfigVersion: version, CheckedAt: time.Now().UTC(), ProbeResult: probe}, nil
}
func aiPublic(r Record) AIConfig {
	p := AIConfig{Protocol: modelapi.ProtocolLlamaCPPNative}
	_ = json.Unmarshal(r.Config, &p)
	p.Enabled = r.Enabled
	p.ConfigVersion = r.ConfigVersion
	p.CredentialConfigured = len(r.Envelope.Ciphertext) > 0
	return p
}
func (s *Service) AI(ctx context.Context) (AIConfig, error) {
	r, err := s.record(ctx, AIProviderID)
	return aiPublic(r), err
}
func (s *Service) SaveAI(ctx context.Context, input AIUpdate) (AIConfig, error) {
	if input.ExpectedVersion == nil || input.Enabled == nil || input.BaseURL == nil || input.ModelID == nil {
		return AIConfig{}, ErrInvalid
	}
	base := ""
	var err error
	if *input.BaseURL != "" {
		base, err = modelapi.NormalizeBaseURL(*input.BaseURL)
		if err != nil {
			return AIConfig{}, ErrInvalid
		}
	}
	if (*input.ModelID != "" && !modelapi.ValidModelID(*input.ModelID)) || (*input.Enabled && (base == "" || *input.ModelID == "")) {
		return AIConfig{}, ErrInvalid
	}
	r, err := s.record(ctx, AIProviderID)
	if err != nil {
		return AIConfig{}, err
	}
	if r.ConfigVersion != *input.ExpectedVersion {
		return AIConfig{}, ErrConflict
	}
	old := aiPublic(r)
	protocol := old.Protocol
	if input.Protocol != nil {
		protocol = *input.Protocol
	}
	if protocol != modelapi.ProtocolLlamaCPPNative && protocol != modelapi.ProtocolLlamaCPPChat {
		return AIConfig{}, ErrInvalid
	}
	if base != old.BaseURL && len(r.Envelope.Ciphertext) > 0 && input.Credential.Action == "keep" {
		return AIConfig{}, ErrInvalid
	}
	if err = s.changeCredential(&r, input.Credential, true); err != nil {
		return AIConfig{}, err
	}
	r.Enabled = *input.Enabled
	r.Config, _ = json.Marshal(map[string]string{"protocol": protocol, "base_url": base, "model_id": *input.ModelID})
	r, err = s.repo.Save(ctx, r, *input.ExpectedVersion)
	return aiPublic(r), err
}
func (s *Service) aiSnapshot(ctx context.Context, version int64) (Record, AIConfig, credentials.Secret, error) {
	r, err := s.record(ctx, AIProviderID)
	if err != nil {
		return r, AIConfig{}, credentials.Secret{}, err
	}
	p := aiPublic(r)
	if r.ConfigVersion != version {
		return r, p, credentials.Secret{}, ErrConflict
	}
	if p.BaseURL == "" || s.models == nil || (p.Protocol != modelapi.ProtocolLlamaCPPNative && p.Protocol != modelapi.ProtocolLlamaCPPChat) {
		return r, p, credentials.Secret{}, ErrInvalid
	}
	secret, err := s.decrypt(r)
	return r, p, secret, err
}
func (s *Service) checkVersion(ctx context.Context, id string, version int64) error {
	r, err := s.record(ctx, id)
	if err != nil {
		return err
	}
	if r.ConfigVersion != version {
		return ErrConflict
	}
	return nil
}
func (s *Service) Models(ctx context.Context, version int64) (ModelsResult, error) {
	_, p, key, err := s.aiSnapshot(ctx, version)
	if err != nil {
		return ModelsResult{}, err
	}
	result, err := s.models.Models(ctx, p.BaseURL, key)
	if err != nil {
		return ModelsResult{}, err
	}
	if err = s.checkVersion(ctx, AIProviderID, version); err != nil {
		return ModelsResult{}, err
	}
	return ModelsResult{ConfigVersion: version, ModelsResult: result}, nil
}
func (s *Service) TestAI(ctx context.Context, version int64, model string) (AITestResult, error) {
	r, p, key, err := s.aiSnapshot(ctx, version)
	if err != nil {
		return AITestResult{}, err
	}
	if model != p.ModelID || !modelapi.ValidModelID(model) {
		return AITestResult{}, ErrInvalid
	}
	if s.contract == nil {
		return AITestResult{}, &modelapi.Error{Code: "unsupported"}
	}
	fixture, err := s.contract.Fixture(ctx)
	if err != nil {
		return AITestResult{}, err
	}
	output, err := s.models.Infer(ctx, p.BaseURL, key, model, fixture.Text, fixture.Schema)
	if err != nil {
		return AITestResult{}, err
	}
	if err = s.contract.Validate(ctx, fixture, output); err != nil {
		return AITestResult{}, &modelapi.Error{Code: "invalid_response"}
	}
	if err = s.checkVersion(ctx, AIProviderID, version); err != nil {
		return AITestResult{}, err
	}
	current, err := s.contract.Fixture(ctx)
	if err != nil {
		return AITestResult{}, err
	}
	if current.DefinitionsVersion != fixture.DefinitionsVersion || current.SchemaVersion != fixture.SchemaVersion || current.FixtureVersion != fixture.FixtureVersion || !slices.Equal(current.FieldKeys, fixture.FieldKeys) || !bytes.Equal(current.Schema, fixture.Schema) {
		return AITestResult{}, ErrConflict
	}
	return AITestResult{ConfigVersion: version, CredentialVersion: r.CredentialVersion, ModelID: model, Status: "passed", CheckedAt: time.Now().UTC(), InferenceFixture: fixture}, nil
}
