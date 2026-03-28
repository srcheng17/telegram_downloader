package komga

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTargetSubdirFallsBackToTanbokonWhenSeriesMissing(t *testing.T) {
	t.Parallel()

	if got := TargetSubdir(""); got != "tanbokon" {
		t.Fatalf("expected tanbokon fallback, got %q", got)
	}
}

func TestBuildTargetPathUsesSeriesDirectoryWhenPresent(t *testing.T) {
	t.Parallel()

	got, err := BuildTargetPath("/library", "demo.cbz", "系列A")
	if err != nil {
		t.Fatalf("BuildTargetPath returned error: %v", err)
	}
	want := filepath.Join("/library", "系列A", "demo.cbz")
	if !strings.Contains(got, want) && got != want {
		t.Fatalf("expected path %q, got %q", want, got)
	}
}

