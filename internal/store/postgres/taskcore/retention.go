package taskcore

import (
	"context"
	"encoding/json"
	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

func (s *Store) RecordRetention(ctx context.Context, task app.Task, manifest domain.RetentionManifest) error {
	if err := domain.ValidateRetentionManifest(manifest); err != nil {
		return err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	var valid bool
	err = tx.QueryRow(ctx, `SELECT status IN ('RUNNING','CANCELING') AND lease_owner=$2 AND attempt=$3 AND generation=$4 FROM task_core_tasks WHERE id=$1 FOR UPDATE`, task.ID, task.LeaseOwner, task.Attempt, task.Generation).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return app.ErrConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO task_core_retention(task_id,generation,manifest) VALUES($1,$2,$3::jsonb) ON CONFLICT(task_id,generation) DO UPDATE SET manifest=EXCLUDED.manifest`, task.ID, task.Generation, string(data))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
