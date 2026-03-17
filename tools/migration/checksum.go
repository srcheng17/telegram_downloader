package migration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"math"
	"strings"
	"time"
)

const defaultBatchSize = 200

type MigrationStats struct {
	LegacyTotal int

	MigratedTasks int
	V2TasksTotal  int

	LegacyTasksChecksum string
	V2TasksChecksum     string

	LegacyMigratedEventsTotal    int
	V2MigratedEventsTotal        int
	LegacyMigratedEventsChecksum string
	V2MigratedEventsChecksum     string
}

type Migrator struct {
	Reader    LegacyReader
	Writer    V2Writer
	BatchSize int
}

func (m Migrator) Run(ctx context.Context) (MigrationStats, error) {
	if m.Reader == nil {
		return MigrationStats{}, errors.New("migration requires legacy reader")
	}
	if m.Writer == nil {
		return MigrationStats{}, errors.New("migration requires v2 writer")
	}

	batchSize := m.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	legacyTotal, err := m.Reader.CountLegacyTasks(ctx)
	if err != nil {
		return MigrationStats{}, fmt.Errorf("count legacy tasks: %w", err)
	}

	expectedTasksChecksum := newChecksumBuilder()
	expectedMigratedEventsChecksum := newChecksumBuilder()
	migratedTasks := 0
	lastID := ""

	for {
		legacyTasks, err := m.Reader.ReadLegacyTasksAfterID(ctx, lastID, batchSize)
		if err != nil {
			return MigrationStats{}, fmt.Errorf("read legacy tasks after id=%q: %w", lastID, err)
		}
		if len(legacyTasks) == 0 {
			break
		}

		v2Tasks := make([]V2Task, 0, len(legacyTasks))
		v2Events := make([]V2TaskEvent, 0, len(legacyTasks))
		for _, legacy := range legacyTasks {
			mappedStatus := mapLegacyStatus(legacy.Status)
			createdAt := timeFromStartTime(legacy.StartTime)
			normalizedURL := strings.TrimSpace(legacy.URL)
			canonicalURL := strings.TrimSpace(stringValue(legacy.CanonicalURL))
			if canonicalURL == "" {
				canonicalURL = normalizedURL
			}

			mappedTask := V2Task{
				ID:            strings.TrimSpace(legacy.ID),
				URL:           normalizedURL,
				CanonicalURL:  canonicalURL,
				Status:        mappedStatus,
				EnqueueToken:  migratedEnqueueToken(legacy.ID),
				Error:         legacy.Error,
				ResultZipPath: legacy.ResultZipPath,
				CreatedAt:     createdAt,
				UpdatedAt:     createdAt,
			}
			v2Tasks = append(v2Tasks, mappedTask)
			if err := expectedTasksChecksum.AddTask(mappedTask); err != nil {
				return MigrationStats{}, fmt.Errorf("add expected task checksum: %w", err)
			}

			sourceStatus := strings.ToUpper(strings.TrimSpace(legacy.Status))
			if sourceStatus == "" {
				sourceStatus = "UNKNOWN"
			}
			migratedEvent := V2TaskEvent{
				TaskID:      strings.TrimSpace(legacy.ID),
				EventType:   "MIGRATED",
				FromStatus:  stringPtr(sourceStatus),
				ToStatus:    stringPtr(mappedStatus),
				PayloadJSON: migrateEventPayload(sourceStatus),
				CreatedAt:   createdAt,
			}
			v2Events = append(v2Events, migratedEvent)
			if err := expectedMigratedEventsChecksum.AddMigratedEvent(migratedEvent); err != nil {
				return MigrationStats{}, fmt.Errorf("add expected migrated event checksum: %w", err)
			}
		}

		if err := m.Writer.WriteV2Batch(ctx, v2Tasks, v2Events); err != nil {
			return MigrationStats{}, fmt.Errorf("write v2 migration batch last_id=%q: %w", lastID, err)
		}

		migratedTasks += len(v2Tasks)
		nextLastID := strings.TrimSpace(legacyTasks[len(legacyTasks)-1].ID)
		if nextLastID == "" {
			return MigrationStats{}, errors.New("migration data anomaly: empty keyset cursor")
		}
		if lastID != "" && nextLastID <= lastID {
			return MigrationStats{}, fmt.Errorf(
				"migration keyset cursor did not advance: previous=%q next=%q",
				lastID,
				nextLastID,
			)
		}
		lastID = nextLastID
	}

	v2TasksTotal, err := m.Writer.CountV2Tasks(ctx)
	if err != nil {
		return MigrationStats{}, fmt.Errorf("count v2 tasks: %w", err)
	}
	v2MigratedEventsTotal, err := m.Writer.CountV2MigratedEvents(ctx)
	if err != nil {
		return MigrationStats{}, fmt.Errorf("count v2 migrated events: %w", err)
	}
	v2TasksChecksum, err := m.Writer.ChecksumV2Tasks(ctx)
	if err != nil {
		return MigrationStats{}, fmt.Errorf("checksum v2 tasks: %w", err)
	}
	v2MigratedEventsChecksum, err := m.Writer.ChecksumV2MigratedEvents(ctx)
	if err != nil {
		return MigrationStats{}, fmt.Errorf("checksum v2 migrated events: %w", err)
	}

	expectedTasksValue := expectedTasksChecksum.SumHex()
	expectedMigratedEventsValue := expectedMigratedEventsChecksum.SumHex()
	expectedMigratedEventsTotal := expectedMigratedEventsChecksum.Count()

	if err := ValidateMigrationChecksums(
		legacyTotal,
		v2TasksTotal,
		expectedTasksValue,
		v2TasksChecksum,
		expectedMigratedEventsTotal,
		v2MigratedEventsTotal,
		expectedMigratedEventsValue,
		v2MigratedEventsChecksum,
	); err != nil {
		return MigrationStats{}, err
	}

	return MigrationStats{
		LegacyTotal: legacyTotal,

		MigratedTasks: migratedTasks,
		V2TasksTotal:  v2TasksTotal,

		LegacyTasksChecksum: expectedTasksValue,
		V2TasksChecksum:     v2TasksChecksum,

		LegacyMigratedEventsTotal:    expectedMigratedEventsTotal,
		V2MigratedEventsTotal:        v2MigratedEventsTotal,
		LegacyMigratedEventsChecksum: expectedMigratedEventsValue,
		V2MigratedEventsChecksum:     v2MigratedEventsChecksum,
	}, nil
}

