package httpv2

import (
	"testing"
	"time"
)

type fakeTaskScanRow struct {
	values []any
}

func (f fakeTaskScanRow) Scan(dest ...any) error {
	*dest[0].(*string) = f.values[0].(string)
	*dest[1].(*string) = f.values[1].(string)
	*dest[2].(**string) = optionalTaskString(f.values[2])
	*dest[3].(*string) = f.values[3].(string)
	*dest[4].(**string) = optionalTaskString(f.values[4])
	*dest[5].(**string) = optionalTaskString(f.values[5])
	*dest[6].(**string) = optionalTaskString(f.values[6])
	*dest[7].(*int64) = f.values[7].(int64)
	*dest[8].(*int64) = f.values[8].(int64)
	*dest[9].(*bool) = f.values[9].(bool)
	*dest[10].(**string) = optionalTaskString(f.values[10])
	*dest[11].(**string) = optionalTaskString(f.values[11])
	*dest[12].(**string) = optionalTaskString(f.values[12])
	*dest[13].(**string) = optionalTaskString(f.values[13])
	*dest[14].(**string) = optionalTaskString(f.values[14])
	*dest[15].(**string) = optionalTaskString(f.values[15])
	*dest[16].(**string) = optionalTaskString(f.values[16])
	*dest[17].(**string) = optionalTaskString(f.values[17])
	*dest[18].(**string) = cloneTaskString(f.values[18].(*string))
	*dest[19].(**string) = cloneTaskString(f.values[19].(*string))
	*dest[20].(*time.Time) = f.values[20].(time.Time)
	*dest[21].(*time.Time) = f.values[21].(time.Time)
	return nil
}

func TestScanTaskRowPreservesUploadFields(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	row := fakeTaskScanRow{
		values: []any{
			"task-upload-1",
			"",
			stringPtr(""),
			TaskStatusUploading,
			stringPtr("upload"),
			stringPtr("/tmp/source/demo.7z"),
			stringPtr("demo.7z"),
			int64(12),
			int64(40),
			true,
			nil,
			nil,
			stringPtr("作者A"),
			stringPtr("系列B"),
			stringPtr("漫画C"),
			stringPtr("简介D"),
			stringPtr("tag1,tag2"),
			stringPtr("tag1,tag2"),
			stringPtr("genre1"),
			stringPtr("genre1"),
			now,
			now,
		},
	}

	task, err := scanTaskRow(row)
	if err != nil {
		t.Fatalf("scan task row: %v", err)
	}
	if task.TaskType == nil || *task.TaskType != "upload" {
		t.Fatalf("expected task_type upload, got %#v", task.TaskType)
	}
	if task.SourceArchiveName == nil || *task.SourceArchiveName != "demo.7z" {
		t.Fatalf("expected source_archive_name demo.7z, got %#v", task.SourceArchiveName)
	}
	if task.SourceArchivePath == nil || *task.SourceArchivePath != "/tmp/source/demo.7z" {
		t.Fatalf("expected source_archive_path preserved, got %#v", task.SourceArchivePath)
	}
	if task.UploadLoadedBytes != 12 || task.UploadTotalBytes != 40 {
		t.Fatalf("expected upload bytes 12/40, got %d/%d", task.UploadLoadedBytes, task.UploadTotalBytes)
	}
	if !task.Retryable {
		t.Fatalf("expected retryable=true")
	}
	if task.Author == nil || *task.Author != "作者A" {
		t.Fatalf("expected author preserved, got %#v", task.Author)
	}
}

func cloneTaskString(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func optionalTaskString(value any) *string {
	if value == nil {
		return nil
	}
	typed, ok := value.(*string)
	if !ok {
		return nil
	}
	return cloneTaskString(typed)
}
