package naming

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAuthorPlaceholder = "未知作者"
	defaultComicPlaceholder  = "未命名漫画"
	maxFilenamePartBytes     = 100
)

var (
	invalidFilenameCharsPattern = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)
	filenameWhitespacePattern   = regexp.MustCompile(`\s+`)
)

type CBZFileNameInput struct {
	Author    string
	Series    string
	Title     string
	Timestamp int64
}

func BuildCBZFileName(in CBZFileNameInput) string {
	unixTimestamp := in.Timestamp
	if unixTimestamp <= 0 {
		unixTimestamp = time.Now().Unix()
	}

	safeAuthor := sanitizeRequiredFilenamePart(in.Author, defaultAuthorPlaceholder)
	safeSeries := sanitizeOptionalFilenamePart(in.Series)
	safeComic := sanitizeRequiredFilenamePart(in.Title, defaultComicPlaceholder)

	parts := []string{safeAuthor}
	if safeSeries != "" {
		parts = append(parts, safeSeries)
	}
	parts = append(parts, safeComic, strconv.FormatInt(unixTimestamp, 10))
	return strings.Join(parts, "_") + ".cbz"
}

func sanitizeRequiredFilenamePart(value, fallback string) string {
	safeValue := sanitizeOptionalFilenamePart(value)
	if safeValue != "" {
		return safeValue
	}
	safeFallback := sanitizeOptionalFilenamePart(fallback)
	if safeFallback != "" {
		return safeFallback
	}
	return "d"
}

func sanitizeOptionalFilenamePart(value string) string {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return ""
	}
	safeValue := invalidFilenameCharsPattern.ReplaceAllString(normalized, " ")
	safeValue = filenameWhitespacePattern.ReplaceAllString(safeValue, " ")
	safeValue = strings.Trim(safeValue, " .")
	if safeValue == "" || safeValue == "." || safeValue == ".." {
		return ""
	}

	safeValue = trimToMaxBytes(safeValue, maxFilenamePartBytes)
	safeValue = strings.Trim(safeValue, " .")
	if safeValue == "" || safeValue == "." || safeValue == ".." {
		return ""
	}
	return safeValue
}

func trimToMaxBytes(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len([]byte(value)) <= maxBytes {
		return value
	}

	collected := make([]rune, 0, len(value))
	usedBytes := 0
	for _, ch := range value {
		charBytes := len(string(ch))
		if usedBytes+charBytes > maxBytes {
			break
		}
		collected = append(collected, ch)
		usedBytes += charBytes
	}
	return string(collected)
}

