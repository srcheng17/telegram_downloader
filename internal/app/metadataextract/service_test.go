package metadataextract

import (
	"context"
	"encoding/json"
	"errors"
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
