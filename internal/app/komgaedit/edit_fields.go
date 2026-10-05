package komgaedit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/ryancheng/telegram-downloader/internal/archive/cbzedit"
	"github.com/ryancheng/telegram-downloader/internal/comicinfo"
	"github.com/ryancheng/telegram-downloader/internal/domain/metadata"
	"github.com/ryancheng/telegram-downloader/internal/infra/komga"
)

var directElements = map[string]string{
	"title": "Title", "series": "Series", "number": "Number", "count": "Count", "volume": "Volume",
	"summary": "Summary", "publisher": "Publisher", "imprint": "Imprint", "language": "LanguageISO",
	"format": "Format", "web": "Web", "age_rating": "AgeRating", "tags": "Tags", "genres": "Genre",
	"creators.writer": "Writer", "creators.penciller": "Penciller", "creators.inker": "Inker",
	"creators.colorist": "Colorist", "creators.letterer": "Letterer", "creators.cover_artist": "CoverArtist",
	"creators.editor": "Editor", "creators.translator": "Translator",
}

func clearAllowed(key string, library komga.Library) bool {
	switch key {
	case "summary", "publication_date", "tags", "web":
		return true
	case "identifiers":
		return !library.ImportBarcodeIsbn
	default:
		return strings.HasPrefix(key, "creators.")
	}
}

func mappedField(key string) bool {
	if _, ok := directElements[key]; ok {
		return true
	}
	return slices.Contains([]string{"publication_date", "identifiers", "manga", "reading_direction"}, key)
}

func lockedField(key string, book komga.Book) bool {
	switch key {
	case "title":
		return book.Metadata.TitleLock
	case "number":
		return book.Metadata.NumberLock || book.Metadata.NumberSortLock
	case "summary":
		return book.Metadata.SummaryLock
	case "publication_date":
		return book.Metadata.ReleaseDateLock
	case "tags":
		return book.Metadata.TagsLock
	case "identifiers":
		return book.Metadata.ISBNLock
	case "web":
		return book.Metadata.LinksLock
	default:
		return strings.HasPrefix(key, "creators.") && book.Metadata.AuthorsLock
	}
}

func editFields(reg metadata.Registry, book komga.Book, library komga.Library, editable bool) []EditField {
	keys := make([]string, 0, len(reg.Definitions))
	for key := range reg.Definitions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]EditField, 0, len(keys))
	for _, key := range keys {
		def := reg.Definitions[key]
		field := EditField{Key: key, Label: def.Label, Type: def.Type}
		switch {
		case !editable:
			field.Reason = "book_read_only"
		case !def.Enabled || !def.Editable:
			field.Reason = "field_disabled"
		case key == "page_count":
			field.Reason = "derived_page_count"
		case !mappedField(key) || def.ExportStatus != "mapped":
			field.Reason = "internal_only"
		case !bookProjectionKey(key):
			field.Reason = "series_scope"
		case lockedField(key, book):
			field.Reason = "komga_field_locked"
		default:
			field.CanSet = true
			field.CanClear = clearAllowed(key, library)
			if key == "identifiers" && library.ImportBarcodeIsbn {
				field.Reason = "barcode_isbn_clear_disabled"
			}
		}
		result = append(result, field)
	}
	return result
}

type compiledEdit struct {
	newXML           []byte
	changedElements  []string
	fileChanged      bool
	correctPageCount bool
	diffs            []FieldDiff
	warnings         []metadata.Warning
}

func documentFromArchive(raw []byte, count int, registry metadata.Registry) (metadata.Document, []metadata.Warning, error) {
	mapping := make([]int, count)
	for i := range mapping {
		mapping[i] = i
	}
	merged, err := comicinfo.Merge(raw, metadata.EmptyDocument(registry), registry, mapping, count)
	if err != nil {
		return metadata.Document{}, nil, err
	}
	return merged.EffectiveDocument, merged.Warnings, nil
}

