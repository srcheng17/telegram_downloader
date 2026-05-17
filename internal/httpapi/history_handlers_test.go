package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ryancheng/telegram-downloader/internal/store/postgres"
)

func TestHandleMetadataHistoryReturnsLatestTwenty(t *testing.T) {
	now := time.Unix(1700000123, 0).UTC()
	store := &fakeMetadataHistoryStore{
		entries: []postgres.MetadataHistoryEntry{
			{
				TaskType:   "url",
				URL:        stringPtr("https://telegra.ph/demo"),
				Author:     stringPtr("作者A"),
				SeriesName: stringPtr("系列B"),
				ComicName:  stringPtr("漫画C"),
				CreatedAt:  now,
			},
		},
	}
	handler := NewRouterWithOptions(&fakeTaskReader{}, RouterOptions{
		UploadTaskStore: store,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/metadata-history?limit=20", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}

	var payload []postgres.MetadataHistoryEntry
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode metadata history response: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("expected one history entry, got %d", len(payload))
	}
	if payload[0].TaskType != "url" {
		t.Fatalf("expected task_type url, got %q", payload[0].TaskType)
	}
	if payload[0].URL == nil || *payload[0].URL != "https://telegra.ph/demo" {
		t.Fatalf("expected url preserved, got %#v", payload[0].URL)
	}
}

type fakeMetadataHistoryStore struct {
	entries []postgres.MetadataHistoryEntry
}

func (f *fakeMetadataHistoryStore) CreateUploadTask(context.Context, postgres.CreateTaskInput) (postgres.TaskRecord, error) {
	return postgres.TaskRecord{}, nil
}

func (f *fakeMetadataHistoryStore) GetTask(context.Context, string) (*postgres.TaskRecord, error) {
	return nil, nil
}

func (f *fakeMetadataHistoryStore) UpdateUploadProgress(context.Context, string, int64, int64) error {
	return nil
}

func (f *fakeMetadataHistoryStore) MarkUploadTaskQueued(context.Context, string, string, int64) error {
	return nil
}

func (f *fakeMetadataHistoryStore) MarkTaskFailed(context.Context, string, string) error {
	return nil
}

func (f *fakeMetadataHistoryStore) RetryUploadTask(context.Context, string, string) error {
	return nil
}

func (f *fakeMetadataHistoryStore) InsertMetadataHistory(context.Context, postgres.MetadataHistoryEntry) error {
	return nil
}

func (f *fakeMetadataHistoryStore) ListMetadataHistory(context.Context, int) ([]postgres.MetadataHistoryEntry, error) {
	return append([]postgres.MetadataHistoryEntry(nil), f.entries...), nil
}
