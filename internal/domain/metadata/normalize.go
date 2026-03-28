package metadata

import (
	"strings"
	"unicode"
)

func NormalizeOptionalTextPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func NormalizeAuthorListPtr(value *string) *string {
	normalized := NormalizeOptionalTextPtr(value)
	if normalized == nil {
		return nil
	}
	replaced := normalizeDelimitedList(*normalized, func(r rune) bool {
		return r == ',' || r == '，' || r == '#' || r == '＃'
	})
	return NormalizeOptionalTextPtr(&replaced)
}

func NormalizeTagLikeListPtr(value *string) *string {
	normalized := NormalizeOptionalTextPtr(value)
	if normalized == nil {
		return nil
	}
	replaced := normalizeDelimitedList(*normalized, func(r rune) bool {
		return r == ',' || r == '，' || r == '#' || r == '＃' || unicode.IsSpace(r)
	})
	return NormalizeOptionalTextPtr(&replaced)
}

func normalizeDelimitedList(raw string, isDelimiter func(rune) bool) string {
	items := strings.FieldsFunc(raw, isDelimiter)
	if len(items) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(items))
	normalized := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return strings.Join(normalized, ",")
}

