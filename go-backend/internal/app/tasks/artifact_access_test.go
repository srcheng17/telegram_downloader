package tasks

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestArtifactAccessRejectsMissingFile(t *testing.T) {
	downloadRoot := t.TempDir()
	access := NewArtifactAccess(ArtifactAccessConfig{DownloadRoot: downloadRoot})
	_, err := access.Open(filepath.Join(downloadRoot, "missing.cbz"))
	if !errors.Is(err, ErrArtifactUnavailable) {
		t.Fatalf("expected ErrArtifactUnavailable, got %v", err)
	}
}
