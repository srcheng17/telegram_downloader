package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveImagesUseNaturalOrder(t *testing.T) {
	// Reverse input also checks that padding and directory ties have a consistent order.
	want := []string{
		"1.jpg", "01.jpg", "001.jpg", "2.jpg", "02.jpg", "10.jpg",
		"chapter2/1.jpg", "chapter2/2.jpg", "chapter2/10.jpg", "chapter10/1.jpg",
		"page99999999999999999999999999999.jpg", "page100000000000000000000000000000.jpg",
		"页面0.png", "页面00.png", "页面2.png", "页面10.png",
	}
	for _, format := range []string{"zip", "tar stream for rar and 7z"} {
		t.Run(format, func(t *testing.T) {
			var images []ExtractedImage
			var err error
			if format == "zip" {
				archivePath := filepath.Join(t.TempDir(), "pages.zip")
				file, err := os.Create(archivePath)
				if err != nil {
					t.Fatal(err)
				}
				writer := zip.NewWriter(file)
				for i := len(want) - 1; i >= 0; i-- {
					writeZipFixture(t, writer, want[i], []byte(want[i]))
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
				images, err = NewExtractor(ExtractorConfig{}).Extract(context.Background(), archivePath)
			} else {
				var stream bytes.Buffer
				writer := tar.NewWriter(&stream)
				for i := len(want) - 1; i >= 0; i-- {
					if err := writer.WriteHeader(&tar.Header{Name: want[i], Mode: 0600, Size: int64(len(want[i]))}); err != nil {
						t.Fatal(err)
					}
					if _, err := writer.Write([]byte(want[i])); err != nil {
						t.Fatal(err)
					}
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				images, err = extractTarImages(context.Background(), &stream, archiveLimits(ExtractorConfig{}))
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(images) != len(want) {
				t.Fatalf("image count = %d, want %d", len(images), len(want))
			}
			for i, name := range want {
				if images[i].Name != name || string(images[i].Data) != name {
					t.Errorf("page %d = %q (%q), want %q", i+1, images[i].Name, images[i].Data, name)
				}
			}
		})
	}
}
