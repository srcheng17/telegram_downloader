package komga

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

const DefaultFallbackDir = "tankobon"

var invalidKomgaPathCharsPattern = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)

func BuildTargetPath(root, fileName, seriesName string) (string, error) {
	normalizedRoot := strings.TrimSpace(root)
	if normalizedRoot == "" {
		return "", fmt.Errorf("komga root is required")
	}
	baseName := strings.TrimSpace(filepath.Base(fileName))
	if baseName == "" || baseName == "." || baseName == string(filepath.Separator) {
		return "", fmt.Errorf("artifact file name is required")
	}
	return filepath.Join(normalizedRoot, TargetSubdir(seriesName), baseName), nil
}

func TargetSubdir(seriesName string) string {
	safeSeries := sanitizePathSegment(seriesName)
	if safeSeries != "" {
		return safeSeries
	}
	return DefaultFallbackDir
}

func sanitizePathSegment(value string) string {
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
