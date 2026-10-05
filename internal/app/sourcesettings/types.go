package sourcesettings

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"github.com/ryancheng/telegram-downloader/internal/modelapi"
	"time"
)

var (
	ErrInvalid  = errors.New("invalid configuration")
	ErrConflict = errors.New("configuration changed")
	ErrNotFound = errors.New("configuration not found")
)

const AIProviderID = "__ai__"

type CredentialChange struct {
	Action string `json:"action"`
	Value  string `json:"value,omitempty"`
}
type PublicSourceConfig struct {
	ProviderID           string            `json:"provider_id"`
	Enabled              bool              `json:"enabled"`
	Priority             int               `json:"priority"`
	AuthMode             string            `json:"auth_mode"`
	CredentialConfigured bool              `json:"credential_configured"`
	ConfigVersion        int64             `json:"config_version"`
	Filters              map[string]string `json:"filters"`
	FieldPreferences     map[string]string `json:"field_preferences"`
}
type SourceUpdate struct {
	ExpectedVersion  *int64            `json:"expected_version"`
	Enabled          *bool             `json:"enabled"`
	Priority         *int              `json:"priority"`
	Filters          map[string]string `json:"filters"`
	FieldPreferences map[string]string `json:"field_preferences"`
	Credential       CredentialChange  `json:"credential"`
}
type AIConfig struct {
	Enabled              bool   `json:"enabled"`
	BaseURL              string `json:"base_url"`
	ModelID              string `json:"model_id"`
	CredentialConfigured bool   `json:"credential_configured"`
	ConfigVersion        int64  `json:"config_version"`
}
type AIUpdate struct {
	ExpectedVersion *int64           `json:"expected_version"`
	Enabled         *bool            `json:"enabled"`
	BaseURL         *string          `json:"base_url"`
	ModelID         *string          `json:"model_id"`
	Credential      CredentialChange `json:"credential"`
}
type Record struct {
	ProviderID                       string
	Enabled                          bool
	Priority                         int
	Config                           json.RawMessage
	AuthMode                         string
	Envelope                         credentials.Envelope
	ConfigVersion, CredentialVersion int64
}
type Repository interface {
	Get(context.Context, string) (Record, error)
	Save(context.Context, Record, int64) (Record, error)
}

// Descriptors are injected by provider-search; settings never own network targets.
type Descriptor struct {
	ID               string              `json:"id"`
	Label            string              `json:"label"`
	AuthModes        []string            `json:"auth_modes"`
	Filters          map[string][]string `json:"filters"`
	FieldPreferences map[string][]string `json:"field_preferences"`
	Capabilities     []string            `json:"capabilities"`
}
type Catalog interface {
	Descriptors() []Descriptor
	Test(context.Context, string, PublicSourceConfig, credentials.Secret) (ProbeResult, error)
}
type ProbeResult struct {
	Status string `json:"status"`
	Scope  string `json:"scope"`
}
type SourceView struct {
	PublicSourceConfig
	Descriptor Descriptor `json:"descriptor"`
}
type SourceTestResult struct {
	ProviderID    string    `json:"provider_id"`
	ConfigVersion int64     `json:"config_version"`
	CheckedAt     time.Time `json:"checked_at"`
	ProbeResult
}
type ModelClient interface {
	Models(context.Context, string, credentials.Secret) (modelapi.ModelsResult, error)
	Infer(context.Context, string, credentials.Secret, string, string, json.RawMessage) (json.RawMessage, error)
}
type InferenceFixture struct {
	SchemaVersion      int             `json:"schema_version"`
	DefinitionsVersion string          `json:"definitions_version"`
	FieldKeys          []string        `json:"field_keys"`
	FixtureVersion     string          `json:"fixture_version"`
	Schema             json.RawMessage `json:"-"`
	Text               string          `json:"-"`
}
type InferenceContract interface {
	Fixture(context.Context) (InferenceFixture, error)
	Validate(context.Context, InferenceFixture, json.RawMessage) error
}
type ModelsResult struct {
	ConfigVersion int64 `json:"config_version"`
	modelapi.ModelsResult
}
type AITestResult struct {
	ConfigVersion     int64     `json:"config_version"`
	CredentialVersion int64     `json:"credential_version"`
	ModelID           string    `json:"model_id"`
	Status            string    `json:"status"`
	CheckedAt         time.Time `json:"checked_at"`
	InferenceFixture
}
