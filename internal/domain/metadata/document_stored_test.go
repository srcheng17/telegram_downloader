package metadata

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestDecodeStoredPreservesOriginalCustomSnapshot(t *testing.T) {
	custom := FieldDefinition{Key: "custom.user.note", Label: "原备注", Type: "string[]", MaxItems: 64, ItemMaxBytes: 1024, Enabled: true, Editable: true, Extractable: []string{"legacy"}, ExportStatus: "internal_only"}
	reg, err := NewRegistry(StandardRegistry(), []FieldDefinition{custom})
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err := ApplyPatch(EmptyDocument(reg), reg, 0, []Operation{{Op: "set", Key: "title", Value: json.RawMessage(`"旧标题"`)}, {Op: "set", Key: custom.Key, Value: json.RawMessage(`[]`)}}, MergeContext{Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	custom.Label, custom.Enabled = "已停用备注", false
	current, err := NewRegistry(reg, []FieldDefinition{custom})
	if err != nil {
		t.Fatal(err)
	}
	if current.DefinitionsVersion == reg.DefinitionsVersion {
		t.Fatal("test did not advance settings")
	}
	raw, _ := json.Marshal(doc)
	stored, err := DecodeStored(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(doc, stored) || stored.DefinitionSnapshot[custom.Key].Label != "原备注" || !stored.DefinitionSnapshot[custom.Key].Enabled {
		t.Fatal("stored document reinterpreted with current settings")
	}
}

func TestDecodeStoredRejectsUnsupportedOrCorruptDocuments(t *testing.T) {
	name := "保存标题"
	base, err := FromLegacy(Legacy{ComicName: &name})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*Document)
	}{
		{"future schema", func(d *Document) { d.SchemaVersion++ }},
		{"future registry", func(d *Document) { d.DefinitionsVersion = "standard-v2" }},
		{"unknown registry", func(d *Document) { d.DefinitionsVersion = "custom-unknown" }},
		{"invalid value type", func(d *Document) { f := d.Fields["title"]; f.Value = json.RawMessage(`7`); d.Fields["title"] = f }},
		{"changed standard type", func(d *Document) {
			f := d.DefinitionSnapshot["title"]
			f.Type = "integer"
			d.DefinitionSnapshot["title"] = f
		}},
		{"changed standard bound", func(d *Document) { f := d.DefinitionSnapshot["title"]; f.MaxBytes++; d.DefinitionSnapshot["title"] = f }},
		{"missing snapshot", func(d *Document) { delete(d.DefinitionSnapshot, "title") }},
		{"unregistered field", func(d *Document) {
			f := d.Fields["title"]
			d.Fields["arbitrary"] = f
			d.DefinitionSnapshot["arbitrary"] = FieldDefinition{Key: "arbitrary", Type: "string"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(base)
			var doc Document
			if err := DecodeJSON(raw, &doc); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&doc)
			raw, _ = json.Marshal(doc)
			if _, err := DecodeStored(raw); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("corrupt document accepted: %v", err)
			}
		})
	}
}

func TestDecodeStoredRejectsUnsafeCustomSnapshot(t *testing.T) {
	custom := FieldDefinition{Key: "custom.user.note", Label: "备注", Type: "string", MaxBytes: 100, Enabled: true, Editable: true, Extractable: []string{}, ExportStatus: "internal_only"}
	reg, err := NewRegistry(StandardRegistry(), []FieldDefinition{custom})
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err := ApplyPatch(EmptyDocument(reg), reg, 0, []Operation{{Op: "set", Key: custom.Key, Value: json.RawMessage(`"保存值"`)}}, MergeContext{Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []func(*FieldDefinition){func(d *FieldDefinition) { d.ExportMapping = "InjectedXML" }, func(d *FieldDefinition) { d.Type = "object" }, func(d *FieldDefinition) { d.Key = "custom.user.other" }} {
		forged := custom
		mutation(&forged)
		doc.DefinitionSnapshot[custom.Key] = forged
		raw, _ := json.Marshal(doc)
		if _, err := DecodeStored(raw); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("unsafe custom snapshot accepted: %v", err)
		}
	}
}
