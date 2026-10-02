package downloader

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var invalidXML10CharsPattern = regexp.MustCompile("[\x00-\x08\x0B\x0C\x0E-\x1F]")

type TaskMetadata struct {
	Writer  string
	Series  string
	Number  string
	Title   string
	Summary string
	Tags    string
	Genre   string
}

type LocalImage struct {
	Name string
	Data []byte
}

type comicInfoXML struct {
	XMLName xml.Name `xml:"ComicInfo"`
	Writer  string   `xml:"Writer"`
	Series  string   `xml:"Series"`
	Number  string   `xml:"Number"`
	Title   string   `xml:"Title"`
	Summary string   `xml:"Summary"`
	Tags    string   `xml:"Tags"`
	Genre   string   `xml:"Genre"`
}

func WriteComicInfoXML(meta TaskMetadata) ([]byte, error) {
	payload, err := xml.MarshalIndent(
		comicInfoXML{
			Writer:  sanitizeComicInfoValue(meta.Writer),
			Series:  sanitizeComicInfoValue(meta.Series),
			Number:  sanitizeComicInfoValue(meta.Number),
			Title:   sanitizeComicInfoValue(meta.Title),
			Summary: sanitizeComicInfoValue(meta.Summary),
			Tags:    sanitizeComicInfoValue(meta.Tags),
			Genre:   sanitizeComicInfoValue(meta.Genre),
		},
		"",
		"  ",
	)
	if err != nil {
		return nil, fmt.Errorf("marshal comicinfo xml: %w", err)
	}
	return append([]byte(xml.Header), payload...), nil
}

func PackCBZ(images []LocalImage, comicInfo []byte, outputPath string) error {
	return PackCBZContext(context.Background(), images, comicInfo, outputPath)
}

func PackCBZContext(ctx context.Context, images []LocalImage, comicInfo []byte, outputPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	outputPath = strings.TrimSpace(outputPath)
	if outputPath == "" {
		return errors.New("cbz output path is required")
	}

	destDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create cbz output directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(destDir, ".cbz-tmp-*.cbz")
	if err != nil {
		return fmt.Errorf("create temporary cbz file: %w", err)
	}

	tmpPath := tmpFile.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	zipWriter := zip.NewWriter(tmpFile)
	if len(comicInfo) > 0 {
		if err := writeZipEntry(zipWriter, "ComicInfo.xml", comicInfo); err != nil {
			_ = zipWriter.Close()
			_ = tmpFile.Close()
			return err
		}
	}
	for index, image := range images {
		if err := ctx.Err(); err != nil {
			_ = zipWriter.Close()
			_ = tmpFile.Close()
			return err
		}
		entryName := normalizeArchiveImageName(image.Name, index)
		if err := writeZipEntry(zipWriter, entryName, image.Data); err != nil {
			_ = zipWriter.Close()
			_ = tmpFile.Close()
			return err
		}
	}
	if err := zipWriter.Close(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("close cbz writer: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close cbz file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, outputPath); err != nil {
		return fmt.Errorf("move cbz into place: %w", err)
	}

	cleanupTmp = false
	return nil
}

func sanitizeComicInfoValue(value string) string {
	return invalidXML10CharsPattern.ReplaceAllString(value, "")
}

func normalizeArchiveImageName(name string, index int) string {
	normalized := strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	normalized = path.Base(normalized)
	if normalized == "" || normalized == "." || normalized == "/" || strings.EqualFold(normalized, "ComicInfo.xml") {
		return fmt.Sprintf("%d.jpg", index+1)
	}
	return normalized
}

func writeZipEntry(zipWriter *zip.Writer, entryName string, payload []byte) error {
	entryWriter, err := zipWriter.Create(entryName)
	if err != nil {
		return fmt.Errorf("create cbz entry %q: %w", entryName, err)
	}
	if _, err := entryWriter.Write(payload); err != nil {
		return fmt.Errorf("write cbz entry %q: %w", entryName, err)
	}
	return nil
}
