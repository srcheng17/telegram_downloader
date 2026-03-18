package archive

import (
	"archive/zip"
	"context"
	"os"
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
