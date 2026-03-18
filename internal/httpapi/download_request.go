package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode"

	apptasks "github.com/ryancheng/telegram-downloader/internal/app/tasks"
)

type downloadMetadata struct {
	author           *string
	seriesName       *string
	comicName        *string
	summary          *string
	tagsRaw          *string
	tagsNormalized   *string
	genresRaw        *string
	genresNormalized *string
}

func extractDownloadRequest(r *http.Request) (string, bool, downloadMetadata, error) {
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return "", false, downloadMetadata{}, err
		}
		rawURL, forceDownload, metadata := extractFromMap(payload)
		return rawURL, forceDownload, metadata, nil
	}

	if err := r.ParseForm(); err != nil {
		return "", false, downloadMetadata{}, err
	}
	payload := map[string]any{}
	for key, values := range r.PostForm {
		if len(values) == 0 {
			continue
		}
		payload[key] = values[0]
	}
	rawURL, forceDownload, metadata := extractFromMap(payload)
	return rawURL, forceDownload, metadata, nil
}

func extractFromMap(payload map[string]any) (string, bool, downloadMetadata) {
	rawURL := normalizePayloadText(payload["url"], maxURLLength)
	forceDownload := coerceBool(payload["force"])
	rawAuthor := normalizePayloadText(payload["author"], maxMetadataFieldLength)
	tagsRaw := normalizePayloadText(payload["tags"], maxMetadataFieldLength)
	genresRaw := normalizePayloadText(payload["genres"], maxMetadataFieldLength)
	normalized := apptasks.NormalizeMetadata(apptasks.MetadataInput{
		Author:     optionalString(rawAuthor),
		SeriesName: optionalString(normalizePayloadText(payload["series_name"], maxMetadataFieldLength)),
		ComicName:  optionalString(normalizePayloadText(payload["comic_name"], maxMetadataFieldLength)),
		Summary:    optionalString(normalizePayloadText(payload["summary"], maxMetadataFieldLength)),
		TagsRaw:    optionalString(tagsRaw),
		GenresRaw:  optionalString(genresRaw),
	})

	metadata := downloadMetadata{
		author:           normalized.Author,
		seriesName:       normalized.SeriesName,
		comicName:        normalized.ComicName,
		summary:          normalized.Summary,
		tagsRaw:          normalized.TagsRaw,
		tagsNormalized:   normalized.TagsNormalized,
		genresRaw:        normalized.GenresRaw,
		genresNormalized: normalized.GenresNormalized,
	}
	return rawURL, forceDownload, metadata
}

func normalizeAuthorList(raw string) string {
	return normalizeDelimitedList(raw, func(r rune) bool {
		return r == ',' || r == '，'
	})
}

func normalizeTagLikeList(raw string) string {
	return normalizeDelimitedList(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == '#' || unicode.IsSpace(r)
	})
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

func coerceBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		normalized := strings.ToLower(strings.TrimSpace(typed))
		return normalized == "1" || normalized == "true" || normalized == "t" || normalized == "yes" || normalized == "y" || normalized == "on"
	case json.Number:
		parsed, err := typed.Int64()
		return err == nil && parsed != 0
	case float64:
		return typed != 0
	case float32:
		return typed != 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	default:
		return false
	}
}

func normalizePayloadText(value any, maxLen int) string {
	var text string
	switch typed := value.(type) {
	case string:
		text = typed
	case json.Number:
		text = typed.String()
	case nil:
		text = ""
	default:
		text = fmt.Sprintf("%v", typed)
	}
	text = strings.TrimSpace(text)
	if maxLen > 0 && len(text) > maxLen {
		text = text[:maxLen]
	}
	return text
}

func optionalString(value string) *string {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return nil
	}
	copied := normalized
	return &copied
}
