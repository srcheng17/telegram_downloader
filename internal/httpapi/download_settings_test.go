package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/ryancheng/telegram-downloader/internal/config"
)

type fakeVersionedSettings struct {
	current config.VersionedSettingsSnapshot
}

func (f *fakeVersionedSettings) GetDownloadSettings(context.Context) (config.VersionedSettingsSnapshot, error) {
	return f.current, nil
}
func (f *fakeVersionedSettings) UpdateDownloadSettings(_ context.Context, value config.SettingsSnapshot, expected int64) (config.VersionedSettingsSnapshot, error) {
	if config.NormalizeSettingsSnapshot(value) != value {
		return config.VersionedSettingsSnapshot{}, config.ErrInvalidSettings
	}
	if expected != f.current.ConfigVersion {
		return config.VersionedSettingsSnapshot{}, config.ErrSettingsConflict
	}
	f.current.SettingsSnapshot = value
	f.current.ConfigVersion++
	return f.current, nil
}

func TestDownloadSettingsCASAndValidation(t *testing.T) {
	store := &fakeVersionedSettings{current: config.VersionedSettingsSnapshot{SettingsSnapshot: config.DefaultSettingsSnapshot(), ConfigVersion: 2}}
	r := chi.NewRouter()
	NewDownloadSettingsHandler(store).RegisterRoutes(r)
	get := httptest.NewRecorder()
	r.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/settings/download", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", get.Code, get.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(get.Body.Bytes(), &got); err != nil || got["config_version"] != float64(2) || got["timeout"] != float64(30) {
		t.Fatalf("GET payload=%v err=%v", got, err)
	}
	for _, testcase := range []struct {
		body string
		want int
	}{
		{`{"expected_version":1,"timeout":45,"retries":3,"image_concurrency":2,"download_action_mode":"browser"}`, http.StatusConflict},
		{`{"expected_version":2,"timeout":0,"retries":3,"image_concurrency":2,"download_action_mode":"browser"}`, http.StatusUnprocessableEntity},
		{`{"expected_version":2,"timeout":45,"retries":3,"image_concurrency":2,"download_action_mode":"browser"}`, http.StatusOK},
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/settings/download", strings.NewReader(testcase.body)))
		if rec.Code != testcase.want {
			t.Fatalf("PUT status=%d want=%d body=%s", rec.Code, testcase.want, rec.Body.String())
		}
	}
	if store.current.ConfigVersion != 3 || store.current.Timeout != 45 {
		t.Fatalf("settings lost: %#v", store.current)
	}
}
