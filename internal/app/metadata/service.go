// Package metadata orchestrates registry persistence and pure document rules.
// Draft validation/patching deliberately has no server-side draft state.
package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domain "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

type Repository interface {
	Current(context.Context) (domain.Registry, error)
	Get(context.Context, string) (domain.Registry, error)
	Save(context.Context, string, domain.Registry) error
}
type Service struct {
	repository Repository
	now        func() time.Time
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, now: time.Now}
}

type Result struct {
	Document domain.Document  `json:"document"`
	Warnings []domain.Warning `json:"warnings"`
}
type FieldsResult struct {
	DefinitionsVersion string                   `json:"definitions_version"`
	Definitions        []domain.FieldDefinition `json:"definitions"`
	Limits             domain.Limits            `json:"limits"`
}
type UpdateFieldsInput struct {
	ExpectedDefinitionsVersion string                   `json:"expected_definitions_version"`
	Definitions                []domain.FieldDefinition `json:"definitions"`
}
type PatchInput struct {
	Document         domain.Document    `json:"document"`
	ExpectedRevision uint64             `json:"expected_revision"`
	InputRevision    uint64             `json:"input_revision"`
	ConfigRevision   *uint64            `json:"config_revision,omitempty"`
	Operations       []domain.Operation `json:"operations"`
}

func (s *Service) ready() error {
	if s == nil || s.repository == nil {
		return errors.New("metadata repository is not configured")
	}
	return nil
}
func (s *Service) Schema(ctx context.Context) (domain.Registry, error) {
	if err := s.ready(); err != nil {
		return domain.Registry{}, err
	}
	registry, err := s.repository.Current(ctx)
	if err != nil {
		return domain.Registry{}, fmt.Errorf("read metadata registry: %w", err)
	}
	if err := domain.ValidateRegistry(registry); err != nil {
		return domain.Registry{}, fmt.Errorf("verify metadata registry: %w", err)
	}
	return registry, nil
}
func (s *Service) Fields(ctx context.Context) (FieldsResult, error) {
	r, err := s.Schema(ctx)
	if err != nil {
		return FieldsResult{}, err
	}
	return FieldsResult{r.DefinitionsVersion, r.CustomDefinitions(), r.Limits}, nil
}
func (s *Service) UpdateFields(ctx context.Context, input UpdateFieldsInput) (FieldsResult, error) {
	previous, err := s.Schema(ctx)
	if err != nil {
		return FieldsResult{}, err
	}
	if input.ExpectedDefinitionsVersion != previous.DefinitionsVersion {
		return FieldsResult{}, domain.ErrConflict
	}
	next, err := domain.NewRegistry(previous, input.Definitions)
	if err != nil {
		return FieldsResult{}, err
	}
	if err := s.repository.Save(ctx, input.ExpectedDefinitionsVersion, next); err != nil {
		return FieldsResult{}, fmt.Errorf("save metadata definitions: %w", err)
	}
	return FieldsResult{next.DefinitionsVersion, next.CustomDefinitions(), next.Limits}, nil
}
func (s *Service) registry(ctx context.Context, version string) (domain.Registry, error) {
	if err := s.ready(); err != nil {
		return domain.Registry{}, err
	}
	r, err := s.repository.Get(ctx, version)
	if err != nil {
		return domain.Registry{}, fmt.Errorf("read metadata definition version: %w", err)
	}
	if err := domain.ValidateRegistry(r); err != nil {
		return domain.Registry{}, fmt.Errorf("verify metadata definition version: %w", err)
	}
	return r, nil
}
func (s *Service) Validate(ctx context.Context, doc domain.Document) (Result, error) {
	r, err := s.registry(ctx, doc.DefinitionsVersion)
	if err != nil {
		return Result{}, err
	}
	result, warnings, err := domain.Validate(doc, r)
	if err != nil {
		return Result{}, err
	}
	return Result{result, warnings}, nil
}
func (s *Service) Patch(ctx context.Context, input PatchInput) (Result, error) {
	r, err := s.registry(ctx, input.Document.DefinitionsVersion)
	if err != nil {
		return Result{}, err
	}
	doc, warnings, err := domain.ApplyPatch(input.Document, r, input.ExpectedRevision, input.Operations, domain.MergeContext{InputRevision: input.InputRevision, ConfigRevision: input.ConfigRevision, Now: s.now()})
	if err != nil {
		return Result{}, err
	}
	return Result{doc, warnings}, nil
}
func (s *Service) Decode(ctx context.Context, data []byte) (domain.Document, error) {
	if len(data) > domain.MaxDocumentBytes {
		return domain.Document{}, fmt.Errorf("%w: document exceeds byte limit", domain.ErrInvalidInput)
	}
	var doc domain.Document
	if err := domain.DecodeJSON(data, &doc); err != nil {
		return domain.Document{}, err
	}
	result, err := s.Validate(ctx, doc)
	return result.Document, err
}
func (s *Service) NormalizeInput(ctx context.Context, doc *domain.Document, legacy domain.Legacy) (domain.Document, domain.Legacy, error) {
	if doc == nil {
		converted, err := domain.FromLegacy(legacy)
		if err != nil {
			return domain.Document{}, domain.Legacy{}, err
		}
		return converted, domain.ToLegacy(converted), nil
	}
	result, err := s.Validate(ctx, *doc)
	if err != nil {
		return domain.Document{}, domain.Legacy{}, err
	}
	if err := domain.CheckLegacy(result.Document, legacy); err != nil {
		return domain.Document{}, domain.Legacy{}, err
	}
	return result.Document, domain.ToLegacy(result.Document), nil
}

// Encode validates the complete snapshot before returning JSONB input for a
// caller-owned task transaction. It never writes compatibility columns itself.
func (s *Service) Encode(ctx context.Context, doc domain.Document) ([]byte, error) {
	result, err := s.Validate(ctx, doc)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result.Document)
}
