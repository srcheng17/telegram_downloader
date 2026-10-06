package taskcore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
)

var ErrSubmissionSourceConflict = errors.New("source already has a task with different reviewed metadata")

var ErrIdempotencyConflict = errors.New("task submission key conflicts with input")

var submissionKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)
var sourceHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Submission binds the caller's reviewed input, independently of server-side
// settings resolved at first creation. It is not another task state machine.
type Submission struct {
	Key         string
	RequestHash string
}

type SubmissionRepository interface {
	FindSubmission(context.Context, string) (*TaskView, string, bool, error)
}

func newSubmission(key, target string, value any) (*Submission, error) {
	if target != "" && target != "download" && target != "komga" {
		return nil, ErrInvalidInput
	}
	if key == "" {
		return nil, nil
	}
	if !submissionKeyPattern.MatchString(key) {
		return nil, ErrInvalidInput
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, ErrInvalidInput
	}
	hash := sha256.Sum256(data)
	return &Submission{Key: key, RequestHash: hex.EncodeToString(hash[:])}, nil
}

func (s *Service) replaySubmission(ctx context.Context, submission *Submission) (*CreateURLResult, error) {
	if submission == nil {
		return nil, nil
	}
	repo, ok := s.repo.(SubmissionRepository)
	if !ok {
		return nil, ErrInvalidInput
	}
	view, requestHash, confirmation, err := repo.FindSubmission(ctx, submission.Key)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if requestHash != submission.RequestHash {
		return nil, ErrIdempotencyConflict
	}
	return &CreateURLResult{Task: view.Task, Result: view.Result, Reused: true, NeedsConfirmation: confirmation}, nil
}

// GetSubmission recovers a lost first response without requiring a task ID.
func (s *Service) GetSubmission(ctx context.Context, key string) (*TaskView, error) {
	if !submissionKeyPattern.MatchString(key) {
		return nil, ErrInvalidInput
	}
	repo, ok := s.repo.(SubmissionRepository)
	if !ok {
		return nil, ErrNotFound
	}
	view, _, _, err := repo.FindSubmission(ctx, key)
	return view, err
}

func verifyUploadSnapshot(input Input, source AttachUploadSourceInput) error {
	if input.SourceSHA256 == "" {
		return nil
	}
	if source.Name != input.SourceArchiveName || source.Size != input.SourceArchiveSize {
		return ErrIdempotencyConflict
	}
	file, err := os.Open(source.Path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != input.SourceSHA256 {
		return ErrIdempotencyConflict
	}
	return nil
}