func ValidateMigrationChecksums(
	legacyTotal int,
	v2TasksTotal int,
	legacyTasksChecksum string,
	v2TasksChecksum string,
	legacyMigratedEventsTotal int,
	v2MigratedEventsTotal int,
	legacyMigratedEventsChecksum string,
	v2MigratedEventsChecksum string,
) error {
	if legacyTotal != v2TasksTotal {
		return fmt.Errorf(
			"migration count mismatch: legacy_tasks=%d v2_tasks=%d",
			legacyTotal,
			v2TasksTotal,
		)
	}
	if legacyMigratedEventsTotal != v2MigratedEventsTotal {
		return fmt.Errorf(
			"migration migrated-event count mismatch: expected=%d actual=%d",
			legacyMigratedEventsTotal,
			v2MigratedEventsTotal,
		)
	}
	if legacyTasksChecksum != v2TasksChecksum {
		return fmt.Errorf(
			"migration tasks checksum mismatch: expected=%s actual=%s",
			legacyTasksChecksum,
			v2TasksChecksum,
		)
	}
	if legacyMigratedEventsChecksum != v2MigratedEventsChecksum {
		return fmt.Errorf(
			"migration migrated-event checksum mismatch: expected=%s actual=%s",
			legacyMigratedEventsChecksum,
			v2MigratedEventsChecksum,
		)
	}
	return nil
}

func mapLegacyStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "PENDING":
		return "QUEUED"
	case "IN_PROGRESS", "CANCEL_REQUESTED":
		return "RUNNING"
	case "SUCCESS":
		return "SUCCESS"
	case "FAILED":
		return "FAILED"
	case "CANCELED":
		return "CANCELED"
	default:
		return "FAILED"
	}
}

func timeFromStartTime(startTime float64) time.Time {
	if startTime <= 0 {
		return time.Now().UTC()
	}

	seconds, frac := math.Modf(startTime)
	nanos := int64(math.Round(frac * float64(time.Second)))
	return time.Unix(int64(seconds), nanos).UTC()
}

func migrateEventPayload(sourceStatus string) string {
	payload := map[string]any{
		"source":        "legacy_tasks",
		"source_status": strings.TrimSpace(sourceStatus),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func migratedEnqueueToken(taskID string) string {
	return "migrated-" + strings.TrimSpace(taskID)
}

func stringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	copied := trimmed
	return &copied
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

type checksumBuilder struct {
	hash  hash.Hash
	count int
}

func newChecksumBuilder() *checksumBuilder {
	return &checksumBuilder{hash: sha256.New()}
}

func (b *checksumBuilder) AddTask(task V2Task) error {
	return b.addRecord(taskChecksumRecord{
		ID:            strings.TrimSpace(task.ID),
		URL:           strings.TrimSpace(task.URL),
		CanonicalURL:  strings.TrimSpace(task.CanonicalURL),
		Status:        strings.TrimSpace(task.Status),
		Error:         stringValue(task.Error),
		ResultZipPath: stringValue(task.ResultZipPath),
	})
}

func (b *checksumBuilder) AddMigratedEvent(event V2TaskEvent) error {
	normalizedPayload, err := normalizeJSONForChecksum(event.PayloadJSON)
	if err != nil {
		return err
	}
	return b.addRecord(migratedEventChecksumRecord{
		TaskID:      strings.TrimSpace(event.TaskID),
		EventType:   strings.TrimSpace(event.EventType),
		FromStatus:  stringValue(event.FromStatus),
		ToStatus:    stringValue(event.ToStatus),
		PayloadJSON: normalizedPayload,
	})
}

func (b *checksumBuilder) addRecord(record any) error {
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, _ = b.hash.Write(encoded)
	_, _ = b.hash.Write([]byte{'\n'})
	b.count++
	return nil
}

func (b *checksumBuilder) SumHex() string {
	return hex.EncodeToString(b.hash.Sum(nil))
}

func (b *checksumBuilder) Count() int {
	return b.count
}

type taskChecksumRecord struct {
	ID            string `json:"id"`
	URL           string `json:"url"`
	CanonicalURL  string `json:"canonical_url"`
	Status        string `json:"status"`
	Error         string `json:"error"`
	ResultZipPath string `json:"result_zip_path"`
}

type migratedEventChecksumRecord struct {
	TaskID      string `json:"task_id"`
	EventType   string `json:"event_type"`
	FromStatus  string `json:"from_status"`
	ToStatus    string `json:"to_status"`
	PayloadJSON string `json:"payload_json"`
}

func normalizeJSONForChecksum(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		trimmed = "{}"
	}

	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return "", fmt.Errorf("normalize checksum json: %w", err)
	}
	normalized, err := json.Marshal(decoded)
	if err != nil {
		return "", fmt.Errorf("marshal normalized checksum json: %w", err)
	}
	return string(normalized), nil
}
