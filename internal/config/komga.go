package config

import (
	"encoding/json"
	"errors"
	"io"
	"path"
	"path/filepath"
	"strings"
)

// KomgaLibraryMapping is deployment-owned. Browser requests may identify a
// book, but may never choose either filesystem root.
type KomgaLibraryMapping struct {
	LibraryID string `json:"library_id"`
	KomgaRoot string `json:"komga_root"`
	LocalRoot string `json:"local_root"`
}

func parseKomgaLibraryMappings(raw string) ([]KomgaLibraryMapping, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	if len(raw) > 16384 {
		return nil, errors.New("KOMGA_LIBRARY_MAPPINGS is too large")
	}
	var result []KomgaLibraryMapping
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return nil, errors.New("KOMGA_LIBRARY_MAPPINGS must be a JSON array")
	}
	if decoder.Decode(new(any)) != io.EOF || len(result) > 16 {
		return nil, errors.New("KOMGA_LIBRARY_MAPPINGS has invalid length or trailing data")
	}
	seen := make(map[string]bool, len(result))
	for i := range result {
		m := &result[i]
		if m.LibraryID == "" || len(m.LibraryID) > 128 || strings.TrimSpace(m.LibraryID) != m.LibraryID || strings.ContainsAny(m.LibraryID, "/\\\x00\r\n") || seen[m.LibraryID] {
			return nil, errors.New("KOMGA_LIBRARY_MAPPINGS has an invalid or duplicate library_id")
		}
		if !path.IsAbs(m.KomgaRoot) || path.Clean(m.KomgaRoot) != m.KomgaRoot || m.KomgaRoot == "/" || !filepath.IsAbs(m.LocalRoot) || filepath.Clean(m.LocalRoot) != m.LocalRoot || m.LocalRoot == string(filepath.Separator) {
			return nil, errors.New("KOMGA_LIBRARY_MAPPINGS requires clean absolute non-root paths")
		}
		seen[m.LibraryID] = true
	}
	return result, nil
}

func parseKomgaReadOnlyLibraries(raw string, mappings []KomgaLibraryMapping) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	if len(raw) > 8192 {
		return nil, errors.New("KOMGA_READONLY_LIBRARY_IDS is too large")
	}
	var ids []string
	decoder := json.NewDecoder(strings.NewReader(raw))
	if err := decoder.Decode(&ids); err != nil || decoder.Decode(new(any)) != io.EOF || len(ids) > 64 {
		return nil, errors.New("KOMGA_READONLY_LIBRARY_IDS must be a JSON array")
	}
	seen := make(map[string]bool, len(ids)+len(mappings))
	for _, mapping := range mappings {
		seen[mapping.LibraryID] = true
	}
	for _, id := range ids {
		if id == "" || len(id) > 128 || strings.TrimSpace(id) != id || strings.ContainsAny(id, "/\\\x00\r\n") || seen[id] {
			return nil, errors.New("KOMGA_READONLY_LIBRARY_IDS has an invalid or duplicate library_id")
		}
		seen[id] = true
	}
	return ids, nil
}

func validateKomgaBackupRoot(root string, mappings []KomgaLibraryMapping) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || root == string(filepath.Separator) {
		return errors.New("KOMGA_EDIT_BACKUP_ROOT must be a clean absolute non-root path")
	}
	for _, mapping := range mappings {
		rel, err := filepath.Rel(mapping.LocalRoot, root)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errors.New("KOMGA_EDIT_BACKUP_ROOT must be outside mapped Komga libraries")
		}
	}
	return nil
}
