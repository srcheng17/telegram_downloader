package sourcesettings

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ryancheng/telegram-downloader/internal/app/metadataextract"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"github.com/ryancheng/telegram-downloader/internal/modelapi"
)

type extractionModel interface {
	ExtractionCapability(context.Context, string, credentials.Secret, string) (modelapi.ExtractionCapability, error)
	Extract(context.Context, string, credentials.Secret, string, string, json.RawMessage, int, modelapi.ExtractionCapability) (json.RawMessage, error)
}
type extractionSnapshot struct {
	config     AIConfig
	key        credentials.Secret
	capability modelapi.ExtractionCapability
}

// fmt does not invoke Secret.String when it descends through an unexported
// containing field. Redact the whole opaque handle before reflection reaches it.
func (extractionSnapshot) String() string               { return "[redacted extraction snapshot]" }
func (extractionSnapshot) GoString() string             { return "[redacted extraction snapshot]" }
func (extractionSnapshot) MarshalJSON() ([]byte, error) { return nil, credentials.ErrSecret }

// Snapshot is internal to extraction. Credentials never enter a public DTO.
func (s *Service) Snapshot(ctx context.Context, expected uint64) (metadataextract.ModelSnapshot, error) {
	if expected > uint64(^uint64(0)>>1) {
		return metadataextract.ModelSnapshot{}, metadataextract.Failure("config_changed")
	}
	if err := ctx.Err(); err != nil {
		return metadataextract.ModelSnapshot{}, extractionError(err)
	}
	r, err := s.record(ctx, AIProviderID)
	if err != nil {
		return metadataextract.ModelSnapshot{}, extractionError(err)
	}
	p := aiPublic(r)
	if r.ConfigVersion != int64(expected) {
		return metadataextract.ModelSnapshot{}, metadataextract.Failure("config_changed")
	}
	if r.ConfigVersion == 0 {
		return metadataextract.ModelSnapshot{}, metadataextract.Failure("not_configured")
	}
	if !p.Enabled {
		return metadataextract.ModelSnapshot{}, metadataextract.Failure("disabled")
	}
	if p.BaseURL == "" || !modelapi.ValidModelID(p.ModelID) || s.models == nil {
		return metadataextract.ModelSnapshot{}, metadataextract.Failure("not_configured")
	}
	client, ok := s.models.(extractionModel)
	if !ok {
		return metadataextract.ModelSnapshot{}, metadataextract.Failure("schema_unsupported")
	}
	key, err := s.decrypt(r)
	if err != nil {
		return metadataextract.ModelSnapshot{}, extractionError(err)
	}
	capability, err := client.ExtractionCapability(ctx, p.BaseURL, key, p.ModelID)
	if err != nil {
		return metadataextract.ModelSnapshot{}, extractionError(err)
	}
	if err = s.checkVersion(ctx, AIProviderID, p.ConfigVersion); err != nil {
		return metadataextract.ModelSnapshot{}, extractionError(err)
	}
	return metadataextract.ModelSnapshot{ConfigRevision: expected, ModelID: p.ModelID, Destination: p.BaseURL, ContextTokens: capability.ContextTokens, BudgetVerified: true, Handle: extractionSnapshot{p, key, capability}}, nil
}
func (s *Service) ExtractJSON(ctx context.Context, snapshot metadataextract.ModelSnapshot, prompt string, schema json.RawMessage, budget int) (json.RawMessage, error) {
	h, ok := snapshot.Handle.(extractionSnapshot)
	if !ok || snapshot.ConfigRevision != uint64(h.config.ConfigVersion) || snapshot.ModelID != h.config.ModelID {
		return nil, metadataextract.Failure("config_changed")
	}
	client, ok := s.models.(extractionModel)
	if !ok {
		return nil, metadataextract.Failure("schema_unsupported")
	}
	if err := s.checkVersion(ctx, AIProviderID, h.config.ConfigVersion); err != nil {
		return nil, extractionError(err)
	}
	result, err := client.Extract(ctx, h.config.BaseURL, h.key, h.config.ModelID, prompt, schema, budget, h.capability)
	if err != nil {
		return nil, extractionError(err)
	}
	if err = s.checkVersion(ctx, AIProviderID, h.config.ConfigVersion); err != nil {
		return nil, extractionError(err)
	}
	return result, nil
}
func extractionError(err error) error {
	if errors.Is(err, ErrConflict) {
		return metadataextract.Failure("config_changed")
	}
	if errors.Is(err, ErrInvalid) {
		return metadataextract.Failure("not_configured")
	}
	if errors.Is(err, context.Canceled) {
		return metadataextract.Failure("cancelled")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return metadataextract.Failure("timeout")
	}
	if errors.Is(err, credentials.ErrSecret) {
		return metadataextract.Failure("not_configured")
	}
	var upstream *modelapi.Error
	if errors.As(err, &upstream) {
		code := upstream.Code
		switch code {
		case "unsupported":
			code = "schema_unsupported"
		case "response_too_large":
			code = "invalid_response"
		case "invalid_target":
			code = "not_configured"
		case "schema_unsupported", "context_exceeded", "input_too_large", "refused", "unauthorized", "forbidden", "rate_limited", "timeout", "unreachable", "invalid_response", "cancelled", "config_changed":
		default:
			code = "unreachable"
		}
		return metadataextract.Failure(code)
	}
	return metadataextract.Failure("unreachable")
}
