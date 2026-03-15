package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteAPIErrorResponse_Validation(t *testing.T) {
	rec := httptest.NewRecorder()
	writeAPIErrorResponse(rec, http.StatusBadRequest, "validation_error", "invalid url", map[string]any{"field": "url"})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	payload := decodeJSONResponse(t, rec.Body.Bytes())
	assertExactJSONKeys(t, payload, "error", "code", "message", "details")

	if payload["error"] != "invalid url" {
		t.Fatalf("expected error=invalid url, got %#v", payload["error"])
	}
	if payload["code"] != "validation_error" {
		t.Fatalf("expected code=validation_error, got %#v", payload["code"])
	}
	if payload["message"] != "invalid url" {
		t.Fatalf("expected message=invalid url, got %#v", payload["message"])
	}

	details, ok := payload["details"].(map[string]any)
	if !ok {
		t.Fatalf("expected details object, got %#v", payload["details"])
	}
	if details["field"] != "url" {
		t.Fatalf("expected details.field=url, got %#v", details["field"])
	}
}

func TestWriteAPIErrorResponse_InternalFallbackCode(t *testing.T) {
	rec := httptest.NewRecorder()
	writeAPIErrorResponse(rec, http.StatusInternalServerError, "", "", nil)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}

	payload := decodeJSONResponse(t, rec.Body.Bytes())
	assertExactJSONKeys(t, payload, "error", "code", "message")

	if payload["error"] != "internal server error" {
		t.Fatalf("expected internal error message, got %#v", payload["error"])
	}
	if payload["code"] != "internal_error" {
		t.Fatalf("expected code=internal_error, got %#v", payload["code"])
	}
	if payload["message"] != "internal server error" {
		t.Fatalf("expected message=internal server error, got %#v", payload["message"])
	}
}
