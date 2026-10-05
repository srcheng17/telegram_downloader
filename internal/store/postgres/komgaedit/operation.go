package komgaedit

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	app "github.com/ryancheng/telegram-downloader/internal/app/komgaedit"
	"github.com/ryancheng/telegram-downloader/internal/archive/cbzedit"
)

type OperationStore struct{ pool *pgxpool.Pool }

var _ app.OperationRepository = (*OperationStore)(nil)

func NewOperationStore(pool *pgxpool.Pool) *OperationStore { return &OperationStore{pool: pool} }

const operationColumns = `id,idempotency_key,request_digest,book_id,library_id,relative_path,
    source_sha256,target_sha256,backup_ref,desired_fields,prepared,state,file_committed,
    file_no_change,file_restored,projection_consistent,analyze_verified,last_error_code,version,created_at,updated_at`

func scanOperation(row pgx.Row) (app.Operation, error) {
	var op app.Operation
	var fields, prepared []byte
	err := row.Scan(&op.ID, &op.IdempotencyKey, &op.RequestDigest, &op.BookID, &op.LibraryID,
		&op.RelativePath, &op.SourceSHA256, &op.TargetSHA256, &op.BackupRef, &fields,
		&prepared, &op.State, &op.FileCommitted, &op.FileNoChange, &op.FileRestored, &op.ProjectionConsistent,
		&op.AnalyzeVerified, &op.LastErrorCode, &op.Version, &op.CreatedAt, &op.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Operation{}, app.ErrNotFound
	}
	if err != nil {
		return app.Operation{}, err
	}
	if err := json.Unmarshal(fields, &op.DesiredFields); err != nil {
		return app.Operation{}, fmt.Errorf("decode Komga edit fields: %w", err)
	}
	if len(prepared) > 0 {
		var value cbzedit.Prepared
		if err := json.Unmarshal(prepared, &value); err != nil {
			return app.Operation{}, fmt.Errorf("decode Komga edit preparation: %w", err)
		}
		op.Prepared = &value
	}
	return op, nil
}

func encodeOperation(op app.Operation) (string, any, error) {
	fields, err := json.Marshal(op.DesiredFields)
	if err != nil {
		return "", nil, err
	}
	if op.DesiredFields == nil {
		fields = []byte("[]")
	}
	var prepared any
	if op.Prepared != nil {
		data, err := json.Marshal(op.Prepared)
		if err != nil {
			return "", nil, err
		}
		prepared = string(data)
	}
	return string(fields), prepared, nil
}

func (s *OperationStore) Create(ctx context.Context, op app.Operation) (app.Operation, error) {
	if s == nil || s.pool == nil {
		return app.Operation{}, app.ErrDisabled
	}
	fields, prepared, err := encodeOperation(op)
	if err != nil {
		return app.Operation{}, err
	}
	query := `INSERT INTO komga_edit_operations (id,idempotency_key,request_digest,book_id,library_id,
        relative_path,source_sha256,target_sha256,backup_ref,desired_fields,prepared,state,
        file_committed,file_no_change,file_restored,projection_consistent,analyze_verified,last_error_code)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11::jsonb,$12,$13,$14,$15,$16,$17,$18)
        RETURNING ` + operationColumns
	created, err := scanOperation(s.pool.QueryRow(ctx, query, op.ID, op.IdempotencyKey, op.RequestDigest,
		op.BookID, op.LibraryID, op.RelativePath, op.SourceSHA256, op.TargetSHA256,
		op.BackupRef, fields, prepared, op.State, op.FileCommitted, op.FileNoChange,
		op.FileRestored, op.ProjectionConsistent, op.AnalyzeVerified, op.LastErrorCode))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return app.Operation{}, app.ErrConflict
		}
		return app.Operation{}, err
	}
	return created, nil
}

func (s *OperationStore) Get(ctx context.Context, id string) (app.Operation, error) {
	if s == nil || s.pool == nil {
		return app.Operation{}, app.ErrDisabled
	}
	return scanOperation(s.pool.QueryRow(ctx, `SELECT `+operationColumns+` FROM komga_edit_operations WHERE id=$1`, id))
}

func (s *OperationStore) GetByKey(ctx context.Context, key string) (app.Operation, error) {
	if s == nil || s.pool == nil {
		return app.Operation{}, app.ErrDisabled
	}
	return scanOperation(s.pool.QueryRow(ctx, `SELECT `+operationColumns+` FROM komga_edit_operations WHERE idempotency_key=$1`, key))
}

// Update uses the operation version as a compare-and-swap guard. Filesystem
// work is never retried merely because this database update failed.
func (s *OperationStore) Update(ctx context.Context, op app.Operation) (app.Operation, error) {
	if s == nil || s.pool == nil || op.Version < 1 {
		return app.Operation{}, app.ErrConflict
	}
	fields, prepared, err := encodeOperation(op)
	if err != nil {
		return app.Operation{}, err
	}
	query := `UPDATE komga_edit_operations SET target_sha256=$2,backup_ref=$3,desired_fields=$4::jsonb,
		prepared=$5::jsonb,state=$6,file_committed=$7,file_no_change=$8,file_restored=$9,projection_consistent=$10,
		analyze_verified=$11,last_error_code=$12,version=version+1,updated_at=now()
		WHERE id=$1 AND version=$13 RETURNING ` + operationColumns
	updated, err := scanOperation(s.pool.QueryRow(ctx, query, op.ID, op.TargetSHA256, op.BackupRef, fields,
		prepared, op.State, op.FileCommitted, op.FileNoChange, op.FileRestored, op.ProjectionConsistent,
		op.AnalyzeVerified, op.LastErrorCode, op.Version))
	if errors.Is(err, app.ErrNotFound) {
		return app.Operation{}, app.ErrConflict
	}
	return updated, err
}

func (s *OperationStore) ListRecoverable(ctx context.Context, limit int) ([]app.Operation, error) {
	if s == nil || s.pool == nil {
		return nil, app.ErrDisabled
	}
	if limit < 1 || limit > 100 {
		return nil, app.ErrInvalid
	}
	rows, err := s.pool.Query(ctx, `SELECT `+operationColumns+` FROM komga_edit_operations
		WHERE state IN ('preparing','prepared','file_committed','sync_pending','sync_failed','restore_needed')
        ORDER BY updated_at ASC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]app.Operation, 0)
	for rows.Next() {
		op, err := scanOperation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, op)
	}
	return result, rows.Err()
}

// WithBookLock holds a PostgreSQL session advisory lock through the caller's
// complete backup/replace/sync transition. It serializes app instances sharing
// the same database, not just goroutines in one process.
func (s *OperationStore) WithBookLock(ctx context.Context, key string, fn func(context.Context) error) error {
	if s == nil || s.pool == nil || key == "" || fn == nil {
		return app.ErrInvalid
	}
	digest := sha256.Sum256([]byte("komga-edit:" + key))
	lockID := int64(binary.BigEndian.Uint64(digest[:8]))
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire Komga edit lock connection: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1::bigint)`, lockID); err != nil {
		// Cancellation can race with a server-side lock grant. Do not return
		// this session to the pool when its lock state is uncertain.
		closeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = conn.Conn().Close(closeCtx)
		return fmt.Errorf("lock Komga edit path: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var unlocked bool
		if err := conn.QueryRow(unlockCtx, `SELECT pg_advisory_unlock($1::bigint)`, lockID).Scan(&unlocked); err != nil || !unlocked {
			// A broken session must not be returned to the pool with a held lock.
			closeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			_ = conn.Conn().Close(closeCtx)
		}
	}()
	return fn(ctx)
}
