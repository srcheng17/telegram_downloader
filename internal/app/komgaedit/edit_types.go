package komgaedit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ryancheng/telegram-downloader/internal/archive/cbzedit"
	"github.com/ryancheng/telegram-downloader/internal/domain/metadata"
	"github.com/ryancheng/telegram-downloader/internal/infra/komga"
)

// RegistryProvider keeps book editing aligned with the current workspace
// definitions, including custom fields that have no ComicInfo mapping.
type RegistryProvider interface {
	Schema(context.Context) (metadata.Registry, error)
}

type EditField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	CanSet   bool   `json:"can_set"`
	CanClear bool   `json:"can_clear"`
	Reason   string `json:"reason,omitempty"`
}

type EditDetail struct {
	Book                      BookView           `json:"book"`
	SourceVersion             string             `json:"source_version,omitempty"`
	Document                  metadata.Document  `json:"document"`
	Schema                    metadata.Registry  `json:"schema"`
	Fields                    []EditField        `json:"fields"`
	HasComicInfo              bool               `json:"has_comicinfo"`
	PageCount                 int                `json:"page_count"`
	PageCountCorrectionNeeded bool               `json:"page_count_correction_needed"`
	Warnings                  []metadata.Warning `json:"warnings"`
	CanSave                   bool               `json:"can_save"`
	BlockReason               string             `json:"block_reason,omitempty"`
}

// FieldChange only describes an explicit edit. An omitted key retains its
// original XML value. "cleared" removes its ComicInfo element.
type FieldChange struct {
	Key   string          `json:"key"`
	State string          `json:"state"`
	Value json.RawMessage `json:"value,omitempty"`
}

type PreviewRequest struct {
	SourceVersion      string        `json:"source_version"`
	DefinitionsVersion string        `json:"definitions_version"`
	Changes            []FieldChange `json:"changes"`
	CorrectPageCount   bool          `json:"correct_page_count"`
}

type FieldDiff struct {
	Key    string          `json:"key"`
	Action string          `json:"action"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

type PreviewResult struct {
	PreviewToken string             `json:"preview_token,omitempty"`
	ExpiresAt    time.Time          `json:"expires_at,omitempty"`
	FileChanged  bool               `json:"file_changed"`
	Diffs        []FieldDiff        `json:"diffs"`
	Warnings     []metadata.Warning `json:"warnings"`
	CanSave      bool               `json:"can_save"`
	BlockReason  string             `json:"block_reason,omitempty"`
}

type SaveRequest struct {
	PreviewRequest
	PreviewToken   string `json:"preview_token"`
	IdempotencyKey string `json:"idempotency_key"`
}

type OperationView struct {
	ID                   string   `json:"id"`
	BookID               string   `json:"book_id"`
	State                string   `json:"state"`
	FileCommitted        bool     `json:"file_committed"`
	FileNoChange         bool     `json:"file_no_change"`
	FileRestored         bool     `json:"file_restored"`
	ProjectionApplicable bool     `json:"projection_applicable"`
	ProjectionConsistent bool     `json:"projection_consistent"`
	AnalyzeVerified      bool     `json:"analyze_verified"`
	LastErrorCode        string   `json:"last_error_code,omitempty"`
	AvailableActions     []string `json:"available_actions"`
}

const (
	StatePreparing              = "preparing"
	StatePrepared               = "prepared"
	StateFileCommitted          = "file_committed"
	StateSyncPending            = "sync_pending"
	StateCurrentValueConsistent = "current_value_consistent"
	StateSyncFailed             = "sync_failed"
	StateRestoreNeeded          = "restore_needed"
	StateRestored               = "restored"
	StateAborted                = "aborted"
)

// Operation is private application state; never serialize it to a browser.
// It contains a library-relative path and field values needed for retry.
type Operation struct {
	ID                   string
	IdempotencyKey       string
	RequestDigest        string
	BookID               string
	LibraryID            string
	RelativePath         string
	SourceSHA256         string
	TargetSHA256         string
	BackupRef            string
	Prepared             *cbzedit.Prepared
	DesiredFields        []FieldChange
	State                string
	FileCommitted        bool
	FileNoChange         bool
	FileRestored         bool
	ProjectionConsistent bool
	AnalyzeVerified      bool
	LastErrorCode        string
	Version              int64
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type OperationRepository interface {
	Create(context.Context, Operation) (Operation, error)
	Get(context.Context, string) (Operation, error)
	GetByKey(context.Context, string) (Operation, error)
	Update(context.Context, Operation) (Operation, error)
	ListRecoverable(context.Context, int) ([]Operation, error)
	WithBookLock(context.Context, string, func(context.Context) error) error
}

type editGateway interface {
	Book(context.Context, string) (komga.Book, error)
	Library(context.Context, string) (komga.Library, error)
	AnalyzeBook(context.Context, string) error
	ClearBookMetadata(context.Context, string, ...komga.ClearField) error
}
