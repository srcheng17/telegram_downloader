package httpv2

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	defaultMetadataHistoryLimit = 20
	maxMetadataHistoryLimit     = 20
)

type MetadataHistoryEntry struct {
	TaskType   string    `json:"task_type"`
	URL        *string   `json:"url,omitempty"`
	Author     *string   `json:"author,omitempty"`
	SeriesName *string   `json:"series_name,omitempty"`
	ComicName  *string   `json:"comic_name,omitempty"`
	Summary    *string   `json:"summary,omitempty"`
	Tags       *string   `json:"tags,omitempty"`
	Genres     *string   `json:"genres,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type metadataHistoryRowScanner interface {
	Scan(dest ...any) error
}

func scanMetadataHistoryEntry(scanner metadataHistoryRowScanner) (MetadataHistoryEntry, error) {
	var entry MetadataHistoryEntry
	err := scanner.Scan(
		&entry.TaskType,
		&entry.URL,
		&entry.Author,
		&entry.SeriesName,
		&entry.ComicName,
		&entry.Summary,
		&entry.Tags,
		&entry.Genres,
		&entry.CreatedAt,
	)
	if err != nil {
		return MetadataHistoryEntry{}, err
	}
	return entry, nil
}

func normalizeMetadataHistoryLimit(limit int) int {
	if limit <= 0 {
		return defaultMetadataHistoryLimit
	}
	if limit > maxMetadataHistoryLimit {
		return maxMetadataHistoryLimit
	}
	return limit
}

func (s *PostgresTaskStore) InsertMetadataHistory(ctx context.Context, entry MetadataHistoryEntry) error {
	if s == nil || s.db == nil {
		return errors.New("v2 task database is not configured")
	}
	_, err := s.db.Exec(
		ctx,
		`
		INSERT INTO metadata_history (
			task_type,
			url,
			author,
			series_name,
			comic_name,
			summary,
			tags,
			genres,
			created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, NOW()
		)
		`,
		strings.TrimSpace(entry.TaskType),
		entry.URL,
		entry.Author,
		entry.SeriesName,
		entry.ComicName,
		entry.Summary,
		entry.Tags,
		entry.Genres,
	)
	return err
}

func (s *PostgresTaskStore) ListMetadataHistory(ctx context.Context, limit int) ([]MetadataHistoryEntry, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("v2 task database is not configured")
	}
	rows, err := s.db.Query(
		ctx,
		`
		SELECT
			task_type,
			url,
			author,
			series_name,
			comic_name,
			summary,
			tags,
			genres,
			created_at
		FROM metadata_history
		ORDER BY created_at DESC
		LIMIT $1
		`,
		normalizeMetadataHistoryLimit(limit),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]MetadataHistoryEntry, 0, normalizeMetadataHistoryLimit(limit))
	for rows.Next() {
		entry, scanErr := scanMetadataHistoryEntry(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}
