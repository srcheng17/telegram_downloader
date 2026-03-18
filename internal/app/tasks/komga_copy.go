package tasks

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const defaultKomgaFallbackDir = "tanbokon"

var invalidKomgaPathCharsPattern = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)

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
	root := strings.TrimSpace(c.root)
	if root == "" {
		return "", fmt.Errorf("komga root is required")
	}
	baseName := strings.TrimSpace(filepath.Base(fileName))
	if baseName == "" || baseName == "." || baseName == string(filepath.Separator) {
		return "", fmt.Errorf("artifact file name is required")
	}
	subdir := targetSubdir(seriesName)
	return filepath.Join(root, subdir, baseName), nil
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

func targetSubdir(seriesName string) string {
	safeSeries := sanitizeKomgaPathSegment(seriesName)
	if safeSeries != "" {
		return safeSeries
	}
	return defaultKomgaFallbackDir
}

func sanitizeKomgaPathSegment(value string) string {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return ""
	}
	normalized = invalidKomgaPathCharsPattern.ReplaceAllString(normalized, " ")
	normalized = strings.Join(strings.Fields(normalized), " ")
	normalized = strings.Trim(normalized, " .")
	if normalized == "" || normalized == "." || normalized == ".." {
		return ""
	}
	return normalized
}
