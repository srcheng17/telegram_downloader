package migration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

const defaultBatchSize = 200

type MigrationStats struct {
	LegacyTotal   int
	MigratedTasks int
	V2TasksTotal  int
	V2EventsTotal int
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

	migratedTasks := 0
	for offset := 0; ; offset += batchSize {
		legacyTasks, err := m.Reader.ReadLegacyTasks(ctx, offset, batchSize)
		if err != nil {
			return MigrationStats{}, fmt.Errorf("read legacy tasks batch offset=%d: %w", offset, err)
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

			v2Tasks = append(v2Tasks, V2Task{
				ID:            strings.TrimSpace(legacy.ID),
				URL:           normalizedURL,
				CanonicalURL:  canonicalURL,
				Status:        mappedStatus,
				EnqueueToken:  migratedEnqueueToken(legacy.ID),
				Error:         legacy.Error,
				ResultZipPath: legacy.ResultZipPath,
				CreatedAt:     createdAt,
				UpdatedAt:     createdAt,
			})

			sourceStatus := strings.ToUpper(strings.TrimSpace(legacy.Status))
			if sourceStatus == "" {
				sourceStatus = "UNKNOWN"
			}
			targetStatus := mappedStatus
			v2Events = append(v2Events, V2TaskEvent{
				TaskID:      strings.TrimSpace(legacy.ID),
				EventType:   "MIGRATED",
				FromStatus:  stringPtr(sourceStatus),
				ToStatus:    stringPtr(targetStatus),
				PayloadJSON: migrateEventPayload(sourceStatus),
				CreatedAt:   createdAt,
			})
		}

		if err := m.Writer.WriteV2Batch(ctx, v2Tasks, v2Events); err != nil {
			return MigrationStats{}, fmt.Errorf("write v2 migration batch offset=%d: %w", offset, err)
		}

		migratedTasks += len(v2Tasks)
		if len(legacyTasks) < batchSize {
			break
		}
	}

	v2TasksTotal, err := m.Writer.CountV2Tasks(ctx)
	if err != nil {
		return MigrationStats{}, fmt.Errorf("count v2 tasks: %w", err)
	}
	v2EventsTotal, err := m.Writer.CountV2Events(ctx)
	if err != nil {
		return MigrationStats{}, fmt.Errorf("count v2 task events: %w", err)
	}
	if err := ValidateMigrationCounts(legacyTotal, v2TasksTotal, v2EventsTotal); err != nil {
		return MigrationStats{}, err
	}

	return MigrationStats{
		LegacyTotal:   legacyTotal,
		MigratedTasks: migratedTasks,
		V2TasksTotal:  v2TasksTotal,
		V2EventsTotal: v2EventsTotal,
	}, nil
}

func ValidateMigrationCounts(legacyTotal, v2TasksTotal, v2EventsTotal int) error {
	if legacyTotal != v2TasksTotal {
		return fmt.Errorf(
			"migration count mismatch: legacy_tasks=%d v2_tasks=%d",
			legacyTotal,
			v2TasksTotal,
		)
	}
	if v2EventsTotal < v2TasksTotal {
		return fmt.Errorf(
			"migration event count below minimum: v2_events=%d expected_at_least=%d",
			v2EventsTotal,
			v2TasksTotal,
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
