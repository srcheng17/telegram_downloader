package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKomgaCopierUsesSeriesFolderOrTanbokon(t *testing.T) {
	root := t.TempDir()
	copier := NewKomgaCopier(KomgaCopyConfig{Root: root})

	seriesTarget, err := copier.TargetPath("demo.cbz", "系列A")
	if err != nil {
		t.Fatalf("target path with series: %v", err)
	}
	if !strings.Contains(seriesTarget, filepath.Join(root, "系列A", "demo.cbz")) {
		t.Fatalf("unexpected series target path %q", seriesTarget)
	}

	tanbokonTarget, err := copier.TargetPath("demo.cbz", "")
	if err != nil {
		t.Fatalf("target path without series: %v", err)
	}
	if !strings.Contains(tanbokonTarget, filepath.Join(root, "tanbokon", "demo.cbz")) {
		t.Fatalf("unexpected tanbokon target path %q", tanbokonTarget)
	}
}

func TestKomgaCopierCopiesArtifactToTargetFolder(t *testing.T) {
	root := t.TempDir()
	sourceDir := t.TempDir()
	sourcePath := filepath.Join(sourceDir, "source.cbz")
	if err := os.WriteFile(sourcePath, []byte("cbz-data"), 0o644); err != nil {
		t.Fatalf("write source artifact: %v", err)
	}

	copier := NewKomgaCopier(KomgaCopyConfig{Root: root})
	targetPath, err := copier.CopyFromPath(sourcePath, "result.cbz", "系列A")
	if err != nil {
		t.Fatalf("copy artifact: %v", err)
	}
	if content, err := os.ReadFile(targetPath); err != nil {
		t.Fatalf("read copied artifact: %v", err)
	} else if string(content) != "cbz-data" {
		t.Fatalf("unexpected copied content %q", string(content))
	}
}
