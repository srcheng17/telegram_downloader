package tasks

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	domainkomga "github.com/ryancheng/telegram-downloader/internal/domain/komga"
)

type KomgaCopyConfig struct {
	Root string
}

type KomgaCopier struct {
	root string
}

func NewKomgaCopier(cfg KomgaCopyConfig) *KomgaCopier {
	return &KomgaCopier{root: strings.TrimSpace(cfg.Root)}
}

func (c *KomgaCopier) TargetPath(fileName, seriesName string) (string, error) {
	targetPath, err := domainkomga.BuildTargetPath(c.root, fileName, seriesName)
	if err != nil {
		return "", err
	}
	return targetPath, nil
}

func (c *KomgaCopier) CopyFromPath(sourcePath, fileName, seriesName string) (string, error) {
	targetPath, err := c.TargetPath(fileName, seriesName)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return "", err
	}

	sourceFile, err := os.Open(strings.TrimSpace(sourcePath))
	if err != nil {
		return "", err
	}
	defer sourceFile.Close()

	tempFile, err := os.CreateTemp(filepath.Dir(targetPath), ".komga-copy-*.tmp")
	if err != nil {
		return "", err
	}
	tempPath := tempFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := io.Copy(tempFile, sourceFile); err != nil {
		_ = tempFile.Close()
		return "", err
	}
	if err := tempFile.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tempPath, targetPath); err != nil {
		return "", err
	}
	cleanup = false
	return targetPath, nil
}
