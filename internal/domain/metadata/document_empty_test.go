package metadata

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestEmptyListsRoundTripAsValuesAndRemainDistinctFromClear(t *testing.T) {
	custom := FieldDefinition{Key: "custom.user.notes", Label: "自定义备注", Type: "string[]", MaxItems: 64, ItemMaxBytes: 1024, Editable: true, Enabled: true, Extractable: []string{"provider"}, ExportStatus: "internal_only"}
	reg, err := NewRegistry(StandardRegistry(), []FieldDefinition{custom})
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{"aliases", "identifiers", "tags", "genres", "creators.writer", custom.Key}
	ops := make([]Operation, 0, len(keys))
	for _, key := range keys {
		ops = append(ops, Operation{Op: "set", Key: key, Value: json.RawMessage(`[]`)})
	}
	doc, _, err := ApplyPatch(EmptyDocument(reg), reg, 0, ops, MergeContext{Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(data, reg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, doc) {
		t.Fatal("empty list document changed on roundtrip")
	}
	for _, key := range keys {
		field, ok := decoded.Fields[key]
		if !ok || field.State != "value" || string(field.Value) != "[]" || !field.ManualLocked || field.Revision != 1 {
			t.Fatalf("empty %s became absent or cleared: %#v", key, field)
		}
		if _, _, err := ApplyPatch(doc, reg, doc.Revision, []Operation{{Op: "set", Key: key, Value: json.RawMessage(`null`)}}, MergeContext{Now: time.Unix(2, 0)}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("null %s accepted: %v", key, err)
		}
	}
	cleared, _, err := ApplyPatch(doc, reg, 1, []Operation{{Op: "clear", Key: "aliases"}}, MergeContext{Now: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Fields["aliases"].State != "cleared" || len(cleared.Fields["aliases"].Value) != 0 || cleared.Fields["aliases"].Revision != 2 {
		t.Fatal("explicit clear lost its distinct tombstone")
	}
	if doc.Fields["aliases"].State != "value" || string(doc.Fields["aliases"].Value) != "[]" {
		t.Fatal("clear mutated original empty-list snapshot")
	}
}

func TestEmptyListLegacyProjectionAndMixedInput(t *testing.T) {
	reg := StandardRegistry()
	doc, _, err := ApplyPatch(EmptyDocument(reg), reg, 0, []Operation{
		{Op: "set", Key: "tags", Value: json.RawMessage(`[]`)},
		{Op: "set", Key: "genres", Value: json.RawMessage(`[]`)},
		{Op: "set", Key: "creators.writer", Value: json.RawMessage(`[]`)},
	}, MergeContext{Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	projection := ToLegacy(doc)
	if projection.Tags != nil || projection.Genres != nil || projection.Author != nil {
		t.Fatal("legacy projection fabricated nonempty content")
	}
	if err := CheckLegacy(doc, projection); err != nil {
		t.Fatal("omitted legacy values should not conflict", err)
	}
	empty := ""
	for _, input := range []Legacy{{Tags: &empty}, {Genres: &empty}, {Author: &empty}} {
		if err := CheckLegacy(doc, input); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("legacy absence silently replaced an explicit empty-list value")
		}
		legacyDoc, err := FromLegacy(input)
		if err != nil {
			t.Fatal(err)
		}
		if len(legacyDoc.Fields) != 0 {
			t.Fatal("legacy empty string semantics changed")
		}
	}
	if len(doc.Fields) != 3 || doc.Fields["tags"].State != "value" {
		t.Fatal("legacy projection mutated document")
	}
}

func TestCandidateAdoptsEmptyListsAsValues(t *testing.T) {
	reg := StandardRegistry()
	candidate := MetadataCandidate{CandidateID: "empty-list-candidate", RequestID: "request", Origin: "provider", SchemaVersion: SchemaVersion, DefinitionsVersion: reg.DefinitionsVersion,
		FieldRevisions: map[string]uint64{"aliases": 0, "identifiers": 0}, Fields: map[string]CandidateField{
			"aliases":     {State: "value", Value: json.RawMessage(`[]`), Provenance: []Provenance{{Kind: "provider", SourceID: "catalog"}}},
			"identifiers": {State: "value", Value: json.RawMessage(`[]`), Provenance: []Provenance{{Kind: "provider", SourceID: "catalog"}}},
		}}
	if err := ValidateCandidate(candidate, reg); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	var decoded MetadataCandidate
	if err := DecodeJSON(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	doc, _, err := ApplyCandidate(EmptyDocument(reg), reg, 0, decoded, []string{"aliases", "identifiers"}, false, MergeContext{Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"aliases", "identifiers"} {
		field := doc.Fields[key]
		if field.State != "value" || string(field.Value) != "[]" || field.ManualLocked || field.Revision != 1 || field.Provenance[0].SourceID != "catalog" {
			t.Fatalf("candidate %s did not remain an empty-list value", key)
		}
		bad := candidate.Fields[key]
		bad.Value = json.RawMessage(`null`)
		candidate.Fields[key] = bad
		if err := ValidateCandidate(candidate, reg); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("null candidate %s accepted", key)
		}
		bad.Value = json.RawMessage(`[]`)
		candidate.Fields[key] = bad
	}
}
