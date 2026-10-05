package metadata

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDocumentLegacyAndExtendedRoundTrip(t *testing.T) {
	author, tags := "Ada Lovelace，原作", "科幻 #日常"
	doc, err := FromLegacy(Legacy{Author: &author, Tags: &tags})
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err = ApplyPatch(doc, StandardRegistry(), doc.Revision, []Operation{{Op: "set", Key: "aliases", Value: json.RawMessage(`["别名 A","Alias B"]`)}, {Op: "set", Key: "count", Value: json.RawMessage(`12`)}, {Op: "set", Key: "number", Value: json.RawMessage(`"特别篇"`)}}, MergeContext{Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(doc)
	got, err := Decode(data, StandardRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Fields["aliases"].Value) != `["别名 A","Alias B"]` || *ToLegacy(got).Author != "Ada Lovelace,原作" {
		t.Fatalf("lost metadata: %s", data)
	}
}

func TestManualClearRejectsLateCandidateAtomically(t *testing.T) {
	reg := StandardRegistry()
	doc := EmptyDocument(reg)
	candidate := MetadataCandidate{CandidateID: "candidate-1", RequestID: "request-1", Origin: "provider", SchemaVersion: 1, DefinitionsVersion: reg.DefinitionsVersion, FieldRevisions: map[string]uint64{"title": 0, "summary": 0}, Fields: map[string]CandidateField{"title": {State: "value", Value: json.RawMessage(`"旧标题"`)}, "summary": {State: "value", Value: json.RawMessage(`"简介"`)}}}
	doc, _, err := ApplyPatch(doc, reg, 0, []Operation{{Op: "clear", Key: "title"}}, MergeContext{Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = ApplyPatch(doc, reg, 1, []Operation{{Op: "adopt", Candidate: &candidate, Keys: []string{"title", "summary"}, ConfirmLocked: true}}, MergeContext{Now: time.Unix(2, 0)})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	if doc.Fields["title"].State != "cleared" || !doc.Fields["title"].ManualLocked {
		t.Fatal("clear lost")
	}
	if _, ok := doc.Fields["summary"]; ok {
		t.Fatal("partial adoption")
	}
}

func TestDecodeRejectsAmbiguityAndForgedSnapshot(t *testing.T) {
	doc := EmptyDocument(StandardRegistry())
	data, _ := json.Marshal(doc)
	for _, invalid := range []string{strings.Replace(string(data), `"revision":0`, `"revision":0,"revision":1`, 1), strings.Replace(string(data), `"revision":0`, `"revision":0,"raw_ocr":"secret"`, 1)} {
		if _, err := Decode([]byte(invalid), StandardRegistry()); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("accepted %s: %v", invalid, err)
		}
	}
	doc.Fields["title"] = FieldState{State: "value", Value: json.RawMessage(`"Title"`)}
	def := StandardRegistry().Definitions["title"]
	def.Type = "boolean"
	doc.DefinitionSnapshot["title"] = def
	if _, _, err := Validate(doc, StandardRegistry()); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("accepted forged definition")
	}
}

func TestDecodeRejectsNullRevisionAndLockAndPreservesNumbers(t *testing.T) {
	doc, _, err := ApplyPatch(EmptyDocument(StandardRegistry()), StandardRegistry(), 0, []Operation{{Op: "clear", Key: "title"}}, MergeContext{Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(doc)
	for _, invalid := range []string{
		strings.Replace(string(raw), `"revision":1`, `"revision":null`, 1),
		strings.Replace(string(raw), `"manual_locked":true`, `"manual_locked":null`, 1),
		strings.Replace(string(raw), `"manual_locked":true,`, ``, 1),
	} {
		if _, err := Decode([]byte(invalid), StandardRegistry()); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("accepted missing or null conflict member: %v", err)
		}
	}
	var envelope map[string]any
	if err := DecodeJSON([]byte(`{"file_size":9007199254740991}`), &envelope); err != nil {
		t.Fatal(err)
	}
	if number, ok := envelope["file_size"].(json.Number); !ok || number.String() != "9007199254740991" {
		t.Fatal("generic envelope lost integer precision")
	}
}
