package metadata

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
)

type contractFixture struct {
	Schema        Registry          `json:"schema"`
	EmptyDocument Document          `json:"empty_document"`
	Document      Document          `json:"document"`
	Candidate     MetadataCandidate `json:"candidate"`
	Patch         fixturePatch      `json:"patch"`
	Result        fixtureResult     `json:"result"`
}
type fixturePatch struct {
	Document         Document    `json:"document"`
	ExpectedRevision uint64      `json:"expected_revision"`
	InputRevision    uint64      `json:"input_revision"`
	Operations       []Operation `json:"operations"`
}
type fixtureResult struct {
	Document Document  `json:"document"`
	Warnings []Warning `json:"warnings"`
}

func TestSharedContractFixture(t *testing.T) {
	reg := StandardRegistry()
	empty := EmptyDocument(reg)
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	operations := []Operation{{Op: "set", Key: "title", Value: json.RawMessage(`"星海图书馆"`)}, {Op: "set", Key: "aliases", Value: json.RawMessage(`["星海圖書館","Library of Stars"]`)}, {Op: "set", Key: "creators.writer", Value: json.RawMessage(`["林青","Ada Example"]`)}, {Op: "set", Key: "series", Value: json.RawMessage(`"星海"`)}, {Op: "set", Key: "number", Value: json.RawMessage(`"2.5 特别篇"`)}, {Op: "set", Key: "count", Value: json.RawMessage(`12`)}, {Op: "set", Key: "volume", Value: json.RawMessage(`2024`)}, {Op: "clear", Key: "summary"}, {Op: "set", Key: "tags", Value: json.RawMessage(`["科幻","Slice of Life"]`)}, {Op: "set", Key: "language", Value: json.RawMessage(`"zh-Hant"`)}, {Op: "set", Key: "publication_date", Value: json.RawMessage(`{"year":2024,"month":2}`)}, {Op: "set", Key: "identifiers", Value: json.RawMessage(`[{"scheme":"provider.example","value":"record-17"}]`)}}
	doc, _, err := ApplyPatch(empty, reg, 0, operations, MergeContext{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	candidate := MetadataCandidate{CandidateID: "candidate-example-1", RequestID: "request-example-1", Origin: "provider", SchemaVersion: 1, DefinitionsVersion: reg.DefinitionsVersion, BaseDocumentRevision: 1, InputRevision: 3, FieldRevisions: map[string]uint64{"publisher": 0, "summary": 1}, Fields: map[string]CandidateField{"publisher": {State: "value", Value: json.RawMessage(`"示例出版社"`), Provenance: []Provenance{{Kind: "provider", SourceID: "provider.example", RecordID: "record-17", PublicURL: "https://example.org/books/17"}}}, "summary": {State: "value", Value: json.RawMessage(`"待核对简介"`), Provenance: []Provenance{{Kind: "provider", SourceID: "provider.example", RecordID: "record-17"}}}}}
	patch := fixturePatch{doc, 1, 3, []Operation{{Op: "adopt", Candidate: &candidate, Keys: []string{"publisher"}}}}
	adopted, warnings, err := ApplyPatch(doc, reg, patch.ExpectedRevision, patch.Operations, MergeContext{Now: now, InputRevision: 3})
	if err != nil {
		t.Fatal(err)
	}
	fixture := contractFixture{reg, empty, doc, candidate, patch, fixtureResult{adopted, warnings}}
	path := "testdata/contract-v1.json"
	if os.Getenv("UPDATE_METADATA_FIXTURES") == "1" {
		raw, err := json.MarshalIndent(fixture, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(raw, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved contractFixture
	if err := DecodeJSON(raw, &saved); err != nil {
		t.Fatal(err)
	}
	// Decode values canonicalizes JSON whitespace; compare the serialized objects.
	savedRaw, _ := json.Marshal(saved)
	expectedRaw, _ := json.Marshal(fixture)
	var savedValue, expectedValue any
	_ = json.Unmarshal(savedRaw, &savedValue)
	_ = json.Unmarshal(expectedRaw, &expectedValue)
	if !reflect.DeepEqual(savedValue, expectedValue) {
		t.Fatal("shared fixture drift: regenerate explicitly and notify consumers")
	}
}