func compileEdit(inspection cbzedit.Inspection, baseline metadata.Document, registry metadata.Registry, library komga.Library, book komga.Book, changes []FieldChange, correctPageCount bool) (compiledEdit, error) {
	result := compiledEdit{diffs: []FieldDiff{}, warnings: []metadata.Warning{}}
	result.correctPageCount = correctPageCount
	if len(changes) > 32 {
		return result, ErrInvalid
	}
	current := make(map[string]metadata.FieldState, len(baseline.Fields)+len(changes))
	for key, field := range baseline.Fields {
		current[key] = field
	}
	seen := map[string]bool{}
	componentChanges := []comicinfo.ElementChange{}
	for _, change := range changes {
		if seen[change.Key] || !mappedField(change.Key) || !bookProjectionKey(change.Key) || change.Key == "page_count" {
			return result, ErrInvalid
		}
		seen[change.Key] = true
		def, ok := registry.Definitions[change.Key]
		if !ok || !def.Enabled || !def.Editable || def.ExportStatus != "mapped" || lockedField(change.Key, book) {
			return result, ErrInvalid
		}
		if change.State != "value" && change.State != "cleared" || change.State == "cleared" && (!clearAllowed(change.Key, library) || len(change.Value) != 0) || change.State == "value" && len(change.Value) == 0 {
			return result, ErrInvalid
		}
		probe := metadata.EmptyDocument(registry)
		probe.Fields[change.Key] = metadata.FieldState{State: change.State, Value: change.Value, Provenance: []metadata.Provenance{}}
		probe.DefinitionSnapshot[change.Key] = def
		validated, warnings, err := metadata.Validate(probe, registry)
		if err != nil || len(warnings) > 1 || len(warnings) == 1 && warnings[0].Code != "identifier_export" {
			return result, ErrInvalid
		}
		value := validated.Fields[change.Key]
		before, beforeExists := current[change.Key]
		if !beforeExists || before.State != value.State || !bytes.Equal(before.Value, value.Value) {
			diff := FieldDiff{Key: change.Key, Action: change.State, After: value.Value}
			if beforeExists && before.State == "value" {
				diff.Before = before.Value
			}
			result.diffs = append(result.diffs, diff)
		}
		if change.State == "cleared" {
			delete(current, change.Key)
		} else {
			current[change.Key] = value
		}
		if element, direct := directElements[change.Key]; direct {
			text, err := elementValue(def, value)
			if err != nil {
				return result, err
			}
			componentChanges = append(componentChanges, comicinfo.ElementChange{Name: element, Value: text})
		}
	}
	if seen["publication_date"] {
		var date metadata.PublicationDate
		if state, ok := current["publication_date"]; ok {
			if err := json.Unmarshal(state.Value, &date); err != nil {
				return result, ErrInvalid
			}
		}
		for _, item := range []struct {
			name string
			part *int
		}{{"Year", &date.Year}, {"Month", date.Month}, {"Day", date.Day}} {
			var value *string
			if item.part != nil && *item.part > 0 {
				s := strconv.Itoa(*item.part)
				value = &s
			}
			componentChanges = append(componentChanges, comicinfo.ElementChange{Name: item.name, Value: value})
		}
	}
	if seen["identifiers"] {
		var value *string
		if state, ok := current["identifiers"]; ok {
			var ids []metadata.Identifier
			if err := json.Unmarshal(state.Value, &ids); err != nil || len(ids) != 1 || !validBookIdentifier(ids[0]) {
				return result, ErrInvalid
			}
			normalized := strings.ReplaceAll(strings.ReplaceAll(ids[0].Value, "-", ""), " ", "")
			value = &normalized
		}
		componentChanges = append(componentChanges, comicinfo.ElementChange{Name: "GTIN", Value: value})
	}
	if seen["manga"] || seen["reading_direction"] {
		var value *string
		manga, direction := currentString(current, "manga"), currentString(current, "reading_direction")
		if manga == "unknown" || direction == "unknown" {
			return result, ErrInvalid
		}
		switch {
		case manga == "yes" && direction == "rtl":
			s := "YesAndRightToLeft"
			value = &s
		case manga == "yes":
			s := "Yes"
			value = &s
		case manga == "no" && direction != "rtl":
			s := "No"
			value = &s
		case manga == "" && direction == "":
		default:
			return result, ErrInvalid
		}
		componentChanges = append(componentChanges, comicinfo.ElementChange{Name: "Manga", Value: value})
	}
	if len(componentChanges) == 0 && !correctPageCount {
		return result, nil
	}
	updated, err := comicinfo.EditExisting(inspection.ComicInfo, componentChanges, inspection.PageCount, correctPageCount)
	if err != nil {
		return result, fmt.Errorf("edit ComicInfo: %w", err)
	}
	oldParsed, err := comicinfo.Parse(inspection.ComicInfo)
	if err != nil {
		return result, ErrUnsafe
	}
	newParsed, err := comicinfo.Parse(updated.XML)
	if err != nil {
		return result, ErrUnsafe
	}
	for _, change := range componentChanges {
		if oldParsed.Values[change.Name] != newParsed.Values[change.Name] || hasKey(oldParsed.Values, change.Name) != hasKey(newParsed.Values, change.Name) {
			result.changedElements = append(result.changedElements, change.Name)
		}
	}
	if oldParsed.Values["PageCount"] != newParsed.Values["PageCount"] {
		if !correctPageCount {
			return result, ErrUnsafe
		}
		result.changedElements = append(result.changedElements, "PageCount")
		oldCount, _ := strconv.Atoi(oldParsed.Values["PageCount"])
		before, _ := json.Marshal(oldCount)
		after, _ := json.Marshal(inspection.PageCount)
		result.diffs = append(result.diffs, FieldDiff{Key: "page_count", Action: "corrected", Before: before, After: after})
	} else if correctPageCount {
		return result, ErrInvalid
	}
	result.fileChanged = len(result.changedElements) > 0
	if result.fileChanged {
		result.newXML = updated.XML
	}
	return result, nil
}

