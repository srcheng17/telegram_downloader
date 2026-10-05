package komgaedit

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ryancheng/telegram-downloader/internal/comicinfo"
	"github.com/ryancheng/telegram-downloader/internal/infra/komga"
)

var authorElements = map[string]string{
	"Writer": "writer", "Penciller": "penciller", "Inker": "inker", "Colorist": "colorist",
	"Letterer": "letterer", "CoverArtist": "cover_artist", "Editor": "editor", "Translator": "translator",
}

func projectionApplicable(changes []FieldChange) bool {
	for _, change := range changes {
		if bookProjectionKey(change.Key) {
			return true
		}
	}
	return false
}

func bookProjectionKey(key string) bool {
	switch key {
	case "title", "number", "summary", "publication_date", "tags", "identifiers", "web":
		return true
	default:
		return strings.HasPrefix(key, "creators.")
	}
}

func projectionMatches(book komga.Book, raw []byte, changes []FieldChange) bool {
	parsed, err := comicinfo.Parse(raw)
	if err != nil {
		return false
	}
	values := parsed.Values
	authorsChecked := false
	for _, change := range changes {
		switch change.Key {
		case "title":
			if strings.TrimSpace(book.Metadata.Title) != strings.TrimSpace(values["Title"]) {
				return false
			}
		case "number":
			if strings.TrimSpace(book.Metadata.Number) != strings.TrimSpace(values["Number"]) {
				return false
			}
		case "summary":
			if strings.TrimSpace(book.Metadata.Summary) != strings.TrimSpace(values["Summary"]) {
				return false
			}
		case "publication_date":
			expected, ok := releaseDate(values)
			if !ok || expected == "" && book.Metadata.ReleaseDate != nil || expected != "" && (book.Metadata.ReleaseDate == nil || *book.Metadata.ReleaseDate != expected) {
				return false
			}
		case "tags":
			if !slices.Equal(normalizedTags(book.Metadata.Tags), normalizedTags(splitComma(values["Tags"]))) {
				return false
			}
		case "identifiers":
			if normalizeIdentifier(book.Metadata.ISBN) != normalizeIdentifier(values["GTIN"]) {
				return false
			}
		case "web":
			if !linksMatch(book.Metadata.Links, strings.TrimSpace(values["Web"])) {
				return false
			}
		default:
			if strings.HasPrefix(change.Key, "creators.") && !authorsChecked {
				authorsChecked = true
				if !authorsMatch(book.Metadata.Authors, values) {
					return false
				}
			}
		}
	}
	return true
}

func observableNonClearChanged(before, after komga.Book, changes []FieldChange) bool {
	if before.Metadata.LastModified == "" || after.Metadata.LastModified == "" || before.Metadata.LastModified == after.Metadata.LastModified {
		return false
	}
	for _, change := range changes {
		if change.State != "value" || !bookProjectionKey(change.Key) {
			continue
		}
		switch change.Key {
		case "title":
			if before.Metadata.Title != after.Metadata.Title {
				return true
			}
		case "number":
			if before.Metadata.Number != after.Metadata.Number {
				return true
			}
		case "summary":
			if before.Metadata.Summary != after.Metadata.Summary {
				return true
			}
		case "publication_date":
			if !sameOptionalDate(before.Metadata.ReleaseDate, after.Metadata.ReleaseDate) {
				return true
			}
		case "tags":
			if !slices.Equal(normalizedTags(before.Metadata.Tags), normalizedTags(after.Metadata.Tags)) {
				return true
			}
		case "identifiers":
			if normalizeIdentifier(before.Metadata.ISBN) != normalizeIdentifier(after.Metadata.ISBN) {
				return true
			}
		case "web":
			if !slices.Equal(linkURLs(before.Metadata.Links), linkURLs(after.Metadata.Links)) {
				return true
			}
		default:
			if strings.HasPrefix(change.Key, "creators.") && !slices.Equal(authorPairs(before.Metadata.Authors), authorPairs(after.Metadata.Authors)) {
				return true
			}
		}
	}
	return false
}

func clearProjectionFields(raw []byte, changes []FieldChange) []komga.ClearField {
	parsed, err := comicinfo.Parse(raw)
	if err != nil {
		return nil
	}
	values := parsed.Values
	result := []komga.ClearField{}
	authorClearRequested := false
	for _, change := range changes {
		if change.State != "cleared" {
			continue
		}
		switch change.Key {
		case "summary":
			if values["Summary"] == "" {
				result = append(result, komga.ClearSummary)
			}
		case "publication_date":
			if values["Year"] == "" {
				result = append(result, komga.ClearReleaseDate)
			}
		case "tags":
			if values["Tags"] == "" {
				result = append(result, komga.ClearTags)
			}
		case "identifiers":
			if values["GTIN"] == "" {
				result = append(result, komga.ClearISBN)
			}
		case "web":
			if values["Web"] == "" {
				result = append(result, komga.ClearLinks)
			}
		default:
			if strings.HasPrefix(change.Key, "creators.") {
				authorClearRequested = true
			}
		}
	}
	if authorClearRequested && len(expectedAuthorPairs(values)) == 0 {
		result = append(result, komga.ClearAuthors)
	}
	slices.Sort(result)
	return slices.Compact(result)
}

