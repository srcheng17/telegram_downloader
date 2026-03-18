package tasks

import (
	"strings"
	"unicode"
)

func NormalizeMetadata(input MetadataInput) Metadata {
	return Metadata{
		Author:           normalizeOptionalAuthorList(input.Author),
		SeriesName:       normalizeOptionalText(input.SeriesName),
		ComicName:        normalizeOptionalText(input.ComicName),
		Summary:          normalizeOptionalText(input.Summary),
		TagsRaw:          normalizeOptionalText(input.TagsRaw),
		TagsNormalized:   normalizeOptionalTagLikeList(input.TagsRaw),
		GenresRaw:        normalizeOptionalText(input.GenresRaw),
		GenresNormalized: normalizeOptionalTagLikeList(input.GenresRaw),
	}
}

func normalizeOptionalAuthorList(value *string) *string {
	normalized := normalizeOptionalText(value)
	if normalized == nil {
		return nil
	}
	replaced := normalizeDelimitedList(*normalized, func(r rune) bool {
		return r == ',' || r == '，' || r == '#' || r == '＃'
	})
	return normalizeOptionalText(&replaced)
}

func normalizeOptionalTagLikeList(value *string) *string {
	normalized := normalizeOptionalText(value)
	if normalized == nil {
		return nil
	}
	replaced := normalizeDelimitedList(*normalized, func(r rune) bool {
		return r == ',' || r == '，' || r == '#' || r == '＃' || unicode.IsSpace(r)
	})
	return normalizeOptionalText(&replaced)
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

func normalizeOptionalText(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