func hasKey(values map[string]string, key string) bool { _, ok := values[key]; return ok }

func currentString(fields map[string]metadata.FieldState, key string) string {
	field, ok := fields[key]
	if !ok || field.State != "value" {
		return ""
	}
	var value string
	_ = json.Unmarshal(field.Value, &value)
	return value
}

func elementValue(def metadata.FieldDefinition, field metadata.FieldState) (*string, error) {
	if field.State == "cleared" {
		return nil, nil
	}
	switch def.Type {
	case "string":
		var value string
		if json.Unmarshal(field.Value, &value) != nil || strings.TrimSpace(value) == "" || value == "unknown" || value == "Unknown" {
			return nil, ErrInvalid
		}
		return &value, nil
	case "string[]":
		var values []string
		if json.Unmarshal(field.Value, &values) != nil || len(values) == 0 {
			return nil, ErrInvalid
		}
		for _, value := range values {
			if strings.Contains(value, ",") {
				return nil, ErrInvalid
			}
		}
		value := strings.Join(values, ", ")
		return &value, nil
	case "integer":
		var value int64
		if json.Unmarshal(field.Value, &value) != nil || value < 1 {
			return nil, ErrInvalid
		}
		text := strconv.FormatInt(value, 10)
		return &text, nil
	default:
		return nil, errors.New("unsupported ComicInfo field type")
	}
}

func validBookIdentifier(id metadata.Identifier) bool {
	value := strings.ReplaceAll(strings.ReplaceAll(id.Value, "-", ""), " ", "")
	if len(value) != 10 && len(value) != 13 {
		return false
	}
	if id.Scheme != "isbn" && id.Scheme != "isbn10" && id.Scheme != "isbn13" && id.Scheme != "gtin" {
		return false
	}
	if len(value) == 10 {
		if id.Scheme == "isbn13" || id.Scheme == "gtin" {
			return false
		}
		sum := 0
		for i, r := range value {
			digit := int(r - '0')
			if i == 9 && r == 'X' {
				digit = 10
			} else if r < '0' || r > '9' {
				return false
			}
			sum += (10 - i) * digit
		}
		return sum%11 == 0
	}
	if id.Scheme == "isbn10" || id.Scheme == "isbn" && !strings.HasPrefix(value, "978") && !strings.HasPrefix(value, "979") {
		return false
	}
	sum := 0
	for i, r := range value {
		if r < '0' || r > '9' {
			return false
		}
		weight := 1
		if i%2 == 1 {
			weight = 3
		}
		sum += int(r-'0') * weight
	}
	return sum%10 == 0
}
