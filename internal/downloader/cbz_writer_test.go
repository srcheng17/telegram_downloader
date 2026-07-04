package downloader

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"path/filepath"
	"testing"

	"github.com/ryancheng/telegram-downloader/internal/domain"
)

func TestCBZContainsComicInfoMetadata(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "test.cbz")

	comicInfo, err := WriteComicInfoXML(TaskMetadata{
		Writer:  "Author",
		Series:  "Series Name",
		Number:  "3",
		Title:   "Comic Title",
		Summary: "A summary",
		Tags:    "tag1,tag2",
		Genre:   "genre1,genre2",
	})
	if err != nil {
		t.Fatalf("WriteComicInfoXML returned error: %v", err)
	}

	images := []LocalImage{
		{
			Name: "1.jpg",
			Data: []byte("image-bytes"),
		},
	}
	if err := PackCBZ(images, comicInfo, outputPath); err != nil {
		t.Fatalf("PackCBZ returned error: %v", err)
	}

	archive, err := zip.OpenReader(outputPath)
	if err != nil {
		t.Fatalf("open cbz: %v", err)
	}
	defer archive.Close()

	comicInfoPayload := readFileFromZip(t, archive.File, "ComicInfo.xml")

	var payload struct {
		XMLName xml.Name `xml:"ComicInfo"`
		Writer  string   `xml:"Writer"`
		Series  string   `xml:"Series"`
		Number  string   `xml:"Number"`
		Title   string   `xml:"Title"`
		Summary string   `xml:"Summary"`
		Tags    string   `xml:"Tags"`
		Genre   string   `xml:"Genre"`
	}
	if err := xml.Unmarshal(comicInfoPayload, &payload); err != nil {
		t.Fatalf("unmarshal ComicInfo.xml: %v", err)
	}

	if payload.Writer != "Author" {
		t.Fatalf("expected Writer=Author, got %q", payload.Writer)
	}
	if payload.Series != "Series Name" {
		t.Fatalf("expected Series=Series Name, got %q", payload.Series)
	}
	if payload.Number != "3" {
		t.Fatalf("expected Number=3, got %q", payload.Number)
	}
	if payload.Title != "Comic Title" {
		t.Fatalf("expected Title=Comic Title, got %q", payload.Title)
	}
	if payload.Summary != "A summary" {
		t.Fatalf("expected Summary=A summary, got %q", payload.Summary)
	}
	if payload.Tags != "tag1,tag2" {
		t.Fatalf("expected Tags=tag1,tag2, got %q", payload.Tags)
	}
	if payload.Genre != "genre1,genre2" {
		t.Fatalf("expected Genre=genre1,genre2, got %q", payload.Genre)
	}
}

func TestPackageCBZUsesContentTypeWhenURLSuffixIsNotImage(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "test.cbz")
	service := Service{}

	err := service.PackageCBZ(
		[]domain.DownloadedImage{
			{
				URL:         "https://example.com/suspicious.php",
				ContentType: "image/png; charset=utf-8",
				Data:        []byte("image-bytes"),
			},
		},
		TaskMetadata{},
		outputPath,
	)
	if err != nil {
		t.Fatalf("PackageCBZ returned error: %v", err)
	}

	archive, err := zip.OpenReader(outputPath)
	if err != nil {
		t.Fatalf("open cbz: %v", err)
	}
	defer archive.Close()

	if !zipContainsFile(archive.File, "1.png") {
		t.Fatalf("expected image entry 1.png in archive")
	}
	if zipContainsFile(archive.File, "1.php") {
		t.Fatalf("did not expect image entry 1.php in archive")
	}
}

func TestWriteComicInfoXMLSanitizesInvalidControlCharacters(t *testing.T) {
	comicInfo, err := WriteComicInfoXML(TaskMetadata{
		Summary: "line\x01with\x0Bcontrol\x1Fchars",
	})
	if err != nil {
		t.Fatalf("WriteComicInfoXML returned error: %v", err)
	}

	var payload struct {
		Summary string `xml:"Summary"`
	}
	if err := xml.Unmarshal(comicInfo, &payload); err != nil {
		t.Fatalf("unmarshal ComicInfo.xml: %v", err)
	}
	if payload.Summary != "linewithcontrolchars" {
		t.Fatalf("expected sanitized summary, got %q", payload.Summary)
	}
}

func readFileFromZip(t *testing.T, files []*zip.File, name string) []byte {
	t.Helper()

	for _, file := range files {
		if file.Name != name {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatalf("open %s from zip: %v", name, err)
		}
		defer reader.Close()

		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read %s from zip: %v", name, err)
		}
		return bytes.Clone(content)
	}

	t.Fatalf("%s not found in archive", name)
	return nil
}

func zipContainsFile(files []*zip.File, name string) bool {
	for _, file := range files {
		if file.Name == name {
			return true
		}
	}
	return false
}
