package comicinfo

import (
	"errors"
	"strconv"
	"strings"
)

var ErrUnsafeExisting = errors.New("existing ComicInfo cannot be preserved by this editor")

// EditExisting applies explicit element changes to a conservative, lossless
// subset of ComicInfo. A nil value clears an element; absent keys are retained.
// The ZIP writer separately checks that only ChangedElements differ.
type ElementChange struct {
	Name  string
	Value *string
}

type EditResult struct {
	XML             []byte
	ChangedElements []string
	FileChanged     bool
}

func EditExisting(raw []byte, changes []ElementChange, actualPageCount int, correctPageCount bool) (EditResult, error) {
	result := EditResult{}
	if actualPageCount < 1 || actualPageCount > 300 {
		return result, ErrUnsafeExisting
	}
	parsed, err := Parse(raw)
	if err != nil || len(parsed.Warnings) > 0 {
		return result, ErrUnsafeExisting
	}
	values := make(map[string]string, len(parsed.Values))
	for key, value := range parsed.Values {
		values[key] = value
	}
	seen := make(map[string]bool, len(changes))
	for _, change := range changes {
		spec, ok := specFor(change.Name)
		if !ok || spec.Kind == "pages" || change.Name == "PageCount" || seen[change.Name] {
			return result, ErrUnsafeExisting
		}
		seen[change.Name] = true
		if change.Value == nil {
			delete(values, change.Name)
			continue
		}
		if strings.TrimSpace(*change.Value) == "" || !validScalar(spec, *change.Value) {
			return result, ErrUnsafeExisting
		}
		values[change.Name] = *change.Value
	}
	if old, exists := values["PageCount"]; exists {
		count, err := strconv.Atoi(old)
		if err != nil || count != actualPageCount {
			if !correctPageCount {
				return result, ErrUnsafeExisting
			}
			values["PageCount"] = strconv.Itoa(actualPageCount)
			seen["PageCount"] = true
		}
	}
	result.XML, err = serialize(values, parsed.Pages)
	if err != nil || len(result.XML) > MaxXMLBytes {
		return EditResult{}, ErrUnsafeExisting
	}
	verified, err := Parse(result.XML)
	if err != nil || len(verified.Warnings) > 0 || len(verified.Pages) != len(parsed.Pages) {
		return EditResult{}, ErrUnsafeExisting
	}
	for _, spec := range elements {
		if seen[spec.Name] {
			result.ChangedElements = append(result.ChangedElements, spec.Name)
		}
	}
	// Canonical serialization can change XML bytes even when values match. The
	// caller decides whether this is a user-visible no-op before rewriting.
	result.FileChanged = len(result.ChangedElements) > 0 || len(raw) == 0
	return result, nil
}
