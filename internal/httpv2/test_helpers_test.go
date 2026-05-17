package httpv2

import (
	"encoding/json"
	"testing"
)

func assertErrorResponseShape(t *testing.T, body []byte) {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	assertExactJSONKeys(t, payload, "error")
	assertJSONValueType(t, payload, "error", "string")
}

func assertExactJSONKeys(t *testing.T, payload map[string]any, expected ...string) {
	t.Helper()

	if len(payload) != len(expected) {
		t.Fatalf("unexpected key count: got=%d expected=%d payload=%v", len(payload), len(expected), payload)
	}
	for _, key := range expected {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing expected key %q in payload=%v", key, payload)
		}
	}
}

func assertJSONValueType(t *testing.T, payload map[string]any, key string, expectedType string) {
	t.Helper()

	value, ok := payload[key]
	if !ok {
		t.Fatalf("missing key %q in payload=%v", key, payload)
	}

	switch expectedType {
	case "string":
		if _, ok := value.(string); !ok {
			t.Fatalf("expected key %q to be string, got %T (%#v)", key, value, value)
		}
	case "number":
		if _, ok := value.(float64); !ok {
			t.Fatalf("expected key %q to be number, got %T (%#v)", key, value, value)
		}
	case "bool":
		if _, ok := value.(bool); !ok {
			t.Fatalf("expected key %q to be bool, got %T (%#v)", key, value, value)
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			t.Fatalf("expected key %q to be object, got %T (%#v)", key, value, value)
		}
	case "array":
		if _, ok := value.([]any); !ok {
			t.Fatalf("expected key %q to be array, got %T (%#v)", key, value, value)
		}
	default:
		t.Fatalf("unsupported expected type assertion %q", expectedType)
	}
}
