package metadataextract

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	domain "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
)

type schemaStub struct{ registry domain.Registry }

func (s schemaStub) Schema(context.Context) (domain.Registry, error) { return s.registry, nil }

type aiStub struct {
	output    json.RawMessage
	calls     int
	snapshot  ModelSnapshot
	prompt    string
	schema    json.RawMessage
	err       error
	after     func()
	deadlines []time.Time
}

func (a *aiStub) Snapshot(ctx context.Context, _ uint64) (ModelSnapshot, error) {
	if deadline, ok := ctx.Deadline(); ok {
		a.deadlines = append(a.deadlines, deadline)
	}
	return a.snapshot, nil
}
func (a *aiStub) ExtractJSON(ctx context.Context, _ ModelSnapshot, prompt string, schema json.RawMessage, _ int) (json.RawMessage, error) {
	if deadline, ok := ctx.Deadline(); ok {
		a.deadlines = append(a.deadlines, deadline)
	}
	a.calls++
	a.prompt = prompt
	a.schema = schema
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if a.after != nil {
		a.after()
	}
	return a.output, a.err
}

func TestExtractionUsesOneOverallDeadline(t *testing.T) {
	a := model(`{"fields":{}}`)
	_, err := NewService(schemaStub{domain.StandardRegistry()}, a).Extract(context.Background(), sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(a.deadlines) != 3 {
		t.Fatalf("discovery/inference/recheck do not share a deadline: %d", len(a.deadlines))
	}
	for _, deadline := range a.deadlines {
		if !deadline.Equal(a.deadlines[0]) || time.Until(deadline) > 120*time.Second {
			t.Fatal("model stages extend the overall deadline")
		}
	}
}

func TestCustomFieldsAndConfigurationChangedDuringInference(t *testing.T) {
	base := domain.StandardRegistry()
	registry, err := domain.NewRegistry(base, []domain.FieldDefinition{{Key: "custom.user.complete", Label: "是否完结", Type: "boolean", Enabled: true, Editable: true, Extractable: []string{"ai"}, ExportStatus: "internal_only"}})
	if err != nil {
		t.Fatal(err)
	}
	in := sampleInput()
	in.DefinitionsVersion = registry.DefinitionsVersion
	in.Text = "是否完结：否"
	in.FieldKeys = []string{"custom.user.complete"}
	in.FieldRevisions = map[string]uint64{"custom.user.complete": 0}
	a := model(`{"fields":{"custom.user.complete":{"value":false,"evidence_quote":"是否完结：否"}}}`)
	got, err := NewService(schemaStub{registry}, a).Extract(context.Background(), in)
	if err != nil || len(got.Candidates) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if !strings.Contains(string(a.schema), "boolean") || strings.Contains(string(a.schema), "page_count") {
		t.Fatal("schema includes unselected fields or wrong type")
	}
	a.after = func() { a.snapshot.ConfigRevision++ }
	_, err = NewService(schemaStub{registry}, a).Extract(context.Background(), in)
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "config_changed" {
		t.Fatalf("old configuration response was accepted: %v", err)
	}
}
func sampleInput() Input {
	return Input{RequestID: "req-1", Text: "标题：星海图书\n作者：示例作者", FieldKeys: []string{"title"}, SchemaVersion: 1, DefinitionsVersion: "standard-v1", FieldRevisions: map[string]uint64{"title": 0}, InputRevision: 3, ConfigRevision: 1}
}
func model(output string) *aiStub {
	return &aiStub{output: json.RawMessage(output), snapshot: ModelSnapshot{ConfigRevision: 1, ModelID: "test-model", BudgetVerified: true, ContextTokens: 4096}}
}
func TestVerifiedChatResponseRetainsEvidenceAndSnapshotChecks(t *testing.T) {
	for _, change := range []struct {
		name     string
		mutate   func(*aiStub)
		wantCode string
	}{
		{"grounded", func(*aiStub) {}, ""},
		{"ungrounded", func(a *aiStub) {
			a.output = json.RawMessage(`{"fields":{"title":{"value":"invented","evidence_quote":"星海图书"}}}`)
		}, "invalid_response"},
		{"protocol drift", func(a *aiStub) { a.after = func() { a.snapshot.Protocol = "llama_cpp_native" } }, "config_changed"},
		{"budget mode drift", func(a *aiStub) { a.after = func() { a.snapshot.BudgetMode = "exact_tokens" } }, "config_changed"},
		{"identity drift", func(a *aiStub) { a.after = func() { a.snapshot.CapabilityFingerprint = "changed" } }, "config_changed"},
		{"destination drift", func(a *aiStub) { a.after = func() { a.snapshot.Destination = "http://changed/v1" } }, "config_changed"},
	} {
		t.Run(change.name, func(t *testing.T) {
			a := model(`{"fields":{"title":{"value":"星海图书","evidence_quote":"星海图书"}}}`)
			a.snapshot.Protocol, a.snapshot.BudgetMode, a.snapshot.CapabilityFingerprint, a.snapshot.ContextTokens = "llama_cpp_chat", "verified_untruncated_response", "synthetic-chat", 0
			change.mutate(a)
			got, err := NewService(schemaStub{domain.StandardRegistry()}, a).Extract(context.Background(), sampleInput())
			if change.wantCode == "" {
				if err != nil || len(got.Candidates) != 1 {
					t.Fatalf("verified chat response rejected: %v", err)
				}
				return
			}
			var typed *Error
			if !errors.As(err, &typed) || typed.Code != change.wantCode || len(got.Candidates) != 0 {
				t.Fatalf("unsafe chat result accepted: %v", err)
			}
		})
	}
}
func TestExtractGroundedCandidate(t *testing.T) {
	a := model(`{"fields":{"title":{"value":"星海图书","evidence_quote":"星海图书"}}}`)
	in := sampleInput()
	got, err := NewService(schemaStub{domain.StandardRegistry()}, a).Extract(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Candidates) != 1 {
		t.Fatalf("%+v", got)
	}
	c := got.Candidates[0]
	if domain.ValidateCandidate(c, domain.StandardRegistry()) != nil {
		t.Fatal("invalid canonical candidate")
	}
	e := c.Fields["title"].Provenance[0].Evidence
	if *e.Start != 3 || *e.End != 7 {
		t.Fatalf("wrong UTF16 offsets: %+v", e)
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "evidence_quote") {
		t.Fatal("quote leaked into provenance")
	}
	if !strings.Contains(a.prompt, "星海图书") || !strings.Contains(string(a.schema), "evidence_quote") {
		t.Fatal("missing prompt/schema")
	}
	instructions, encodedInput, found := strings.Cut(a.prompt, "\nINPUT_DATA_JSON:\n")
	if !found || !strings.Contains(instructions, "用于标识字段的“标题：”等标签及其分隔符") || !strings.Contains(instructions, "保留字段值本身的标点") {
		t.Fatal("fixed instructions must distinguish structural labels from value punctuation")
	}
	var sent struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(encodedInput), &sent); err != nil || sent.Text != in.Text {
		t.Fatal("label guidance must not alter the input or its evidence offsets")
	}
}
func TestSchemaGeneratesValuesBeforeQuotesWithoutChangingConstraints(t *testing.T) {
	schema, err := buildSchema(map[string]domain.FieldDefinition{
		"title":       {Label: "标题", Type: "string", MaxBytes: 12, Enum: []string{"星海图书"}},
		"identifiers": {Label: "标识符", Type: "identifiers", MaxItems: 2, ItemMaxBytes: 32},
	})
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Properties struct {
			Fields struct {
				Properties map[string]struct {
					Properties json.RawMessage `json:"properties"`
				} `json:"properties"`
			} `json:"fields"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &wire); err != nil {
		t.Fatal(err)
	}
	for key, field := range wire.Properties.Fields.Properties {
		if !strings.HasPrefix(string(field.Properties), `{"value":`) || strings.Index(string(field.Properties), `"evidence_quote":`) < 0 {
			t.Fatalf("%s grammar must generate value before evidence_quote", key)
		}
	}
	// JSON Schema meaning is independent of property order. Assert the complete
	// decoded contract so the generation-order fix cannot relax nested bounds,
	// selected fields, required members or additionalProperties restrictions.
	const expected = `{
		"type":"object","required":["fields"],"additionalProperties":false,
		"properties":{"fields":{
			"type":"object","required":[],"additionalProperties":false,
			"properties":{
				"title":{
					"type":"object","required":["value","evidence_quote"],"additionalProperties":false,
					"properties":{
						"value":{"type":"string","minLength":1,"maxLength":12,"enum":["星海图书"],"description":"标题"},
						"evidence_quote":{"type":"string","minLength":1,"maxLength":16384}
					}
				},
				"identifiers":{
					"type":"object","required":["value","evidence_quote"],"additionalProperties":false,
					"properties":{
						"value":{"type":"array","minItems":1,"maxItems":2,"description":"标识符","items":{
							"type":"object","required":["scheme","value"],"additionalProperties":false,
							"properties":{
								"scheme":{"type":"string","pattern":"^[a-z][a-z0-9_.-]{0,63}$"},
								"value":{"type":"string","minLength":1,"maxLength":32}
							}
						}},
						"evidence_quote":{"type":"string","minLength":1,"maxLength":16384}
					}
				}
			}
		}}
	}`
	var got, want any
	if err := json.Unmarshal(schema, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("generation order changed the schema's validation contract")
	}
}
func TestRejectUngroundedWrongTypedExtraAndTruncatedOutput(t *testing.T) {
	for _, body := range []string{`{"fields":{"title":{"value":"invented","evidence_quote":"星海图书"}}}`, `{"fields":{"title":{"value":"星海图书","evidence_quote":"unseen"}}}`, `{"fields":{"title":{"value":4,"evidence_quote":"星海图书"}}}`, `{"fields":{"title":{"value":"星海图书","evidence_quote":"星海图书","manual_locked":false}}}`, `{"fields":{"publisher":{"value":"星海图书","evidence_quote":"星海图书"}}}`, `{"fields":`, `{"fields":{"title":{"value":"","evidence_quote":"星海图书"}}}`} {
		a := model(body)
		_, err := NewService(schemaStub{domain.StandardRegistry()}, a).Extract(context.Background(), sampleInput())
		var typed *Error
		if !errors.As(err, &typed) || typed.Code != "invalid_response" {
			t.Fatalf("accepted invalid body or wrong error: %v", err)
		}
	}
}
func TestRejectBeforeSendingUnsupportedBudgetAndInvalidInputs(t *testing.T) {
	for _, change := range []func(*Input, *aiStub){func(_ *Input, a *aiStub) { a.snapshot.BudgetVerified = false }, func(in *Input, _ *aiStub) {
		in.FieldKeys = []string{"page_count"}
		in.FieldRevisions = map[string]uint64{"page_count": 0}
	}, func(in *Input, _ *aiStub) { in.Text = strings.Repeat("字", 22000) }, func(in *Input, _ *aiStub) { in.DefinitionsVersion = "changed" }, func(in *Input, _ *aiStub) { in.FieldRevisions["title"] = 1 }, func(in *Input, a *aiStub) { a.snapshot.ConfigRevision = in.ConfigRevision + 1 }} {
		in := sampleInput()
		a := model(`{"fields":{}}`)
		change(&in, a)
		if _, err := NewService(schemaStub{domain.StandardRegistry()}, a).Extract(context.Background(), in); err == nil {
			t.Fatal("expected rejection")
		}
		if a.calls != 0 {
			t.Fatal("sent rejected text")
		}
	}
}
func TestEmptyResultAndCancellation(t *testing.T) {
	a := model(`{"fields":{}}`)
	s := NewService(schemaStub{domain.StandardRegistry()}, a)
	got, err := s.Extract(context.Background(), sampleInput())
	if err != nil || len(got.Candidates) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Extract(ctx, sampleInput())
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "cancelled" {
		t.Fatal(err)
	}
}
