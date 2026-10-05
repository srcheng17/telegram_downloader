package metadata

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Legacy carries pointers to preserve omitted versus explicitly supplied input.
// It is only an adapter; Document is the authoritative stored representation.
type Legacy struct {
	Author       *string `json:"author,omitempty"`
	ComicName    *string `json:"comic_name,omitempty"`
	SeriesName   *string `json:"series_name,omitempty"`
	SeriesNumber *string `json:"series_number,omitempty"`
	Summary      *string `json:"summary,omitempty"`
	Tags         *string `json:"tags,omitempty"`
	Genres       *string `json:"genres,omitempty"`
}

func legacyValues(input Legacy) map[string]any {
	result := map[string]any{}
	for key, ptr := range map[string]*string{"title": input.ComicName, "series": input.SeriesName, "number": input.SeriesNumber, "summary": input.Summary} {
		if v := NormalizeOptionalTextPtr(ptr); v != nil {
			result[key] = *v
		}
	}
	if author := NormalizeAuthorListPtr(input.Author); author != nil {
		result["creators.writer"] = strings.Split(*author, ",")
	}
	for key, ptr := range map[string]*string{"tags": input.Tags, "genres": input.Genres} {
		if v := NormalizeTagLikeListPtr(ptr); v != nil {
			result[key] = strings.Split(*v, ",")
		}
	}
	return result
}

// FromLegacy is deterministic even for old rows without timestamps. No current
// registry setting or wall clock can reinterpret their built-in field meanings.
func FromLegacy(input Legacy) (Document, error) {
	r := StandardRegistry()
	doc := EmptyDocument(r)
	for key, value := range legacyValues(input) {
		raw, _ := json.Marshal(value)
		doc.Fields[key] = FieldState{State: "value", Value: raw, Provenance: []Provenance{{Kind: "legacy", SourceID: "legacy"}}}
		doc.DefinitionSnapshot[key] = r.Definitions[key]
	}
	result, _, err := Validate(doc, r)
	return result, err
}

func ToLegacy(doc Document) Legacy {
	var result Legacy
	for key, dest := range map[string]**string{"title": &result.ComicName, "series": &result.SeriesName, "number": &result.SeriesNumber, "summary": &result.Summary} {
		if value := stringValue(doc, key); value != "" {
			v := value
			*dest = &v
		}
	}
	for key, dest := range map[string]**string{"creators.writer": &result.Author, "tags": &result.Tags, "genres": &result.Genres} {
		if f := doc.Fields[key]; f.State == "value" {
			var values []string
			if json.Unmarshal(f.Value, &values) == nil && len(values) > 0 {
				value := strings.Join(values, ",")
				*dest = &value
			}
		}
	}
	return result
}

// CheckLegacy only compares supplied legacy keys, including supplied empty
// strings. Only the legacy input is normalized: re-splitting document arrays
// would hide conflicts such as one "Slice of Life" tag versus three legacy tags.
func CheckLegacy(doc Document, input Legacy) error {
	wanted := legacyValues(input)
	supplied := map[string]struct {
		field string
		value *string
	}{"author": {"creators.writer", input.Author}, "comic_name": {"title", input.ComicName}, "series_name": {"series", input.SeriesName}, "series_number": {"number", input.SeriesNumber}, "summary": {"summary", input.Summary}, "tags": {"tags", input.Tags}, "genres": {"genres", input.Genres}}
	for _, key := range sortedKeys(supplied) {
		pair := supplied[key]
		if pair.value == nil {
			continue
		}
		var actual any
		if field := doc.Fields[pair.field]; field.State == "value" {
			if err := json.Unmarshal(field.Value, &actual); err != nil {
				return invalid(key, "legacy_document_conflict")
			}
		}
		got, _ := json.Marshal(actual)
		want, _ := json.Marshal(wanted[pair.field])
		if !bytes.Equal(got, want) {
			return invalid(key, "legacy_document_conflict")
		}
	}
	return nil
}