// pendingClears refuses to overwrite a value changed by another Komga editor
// between the pre-analyze read and our PATCH-clear. Komga has no conditional
// PATCH, so a narrow check is the strongest available guard here.
func pendingClears(before, current komga.Book, requested []komga.ClearField) ([]komga.ClearField, bool) {
	pending := make([]komga.ClearField, 0, len(requested))
	for _, field := range requested {
		if clearFieldLocked(current.Metadata, field) {
			return nil, false
		}
		if clearFieldEmpty(current.Metadata, field) {
			continue
		}
		if !clearFieldEqual(before.Metadata, current.Metadata, field) {
			return nil, false
		}
		pending = append(pending, field)
	}
	return pending, true
}

func clearFieldLocked(metadata komga.BookMetadata, field komga.ClearField) bool {
	switch field {
	case komga.ClearSummary:
		return metadata.SummaryLock
	case komga.ClearReleaseDate:
		return metadata.ReleaseDateLock
	case komga.ClearAuthors:
		return metadata.AuthorsLock
	case komga.ClearTags:
		return metadata.TagsLock
	case komga.ClearISBN:
		return metadata.ISBNLock
	case komga.ClearLinks:
		return metadata.LinksLock
	}
	return true
}

func clearFieldEmpty(metadata komga.BookMetadata, field komga.ClearField) bool {
	switch field {
	case komga.ClearSummary:
		return metadata.Summary == ""
	case komga.ClearReleaseDate:
		return metadata.ReleaseDate == nil
	case komga.ClearAuthors:
		return len(metadata.Authors) == 0
	case komga.ClearTags:
		return len(metadata.Tags) == 0
	case komga.ClearISBN:
		return metadata.ISBN == ""
	case komga.ClearLinks:
		return len(metadata.Links) == 0
	}
	return false
}

func clearFieldEqual(a, b komga.BookMetadata, field komga.ClearField) bool {
	switch field {
	case komga.ClearSummary:
		return a.Summary == b.Summary
	case komga.ClearReleaseDate:
		return sameOptionalDate(a.ReleaseDate, b.ReleaseDate)
	case komga.ClearAuthors:
		return slices.Equal(authorPairs(a.Authors), authorPairs(b.Authors))
	case komga.ClearTags:
		return slices.Equal(normalizedTags(a.Tags), normalizedTags(b.Tags))
	case komga.ClearISBN:
		return normalizeIdentifier(a.ISBN) == normalizeIdentifier(b.ISBN)
	case komga.ClearLinks:
		return slices.Equal(linkURLs(a.Links), linkURLs(b.Links))
	}
	return false
}

func releaseDate(values map[string]string) (string, bool) {
	if values["Year"] == "" {
		return "", values["Month"] == "" && values["Day"] == ""
	}
	year, err := strconv.Atoi(values["Year"])
	if err != nil || year < 1 || year > 9999 {
		return "", false
	}
	month, day := 1, 1
	if values["Month"] != "" {
		month, err = strconv.Atoi(values["Month"])
		if err != nil || month < 1 || month > 12 {
			return "", false
		}
	}
	if values["Day"] != "" {
		day, err = strconv.Atoi(values["Day"])
		if err != nil || day < 1 || day > 31 {
			return "", false
		}
	}
	return fmt.Sprintf("%04d-%02d-%02d", year, month, day), true
}

func splitComma(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func normalizedTags(tags []string) []string {
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag = strings.ToLower(strings.TrimSpace(tag)); tag != "" {
			result = append(result, tag)
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}

func normalizeIdentifier(value string) string {
	return strings.ToUpper(strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, value))
}

func linksMatch(links []komga.WebLink, expected string) bool {
	if expected == "" {
		return len(links) == 0
	}
	return len(links) == 1 && strings.TrimSpace(links[0].URL) == expected
}

func linkURLs(links []komga.WebLink) []string {
	result := make([]string, 0, len(links))
	for _, link := range links {
		result = append(result, strings.TrimSpace(link.URL))
	}
	slices.Sort(result)
	return result
}

func sameOptionalDate(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func expectedAuthorPairs(values map[string]string) []string {
	result := []string{}
	for element, role := range authorElements {
		for _, name := range splitComma(values[element]) {
			if name != "" {
				result = append(result, strings.ToLower(name)+"|"+role)
			}
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}

func authorPairs(authors []komga.Author) []string {
	result := make([]string, 0, len(authors))
	for _, author := range authors {
		if name := strings.ToLower(strings.TrimSpace(author.Name)); name != "" {
			result = append(result, name+"|"+strings.ToLower(strings.TrimSpace(author.Role)))
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}

func authorsMatch(authors []komga.Author, values map[string]string) bool {
	return slices.Equal(authorPairs(authors), expectedAuthorPairs(values))
}
