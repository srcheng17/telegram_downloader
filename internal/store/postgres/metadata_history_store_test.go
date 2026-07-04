package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeMetadataHistoryRow struct {
	values []any
}

func (f fakeMetadataHistoryRow) Scan(dest ...any) error {
	*dest[0].(*string) = f.values[0].(string)
	*dest[1].(**string) = cloneMetadataHistoryString(f.values[1].(*string))
	*dest[2].(**string) = cloneMetadataHistoryString(f.values[2].(*string))
	*dest[3].(**string) = cloneMetadataHistoryString(f.values[3].(*string))
	*dest[4].(**string) = cloneMetadataHistoryString(f.values[4].(*string))
	*dest[5].(**string) = cloneMetadataHistoryString(f.values[5].(*string))
	*dest[6].(**string) = cloneMetadataHistoryString(f.values[6].(*string))
	*dest[7].(**string) = cloneMetadataHistoryString(f.values[7].(*string))
	*dest[8].(**string) = cloneMetadataHistoryString(f.values[8].(*string))
	*dest[9].(*time.Time) = f.values[9].(time.Time)
	return nil
}

func TestScanMetadataHistoryEntryPreservesTaskTypeAndURL(t *testing.T) {
	now := time.Unix(1700000123, 0).UTC()
	row := fakeMetadataHistoryRow{
		values: []any{
			"upload",
			stringPtr("https://telegra.ph/demo"),
			stringPtr("作者A"),
			stringPtr("系列B"),
			stringPtr("3"),
			stringPtr("漫画C"),
			stringPtr("简介D"),
			stringPtr("tag1,tag2"),
			stringPtr("genre1"),
			now,
		},
	}

	entry, err := scanMetadataHistoryEntry(row)
	if err != nil {
		t.Fatalf("scan metadata history entry: %v", err)
	}
	if entry.TaskType != "upload" {
		t.Fatalf("expected task_type upload, got %q", entry.TaskType)
	}
	if entry.URL == nil || *entry.URL != "https://telegra.ph/demo" {
		t.Fatalf("expected url preserved, got %#v", entry.URL)
	}
	if entry.Author == nil || *entry.Author != "作者A" {
		t.Fatalf("expected author preserved, got %#v", entry.Author)
	}
	if entry.SeriesName == nil || *entry.SeriesName != "系列B" {
		t.Fatalf("expected series_name preserved, got %#v", entry.SeriesName)
	}
	if entry.SeriesNumber == nil || *entry.SeriesNumber != "3" {
		t.Fatalf("expected series_number preserved, got %#v", entry.SeriesNumber)
	}
	if !entry.CreatedAt.Equal(now) {
		t.Fatalf("expected created_at %v, got %v", now, entry.CreatedAt)
	}
}

func TestInsertMetadataHistoryUsesConflictGuard(t *testing.T) {
	executor := &recordingMetadataHistoryExecutor{}
	store := &UploadTaskStore{db: executor}

	err := store.InsertMetadataHistory(context.Background(), MetadataHistoryEntry{
		TaskType:   "url",
		URL:        stringPtr("https://telegra.ph/demo"),
		Author:     stringPtr("作者A"),
		SeriesName: stringPtr("系列B"),
		ComicName:  stringPtr("漫画C"),
	})

	if err != nil {
		t.Fatalf("insert metadata history: %v", err)
	}
	if !strings.Contains(executor.query, "ON CONFLICT DO NOTHING") {
		t.Fatalf("expected duplicate conflict guard in insert query, got %s", executor.query)
	}
}

func cloneMetadataHistoryString(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

type recordingMetadataHistoryExecutor struct {
	query string
}

func (r *recordingMetadataHistoryExecutor) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	r.query = sql
	return pgconn.CommandTag{}, nil
}

func (r *recordingMetadataHistoryExecutor) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, nil
}

func (r *recordingMetadataHistoryExecutor) QueryRow(context.Context, string, ...any) pgx.Row {
	return nil
}

func (r *recordingMetadataHistoryExecutor) Begin(context.Context) (pgx.Tx, error) {
	return nil, nil
}
