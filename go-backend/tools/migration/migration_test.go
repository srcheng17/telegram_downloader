package migration

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeLegacyReader struct {
	tasks []LegacyTask
}

func (f *fakeLegacyReader) CountLegacyTasks(_ context.Context) (int, error) {
	return len(f.tasks), nil
}

func (f *fakeLegacyReader) ReadLegacyTasks(_ context.Context, offset, limit int) ([]LegacyTask, error) {
	if offset >= len(f.tasks) {
		return []LegacyTask{}, nil
	}
	end := offset + limit
	if end > len(f.tasks) {
		end = len(f.tasks)
	}
	out := make([]LegacyTask, 0, end-offset)
	out = append(out, f.tasks[offset:end]...)
	return out, nil
}

type fakeV2Writer struct {
	tasksByID map[string]V2Task
	events    []V2TaskEvent
}

func newFakeV2Writer() *fakeV2Writer {
	return &fakeV2Writer{
		tasksByID: map[string]V2Task{},
		events:    make([]V2TaskEvent, 0),
	}
}

func (f *fakeV2Writer) WriteV2Batch(_ context.Context, tasks []V2Task, events []V2TaskEvent) error {
	for _, task := range tasks {
		f.tasksByID[task.ID] = task
	}
	f.events = append(f.events, events...)
	return nil
}

func (f *fakeV2Writer) CountV2Tasks(_ context.Context) (int, error) {
	return len(f.tasksByID), nil
}

func (f *fakeV2Writer) CountV2Events(_ context.Context) (int, error) {
	return len(f.events), nil
}

func TestMigrationCopiesAllTasksAndEvents(t *testing.T) {
	reader := &fakeLegacyReader{
		tasks: []LegacyTask{
			{
				ID:        "legacy-1",
				URL:       "https://telegra.ph/1",
				Status:    "PENDING",
				StartTime: 1700000001,
			},
			{
				ID:        "legacy-2",
				URL:       "https://telegra.ph/2",
				Status:    "SUCCESS",
				StartTime: 1700000002,
			},
			{
				ID:        "legacy-3",
				URL:       "https://telegra.ph/3",
				Status:    "WHATEVER",
				StartTime: 1700000003,
			},
		},
	}
	writer := newFakeV2Writer()

	migrator := Migrator{
		Reader:    reader,
		Writer:    writer,
		BatchSize: 2,
	}

	stats, err := migrator.Run(context.Background())
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if stats.LegacyTotal != 3 || stats.MigratedTasks != 3 || stats.V2TasksTotal != 3 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
	if stats.V2EventsTotal < 3 {
		t.Fatalf("expected at least 3 events, got %d", stats.V2EventsTotal)
	}

	if writer.tasksByID["legacy-1"].Status != "QUEUED" {
		t.Fatalf("expected PENDING => QUEUED, got %q", writer.tasksByID["legacy-1"].Status)
	}
	if writer.tasksByID["legacy-2"].Status != "SUCCESS" {
		t.Fatalf("expected SUCCESS => SUCCESS, got %q", writer.tasksByID["legacy-2"].Status)
	}
	if writer.tasksByID["legacy-3"].Status != "FAILED" {
		t.Fatalf("expected unknown => FAILED, got %q", writer.tasksByID["legacy-3"].Status)
	}
	if writer.tasksByID["legacy-1"].CreatedAt.IsZero() {
		t.Fatal("expected created_at mapped from legacy start_time")
	}
}

func TestValidateMigrationCounts(t *testing.T) {
	if err := ValidateMigrationCounts(2, 2, 2); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}

	if err := ValidateMigrationCounts(2, 1, 1); err == nil {
		t.Fatal("expected mismatch error")
	}
	if err := ValidateMigrationCounts(2, 2, 1); err == nil {
		t.Fatal("expected events lower bound error")
	}
}

func TestMapLegacyStatus(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "PENDING", want: "QUEUED"},
		{in: "IN_PROGRESS", want: "RUNNING"},
		{in: "CANCEL_REQUESTED", want: "RUNNING"},
		{in: "SUCCESS", want: "SUCCESS"},
		{in: "FAILED", want: "FAILED"},
		{in: "CANCELED", want: "CANCELED"},
		{in: "  unknown  ", want: "FAILED"},
	}

	for _, tc := range cases {
		got := mapLegacyStatus(tc.in)
		if got != tc.want {
			t.Fatalf("mapLegacyStatus(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTimeFromStartTime(t *testing.T) {
	got := timeFromStartTime(1700000000.5)
	want := time.Unix(1700000000, 500000000).UTC()
	if !got.Equal(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

type brokenLegacyReader struct{}

func (b *brokenLegacyReader) CountLegacyTasks(context.Context) (int, error) {
	return 0, errors.New("count failed")
}

func (b *brokenLegacyReader) ReadLegacyTasks(context.Context, int, int) ([]LegacyTask, error) {
	return nil, errors.New("read failed")
}

func TestMigratorReturnsReaderError(t *testing.T) {
	migrator := Migrator{
		Reader:    &brokenLegacyReader{},
		Writer:    newFakeV2Writer(),
		BatchSize: 10,
	}

	if _, err := migrator.Run(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}
