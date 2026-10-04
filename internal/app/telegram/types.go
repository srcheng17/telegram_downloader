package telegram

import (
	"context"
	"errors"
	"os"
	"time"
)

const MaximumSourceBytes int64 = 500 << 20

var ErrBusy = errors.New("telegram busy")
var ErrNotFound = errors.New("telegram attempt not found")
var ErrConflict = errors.New("telegram state conflict")

type Error struct{ Code string }

func (e *Error) Error() string  { return "telegram: " + e.Code }
func Failure(code string) error { return &Error{Code: code} }

// Input is immutable task source data. It never contains credentials or a path.
// Task Core owns metadata, runtime settings, task state and generation fencing.
type Input struct {
	MessageURL      string `json:"message_url"`
	AccountRevision int64  `json:"account_revision"`
	AccountIdentity string `json:"account_identity"`
}
type AccountRecord struct {
	Revision   int64
	Namespace  string
	Identity   string
	VerifiedAt time.Time
}
type Account struct {
	State          string     `json:"state"`
	Revision       int64      `json:"revision"`
	MaxSourceBytes int64      `json:"max_source_bytes"`
	VerifiedAt     *time.Time `json:"verified_at,omitempty"`
	Busy           bool       `json:"busy"`
	Attempt        *Snapshot  `json:"attempt,omitempty"`
}
type Snapshot struct {
	ID          string     `json:"attempt_id"`
	Seq         int64      `json:"seq"`
	Revision    int64      `json:"revision"`
	State       string     `json:"state"`
	Code        string     `json:"code,omitempty"`
	ExpiresAt   time.Time  `json:"expires_at"`
	QRExpiresAt *time.Time `json:"qr_expires_at,omitempty"`
	QR          string     `json:"qr,omitempty"`
}
type AttemptRecord struct {
	Snapshot
	Owner            string
	Namespace        string
	ExpectedRevision int64
}
type Store interface {
	Account(context.Context) (AccountRecord, error)
	Attempt(context.Context, string) (AttemptRecord, error)
	LatestAttempt(context.Context) (AttemptRecord, error)
	CreateAttempt(context.Context, AttemptRecord) error
	UpdateAttempt(context.Context, AttemptRecord) error
	Promote(context.Context, AttemptRecord, string, time.Time) (AccountRecord, error)
}

// Lease.File is inherited by every subprocess as descriptor 3. Callers must
// wait for all children before returning; closing the parent's copy is not an unlock.
type Lease struct{ File *os.File }
type Coordinator interface {
	Do(context.Context, bool, func(context.Context, Lease) error) error
	Busy(context.Context) (bool, error)
}
type BridgeEvent struct {
	Type      string     `json:"type"`
	Code      string     `json:"code,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	QR        string     `json:"qr,omitempty"`
	Identity  string     `json:"identity,omitempty"`
	Size      int64      `json:"size,omitempty"`
	Extension string     `json:"extension,omitempty"`
}
type LoginProcess interface {
	Events() <-chan BridgeEvent
	Password(string) error
	Wait() error
}
type Bridge interface {
	Login(context.Context, Lease, string) (LoginProcess, error)
	Verify(context.Context, Lease, string) (string, error)
	Inspect(context.Context, Lease, string, string, int64) (BridgeEvent, error)
	Download(context.Context, Lease, string, string, string, int64) error
	Discard(context.Context, Lease, string) error
}
type Source struct {
	Path      string
	Size      int64
	SHA256    string
	Extension string
}
