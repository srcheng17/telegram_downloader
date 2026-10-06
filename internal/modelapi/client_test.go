package modelapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ryancheng/telegram-downloader/internal/credentials"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeErrorsRemainCategorizedAndRedacted(t *testing.T) {
	for _, path := range []string{"/completion", "/chat/completions"} {
		for _, item := range []struct{ kind, code string }{{"exceed_context_size_error", "context_exceeded"}, {"invalid_request_error", "schema_unsupported"}, {"secret-private-type", "unavailable"}} {
			t.Run(path+"/"+item.kind, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(400)
					json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"type": item.kind, "message": "synthetic-private-text-and-key"}})
				}))
				defer srv.Close()
				_, err := NewClient().request(context.Background(), srv.URL, path, credentials.NewSecret(""), []byte(`{}`), time.Second)
				assertCode(t, err, item.code)
				if strings.Contains(err.Error(), "synthetic-private") {
					t.Fatal("private upstream body leaked")
				}
			})
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewClient().request(ctx, "http://127.0.0.1:1", "/completion", credentials.NewSecret(""), []byte(`{}`), time.Second)
	assertCode(t, err, "cancelled")
}

func assertCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}
func TestNormalizeTarget(t *testing.T) {
	for _, raw := range []string{"file:///tmp/x", "http://a:p@localhost", "https://example.test?q=x", "https://example.test#", "https://example.test?", "ftp://localhost", " http://localhost"} {
		if _, err := NormalizeBaseURL(raw); err == nil {
			t.Fatalf("accepted invalid target %q", raw)
		}
	}
	got, err := NormalizeBaseURL("http://localhost:8080/v1/")
	if err != nil || got != "http://localhost:8080/v1" {
		t.Fatal("loopback base path failed")
	}
}
func TestRedirectNeverForwardsCredential(t *testing.T) {
	var calls atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(200) }))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL+"/models", 302) }))
	defer server.Close()
	_, err := NewClient().Models(context.Background(), server.URL, credentials.NewSecret("synthetic"))
	assertCode(t, err, "redirect_blocked")
	if calls.Load() != 0 {
		t.Fatal("redirect target contacted")
	}
}
func TestModelsValidationAndBounds(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		code       string
		count      int
	}{{"valid", `{"data":[{"id":"m"},{"id":"m"},{"id":3},{"id":"image-model","type":"image"}]}`, 200, "", 2}, {"empty", `{"data":[]}`, 200, "", 0}, {"missing", `{}`, 200, "invalid_response", 0}, {"unauthorized", "synthetic-secret-upstream", 401, "unauthorized", 0}, {"rate", "", 429, "rate_limited", 0}, {"large", strings.Repeat("x", (1<<20)+1), 200, "response_too_large", 0}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "" {
					t.Error("path or noauth contract failed")
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			result, err := NewClient().Models(context.Background(), server.URL+"/v1", credentials.NewSecret(""))
			if tc.code != "" {
				assertCode(t, err, tc.code)
				return
			}
			if err != nil || len(result.Models) != tc.count {
				t.Fatal("model result invalid")
			}
			if tc.name == "valid" && (result.Ignored != 1 || result.Models[0].Selectable) {
				t.Fatal("capability or ignored entry invalid")
			}
		})
	}
}
func TestInferUsesExactModelAndRejectsTruncatedOutput(t *testing.T) {
	for _, reason := range []string{"stop", "length", "tool_calls"} {
		t.Run(reason, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/chat/completions" {
					t.Error("wrong path")
				}
				var req struct {
					Model string `json:"model"`
				}
				if json.NewDecoder(r.Body).Decode(&req) != nil || req.Model != "exact-model-id" {
					t.Error("wrong model")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": reason, "message": map[string]string{"content": `{"title":"sample"}`}}}})
			}))
			defer server.Close()
			output, err := NewClient().Infer(context.Background(), server.URL, credentials.NewSecret(""), "exact-model-id", "fixed sample", json.RawMessage(`{"type":"object"}`))
			if reason == "stop" {
				if err != nil || !json.Valid(output) {
					t.Fatal("valid output rejected")
				}
			} else {
				assertCode(t, err, "invalid_response")
			}
		})
	}
}

func TestInferSuppliesSchemaToPromptAndResponseFormat(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}},"required":["title"],"additionalProperties":false}`)
	const text = "标题：固定示例"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			ResponseFormat struct {
				Type       string `json:"type"`
				JSONSchema struct {
					Name   string          `json:"name"`
					Strict bool            `json:"strict"`
					Schema json.RawMessage `json:"schema"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		wantSystem := "Extract only explicitly provided metadata into the requested JSON schema. Do not invent missing values. Return JSON only.\nJSON schema:\n" + string(schema)
		if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[0].Content != wantSystem || req.Messages[1].Role != "user" || req.Messages[1].Content != text {
			t.Error("schema instructions and original text must be separate messages")
		}
		format := req.ResponseFormat
		if format.Type != "json_schema" || format.JSONSchema.Name != "metadata" || !format.JSONSchema.Strict || string(format.JSONSchema.Schema) != string(schema) {
			t.Error("strict response format must receive the same schema")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": `{"title":"固定示例"}`}}}})
	}))
	defer server.Close()
	if _, err := NewClient().Infer(context.Background(), server.URL, credentials.NewSecret(""), "exact-model-id", text, schema); err != nil {
		t.Fatal(err)
	}
}
