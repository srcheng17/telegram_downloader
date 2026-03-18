package archive

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"mime"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type ExtractedImage struct {
	Name        string
	ContentType string
	Data        []byte
}

type ExternalExtractor interface {
	Extract(ctx context.Context, archivePath string) ([]ExtractedImage, error)
}

type ExtractorConfig struct {
	External ExternalExtractor
}

type Extractor struct {
	external ExternalExtractor
}

func NewExtractor(cfg ExtractorConfig) *Extractor {
	external := cfg.External
	if external == nil {
		external = NewExternalExtractor()
	}
	return &Extractor{external: external}
}

func (e *Extractor) Extract(ctx context.Context, archivePath string) ([]ExtractedImage, error) {
	normalizedPath := strings.TrimSpace(archivePath)
	switch strings.ToLower(filepath.Ext(normalizedPath)) {
	case ".zip":
		return extractZipImages(normalizedPath)
	case ".rar", ".7z":
		if e == nil || e.external == nil {
			return nil, errors.New("external archive extractor is not configured")
		}
		return e.external.Extract(ctx, normalizedPath)
	default:
		return nil, errors.New("unsupported archive format")
	}
}

func extractZipImages(archivePath string) ([]ExtractedImage, error) {
	reader, err := zip.OpenReader(strings.TrimSpace(archivePath))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	files := make([]*zip.File, 0, len(reader.File))
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if !isImagePath(file.Name) {
			continue
		}
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})

	images := make([]ExtractedImage, 0, len(files))
	for _, file := range files {
		fileReader, err := file.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := readAll(fileReader)
		closeErr := fileReader.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		images = append(images, ExtractedImage{
			Name:        filepath.ToSlash(path.Clean(file.Name)),
			ContentType: contentTypeForPath(file.Name),
			Data:        data,
		})
	}
	return images, nil
}

func isImagePath(filePath string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(filePath))) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp", ".avif":
		return true
	default:
		return false
	}
}

func contentTypeForPath(filePath string) string {
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(strings.TrimSpace(filePath))))
	if contentType == "" {
		return "application/octet-stream"
	}
	return contentType
}

func readAll(reader io.Reader) ([]byte, error) {
	return io.ReadAll(reader)
}
