package modelapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractionBudgetsExactRenderedTokens(t *testing.T) {
	for _, tc := range []struct {
		name       string
		tokens     int
		truncated  bool
		stop, code string
	}{
		{"valid", 20, false, "eos", ""}, {"overflow", 4090, false, "eos", "context_exceeded"}, {"upstream truncation", 20, true, "eos", "invalid_response"}, {"output incomplete", 20, false, "limit", "invalid_response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			rendered := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer synthetic" {
					t.Error("missing credential")
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v1/models":
					w.Write([]byte(`{"data":[{"id":"MiniCPM5-2B-Q4"}]}`))
				case "/props":
					w.Write([]byte(`{"default_generation_settings":{"n_ctx":4096},"chat_template":"synthetic-template","build_info":"b11382-11fe021","model_path":"synthetic.gguf","model_alias":"MiniCPM5-2B-Q4"}`))
				case "/apply-template":
					var body map[string]any
					json.NewDecoder(r.Body).Decode(&body)
					data, _ := json.Marshal(body)
					if !strings.Contains(string(data), "synthetic-field") || !strings.Contains(string(data), "synthetic text") {
						t.Error("schema or input excluded from template")
					}
					rendered = true
					w.Write([]byte(`{"prompt":"exact formatted prompt"}`))
				case "/tokenize":
					var body map[string]any
					json.NewDecoder(r.Body).Decode(&body)
					if !rendered || body["content"] != "exact formatted prompt" {
						t.Error("not tokenizing actual template")
					}
					if body["add_special"] != true || body["parse_special"] != true {
						t.Error("tokenizer differs from pinned chat endpoint: required BOS/EOS can be lost")
					}
					tokens := make([]int, tc.tokens)
					for i := range tokens {
						tokens[i] = i + 1
					}
					json.NewEncoder(w).Encode(map[string]any{"tokens": tokens})
				case "/completion":
					calls++
					var body struct {
						Prompt []int `json:"prompt"`
						Budget int   `json:"n_predict"`
					}
					json.NewDecoder(r.Body).Decode(&body)
					if len(body.Prompt) != tc.tokens || body.Budget != 512 {
						t.Error("sent different prompt or output reserve")
					}
					// Pinned 11fe021 server-task.cpp task_params::to_json() exposes
					// n_predict, NOT n_ctx; the README's n_ctx claim is stale.
					json.NewEncoder(w).Encode(map[string]any{"content": `{"fields":{}}`, "model": "MiniCPM5-2B-Q4", "stop": true, "truncated": tc.truncated, "stop_type": tc.stop, "tokens_evaluated": tc.tokens, "tokens_predicted": 6, "generation_settings": map[string]any{"n_predict": 512, "cache_prompt": false, "stream": false}})
				default:
					t.Error("unexpected path", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			c := NewClient()
			key := credentials.NewSecret("synthetic")
			cap, err := c.ExtractionCapability(context.Background(), srv.URL+"/v1", key, "MiniCPM5-2B-Q4")
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Extract(context.Background(), srv.URL+"/v1", key, "MiniCPM5-2B-Q4", "synthetic text", json.RawMessage(`{"synthetic-field":{}}`), 512, cap)
			if tc.code == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var failure *Error
				if !errors.As(err, &failure) || failure.Code != tc.code {
					t.Fatalf("wanted %s got %v", tc.code, err)
				}
			}
			if tc.code == "context_exceeded" && calls != 0 {
				t.Fatal("overflow reached inference")
			}
		})
	}
}

func TestNativeExtractionRejectsProtocolDriftAndRefusal(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		tokens     any
		mutate     func(map[string]any)
	}{
		{name: "missing evaluated count", code: "invalid_response", mutate: func(r map[string]any) { delete(r, "tokens_evaluated") }},
		{name: "wrong evaluated count", code: "invalid_response", mutate: func(r map[string]any) { r["tokens_evaluated"] = 1 }},
		{name: "missing output count", code: "invalid_response", mutate: func(r map[string]any) { delete(r, "tokens_predicted") }},
		{name: "negative output count", code: "invalid_response", mutate: func(r map[string]any) { r["tokens_predicted"] = -1 }},
		{name: "output exceeds reserve", code: "invalid_response", mutate: func(r map[string]any) { r["tokens_predicted"] = 129 }},
		{name: "not final", code: "invalid_response", mutate: func(r map[string]any) { r["stop"] = false }},
		{name: "wrong model", code: "invalid_response", mutate: func(r map[string]any) { r["model"] = "other" }},
		{name: "budget clamped", code: "invalid_response", mutate: func(r map[string]any) { r["generation_settings"].(map[string]any)["n_predict"] = 64 }},
		{name: "refused", code: "refused", mutate: func(r map[string]any) { r["refusal"] = "synthetic refusal body" }},
		{name: "null token", code: "invalid_response", tokens: []any{1, nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/models":
					w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
				case "/props":
					w.Write([]byte(`{"default_generation_settings":{"n_ctx":4096},"chat_template":"template","build_info":"b11382-11fe021","model_path":"synthetic.gguf","model_alias":"test-model"}`))
				case "/apply-template":
					w.Write([]byte(`{"prompt":"rendered"}`))
				case "/tokenize":
					tokens := tc.tokens
					if tokens == nil {
						tokens = []int{1, 2}
					}
					json.NewEncoder(w).Encode(map[string]any{"tokens": tokens})
				case "/completion":
					calls++
					result := map[string]any{"content": `{"fields":{}}`, "model": "test-model", "stop": true, "truncated": false, "stop_type": "eos", "tokens_evaluated": 2, "tokens_predicted": 6, "generation_settings": map[string]any{"n_predict": 128, "stream": false}}
					if tc.mutate != nil {
						tc.mutate(result)
					}
					json.NewEncoder(w).Encode(result)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			client := NewClient()
			key := credentials.NewSecret("")
			cap, err := client.ExtractionCapability(context.Background(), srv.URL+"/v1", key, "test-model")
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Extract(context.Background(), srv.URL+"/v1", key, "test-model", "synthetic text", json.RawMessage(`{"type":"object"}`), 128, cap)
			assertCode(t, err, tc.code)
			if tc.tokens != nil && calls != 0 {
				t.Fatal("invalid token response reached inference")
			}
		})
	}
}

func TestExtractionCapabilityRejectsGenericAndChangedModelBeforeText(t *testing.T) {
	for _, sample := range []struct {
		name, build, alias string
		models             []map[string]string
	}{
		{"generic service", "compatible", "test", []map[string]string{{"id": "test"}}},
		{"substring build", "b1-bad11fe021extra", "test", []map[string]string{{"id": "test"}}},
		{"different commit", "b1-11fe021abc", "test", []map[string]string{{"id": "test"}}},
		{"model identity mismatch", "b11382-11fe021", "actual-other", []map[string]string{{"id": "test"}}},
		{"multiple models", "b11382-11fe021", "test", []map[string]string{{"id": "test"}, {"id": "another"}}},
	} {
		t.Run(sample.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/models":
					json.NewEncoder(w).Encode(map[string]any{"data": sample.models})
				case "/props":
					json.NewEncoder(w).Encode(map[string]any{"default_generation_settings": map[string]int{"n_ctx": 4096}, "chat_template": "template", "build_info": sample.build, "model_path": "synthetic.gguf", "model_alias": sample.alias})
				default:
					t.Error("unsupported server received text")
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			_, err := NewClient().ExtractionCapability(context.Background(), srv.URL+"/v1", credentials.NewSecret(""), "test")
			assertCode(t, err, "schema_unsupported")
		})
	}
}
