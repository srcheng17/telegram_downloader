// Package metadataextract validates user-initiated text extraction and converts
// grounded model output to the shared, unapplied metadata candidate contract.
package metadataextract

import (
	"bytes"
	"context"
	"encoding/json"

	domain "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

type RegistrySource interface {
	Schema(context.Context) (domain.Registry, error)
}

// ModelSnapshot is internal only. Handle belongs to the saved-settings adapter;
// it must never be encoded in an HTTP response or reconstructed from user input.
type ModelSnapshot struct {
	ConfigRevision uint64
	ModelID        string
	Destination    string
	ContextTokens  int
	BudgetVerified bool
	Handle         any `json:"-"`
}
type AIClient interface {
	Snapshot(context.Context, uint64) (ModelSnapshot, error)
	// ExtractJSON must account for the complete rendered prompt + schema + output
	// reserve, or use a verified server path that rejects overflow without truncation.
	ExtractJSON(context.Context, ModelSnapshot, string, json.RawMessage, int) (json.RawMessage, error)
}
type Error struct{ Code string }

func (e *Error) Error() string  { return "metadata extraction: " + e.Code }
func Failure(code string) error { return &Error{Code: code} }

type Input struct {
	RequestID            string            `json:"request_id"`
	Text                 string            `json:"text"`
	FieldKeys            []string          `json:"field_keys"`
	SchemaVersion        int               `json:"schema_version"`
	DefinitionsVersion   string            `json:"definitions_version"`
	BaseDocumentRevision uint64            `json:"base_document_revision"`
	FieldRevisions       map[string]uint64 `json:"field_revisions"`
	InputRevision        uint64            `json:"input_revision"`
	ConfigRevision       uint64            `json:"config_revision"`
	RulesVersion         *uint64           `json:"rules_version,omitempty"`
}
type Result struct {
	RequestID  string                     `json:"request_id"`
	Candidates []domain.MetadataCandidate `json:"candidates"`
	Warnings   []domain.Warning           `json:"warnings"`
}

func (in *Input) UnmarshalJSON(data []byte) error {
	type plain Input
	var next plain
	if domain.DecodeJSON(data, &next) != nil {
		return Failure("invalid_request")
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(data, &members) != nil {
		return Failure("invalid_request")
	}
	for _, key := range []string{"request_id", "text", "field_keys", "schema_version", "definitions_version", "base_document_revision", "field_revisions", "input_revision", "config_revision"} {
		if raw, exists := members[key]; !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return Failure("invalid_request")
		}
	}
	var revisions map[string]json.RawMessage
	if json.Unmarshal(members["field_revisions"], &revisions) != nil {
		return Failure("invalid_request")
	}
	for _, revision := range revisions {
		if bytes.Equal(bytes.TrimSpace(revision), []byte("null")) {
			return Failure("invalid_request")
		}
	}
	*in = Input(next)
	return nil
}
