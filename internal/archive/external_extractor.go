package archive

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path"
	"sort"
)

type ExternalExtractorImpl struct{ limits ExtractorConfig }

func NewExternalExtractor() *ExternalExtractorImpl {
	return &ExternalExtractorImpl{limits: archiveLimits(ExtractorConfig{})}
}
func (e *ExternalExtractorImpl) Extract(ctx context.Context, archivePath string) ([]ExtractedImage, error) {
	if e == nil {
		return nil, errors.New("external extractor is required")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Convert to a tar stream so Go can bound each member before reading it; nothing is expanded to disk.
	cmd := exec.CommandContext(ctx, "bsdtar", "-cf", "-", "--format=pax", "@"+archivePath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	images, readErr := extractTarImages(ctx, stdout, archiveLimits(e.limits))
	if readErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, readErr
	}
	if waitErr != nil {
		return nil, fmt.Errorf("read external archive: %w", waitErr)
	}
	return images, nil
}
func extractTarImages(ctx context.Context, stream io.Reader, limits ExtractorConfig) ([]ExtractedImage, error) {
	reader := tar.NewReader(contextReader{ctx: ctx, reader: stream})
	var images []ExtractedImage
	var declaredTotal int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		if header.Size > limits.MaxTotalBytes-declaredTotal {
			return nil, errors.New("archive total size limit exceeded")
		}
		declaredTotal += header.Size
		if !isImagePath(header.Name) {
			continue
		}
		if len(images) >= limits.MaxImages {
			return nil, errors.New("archive image count limit exceeded")
		}
		if header.Size > limits.MaxImageBytes {
			return nil, errors.New("archive image size limit exceeded")
		}
		data, err := readArchiveImage(ctx, reader, limits.MaxImageBytes)
		if err != nil {
			return nil, err
		}
		images = append(images, ExtractedImage{Name: path.Clean(header.Name), ContentType: contentTypeForPath(header.Name), Data: data})
	}
	sort.SliceStable(images, func(i, j int) bool { return naturalNameLess(images[i].Name, images[j].Name) })
	return images, nil
}
