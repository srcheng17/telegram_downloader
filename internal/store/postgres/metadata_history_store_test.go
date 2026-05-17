package postgres

import (
	"testing"
	"time"
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
	*dest[8].(*time.Time) = f.values[8].(time.Time)
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
	if !entry.CreatedAt.Equal(now) {
		t.Fatalf("expected created_at %v, got %v", now, entry.CreatedAt)
	}
}

func cloneMetadataHistoryString(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
