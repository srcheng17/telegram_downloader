// Package metadatasearch coordinates explicitly selected book sources. It never
// writes the draft, source settings, task history or user credentials.
package metadatasearch

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ryancheng/telegram-downloader/internal/app/sourcesettings"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

var ErrInvalid = errors.New("invalid metadata search")
var ErrConflict = errors.New("metadata search context changed")

const MaxResults = 20
const MaxKeywordBytes = 512

// Error is deliberately limited to fixed codes. Upstream bodies, URLs and
// credentials must never reach the HTTP error or log boundary.
type Error struct {
	Code       string `json:"code"`
	RetryAfter int    `json:"retry_after,omitempty"`
}

func (e *Error) Error() string { return "metadata source: " + e.Code }

type Attribution struct {
	Label      string `json:"label"`
	URL        string `json:"url"`
	License    string `json:"license"`
	LicenseURL string `json:"license_url"`
}
type Mapping struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type"`
}
type ProviderInfo struct {
	Attribution  Attribution `json:"attribution"`
	CustomFields []Mapping   `json:"custom_fields"`
	Visibility   string      `json:"visibility"`
}
type Source struct {
	sourcesettings.SourceView
	ProviderInfo
}
type Field struct {
	Value json.RawMessage
	Path  string
}
type Record struct {
	ID           string              `json:"record_id"`
	Title        string              `json:"title"`
	Aliases      []string            `json:"aliases,omitempty"`
	Creators     map[string][]string `json:"creators,omitempty"`
	Format       string              `json:"format,omitempty"`
	Relationship string              `json:"relationship"`
	PublicURL    string              `json:"public_url"`
	RetrievedAt  string              `json:"retrieved_at"`
	Fields       map[string]Field    `json:"-"`
	Extra        map[string]Field    `json:"-"`
}
type Settings interface {
	Sources(context.Context) ([]sourcesettings.SourceView, error)
	PublicSourceConfig(context.Context, string) (sourcesettings.PublicSourceConfig, error)
	ResolveCredentials(context.Context, string) (credentials.Secret, error)
}
type Schema interface {
	Schema(context.Context) (metadata.Registry, error)
}
type Provider interface {
	Info(string) ProviderInfo
	Search(context.Context, sourcesettings.PublicSourceConfig, credentials.Secret, string) ([]Record, error)
	Resolve(context.Context, sourcesettings.PublicSourceConfig, credentials.Secret, string) (Record, error)
}
type SearchInput struct {
	Keyword       string   `json:"keyword"`
	ProviderIDs   []string `json:"provider_ids"`
	QueryRevision uint64   `json:"query_revision"`
}
type SourceResult struct {
	ProviderID    string   `json:"provider_id"`
	ConfigVersion int64    `json:"config_version"`
	Records       []Record `json:"candidates"`
	Error         *Error   `json:"error,omitempty"`
	ProviderInfo
}
type SearchResult struct {
	QueryRevision uint64         `json:"query_revision"`
	Sources       []SourceResult `json:"sources"`
}
type ResolveInput struct {
	ProviderID           string            `json:"provider_id"`
	RecordID             string            `json:"record_id"`
	QueryRevision        uint64            `json:"query_revision"`
	BaseDocumentRevision uint64            `json:"base_document_revision"`
	FieldRevisions       map[string]uint64 `json:"field_revisions"`
	SchemaVersion        int               `json:"schema_version"`
	DefinitionsVersion   string            `json:"definitions_version"`
	ConfigVersion        int64             `json:"config_version"`
	ConfigRevision       *uint64           `json:"config_revision,omitempty"`
	CustomMappings       map[string]string `json:"custom_mappings,omitempty"`
}
type ResolveResult struct {
	Candidate     metadata.MetadataCandidate `json:"candidate"`
	Record        Record                     `json:"record"`
	ConfigVersion int64                      `json:"config_version"`
	ProviderInfo
}
