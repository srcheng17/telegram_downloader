package service

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestV2ArtifactServiceOpenArtifact(t *testing.T) {
	downloadRoot := t.TempDir()
	artifactPath := filepath.Join(downloadRoot, "demo.cbz")
	if err := os.WriteFile(artifactPath, []byte("cbz-content"), 0o644); err != nil {
		t.Fatalf("write artifact fixture: %v", err)
	}

	svc := NewV2ArtifactService(V2ArtifactServiceConfig{DownloadRoot: downloadRoot})
	artifact, err := svc.OpenArtifact(artifactPath)
	if err != nil {
		t.Fatalf("open artifact: %v", err)
	}
	t.Cleanup(func() { _ = artifact.Close() })

	if artifact.FileName != "demo.cbz" {
		t.Fatalf("expected filename demo.cbz, got %q", artifact.FileName)
	}
	if artifact.ContentType != "application/vnd.comicbook+zip" {
		t.Fatalf("expected cbz content type, got %q", artifact.ContentType)
	}
	content, err := io.ReadAll(artifact.File)
	if err != nil {
		t.Fatalf("read artifact bytes: %v", err)
	}
	if string(content) != "cbz-content" {
		t.Fatalf("expected artifact bytes cbz-content, got %q", string(content))
	}
}

func TestV2ArtifactServiceRejectsIllegalPath(t *testing.T) {
	svc := NewV2ArtifactService(V2ArtifactServiceConfig{DownloadRoot: t.TempDir()})

	_, err := svc.OpenArtifact("../etc/passwd")
	if !errors.Is(err, ErrV2ArtifactPathInvalid) {
		t.Fatalf("expected ErrV2ArtifactPathInvalid, got %v", err)
	}
}

func TestV2ArtifactServiceReturnsNotFoundForMissingFile(t *testing.T) {
	downloadRoot := t.TempDir()
	svc := NewV2ArtifactService(V2ArtifactServiceConfig{DownloadRoot: downloadRoot})

	_, err := svc.OpenArtifact(filepath.Join(downloadRoot, "missing.cbz"))
	if !errors.Is(err, ErrV2ArtifactNotFound) {
		t.Fatalf("expected ErrV2ArtifactNotFound, got %v", err)
	}
}
