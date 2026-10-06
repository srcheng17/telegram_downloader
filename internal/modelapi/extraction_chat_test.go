package modelapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/ryancheng/telegram-downloader/internal/credentials"
)

func chatResponse(budget int, content string) map[string]any {
	return map[string]any{
		"object": "chat.completion", "model": "selected-model", "system_fingerprint": "b11382-11fe02151",
		"choices":   []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": content}}},
		"usage":     map[string]any{"prompt_tokens": 29, "completion_tokens": 2, "total_tokens": 31},
		"__verbose": map[string]any{"model": "selected-model", "stop": true, "truncated": false, "stop_type": "eos", "tokens_evaluated": 29, "tokens_predicted": 2, "generation_settings": map[string]any{"n_predict": budget, "stream": false}},
	}
}

func TestLlamaCPPChatUsesSelectedRouteAndVerifiedResponseBudget(t *testing.T) {
	const prompt = "  INPUT_DATA_JSON:\n{\"text\":\"标题：固定示例。\"}  "
	schema := json.RawMessage(`{"type":"object","properties":{"fields":{"type":"object"}},"required":["fields"],"additionalProperties":false}`)
	const output = `{"fields":{"title":{"value":"固定示例。","evidence_quote":"固定示例。"}}}`
	chatCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-private-key" {
			t.Error("credential did not stay on the selected route")
		}
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"another-model"},{"id":"selected-model"}]}`))
		case "/v1/chat/completions":
			chatCalls++
			var body struct {
				Model          string                           `json:"model"`
				Messages       []struct{ Role, Content string } `json:"messages"`
				Budget         int                              `json:"max_tokens"`
				Verbose        bool                             `json:"verbose"`
				Stream         bool                             `json:"stream"`
				Thinking       map[string]bool                  `json:"chat_template_kwargs"`
				ResponseFields []string                         `json:"response_fields"`
				Format         struct {
					Type   string `json:"type"`
					Schema struct {
						Strict bool            `json:"strict"`
						Schema json.RawMessage `json:"schema"`
					} `json:"json_schema"`
				} `json:"response_format"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				t.Error("invalid request body")
				return
			}
			if body.Model != "selected-model" || !body.Verbose || body.Stream || len(body.Messages) != 2 || body.Messages[0].Role != "system" || body.Messages[1].Role != "user" || body.Format.Type != "json_schema" || !body.Format.Schema.Strict {
				t.Error("chat request lost its bounded schema contract")
				return
			}
			if thinking, ok := body.Thinking["enable_thinking"]; !ok || thinking {
				t.Error("chat request must explicitly disable thinking")
			}
			if !reflect.DeepEqual(body.ResponseFields, []string{"model", "stop", "truncated", "stop_type", "tokens_evaluated", "tokens_predicted", "generation_settings"}) {
				t.Error("native debug prompt/content must not be requested")
			}
			response := chatResponse(body.Budget, "{}")
			if chatCalls == 1 {
				if body.Budget != chatProbeBudget || strings.Contains(body.Messages[1].Content, prompt) {
					t.Error("capability probe must use only fixed neutral text")
				}
			} else {
				if body.Budget != 128 || body.Messages[1].Content != prompt || !strings.Contains(body.Messages[0].Content, string(schema)) || string(body.Format.Schema.Schema) != string(schema) {
					t.Error("actual prompt/schema/budget were altered")
				}
				response = chatResponse(body.Budget, output)
			}
			_ = json.NewEncoder(w).Encode(response)
		default:
			t.Errorf("chat protocol called a native or unexpected endpoint: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := NewClient()
	key := credentials.NewSecret("synthetic-private-key")
	capability, err := client.LlamaCPPChatCapability(context.Background(), srv.URL+"/v1", key, "selected-model")
	if err != nil {
		t.Fatal(err)
	}
	if capability.ContextTokens != 0 || capability.Protocol != ProtocolLlamaCPPChat || capability.BudgetMode != BudgetModeVerifiedResponse || capability.Fingerprint == "" {
		t.Fatal("chat capability must not claim measured context or exact token budgeting")
	}
	got, err := client.ExtractLlamaCPPChat(context.Background(), srv.URL+"/v1", key, "selected-model", prompt, schema, 128, capability)
	if err != nil || string(got) != output || chatCalls != 2 {
		t.Fatalf("chat completion failed: calls=%d error=%v", chatCalls, err)
	}
}

func TestLlamaCPPChatRejectsMissingModelsBeforeProbe(t *testing.T) {
	for _, models := range []string{`{"data":[]}`, `{"data":[{"id":"other"}]}`, `{"data":[{"id":"selected-model","type":"image"}]}`} {
		called := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/models" {
				called = true
			}
			_, _ = w.Write([]byte(models))
		}))
		_, err := NewClient().LlamaCPPChatCapability(context.Background(), srv.URL, credentials.NewSecret(""), "selected-model")
		srv.Close()
		assertCode(t, err, "schema_unsupported")
		if called {
			t.Fatal("unavailable selected model received a probe")
		}
	}
}

