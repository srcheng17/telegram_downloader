package postgres

import (
	"context"
	"errors"
	metadata "github.com/ryancheng/telegram-downloader/internal/domain/metadata"
	"strings"
	"time"
)

const (
	defaultMetadataHistoryLimit = 20
	maxMetadataHistoryLimit     = 20
)

type MetadataHistoryEntry struct {
	TaskID                    *string            `json:"task_id,omitempty"`
	MetadataDocument          *metadata.Document `json:"metadata_document,omitempty"`
	EffectiveMetadataDocument *metadata.Document `json:"effective_metadata_document,omitempty"`
	TaskType                  string             `json:"task_type"`
	URL                       *string            `json:"url,omitempty"`
	Author                    *string            `json:"author,omitempty"`
	SeriesName                *string            `json:"series_name,omitempty"`
	SeriesNumber              *string            `json:"series_number,omitempty"`
	ComicName                 *string            `json:"comic_name,omitempty"`
	Summary                   *string            `json:"summary,omitempty"`
	Tags                      *string            `json:"tags,omitempty"`
	Genres                    *string            `json:"genres,omitempty"`
	CreatedAt                 time.Time          `json:"created_at"`
}

type metadataHistoryRowScanner interface {
	Scan(dest ...any) error
}

func scanMetadataHistoryEntry(scanner metadataHistoryRowScanner) (MetadataHistoryEntry, error) {
	var entry MetadataHistoryEntry
	var document, effective []byte
	err := scanner.Scan(
		&entry.TaskType,
		&entry.URL,
		&entry.Author,
		&entry.SeriesName,
		&entry.SeriesNumber,
		&entry.ComicName,
		&entry.Summary,
		&entry.Tags,
		&entry.Genres,
		&entry.CreatedAt,
		&document,
		&entry.TaskID,
		&effective,
	)
	if err != nil {
		return MetadataHistoryEntry{}, err
	}
	if len(document) > 0 {
		doc, err := metadata.DecodeStored(document)
		if err != nil {
			return MetadataHistoryEntry{}, err
		}
		entry.MetadataDocument = &doc
	} else {
		doc, err := metadata.FromLegacy(metadata.Legacy{Author: entry.Author, ComicName: entry.ComicName, SeriesName: entry.SeriesName, SeriesNumber: entry.SeriesNumber, Summary: entry.Summary, Tags: entry.Tags, Genres: entry.Genres})
		if err != nil {
			return MetadataHistoryEntry{}, err
		}
		entry.MetadataDocument = &doc
	}
	projection := metadata.ToLegacy(*entry.MetadataDocument)
	entry.Author, entry.ComicName, entry.SeriesName, entry.SeriesNumber = projection.Author, projection.ComicName, projection.SeriesName, projection.SeriesNumber
	entry.Summary, entry.Tags, entry.Genres = projection.Summary, projection.Tags, projection.Genres
	if len(effective) > 0 {
		doc, err := metadata.DecodeStored(effective)
		if err != nil {
			return MetadataHistoryEntry{}, err
		}
		entry.EffectiveMetadataDocument = &doc
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

func (s *UploadTaskStore) InsertMetadataHistory(ctx context.Context, entry MetadataHistoryEntry) error {
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
			series_number,
			comic_name,
			summary,
			tags,
			genres,
			created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, NOW()
		)
		ON CONFLICT DO NOTHING
		`,
		strings.TrimSpace(entry.TaskType),
		entry.URL,
		entry.Author,
		entry.SeriesName,
		entry.SeriesNumber,
		entry.ComicName,
		entry.Summary,
		entry.Tags,
		entry.Genres,
	)
	return err
}

func (s *UploadTaskStore) ListMetadataHistory(ctx context.Context, limit int) ([]MetadataHistoryEntry, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("v2 task database is not configured")
	}
	rows, err := s.db.Query(
		ctx,
		`
		SELECT
            h.task_type, h.url, h.author, h.series_name, h.series_number,
            h.comic_name, h.summary, h.tags, h.genres, h.created_at,
            h.metadata_document, h.task_id, r.effective_metadata_document
        FROM metadata_history h
        LEFT JOIN task_core_results r ON r.task_id::text = h.task_id
        ORDER BY h.created_at DESC, h.id DESC
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
