package migration

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"
)

type fakeLegacyReader struct {
	tasks []LegacyTask

	requestedLastIDs []string
}

func (f *fakeLegacyReader) CountLegacyTasks(_ context.Context) (int, error) {
	return len(f.tasks), nil
}

func (f *fakeLegacyReader) ReadLegacyTasksAfterID(_ context.Context, lastID string, limit int) ([]LegacyTask, error) {
	f.requestedLastIDs = append(f.requestedLastIDs, strings.TrimSpace(lastID))

	if limit <= 0 {
		limit = 1
	}

	sorted := make([]LegacyTask, 0, len(f.tasks))
	sorted = append(sorted, f.tasks...)
	sort.Slice(sorted, func(i, j int) bool {
		return strings.TrimSpace(sorted[i].ID) < strings.TrimSpace(sorted[j].ID)
	})

	out := make([]LegacyTask, 0, limit)
	for _, task := range sorted {
		id := strings.TrimSpace(task.ID)
		if strings.TrimSpace(lastID) != "" && id <= strings.TrimSpace(lastID) {
			continue
		}
		out = append(out, task)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

type fakeV2Writer struct {
	tasksByID         map[string]V2Task
	migratedByTaskID  map[string]V2TaskEvent
	otherEvents       []V2TaskEvent
	taskHashOverride  string
	eventHashOverride string
}

func newFakeV2Writer() *fakeV2Writer {
	return &fakeV2Writer{
		tasksByID:        map[string]V2Task{},
		migratedByTaskID: map[string]V2TaskEvent{},
		otherEvents:      make([]V2TaskEvent, 0),
	}
}

func (f *fakeV2Writer) WriteV2Batch(_ context.Context, tasks []V2Task, events []V2TaskEvent) error {
	for _, task := range tasks {
		f.tasksByID[strings.TrimSpace(task.ID)] = task
	}
	for _, event := range events {
		if strings.TrimSpace(event.EventType) == "MIGRATED" {
			taskID := strings.TrimSpace(event.TaskID)
			if _, exists := f.migratedByTaskID[taskID]; exists {
				continue
			}
			f.migratedByTaskID[taskID] = event
			continue
		}
		f.otherEvents = append(f.otherEvents, event)
	}
	return nil
}

func (f *fakeV2Writer) CountV2Tasks(_ context.Context) (int, error) {
	return len(f.tasksByID), nil
}

func (f *fakeV2Writer) CountV2MigratedEvents(_ context.Context) (int, error) {
	return len(f.migratedByTaskID), nil
}

func (f *fakeV2Writer) ChecksumV2Tasks(_ context.Context) (string, error) {
	if f.taskHashOverride != "" {
		return f.taskHashOverride, nil
	}

	ids := make([]string, 0, len(f.tasksByID))
	for id := range f.tasksByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	builder := newChecksumBuilder()
	for _, id := range ids {
		if err := builder.AddTask(f.tasksByID[id]); err != nil {
			return "", err
		}
	}
	return builder.SumHex(), nil
}

func (f *fakeV2Writer) ChecksumV2MigratedEvents(_ context.Context) (string, error) {
	if f.eventHashOverride != "" {
		return f.eventHashOverride, nil
	}

	ids := make([]string, 0, len(f.migratedByTaskID))
	for id := range f.migratedByTaskID {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	builder := newChecksumBuilder()
	for _, id := range ids {
		if err := builder.AddMigratedEvent(f.migratedByTaskID[id]); err != nil {
			return "", err
		}
	}
	return builder.SumHex(), nil
}

func TestMigrationCopiesAllTasksAndEvents(t *testing.T) {
	reader := &fakeLegacyReader{
		tasks: []LegacyTask{
			{
				ID:        "legacy-2",
				URL:       "https://telegra.ph/2",
				Status:    "SUCCESS",
				StartTime: 1700000002,
			},
			{
				ID:        "legacy-1",
				URL:       "https://telegra.ph/1",
				Status:    "PENDING",
				StartTime: 1700000001,
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
		t.Fatalf("unexpected task stats: %#v", stats)
	}
	if stats.LegacyMigratedEventsTotal != 3 || stats.V2MigratedEventsTotal != 3 {
		t.Fatalf("unexpected migrated event stats: %#v", stats)
	}
	if stats.LegacyTasksChecksum == "" || stats.V2TasksChecksum == "" {
		t.Fatalf("expected non-empty task checksums, got %#v", stats)
	}
	if stats.LegacyMigratedEventsChecksum == "" || stats.V2MigratedEventsChecksum == "" {
		t.Fatalf("expected non-empty event checksums, got %#v", stats)
	}
	if stats.LegacyTasksChecksum != stats.V2TasksChecksum {
		t.Fatalf("task checksum mismatch in stats: %#v", stats)
	}
	if stats.LegacyMigratedEventsChecksum != stats.V2MigratedEventsChecksum {
		t.Fatalf("event checksum mismatch in stats: %#v", stats)
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
	if len(reader.requestedLastIDs) < 2 {
		t.Fatalf("expected keyset paging calls, got %#v", reader.requestedLastIDs)
	}
	if reader.requestedLastIDs[0] != "" {
		t.Fatalf("expected first keyset cursor empty, got %q", reader.requestedLastIDs[0])
	}
	if reader.requestedLastIDs[1] != "legacy-2" {
		t.Fatalf("expected second keyset cursor legacy-2, got %q", reader.requestedLastIDs[1])
	}
}

func TestChecksumNormalizesEquivalentJSONPayload(t *testing.T) {
	a := newChecksumBuilder()
	b := newChecksumBuilder()

	err := a.AddMigratedEvent(V2TaskEvent{
		TaskID:      "task-1",
		EventType:   "MIGRATED",
		FromStatus:  stringPtr("PENDING"),
		ToStatus:    stringPtr("QUEUED"),
		PayloadJSON: `{"a":1,"b":2}`,
	})
	if err != nil {
		t.Fatalf("add event a: %v", err)
	}
	err = b.AddMigratedEvent(V2TaskEvent{
		TaskID:      "task-1",
		EventType:   "MIGRATED",
		FromStatus:  stringPtr("PENDING"),
		ToStatus:    stringPtr("QUEUED"),
		PayloadJSON: `{ "b": 2, "a": 1 }`,
	})
	if err != nil {
		t.Fatalf("add event b: %v", err)
	}

	if a.SumHex() != b.SumHex() {
		t.Fatalf("expected equivalent json payload checksum, got %s vs %s", a.SumHex(), b.SumHex())
	}
}

func TestMigrationIsIdempotent(t *testing.T) {
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
		},
	}
	writer := newFakeV2Writer()

	migrator := Migrator{
		Reader:    reader,
		Writer:    writer,
		BatchSize: 1,
	}

	first, err := migrator.Run(context.Background())
	if err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	second, err := migrator.Run(context.Background())
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	if second.V2MigratedEventsTotal != 2 {
		t.Fatalf("expected deduped migrated events total 2, got %d", second.V2MigratedEventsTotal)
	}
	if first.V2TasksChecksum != second.V2TasksChecksum {
		t.Fatalf("expected same task checksum across reruns, got %s vs %s", first.V2TasksChecksum, second.V2TasksChecksum)
	}
	if first.V2MigratedEventsChecksum != second.V2MigratedEventsChecksum {
		t.Fatalf("expected same event checksum across reruns, got %s vs %s", first.V2MigratedEventsChecksum, second.V2MigratedEventsChecksum)
	}
}

func TestMigrationFailsOnTaskChecksumMismatch(t *testing.T) {
	reader := &fakeLegacyReader{
		tasks: []LegacyTask{
			{
				ID:        "legacy-1",
				URL:       "https://telegra.ph/1",
				Status:    "PENDING",
				StartTime: 1700000001,
			},
		},
	}
	writer := newFakeV2Writer()
	writer.taskHashOverride = "bad-checksum"

	migrator := Migrator{
		Reader:    reader,
		Writer:    writer,
		BatchSize: 1,
	}

	_, err := migrator.Run(context.Background())
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if got := err.Error(); !strings.Contains(got, "tasks checksum mismatch") {
		t.Fatalf("expected tasks checksum mismatch error, got %q", got)
	}
}

func TestMigrationFailsOnMigratedEventChecksumMismatch(t *testing.T) {
	reader := &fakeLegacyReader{
		tasks: []LegacyTask{
			{
				ID:        "legacy-1",
				URL:       "https://telegra.ph/1",
				Status:    "PENDING",
				StartTime: 1700000001,
			},
		},
	}
	writer := newFakeV2Writer()
	writer.eventHashOverride = "bad-event-checksum"

	migrator := Migrator{
		Reader:    reader,
		Writer:    writer,
		BatchSize: 1,
	}

	_, err := migrator.Run(context.Background())
	if err == nil {
		t.Fatal("expected event checksum mismatch error")
	}
	if got := err.Error(); !strings.Contains(got, "migrated-event checksum mismatch") {
		t.Fatalf("expected event checksum mismatch error, got %q", got)
	}
}

func TestValidateMigrationChecksums(t *testing.T) {
	if err := ValidateMigrationChecksums(2, 2, "t", "t", 2, 2, "e", "e"); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}

	cases := []struct {
		name string
		err  error
	}{
		{
			name: "task-count-mismatch",
			err:  ValidateMigrationChecksums(2, 1, "t", "t", 2, 2, "e", "e"),
		},
		{
			name: "migrated-event-count-mismatch",
			err:  ValidateMigrationChecksums(2, 2, "t", "t", 2, 1, "e", "e"),
		},
		{
			name: "task-checksum-mismatch",
			err:  ValidateMigrationChecksums(2, 2, "a", "b", 2, 2, "e", "e"),
		},
		{
			name: "event-checksum-mismatch",
			err:  ValidateMigrationChecksums(2, 2, "t", "t", 2, 2, "a", "b"),
		},
	}
	for _, tc := range cases {
		if tc.err == nil {
			t.Fatalf("expected error for case %s", tc.name)
		}
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

func (b *brokenLegacyReader) ReadLegacyTasksAfterID(context.Context, string, int) ([]LegacyTask, error) {
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

type scriptedLegacyReader struct {
	total   int
	batches [][]LegacyTask
	calls   int
}

func (s *scriptedLegacyReader) CountLegacyTasks(context.Context) (int, error) {
	if s.total > 0 {
		return s.total, nil
	}
	total := 0
	for _, batch := range s.batches {
		total += len(batch)
	}
	return total, nil
}

func (s *scriptedLegacyReader) ReadLegacyTasksAfterID(context.Context, string, int) ([]LegacyTask, error) {
	if s.calls >= len(s.batches) {
		return nil, nil
	}
	batch := s.batches[s.calls]
	s.calls++
	return batch, nil
}

func TestMigratorFailsFastOnEmptyBatchCursor(t *testing.T) {
	reader := &scriptedLegacyReader{
		total: 1,
		batches: [][]LegacyTask{
			{
				{
					ID:        "   ",
					URL:       "https://telegra.ph/1",
					Status:    "PENDING",
					StartTime: 1700000001,
				},
			},
		},
	}
	migrator := Migrator{
		Reader:    reader,
		Writer:    newFakeV2Writer(),
		BatchSize: 1,
	}

	_, err := migrator.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for empty keyset cursor")
	}
	if !strings.Contains(err.Error(), "empty keyset cursor") {
		t.Fatalf("expected empty keyset cursor error, got %q", err.Error())
	}
}

func TestMigratorFailsFastWhenCursorDoesNotAdvance(t *testing.T) {
	reader := &scriptedLegacyReader{
		total: 2,
		batches: [][]LegacyTask{
			{
				{
					ID:        "legacy-1",
					URL:       "https://telegra.ph/1",
					Status:    "PENDING",
					StartTime: 1700000001,
				},
			},
			{
				{
					ID:        "legacy-1",
					URL:       "https://telegra.ph/1",
					Status:    "PENDING",
					StartTime: 1700000001,
				},
			},
		},
	}
	migrator := Migrator{
		Reader:    reader,
		Writer:    newFakeV2Writer(),
		BatchSize: 1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := migrator.Run(ctx)
	if err == nil {
		t.Fatal("expected cursor not advanced error")
	}
	if strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("expected fast cursor error, got timeout: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "keyset cursor did not advance") {
		t.Fatalf("expected keyset cursor did not advance error, got %q", err.Error())
	}
}
