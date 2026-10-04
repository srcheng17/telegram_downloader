package metadata

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCustomRegistryVersionAndDisable(t *testing.T) {
	base := StandardRegistry()
	def := FieldDefinition{Key: "custom.user.approved", Label: "确认状态", Type: "boolean", Editable: true, Enabled: true, ExportStatus: "internal_only", Extractable: []string{"rule"}}
	reg, err := NewRegistry(base, []FieldDefinition{def})
	if err != nil {
		t.Fatal(err)
	}
	if reg.DefinitionsVersion == base.DefinitionsVersion {
		t.Fatal("version unchanged")
	}
	if err := ValidateRegistry(reg); err != nil {
		t.Fatal(err)
	}
	doc, _, err := ApplyPatch(EmptyDocument(reg), reg, 0, []Operation{{Op: "set", Key: def.Key, Value: json.RawMessage(`false`)}}, MergeContext{Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if string(doc.Fields[def.Key].Value) != "false" {
		t.Fatal("false treated as absent")
	}
	def.Enabled = false
	disabled, err := NewRegistry(reg, []FieldDefinition{def})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Validate(doc, reg); err != nil {
		t.Fatal("old definition invalidated", err)
	}
	if _, _, err := ApplyPatch(EmptyDocument(disabled), disabled, 0, []Operation{{Op: "set", Key: def.Key, Value: json.RawMessage(`true`)}}, MergeContext{Now: time.Unix(1, 0)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("disabled field editable")
	}
	if _, err := NewRegistry(reg, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("used key deleted")
	}
	def.Type = "string"
	def.MaxBytes = 100
	if _, err := NewRegistry(reg, []FieldDefinition{def}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("used type changed")
	}
	def.Type = "boolean"
	def.MaxBytes = 0
	def.ExportMapping = "FakeXML"
	if _, err := NewRegistry(base, []FieldDefinition{def}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("custom XML mapping accepted")
	}
}

func TestDocumentBudgetSourceAndSnapshotValidation(t *testing.T) {
	reg := StandardRegistry()
	now := MergeContext{Now: time.Unix(1, 0)}
	huge, _ := json.Marshal(strings.Repeat("中", MaxStringBytes/3+1))
	if _, _, err := ApplyPatch(EmptyDocument(reg), reg, 0, []Operation{{Op: "set", Key: "title", Value: huge}}, now); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("byte cap ignored")
	}
	list := make([]string, 65)
	for i := range list {
		list[i] = "item"
	}
	raw, _ := json.Marshal(list)
	if _, _, err := ApplyPatch(EmptyDocument(reg), reg, 0, []Operation{{Op: "set", Key: "aliases", Value: raw}}, now); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("list limit ignored")
	}
	doc, _, err := ApplyPatch(EmptyDocument(reg), reg, 0, []Operation{{Op: "set", Key: "title", Value: json.RawMessage(`"标题"`)}}, now)
	if err != nil {
		t.Fatal(err)
	}
	field := doc.Fields["title"]
	field.Provenance = make([]Provenance, 9)
	for i := range field.Provenance {
		field.Provenance[i] = Provenance{Kind: "legacy", SourceID: "legacy"}
	}
	doc.Fields["title"] = field
	if _, _, err := Validate(doc, reg); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("provenance limit ignored")
	}
	field.Provenance = []Provenance{{Kind: "provider", SourceID: "provider", PublicURL: "https://user:secret@example.org"}}
	doc.Fields["title"] = field
	if _, _, err := Validate(doc, reg); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("credential URL persisted")
	}
	field.Provenance = []Provenance{}
	doc.Fields["title"] = field
	doc.SchemaVersion = 2
	if _, _, err := Validate(doc, reg); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatal("future version downgraded")
	}
	if _, err := Decode([]byte(strings.Repeat(" ", MaxDocumentBytes+1)), reg); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("document budget ignored")
	}
}

func TestLegacyProjectionIsDeterministicAndMixedInputRejectsConflict(t *testing.T) {
	author, tags, title := "Ada Lovelace，李白", "A #B，C", "标题"
	input := Legacy{Author: &author, Tags: &tags, ComicName: &title}
	a, err := FromLegacy(input)
	if err != nil {
		t.Fatal(err)
	}
	b, err := FromLegacy(input)
	if err != nil {
		t.Fatal(err)
	}
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	if string(aj) != string(bj) {
		t.Fatal("legacy migration nondeterministic")
	}
	if *ToLegacy(a).Author != "Ada Lovelace,李白" || *ToLegacy(a).Tags != "A,B,C" {
		t.Fatal("legacy normalization drift")
	}
	if err := CheckLegacy(a, input); err != nil {
		t.Fatal(err)
	}
	blank := ""
	if err := CheckLegacy(a, Legacy{ComicName: &blank}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("empty legacy silently preferred")
	}
	if err := CheckLegacy(a, Legacy{}); err != nil {
		t.Fatal("omitted legacy fields conflicted")
	}
}

func TestMixedLegacyDoesNotResplitNewArrayValues(t *testing.T) {
	reg := StandardRegistry()
	doc, _, err := ApplyPatch(EmptyDocument(reg), reg, 0, []Operation{
		{Op: "set", Key: "tags", Value: json.RawMessage(`["Slice of Life"]`)},
		{Op: "set", Key: "creators.writer", Value: json.RawMessage(`["Doe, Jane"]`)},
	}, MergeContext{Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	tags, author := "Slice of Life", "Doe, Jane"
	for _, legacy := range []Legacy{{Tags: &tags}, {Author: &author}} {
		if err := CheckLegacy(doc, legacy); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("lossy legacy normalization hid a semantic document conflict")
		}
	}
}
