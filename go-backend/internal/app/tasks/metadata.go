package tasks

import "strings"

func NormalizeMetadata(input MetadataInput) Metadata {
	return Metadata{
		Author:           normalizeOptionalText(input.Author),
		SeriesName:       normalizeOptionalText(input.SeriesName),
		ComicName:        normalizeOptionalText(input.ComicName),
		Summary:          normalizeOptionalText(input.Summary),
		TagsRaw:          normalizeOptionalText(input.TagsRaw),
		TagsNormalized:   normalizeOptionalCSV(input.TagsRaw),
		GenresRaw:        normalizeOptionalText(input.GenresRaw),
		GenresNormalized: normalizeOptionalCSV(input.GenresRaw),
	}
}

func normalizeOptionalCSV(value *string) *string {
	normalized := normalizeOptionalText(value)
	if normalized == nil {
		return nil
	}
	replaced := strings.ReplaceAll(*normalized, "，", ",")
	return normalizeOptionalText(&replaced)
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
