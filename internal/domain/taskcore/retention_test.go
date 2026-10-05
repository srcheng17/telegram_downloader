package taskcore

import (
	"strings"
	"testing"
)

func TestRetentionManifestPathsHashesAndSourceIdentity(t *testing.T) {
	valid := RetentionManifest{Version: 1, SourceRetained: true, Files: []RetainedFile{{Name: "source-archive.bin", Kind: "source_archive", Bytes: 12, SHA256: strings.Repeat("a", 64)}}}
	if err := ValidateRetentionManifest(valid); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*RetentionManifest){func(m *RetentionManifest) { m.Version = 2 }, func(m *RetentionManifest) { m.SourceRetained = false }, func(m *RetentionManifest) { m.Files[0].Name = "../escape.bin" }, func(m *RetentionManifest) { m.Files[0].SHA256 = "bad" }, func(m *RetentionManifest) { m.Files[0].Bytes = -1 }, func(m *RetentionManifest) { m.Files[0].Kind = "secret" }} {
		copy := valid
		copy.Files = append([]RetainedFile(nil), valid.Files...)
		mutate(&copy)
		if ValidateRetentionManifest(copy) == nil {
			t.Fatal("invalid retention manifest accepted")
		}
	}
}
