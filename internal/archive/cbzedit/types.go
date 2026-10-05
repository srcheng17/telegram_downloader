// Package cbzedit safely replaces ComicInfo.xml in an existing CBZ. It does
// not merge metadata fields or decide whether a Komga book is editable.
package cbzedit

import (
	"context"
	"errors"
	"os"
)

var (
	ErrInvalidArchive = errors.New("CBZ cannot be safely rewritten")
	ErrLimit          = errors.New("CBZ exceeds edit limits")
	ErrConflict       = errors.New("CBZ changed since preview")
	ErrInvalidXML     = errors.New("ComicInfo cannot be safely rewritten")
	ErrBackup         = errors.New("original CBZ backup is not verified")
	ErrNoChange       = errors.New("ComicInfo already matches requested bytes")
)

// Limits bounds both declared ZIP sizes and bytes actually read. Zero fields
// receive conservative defaults; callers may lower these limits per library.
type Limits struct {
	MaxArchiveBytes       int64
	MaxEntries            int
	MaxEntryBytes         int64
	MaxTotalExpandedBytes int64
	MaxXMLBytes           int64
}

type Inspection struct {
	SHA256       string
	Size         int64
	ComicInfo    []byte
	HasComicInfo bool
	EntryCount   int
	PageCount    int
}

// ChangedElements contains ComicInfo element names, not document field keys.
// The writer rejects any nonlisted standard value change and any Pages change.
// PageCount correction requires both a ChangedElements entry and the explicit
// AllowPageCountCorrection flag.
type PrepareRequest struct {
	RelativePath             string
	OperationID              string
	ExpectedSHA256           string
	NewComicInfo             []byte
	ChangedElements          []string
	AllowPageCountCorrection bool
	Limits                   Limits
}

// Prepared can be stored with the edit operation before Commit. Paths are
// root-relative; the backup is never placed beside or inside a public CBZ.
type Prepared struct {
	RelativePath             string
	TemporaryPath            string
	BackupPath               string
	OperationID              string
	OriginalSHA256           string
	NewSHA256                string
	OriginalSize             int64
	NewSize                  int64
	ComicInfoSHA256          string
	ChangedElements          []string
	AllowPageCountCorrection bool
	Limits                   Limits
}

type CommitResult struct {
	// FileCommitted stays true if rename succeeded but directory sync or final
	// readback failed. The caller must reconcile before retrying.
	FileCommitted bool
	SHA256        string
}

// RestoreResult reports whether the original archive was put back. A true
// FileRestored is durable evidence of a successful rename even if a later
// directory sync or readback failed and reconciliation is still required.
type RestoreResult struct {
	FileRestored bool
	SHA256       string
	PreservedRef string
}

// ProbeState describes only facts that can be established from the private
// manifests and current archive. Incomplete and Conflict never authorize an
// automatic file replacement.
type ProbeState string

const (
	ProbeMissing    ProbeState = "missing"
	ProbeIncomplete ProbeState = "incomplete"
	ProbePrepared   ProbeState = "prepared"
	ProbeCommitted  ProbeState = "committed"
	ProbeRestored   ProbeState = "restored"
	ProbeConflict   ProbeState = "conflict"
)

type ProbeResult struct {
	State            ProbeState
	BackupExists     bool
	BackupValid      bool
	CurrentSHA256    string
	CurrentSize      int64
	OriginalSize     int64
	TemporaryExists  bool
	PreservedCurrent bool
	Prepared         *Prepared
}

// Inspect validates an existing CBZ and returns the unique root ComicInfo.
// sourceRoot is an application-owned allowlisted library root.
func Inspect(ctx context.Context, sourceRoot *os.Root, relativePath string, limits Limits) (Inspection, error) {
	state, err := inspectRoot(ctx, sourceRoot, relativePath, normalizeLimits(limits))
	if err != nil {
		return Inspection{}, err
	}
	return state.Inspection, nil
}
