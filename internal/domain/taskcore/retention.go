package taskcore

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

type RetainedFile struct {
	Name         string `json:"name"`
	OriginalName string `json:"original_name,omitempty"`
	Kind         string `json:"kind"`
	Bytes        int64  `json:"bytes"`
	SHA256       string `json:"sha256"`
}
type RetentionManifest struct {
	Version        int            `json:"version"`
	Files          []RetainedFile `json:"files"`
	SourceRetained bool           `json:"source_retained"`
}

var retentionName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}\.(?:bin|json|xml)$`)
var retentionHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

func ValidateRetentionManifest(m RetentionManifest) error {
	if m.Version != 1 || len(m.Files) > 512 {
		return errors.New("invalid retention manifest")
	}
	seen := map[string]bool{}
	source := false
	for _, f := range m.Files {
		if !retentionName.MatchString(f.Name) || seen[f.Name] || f.Bytes < 0 || f.Bytes > 500<<20 || !retentionHash.MatchString(f.SHA256) || len(f.OriginalName) > 4096 || !utf8.ValidString(f.OriginalName) || strings.ContainsRune(f.OriginalName, '\x00') {
			return errors.New("invalid retained file")
		}
		switch f.Kind {
		case "source_archive":
			if f.Name != "source-archive.bin" || source {
				return errors.New("invalid source retention")
			}
			source = true
		case "comicinfo", "competing_metadata", "zip_comment", "custom_metadata":
		default:
			return errors.New("invalid retention kind")
		}
		seen[f.Name] = true
	}
	if source != m.SourceRetained {
		return errors.New("source retention mismatch")
	}
	return nil
}