func TestLlamaCPPChatRejectsIncompleteOrInconsistentTelemetry(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		mutate     func(map[string]any, map[string]any, map[string]any, map[string]any)
	}{
		{"generic OpenAI", "schema_unsupported", func(r, _, _, _ map[string]any) { delete(r, "__verbose") }},
		{"wrong build", "schema_unsupported", func(r, _, _, _ map[string]any) { r["system_fingerprint"] = "b11382-deadbeef" }},
		{"wrong model", "invalid_response", func(r, _, _, _ map[string]any) { r["model"] = "other" }},
		{"wrong native model", "invalid_response", func(_, n, _, _ map[string]any) { n["model"] = "other" }},
		{"missing native final flag", "invalid_response", func(_, n, _, _ map[string]any) { delete(n, "stop") }},
		{"null truncation flag", "invalid_response", func(_, n, _, _ map[string]any) { n["truncated"] = nil }},
		{"shifted context", "invalid_response", func(_, n, _, _ map[string]any) { n["truncated"] = true }},
		{"native output incomplete", "invalid_response", func(_, n, _, _ map[string]any) { n["stop_type"] = "limit" }},
		{"word stop", "invalid_response", func(_, n, _, _ map[string]any) { n["stop_type"] = "word" }},
		{"missing usage", "invalid_response", func(r, _, _, _ map[string]any) { delete(r, "usage") }},
		{"missing evaluated tokens", "invalid_response", func(_, n, _, _ map[string]any) { delete(n, "tokens_evaluated") }},
		{"inconsistent evaluated tokens", "invalid_response", func(_, n, _, _ map[string]any) { n["tokens_evaluated"] = 28 }},
		{"inconsistent output tokens", "invalid_response", func(_, n, _, _ map[string]any) { n["tokens_predicted"] = 3 }},
		{"inconsistent total tokens", "invalid_response", func(_, _, u, _ map[string]any) { u["total_tokens"] = 30 }},
		{"negative prompt tokens", "invalid_response", func(_, _, u, _ map[string]any) { u["prompt_tokens"] = -1 }},
		{"missing output tokens", "invalid_response", func(_, _, u, _ map[string]any) { delete(u, "completion_tokens") }},
		{"over budget", "invalid_response", func(_, n, u, _ map[string]any) {
			n["tokens_predicted"] = 129
			u["completion_tokens"] = 129
			u["total_tokens"] = 158
		}},
		{"budget clamped", "invalid_response", func(_, n, _, _ map[string]any) { n["generation_settings"].(map[string]any)["n_predict"] = 64 }},
		{"stream changed", "invalid_response", func(_, n, _, _ map[string]any) { n["generation_settings"].(map[string]any)["stream"] = true }},
		{"chat output length", "invalid_response", func(_, _, _, c map[string]any) { c["finish_reason"] = "length" }},
		{"tool call", "invalid_response", func(_, _, _, c map[string]any) {
			c["message"].(map[string]any)["tool_calls"] = []any{map[string]any{"id": "synthetic"}}
		}},
		{"refusal", "refused", func(_, _, _, c map[string]any) { c["message"].(map[string]any)["refusal"] = "private refusal sentinel" }},
		{"incomplete JSON", "invalid_response", func(_, _, _, c map[string]any) { c["message"].(map[string]any)["content"] = `{"fields":` }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := chatResponse(128, `{"fields":{}}`)
			tc.mutate(response, response["__verbose"].(map[string]any), response["usage"].(map[string]any), response["choices"].([]any)[0].(map[string]any))
			data, _ := json.Marshal(response)
			_, _, err := validateLlamaCPPChatResponse(data, "selected-model", 128)
			assertCode(t, err, tc.code)
			if strings.Contains(err.Error(), "sentinel") {
				t.Fatal("private response content leaked")
			}
		})
	}
}

func TestLlamaCPPChatRejectsCapabilityDrift(t *testing.T) {
	probe, _ := json.Marshal(chatResponse(32, "{}"))
	_, fingerprint, err := validateLlamaCPPChatResponse(probe, "selected-model", 32)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		response := chatResponse(128, `{"fields":{}}`)
		response["system_fingerprint"] = "b11383-11fe02151"
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()
	client := NewClient()
	capability := ExtractionCapability{Protocol: ProtocolLlamaCPPChat, BudgetMode: BudgetModeVerifiedResponse, Fingerprint: fingerprint}
	_, err = client.ExtractLlamaCPPChat(context.Background(), srv.URL, credentials.NewSecret(""), "selected-model", "text", json.RawMessage(`{}`), 128, capability)
	assertCode(t, err, "config_changed")
	capability.Protocol = ProtocolLlamaCPPNative
	_, err = client.ExtractLlamaCPPChat(context.Background(), srv.URL, credentials.NewSecret(""), "selected-model", "must not send", json.RawMessage(`{}`), 128, capability)
	assertCode(t, err, "schema_unsupported")
	if calls != 1 {
		t.Fatal("native capability was reused for chat")
	}
}
