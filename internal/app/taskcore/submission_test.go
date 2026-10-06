package taskcore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestUploadReviewedBytesCannotBeReplacedAtAttachment(t *testing.T) {
	original := []byte("reviewed archive")
	sum := sha256.Sum256(original)
	path := filepath.Join(t.TempDir(), "archive.zip")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	input := Input{SourceArchiveName: "archive.zip", SourceArchiveSize: int64(len(original)), SourceSHA256: hex.EncodeToString(sum[:])}
	source := AttachUploadSourceInput{Name: "archive.zip", Size: int64(len(original)), Path: path}
	if err := verifyUploadSnapshot(input, source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("different archive"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyUploadSnapshot(input, source); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed bytes accepted: %v", err)
	}
	source.Name = "other.zip"
	if err := verifyUploadSnapshot(input, source); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed filename accepted: %v", err)
	}
	// Legacy callers do not acquire an unrequested source fingerprint contract.
	if err := verifyUploadSnapshot(Input{}, source); err != nil {
		t.Fatal(err)
	}
}
