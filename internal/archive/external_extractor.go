package archive

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

type ExternalExtractorImpl struct {
	runner CommandRunner
}

func NewExternalExtractor() *ExternalExtractorImpl {
	return &ExternalExtractorImpl{runner: execCommandRunner{}}
}

func (e *ExternalExtractorImpl) Extract(ctx context.Context, archivePath string) ([]ExtractedImage, error) {
	if e == nil {
		return nil, errors.New("external extractor is required")
	}
	if e.runner == nil {
		e.runner = execCommandRunner{}
	}
	tempDir, err := os.MkdirTemp("", "upload-archive-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	if err := e.runner.Run(ctx, "bsdtar", "-xf", archivePath, "-C", tempDir); err != nil {
		return nil, err
	}

	paths := make([]string, 0)
	walkErr := filepath.Walk(tempDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info == nil || info.IsDir() {
			return nil
		}
		if !isImagePath(path) {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Strings(paths)

	images := make([]ExtractedImage, 0, len(paths))
	for _, imagePath := range paths {
		data, err := os.ReadFile(imagePath)
		if err != nil {
			return nil, err
		}
		relPath, err := filepath.Rel(tempDir, imagePath)
		if err != nil {
			return nil, err
		}
		images = append(images, ExtractedImage{
			Name:        filepath.ToSlash(relPath),
			ContentType: contentTypeForPath(imagePath),
			Data:        data,
		})
	}
	return images, nil
}
