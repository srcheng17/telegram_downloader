package metadata

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestTypedValuesBoundsAndSemantics(t *testing.T) {
	reg := StandardRegistry()
	cases := []struct {
		key, value string
		valid      bool
	}{
		{"count", "0", false}, {"count", "2147483648", false}, {"count", "1.5", false}, {"count", `"5"`, false}, {"volume", "1", true}, {"page_count", "0", true},
		{"aliases", `[]`, true}, {"aliases", `null`, false}, {"aliases", `["別名 with spaces"]`, true}, {"aliases", `[""]`, false}, {"aliases", `[1]`, false},
		{"title", `""`, false}, {"title", `null`, false}, {"title", "false", false},
		{"number", `"1.5 特别篇"`, true}, {"language", `"zh-Hant"`, true}, {"language", `"Chinese"`, false},
		{"publication_date", `{"year":2024,"month":2,"day":29}`, true}, {"publication_date", `{"year":2023,"month":2,"day":29}`, false}, {"publication_date", `{"year":2024,"day":10}`, false}, {"publication_date", `{"year":2024,"month":0}`, false}, {"publication_date", `{"year":2024,"timezone":"secret"}`, false},
		{"age_rating", `"R18"`, false}, {"age_rating", `"Adults Only 18+"`, true},
		{"identifiers", `[]`, true}, {"identifiers", `null`, false}, {"identifiers", `[{"scheme":"bangumi","value":"1234"}]`, true}, {"identifiers", `[{"scheme":"isbn","value":"1234","token":"secret"}]`, false},
		{"web", `"https://example.org/books/1"`, true}, {"web", `"https://user:pass@example.org/"`, false}, {"web", `"http://127.0.0.1/"`, false}, {"web", `"https://example.org/?api_key=secret"`, false},
	}
	for _, tc := range cases {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			doc, _, err := ApplyPatch(EmptyDocument(reg), reg, 0, []Operation{{Op: "set", Key: tc.key, Value: json.RawMessage(tc.value)}}, MergeContext{Now: time.Unix(1, 0)})
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
			if tc.valid && (!doc.Fields[tc.key].ManualLocked || doc.Revision != 1) {
				t.Fatal("manual edit did not lock or advance revision")
			}
		})
	}
}

