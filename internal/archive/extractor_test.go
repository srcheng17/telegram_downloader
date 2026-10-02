package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type fakeExternalExtractor struct {
	calls  []string
	images []ExtractedImage
	err    error
}

func (f *fakeExternalExtractor) Extract(ctx context.Context, archivePath string) ([]ExtractedImage, error) {
	f.calls = append(f.calls, archivePath)
	if f.err != nil {
		return nil, f.err
	}
	return append([]ExtractedImage(nil), f.images...), nil
}

func TestExtractImagesFromZipArchive(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "sample.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zipWriter := zip.NewWriter(file)
	writeZipFixture(t, zipWriter, "002-second.png", []byte("second"))
	writeZipFixture(t, zipWriter, "001-first.jpg", []byte("first"))
	writeZipFixture(t, zipWriter, "notes/readme.txt", []byte("skip"))
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	extractor := NewExtractor(ExtractorConfig{})
	images, err := extractor.Extract(context.Background(), archivePath)
	if err != nil {
		t.Fatalf("extract zip images: %v", err)
	}
	if len(images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(images))
	}
	if images[0].Name != "001-first.jpg" || string(images[0].Data) != "first" {
		t.Fatalf("unexpected first image: %#v", images[0])
	}
	if images[1].Name != "002-second.png" || string(images[1].Data) != "second" {
		t.Fatalf("unexpected second image: %#v", images[1])
	}
}

func TestExtractorDelegatesRarAnd7zToExternalExtractor(t *testing.T) {
	external := &fakeExternalExtractor{
		images: []ExtractedImage{{Name: "page01.jpg", Data: []byte("demo")}},
	}
	extractor := NewExtractor(ExtractorConfig{External: external})

	for _, path := range []string{"/tmp/demo.rar", "/tmp/demo.7z"} {
		images, err := extractor.Extract(context.Background(), path)
		if err != nil {
			t.Fatalf("extract %s: %v", path, err)
		}
		if len(images) != 1 || images[0].Name != "page01.jpg" {
			t.Fatalf("unexpected images for %s: %#v", path, images)
		}
	}

	if len(external.calls) != 2 {
		t.Fatalf("expected 2 external calls, got %d", len(external.calls))
	}
}

func writeZipFixture(t *testing.T, zipWriter *zip.Writer, name string, data []byte) {
	t.Helper()
	entryWriter, err := zipWriter.Create(name)
	if err != nil {
		t.Fatalf("create zip entry %s: %v", name, err)
	}
	if _, err := entryWriter.Write(data); err != nil {
		t.Fatalf("write zip entry %s: %v", name, err)
	}
}

func TestArchiveLimitsAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		count    int
		bytes    int
		canceled bool
		cfg      ExtractorConfig
	}{
		{name: "count", count: 301, bytes: 1},
		{name: "single image", count: 1, bytes: 5, cfg: ExtractorConfig{MaxImageBytes: 4}},
		{name: "total", count: 2, bytes: 3, cfg: ExtractorConfig{MaxTotalBytes: 5}},
		{name: "canceled", count: 1, bytes: 1, canceled: true},
		{name: "no images", count: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "limited.zip")
			f, err := os.Create(p)
			if err != nil {
				t.Fatal(err)
			}
			z := zip.NewWriter(f)
			for i := 0; i < tc.count; i++ {
				writeZipFixture(t, z, fmt.Sprintf("%03d.jpg", i), make([]byte, tc.bytes))
			}
			if err = z.Close(); err != nil {
				t.Fatal(err)
			}
			if err = f.Close(); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			if images, err := NewExtractor(tc.cfg).Extract(ctx, p); err == nil {
				t.Fatalf("expected rejection, got %d images", len(images))
			} else if tc.canceled && !errors.Is(err, context.Canceled) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestExternalArchiveStreamLimits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		size  int64
		cfg   ExtractorConfig
	}{
		{"count", 301, 1, ExtractorConfig{}},
		{"single", 1, 5, ExtractorConfig{MaxImageBytes: 4}},
		{"total", 2, 3, ExtractorConfig{MaxTotalBytes: 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stream bytes.Buffer
			tw := tar.NewWriter(&stream)
			for i := 0; i < tc.count; i++ {
				if err := tw.WriteHeader(&tar.Header{Name: fmt.Sprintf("%03d.jpg", i), Mode: 0600, Size: tc.size}); err != nil {
					t.Fatal(err)
				}
				if _, err := tw.Write(make([]byte, tc.size)); err != nil {
					t.Fatal(err)
				}
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := extractTarImages(context.Background(), &stream, archiveLimits(tc.cfg)); err == nil {
				t.Fatal("stream must reject limits")
			}
		})
	}
}
func TestExternal7zConversion(t *testing.T) {
	if _, err := exec.LookPath("bsdtar"); err != nil {
		t.Skip("bsdtar not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "001 page.jpg"), []byte("page content"), 0600); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(dir, "sample.7z")
	cmd := exec.Command("bsdtar", "--format=7zip", "-cf", archivePath, "-C", dir, "001 page.jpg")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create 7z: %v %s", err, output)
	}
	images, err := NewExtractor(ExtractorConfig{}).Extract(context.Background(), archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || images[0].Name != "001 page.jpg" || string(images[0].Data) != "page content" {
		t.Fatalf("images=%#v", images)
	}
}
