package archive

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ryancheng/telegram-downloader/internal/config"
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
	External      ExternalExtractor
	MaxImages     int
	MaxImageBytes int64
	MaxTotalBytes int64
}
type Extractor struct {
	external ExternalExtractor
	limits   ExtractorConfig
}

func archiveLimits(cfg ExtractorConfig) ExtractorConfig {
	if cfg.MaxImages <= 0 {
		cfg.MaxImages = config.MaxImagesPerTask
	}
	if cfg.MaxImageBytes <= 0 {
		cfg.MaxImageBytes = config.MaxBytesPerImage
	}
	if cfg.MaxTotalBytes <= 0 {
		cfg.MaxTotalBytes = config.MaxBytesPerTask
	}
	return cfg
}
func NewExtractor(cfg ExtractorConfig) *Extractor {
	cfg = archiveLimits(cfg)
	external := cfg.External
	if external == nil {
		external = &ExternalExtractorImpl{limits: cfg}
	}
	return &Extractor{external: external, limits: cfg}
}
func (e *Extractor) Extract(ctx context.Context, archivePath string) ([]ExtractedImage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e == nil {
		return nil, errors.New("archive extractor is required")
	}
	archivePath = strings.TrimSpace(archivePath)
	var images []ExtractedImage
	var err error
	switch strings.ToLower(filepath.Ext(archivePath)) {
	case ".zip":
		images, err = extractZipImages(ctx, archivePath, e.limits)
	case ".rar", ".7z":
		if e.external == nil {
			return nil, errors.New("external archive extractor is not configured")
		}
		images, err = e.external.Extract(ctx, archivePath)
	default:
		return nil, errors.New("unsupported archive format")
	}
	if err != nil {
		return nil, err
	}
	if len(images) == 0 {
		return nil, errors.New("no images found in archive")
	}
	return images, nil
}
func extractZipImages(ctx context.Context, archivePath string, limits ExtractorConfig) ([]ExtractedImage, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	files := make([]*zip.File, 0)
	var declaredTotal uint64
	for _, file := range reader.File {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if file.UncompressedSize64 > uint64(limits.MaxTotalBytes)-declaredTotal {
			return nil, errors.New("archive total size limit exceeded")
		}
		declaredTotal += file.UncompressedSize64
		if !isImagePath(file.Name) {
			continue
		}
		if len(files) >= limits.MaxImages {
			return nil, errors.New("archive image count limit exceeded")
		}
		if file.UncompressedSize64 > uint64(limits.MaxImageBytes) {
			return nil, errors.New("archive image size limit exceeded")
		}
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	images := make([]ExtractedImage, 0, len(files))
	var total int64
	for _, file := range files {
		r, err := file.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := readArchiveImage(ctx, r, min(limits.MaxImageBytes, limits.MaxTotalBytes-total))
		closeErr := r.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		total += int64(len(data))
		images = append(images, ExtractedImage{Name: filepath.ToSlash(path.Clean(file.Name)), ContentType: contentTypeForPath(file.Name), Data: data})
	}
	return images, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
func readArchiveImage(ctx context.Context, r io.Reader, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: r}, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("archive image or total size limit exceeded")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
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