func TestCandidateContextLocksAndUnrelatedChanges(t *testing.T) {
	reg := StandardRegistry()
	now := time.Unix(1, 0)
	base := EmptyDocument(reg)
	candidate := MetadataCandidate{CandidateID: "c-1", RequestID: "r-1", Origin: "provider", SchemaVersion: 1, DefinitionsVersion: reg.DefinitionsVersion, InputRevision: 2, FieldRevisions: map[string]uint64{"title": 0}, Fields: map[string]CandidateField{"title": {State: "value", Value: json.RawMessage(`"建议标题"`)}}}
	// Changes outside the selection do not invalidate the candidate.
	doc, _, err := ApplyPatch(base, reg, 0, []Operation{{Op: "set", Key: "summary", Value: json.RawMessage(`"新简介"`)}}, MergeContext{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err = ApplyCandidate(doc, reg, 1, candidate, []string{"title"}, false, MergeContext{Now: now, InputRevision: 2})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Fields["title"].ManualLocked {
		t.Fatal("candidate established manual lock")
	}
	if _, _, err = ApplyCandidate(base, reg, 0, candidate, []string{"title"}, false, MergeContext{Now: now, InputRevision: 3}); !errors.Is(err, ErrConflict) {
		t.Fatalf("old input accepted: %v", err)
	}
	v := uint64(1)
	candidate.ConfigRevision = &v
	if _, _, err = ApplyCandidate(base, reg, 0, candidate, []string{"title"}, false, MergeContext{Now: now, InputRevision: 2}); !errors.Is(err, ErrConflict) {
		t.Fatalf("old config accepted: %v", err)
	}
	candidate.ConfigRevision = nil
	doc, _, err = ApplyPatch(doc, reg, 2, []Operation{{Op: "set", Key: "title", Value: json.RawMessage(`"人工标题"`)}}, MergeContext{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	candidate.BaseDocumentRevision = 3
	candidate.FieldRevisions["title"] = 3
	if _, _, err = ApplyCandidate(doc, reg, 3, candidate, []string{"title"}, false, MergeContext{Now: now, InputRevision: 2}); !errors.Is(err, ErrConflict) {
		t.Fatal("implicit lock override")
	}
	adopted, _, err := ApplyCandidate(doc, reg, 3, candidate, []string{"title"}, true, MergeContext{Now: now, InputRevision: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !adopted.Fields["title"].ManualLocked {
		t.Fatal("confirmation silently unlocked")
	}
	unlocked, _, err := ApplyPatch(adopted, reg, 4, []Operation{{Op: "unlock", Key: "title"}}, MergeContext{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if unlocked.Fields["title"].ManualLocked || unlocked.Fields["title"].Revision != 5 {
		t.Fatal("unlock failed")
	}
	if !doc.Fields["title"].ManualLocked || doc.Revision != 3 {
		t.Fatal("original document mutated")
	}
}

func TestPurePatchIsNotPersistentCAS(t *testing.T) {
	reg := StandardRegistry()
	doc := EmptyDocument(reg)
	ops := []Operation{{Op: "clear", Key: "title"}}
	ctx := MergeContext{Now: time.Unix(1, 0)}
	first, _, err := ApplyPatch(doc, reg, 0, ops, ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := ApplyPatch(doc, reg, 0, ops, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("pure draft transform is not deterministic")
	}
}

func TestCandidateCannotForgeManualSourceOrPageCount(t *testing.T) {
	reg := StandardRegistry()
	c := MetadataCandidate{CandidateID: "c", RequestID: "r", Origin: "ai", SchemaVersion: 1, DefinitionsVersion: reg.DefinitionsVersion, FieldRevisions: map[string]uint64{"title": 0}, Fields: map[string]CandidateField{"title": {State: "value", Value: json.RawMessage(`"建议"`), Provenance: []Provenance{{Kind: "manual", SourceID: "user"}}}}}
	if err := ValidateCandidate(c, reg); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("forged manual source accepted")
	}
	c.FieldRevisions = map[string]uint64{"page_count": 0}
	c.Fields = map[string]CandidateField{"page_count": {State: "value", Value: json.RawMessage(`5`)}}
	if err := ValidateCandidate(c, reg); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("AI invented page count")
	}
	var parsed MetadataCandidate
	raw := `{"candidate_id":"c","request_id":"r","origin":"ai","schema_version":1,"definitions_version":"standard-v1","base_document_revision":0,"input_revision":0,"field_revisions":{"title":0},"fields":{"title":{"state":"value","value":"hello","manual_locked":true}}}`
	if err := DecodeJSON([]byte(raw), &parsed); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("candidate manual_locked accepted")
	}
}

func TestCandidateRejectsNullConflictRevisions(t *testing.T) {
	for _, raw := range []string{
		`{"candidate_id":"c","request_id":"r","origin":"ai","schema_version":1,"definitions_version":"standard-v1","base_document_revision":null,"input_revision":0,"field_revisions":{"title":0},"fields":{"title":{"state":"value","value":"hello","provenance":[]}}}`,
		`{"candidate_id":"c","request_id":"r","origin":"ai","schema_version":1,"definitions_version":"standard-v1","base_document_revision":0,"input_revision":0,"field_revisions":{"title":null},"fields":{"title":{"state":"value","value":"hello","provenance":[]}}}`,
	} {
		var candidate MetadataCandidate
		if err := DecodeJSON([]byte(raw), &candidate); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("null candidate conflict baseline accepted")
		}
	}
}
