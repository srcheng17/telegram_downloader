package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"

	apptasks "github.com/ryancheng/telegram-downloader/internal/app/tasks"
	metadataDomain "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

type downloadMetadata struct {
	document         *metadataDomain.Document
	explicitLegacy   map[string]string
	author           *string
	seriesName       *string
	seriesNumber     *string
	comicName        *string
	summary          *string
	tagsRaw          *string
	tagsNormalized   *string
	genresRaw        *string
	genresNormalized *string
}

func extractDownloadRequest(r *http.Request) (string, bool, downloadMetadata, error) {
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		payload, err := decodeTaskJSON(r.Body)
		if err != nil {
			return "", false, downloadMetadata{}, err
		}
		rawURL, forceDownload, metadata := extractFromMap(payload)
		if err := extractDocumentPayload(payload, &metadata); err != nil {
			return "", false, downloadMetadata{}, err
		}
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
	if err := extractDocumentPayload(payload, &metadata); err != nil {
		return "", false, downloadMetadata{}, err
	}
	return rawURL, forceDownload, metadata, nil
}

// Validate the original bytes before decoding an envelope to a map. Otherwise
// duplicate keys inside metadata_document would already be lost on re-encoding.
func decodeTaskJSON(body io.Reader) (map[string]any, error) {
	const maxEnvelopeBytes = 1 << 20
	data, err := io.ReadAll(io.LimitReader(body, maxEnvelopeBytes+1))
	if err != nil || len(data) > maxEnvelopeBytes {
		return nil, fmt.Errorf("invalid task JSON payload")
	}
	var payload map[string]any
	if err := metadataDomain.DecodeJSON(data, &payload); err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, fmt.Errorf("task JSON object required")
	}
	return payload, nil
}

func extractFromMap(payload map[string]any) (string, bool, downloadMetadata) {
	rawURL := normalizePayloadText(payload["url"], maxURLLength)
	forceDownload := coerceBool(payload["force"])
	rawAuthor := normalizePayloadText(payload["author"], maxMetadataFieldLength)
	tagsRaw := normalizePayloadText(payload["tags"], maxMetadataFieldLength)
	genresRaw := normalizePayloadText(payload["genres"], maxMetadataFieldLength)
	normalized := apptasks.NormalizeMetadata(apptasks.MetadataInput{
		Author:       optionalString(rawAuthor),
		SeriesName:   optionalString(normalizePayloadText(payload["series_name"], maxMetadataFieldLength)),
		SeriesNumber: optionalString(normalizePayloadText(payload["series_number"], maxMetadataFieldLength)),
		ComicName:    optionalString(normalizePayloadText(payload["comic_name"], maxMetadataFieldLength)),
		Summary:      optionalString(normalizePayloadText(payload["summary"], maxMetadataFieldLength)),
		TagsRaw:      optionalString(tagsRaw),
		GenresRaw:    optionalString(genresRaw),
	})

	metadata := downloadMetadata{
		author:           normalized.Author,
		seriesName:       normalized.SeriesName,
		seriesNumber:     normalized.SeriesNumber,
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

func extractDocumentPayload(payload map[string]any, out *downloadMetadata) error {
	raw, exists := payload["metadata_document"]
	if !exists {
		return nil
	}
	var encoded []byte
	var err error
	if text, ok := raw.(string); ok {
		encoded = []byte(text)
	} else {
		encoded, err = json.Marshal(raw)
	}
	if err != nil || len(encoded) > metadataDomain.MaxDocumentBytes || bytes.Equal(bytes.TrimSpace(encoded), []byte("null")) {
		return fmt.Errorf("invalid metadata document")
	}
	doc := &metadataDomain.Document{}
	if err := metadataDomain.DecodeJSON(encoded, doc); err != nil {
		return fmt.Errorf("invalid metadata document")
	}
	out.document = doc
	out.explicitLegacy = map[string]string{}
	for _, key := range []string{"author", "comic_name", "series_name", "series_number", "summary", "tags", "genres"} {
		if value, ok := payload[key]; ok {
			out.explicitLegacy[key] = normalizePayloadText(value, 0)
		}
	}
	return nil
}
