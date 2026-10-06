package tasks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKomgaCopierUsesSeriesFolderOrTankobon(t *testing.T) {
	root := t.TempDir()
	copier := NewKomgaCopier(KomgaCopyConfig{Root: root})

	seriesTarget, err := copier.TargetPath("demo.cbz", "系列A")
	if err != nil {
		t.Fatalf("target path with series: %v", err)
	}
	if !strings.Contains(seriesTarget, filepath.Join(root, "系列A", "demo.cbz")) {
		t.Fatalf("unexpected series target path %q", seriesTarget)
	}

	tankobonTarget, err := copier.TargetPath("demo.cbz", "")
	if err != nil {
		t.Fatalf("target path without series: %v", err)
	}
	if !strings.Contains(tankobonTarget, filepath.Join(root, "tankobon", "demo.cbz")) {
		t.Fatalf("unexpected tankobon target path %q", tankobonTarget)
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

func TestKomgaCopyRetryIsSameFileAndConflictNeverOverwrites(t *testing.T) {
	root, sourceDir := t.TempDir(), t.TempDir()
	source := filepath.Join(sourceDir, "source.cbz")
	if err := os.WriteFile(source, []byte("first book"), 0600); err != nil {
		t.Fatal(err)
	}
	copier := NewKomgaCopier(KomgaCopyConfig{Root: root})
	target, err := copier.CopyFromPath(source, "book.cbz", "")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(target)
	if _, err := copier.CopyFromPath(source, "book.cbz", ""); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(target)
	if !os.SameFile(before, after) {
		t.Fatal("retry replaced destination")
	}
	if err := os.WriteFile(source, []byte("other book"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := copier.CopyFromPath(source, "book.cbz", ""); !errors.Is(err, ErrKomgaTargetConflict) {
		t.Fatalf("conflict: %v", err)
	}
	actual, _ := os.ReadFile(target)
	if string(actual) != "first book" {
		t.Fatal("unrelated book overwritten")
	}
	entries, _ := os.ReadDir(filepath.Dir(target))
	if len(entries) != 1 {
		t.Fatal("temporary copy leaked")
	}
}

func TestKomgaCopyRejectsSymlinkDirectoryAndTarget(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(fmt.Sprint(directory), func(t *testing.T) {
			root, other := t.TempDir(), t.TempDir()
			source := filepath.Join(other, "source.cbz")
			if err := os.WriteFile(source, []byte("book"), 0600); err != nil {
				t.Fatal(err)
			}
			folder := filepath.Join(root, "tankobon")
			if directory {
				if err := os.Symlink(other, folder); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(folder, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(source, filepath.Join(folder, "book.cbz")); err != nil {
					t.Fatal(err)
				}
			}
			_, err := NewKomgaCopier(KomgaCopyConfig{Root: root}).CopyFromPath(source, "book.cbz", "")
			if !errors.Is(err, ErrKomgaUnsafeTarget) {
				t.Fatalf("symlink accepted: %v", err)
			}
		})
	}
}
