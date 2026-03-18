package httpv2

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ryancheng/telegram-downloader/internal/config"
)

type fakeSettingsStore struct {
	getSnapshot config.SettingsSnapshot
	getErr      error

	updateCalls    []config.SettingsSnapshot
	updateSnapshot config.SettingsSnapshot
	updateErr      error
}

func (f *fakeSettingsStore) GetSettings(_ context.Context) (config.SettingsSnapshot, error) {
	if f.getErr != nil {
		return config.SettingsSnapshot{}, f.getErr
	}
	return f.getSnapshot, nil
}

func (f *fakeSettingsStore) UpdateSettings(_ context.Context, snapshot config.SettingsSnapshot) (config.SettingsSnapshot, error) {
	f.updateCalls = append(f.updateCalls, snapshot)
	if f.updateErr != nil {
		return config.SettingsSnapshot{}, f.updateErr
	}
	if f.updateSnapshot.Timeout != 0 || f.updateSnapshot.Retries != 0 || f.updateSnapshot.ImageConcurrency != 0 || f.updateSnapshot.DownloadActionMode != "" {
		return f.updateSnapshot, nil
	}
	return snapshot, nil
}

func TestUpdateSettingsPersistsAndReturnsSnapshot(t *testing.T) {
	store := &fakeSettingsStore{
		updateSnapshot: config.SettingsSnapshot{
			Timeout:            300,
			Retries:            0,
			ImageConcurrency:   20,
			DownloadActionMode: "komga_copy",
		},
	}

	r := chi.NewRouter()
	RegisterSettingsRoutes(r, NewSettingsHandler(store))

	req := httptest.NewRequest(
		http.MethodPut,
		"/v2/settings",
		bytes.NewBufferString(`{"timeout":999,"retries":-3,"image_concurrency":88,"download_action_mode":"komga_copy"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	r.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(store.updateCalls) != 1 {
		t.Fatalf("expected exactly 1 update call, got %d", len(store.updateCalls))
	}

	if store.updateCalls[0].Timeout != 300 || store.updateCalls[0].Retries != 0 || store.updateCalls[0].ImageConcurrency != 20 || store.updateCalls[0].DownloadActionMode != "komga_copy" {
		t.Fatalf("expected normalized snapshot {300,0,20,komga_copy}, got %#v", store.updateCalls[0])
	}

	var payload config.SettingsSnapshot
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Timeout != 300 || payload.Retries != 0 || payload.ImageConcurrency != 20 || payload.DownloadActionMode != "komga_copy" {
		t.Fatalf("expected response snapshot {300,0,20,komga_copy}, got %#v", payload)
	}

	var payloadMap map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payloadMap); err != nil {
		t.Fatalf("decode response as map: %v", err)
	}
	assertExactJSONKeys(t, payloadMap, "timeout", "retries", "image_concurrency", "download_action_mode")
	assertJSONValueType(t, payloadMap, "timeout", "number")
	assertJSONValueType(t, payloadMap, "retries", "number")
	assertJSONValueType(t, payloadMap, "image_concurrency", "number")
	assertJSONValueType(t, payloadMap, "download_action_mode", "string")
}

func TestGetSettingsReturnsSnapshot(t *testing.T) {
	store := &fakeSettingsStore{
		getSnapshot: config.SettingsSnapshot{
			Timeout:            66,
			Retries:            7,
			ImageConcurrency:   5,
			DownloadActionMode: "komga_copy",
		},
	}

	r := chi.NewRouter()
	RegisterSettingsRoutes(r, NewSettingsHandler(store))

	req := httptest.NewRequest(http.MethodGet, "/v2/settings", nil)
	recorder := httptest.NewRecorder()

	r.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload config.SettingsSnapshot
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Timeout != 66 || payload.Retries != 7 || payload.ImageConcurrency != 5 || payload.DownloadActionMode != "komga_copy" {
		t.Fatalf("unexpected snapshot %#v", payload)
	}

	var payloadMap map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payloadMap); err != nil {
		t.Fatalf("decode response as map: %v", err)
	}
	assertExactJSONKeys(t, payloadMap, "timeout", "retries", "image_concurrency", "download_action_mode")
}

func TestUpdateSettingsReturns400OnBadJSON(t *testing.T) {
	store := &fakeSettingsStore{}

	r := chi.NewRouter()
	RegisterSettingsRoutes(r, NewSettingsHandler(store))

	req := httptest.NewRequest(http.MethodPut, "/v2/settings", bytes.NewBufferString("{"))
	recorder := httptest.NewRecorder()

	r.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	assertErrorResponseShape(t, recorder.Body.Bytes())
}

func TestUpdateSettingsReturns400WhenMissingField(t *testing.T) {
	store := &fakeSettingsStore{}

	r := chi.NewRouter()
	RegisterSettingsRoutes(r, NewSettingsHandler(store))

	req := httptest.NewRequest(
		http.MethodPut,
		"/v2/settings",
		bytes.NewBufferString(`{"timeout":11,"retries":3,"download_action_mode":"browser"}`),
	)
	recorder := httptest.NewRecorder()

	r.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(store.updateCalls) != 0 {
		t.Fatalf("expected no update calls, got %d", len(store.updateCalls))
	}
	assertErrorResponseShape(t, recorder.Body.Bytes())
}

func TestUpdateSettingsReturns400OnUnknownField(t *testing.T) {
	store := &fakeSettingsStore{}

	r := chi.NewRouter()
	RegisterSettingsRoutes(r, NewSettingsHandler(store))

	req := httptest.NewRequest(
		http.MethodPut,
		"/v2/settings",
		bytes.NewBufferString(`{"timeout":11,"retries":3,"image_concurrency":2,"download_action_mode":"browser","extra":1}`),
	)
	recorder := httptest.NewRecorder()

	r.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	assertErrorResponseShape(t, recorder.Body.Bytes())
}

func TestGetSettingsReturns500OnStoreError(t *testing.T) {
	store := &fakeSettingsStore{getErr: errors.New("db down")}

	r := chi.NewRouter()
	RegisterSettingsRoutes(r, NewSettingsHandler(store))

	req := httptest.NewRequest(http.MethodGet, "/v2/settings", nil)
	recorder := httptest.NewRecorder()

	r.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	assertErrorResponseShape(t, recorder.Body.Bytes())
}

func TestUpdateSettingsReturns400WhenDownloadActionModeMissing(t *testing.T) {
	store := &fakeSettingsStore{}

	r := chi.NewRouter()
	RegisterSettingsRoutes(r, NewSettingsHandler(store))

	req := httptest.NewRequest(
		http.MethodPut,
		"/v2/settings",
		bytes.NewBufferString(`{"timeout":11,"retries":3,"image_concurrency":2}`),
	)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	r.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(store.updateCalls) != 0 {
		t.Fatalf("expected no update calls, got %d", len(store.updateCalls))
	}
	assertErrorResponseShape(t, recorder.Body.Bytes())
}
