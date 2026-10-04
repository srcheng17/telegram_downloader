package komgaedit

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ryancheng/telegram-downloader/internal/archive/cbzedit"
)

func (s *EditService) commitPrepared(ctx context.Context, sourceRoot *os.Root, op Operation) (cbzedit.CommitResult, error) {
	if op.Prepared == nil || op.Prepared.OperationID != op.ID || op.Prepared.RelativePath != op.RelativePath || op.Prepared.NewSHA256 != op.TargetSHA256 {
		return cbzedit.CommitResult{}, ErrUnsafe
	}
	backup, err := openPrivateBackupRoot(s.backupRoot)
	if err != nil {
		return cbzedit.CommitResult{}, err
	}
	defer backup.Close()
	return cbzedit.Commit(ctx, sourceRoot, backup, *op.Prepared)
}

func (s *EditService) Operation(ctx context.Context, id string) (OperationView, error) {
	if !validOperationID(id) {
		return OperationView{}, ErrInvalid
	}
	op, err := s.operations.Get(ctx, id)
	if err != nil {
		return OperationView{}, err
	}
	var result OperationView
	err = s.operations.WithBookLock(ctx, op.LibraryID+"/"+op.RelativePath, func(locked context.Context) error {
		fresh, err := s.operations.Get(locked, id)
		if err != nil {
			return err
		}
		result, err = s.reconcileLocked(locked, fresh)
		return err
	})
	return result, err
}

func (s *EditService) RetrySync(ctx context.Context, id string) (OperationView, error) {
	if !validOperationID(id) {
		return OperationView{}, ErrInvalid
	}
	op, err := s.operations.Get(ctx, id)
	if err != nil {
		return OperationView{}, err
	}
	var result OperationView
	err = s.operations.WithBookLock(ctx, op.LibraryID+"/"+op.RelativePath, func(locked context.Context) error {
		fresh, err := s.operations.Get(locked, id)
		if err != nil {
			return err
		}
		fresh, err = s.reconcileOperationLocked(locked, fresh)
		if err != nil {
			return err
		}
		if fresh.State == StateRestoreNeeded || !fresh.FileCommitted && !fresh.FileNoChange {
			result = operationView(fresh)
			return ErrConflict
		}
		if fresh.ProjectionConsistent {
			result = operationView(fresh)
			return nil
		}
		fresh, err = s.syncLocked(locked, fresh)
		result = operationView(fresh)
		return err
	})
	return result, err
}

// Restore explicitly returns the archived file to its verified original.
// It never overwrites a later external version and never treats a queued
// Komga analyze as proof that its displayed projection has reverted.
func (s *EditService) Restore(ctx context.Context, id string) (OperationView, error) {
	if !validOperationID(id) {
		return OperationView{}, ErrInvalid
	}
	op, err := s.operations.Get(ctx, id)
	if err != nil {
		return OperationView{}, err
	}
	var result OperationView
	err = s.operations.WithBookLock(ctx, op.LibraryID+"/"+op.RelativePath, func(locked context.Context) error {
		fresh, err := s.operations.Get(locked, id)
		if err != nil {
			return err
		}
		fresh, err = s.reconcileOperationLocked(locked, fresh)
		if err != nil {
			return err
		}
		if fresh.State == StateRestored {
			result = operationView(fresh)
			return nil
		}
		if !fresh.FileCommitted || fresh.Prepared == nil || fresh.State == StateRestoreNeeded {
			result = operationView(fresh)
			return ErrConflict
		}
		book, err := s.loadBook(locked, fresh.BookID)
		if err != nil {
			return err
		}
		if book.root != nil {
			defer book.root.Close()
		}
		if book.root == nil || book.book.LibraryID != fresh.LibraryID || book.relative != fresh.RelativePath {
			return ErrConflict
		}
		backup, err := openPrivateBackupRoot(s.backupRoot)
		if err != nil {
			return err
		}
		restored, restoreErr := cbzedit.Restore(locked, book.root, backup, *fresh.Prepared)
		_ = backup.Close()
		if restoreErr != nil && !restored.FileRestored {
			return restoreErr
		}
		fresh.FileCommitted = false
		fresh.FileRestored = restoreErr == nil
		fresh.State = StateRestored
		fresh.ProjectionConsistent = false
		fresh.AnalyzeVerified = false
		if restoreErr != nil {
			fresh.State = StateRestoreNeeded
			fresh.LastErrorCode = errorCode(restoreErr)
		} else {
			fresh.LastErrorCode = ""
		}
		fresh, err = s.persist(locked, fresh)
		if err != nil {
			return err
		}
		if restoreErr == nil {
			client, connectErr := s.connectEdit(locked)
			if connectErr == nil {
				connectErr = client.AnalyzeBook(locked, fresh.BookID)
			}
			if connectErr != nil {
				fresh.LastErrorCode = errorCode(connectErr)
				fresh, err = s.persist(locked, fresh)
				if err != nil {
					return err
				}
			}
		}
		result = operationView(fresh)
		return nil
	})
	return result, err
}

// ReconcilePending repairs bounded durable operations after an API restart.
// It does not initiate a new Komga analyze job automatically.
func (s *EditService) ReconcilePending(ctx context.Context, limit int) (int, error) {
	ops, err := s.operations.ListRecoverable(ctx, limit)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, op := range ops {
		if err := s.operations.WithBookLock(ctx, op.LibraryID+"/"+op.RelativePath, func(locked context.Context) error {
			fresh, err := s.operations.Get(locked, op.ID)
			if err != nil {
				return err
			}
			_, err = s.reconcileOperationLocked(locked, fresh)
			return err
		}); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (s *EditService) reconcileLocked(ctx context.Context, op Operation) (OperationView, error) {
	updated, err := s.reconcileOperationLocked(ctx, op)
	return operationView(updated), err
}

func (s *EditService) reconcileOperationLocked(ctx context.Context, op Operation) (Operation, error) {
	if op.State == StateRestored || op.State == StateAborted {
		return op, nil
	}
	book, err := s.loadBook(ctx, op.BookID)
	if err != nil {
		// Komga may be temporarily offline at startup. Without its current
		// file mapping, recovery must not infer that the CBZ moved or changed.
		return op, nil
	}
	if book.root != nil {
		defer book.root.Close()
	}
	if book.root == nil {
		return op, nil
	}
	if book.book.LibraryID != op.LibraryID || book.relative != op.RelativePath {
		if op.State == StateCurrentValueConsistent {
			// A completed edit is historical. Moving that book later must not
			// reactivate its old path's unique-operation constraint.
			return op, nil
		}
		return s.markRestoreNeeded(ctx, op, "path_changed")
	}
	inspection, err := cbzedit.Inspect(ctx, book.root, book.relative, cbzedit.Limits{})
	if err != nil {
		if op.State == StateCurrentValueConsistent {
			return op, nil
		}
		return s.markRestoreNeeded(ctx, op, "source_unavailable")
	}
	switch op.State {
	case StatePreparing, StatePrepared, StateRestoreNeeded:
		backup, err := openPrivateBackupRoot(s.backupRoot)
		if err != nil {
			return op, nil
		}
		probe, probeErr := cbzedit.Probe(ctx, book.root, backup, op.RelativePath, op.ID, op.SourceSHA256)
		_ = backup.Close()
		if probeErr != nil {
			return s.markRestoreNeeded(ctx, op, errorCode(probeErr))
		}
		switch probe.State {
		case cbzedit.ProbeMissing, cbzedit.ProbeIncomplete:
			if inspection.SHA256 != op.SourceSHA256 {
				return s.markRestoreNeeded(ctx, op, "source_changed")
			}
			cleanupRoot, err := openPrivateBackupRoot(s.backupRoot)
			if err != nil {
				return s.markRestoreNeeded(ctx, op, "backup_unavailable")
			}
			cleanupErr := cbzedit.AbandonIncomplete(ctx, book.root, cleanupRoot, op.RelativePath, op.ID, op.SourceSHA256)
			_ = cleanupRoot.Close()
			if cleanupErr != nil {
				return s.markRestoreNeeded(ctx, op, errorCode(cleanupErr))
			}
			op.State = StateAborted
			op.LastErrorCode = "preparation_incomplete"
			return s.persist(ctx, op)
		case cbzedit.ProbeConflict:
			return s.markRestoreNeeded(ctx, op, "source_changed")
		case cbzedit.ProbeRestored:
			op.Prepared = probe.Prepared
			op.FileCommitted = false
			op.FileRestored = true
			op.ProjectionConsistent = false
			op.AnalyzeVerified = false
			op.State = StateRestored
			op.LastErrorCode = "restore_projection_unverified"
			return s.persist(ctx, op)
		case cbzedit.ProbePrepared, cbzedit.ProbeCommitted:
			if probe.Prepared == nil {
				return s.markRestoreNeeded(ctx, op, "preparation_incomplete")
			}
			if op.Prepared != nil && (op.Prepared.NewSHA256 != probe.Prepared.NewSHA256 || op.Prepared.TemporaryPath != probe.Prepared.TemporaryPath) {
				return s.markRestoreNeeded(ctx, op, "preparation_conflict")
			}
			op.Prepared = probe.Prepared
			op.TargetSHA256 = probe.Prepared.NewSHA256
			op.BackupRef = probe.Prepared.BackupPath
		default:
			return s.markRestoreNeeded(ctx, op, "preparation_incomplete")
		}
		if probe.State == cbzedit.ProbeCommitted {
			op.FileCommitted = true
			op.State = StateFileCommitted
			op.LastErrorCode = ""
			return s.persist(ctx, op)
		}
		// Persist recovered descriptor before any possible rename.
		op.State = StatePrepared
		op, err = s.persist(ctx, op)
		if err != nil {
			return op, err
		}
		committed, commitErr := s.commitPrepared(ctx, book.root, op)
		if commitErr != nil && !committed.FileCommitted {
			return s.markRestoreNeeded(ctx, op, errorCode(commitErr))
		}
		op.FileCommitted = true
		op.State = StateFileCommitted
		op.LastErrorCode = errorCode(commitErr)
		if commitErr == nil {
			op.LastErrorCode = ""
		}
		return s.persist(ctx, op)
	case StateCurrentValueConsistent:
		// A completed operation is historical. Later edits may legitimately
		// replace its target; do not turn an old record back into an active
		// path blocker (or conflict with a newer active operation).
		if inspection.SHA256 == op.TargetSHA256 || op.Prepared == nil {
			return op, nil
		}
		if backup, err := openPrivateBackupRoot(s.backupRoot); err == nil {
			probe, probeErr := cbzedit.Probe(ctx, book.root, backup, op.RelativePath, op.ID, op.SourceSHA256)
			_ = backup.Close()
			if probeErr == nil && probe.State == cbzedit.ProbeRestored {
				op.FileCommitted = false
				op.FileRestored = true
				op.ProjectionConsistent = false
				op.AnalyzeVerified = false
				op.State = StateRestored
				op.LastErrorCode = "restore_projection_unverified"
				return s.persist(ctx, op)
			}
		}
		return op, nil
	case StateFileCommitted, StateSyncPending, StateSyncFailed:
		if inspection.SHA256 != op.TargetSHA256 {
			if op.Prepared != nil {
				if backup, err := openPrivateBackupRoot(s.backupRoot); err == nil {
					probe, probeErr := cbzedit.Probe(ctx, book.root, backup, op.RelativePath, op.ID, op.SourceSHA256)
					_ = backup.Close()
					if probeErr == nil && probe.State == cbzedit.ProbeRestored {
						op.FileCommitted = false
						op.FileRestored = true
						op.ProjectionConsistent = false
						op.AnalyzeVerified = false
						op.State = StateRestored
						op.LastErrorCode = "restore_projection_unverified"
						return s.persist(ctx, op)
					}
				}
			}
			return s.markRestoreNeeded(ctx, op, "source_changed")
		}
		return op, nil
	default:
		return s.markRestoreNeeded(ctx, op, "invalid_state")
	}
}

func (s *EditService) markRestoreNeeded(ctx context.Context, op Operation, code string) (Operation, error) {
	op.State = StateRestoreNeeded
	op.ProjectionConsistent = false
	op.AnalyzeVerified = false
	op.LastErrorCode = code
	return s.persist(ctx, op)
}

func (s *EditService) persist(ctx context.Context, op Operation) (Operation, error) {
	// Cancellation after a rename must not prevent the operation record from
	// reflecting the filesystem result. Keep this bounded if the DB is down.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	updated, err := s.operations.Update(writeCtx, op)
	if err != nil {
		return op, fmt.Errorf("persist Komga edit operation: %w", err)
	}
	return updated, nil
}

func (s *EditService) syncLocked(ctx context.Context, op Operation) (Operation, error) {
	if op.State == StateRestoreNeeded || !op.FileCommitted && !op.FileNoChange {
		return op, ErrConflict
	}
	book, err := s.loadBook(ctx, op.BookID)
	if err != nil {
		return s.syncFailure(ctx, op, "book_unavailable")
	}
	if book.root != nil {
		defer book.root.Close()
	}
	if !book.view.Editable || book.root == nil || !book.library.ImportComicInfoBook || book.book.Media.Status != "READY" {
		return s.syncFailure(ctx, op, "library_or_media_unavailable")
	}
	if book.book.LibraryID != op.LibraryID || book.relative != op.RelativePath {
		return s.markRestoreNeeded(ctx, op, "path_changed")
	}
	inspection, err := cbzedit.Inspect(ctx, book.root, book.relative, cbzedit.Limits{})
	if err != nil || inspection.SHA256 != op.TargetSHA256 {
		return s.markRestoreNeeded(ctx, op, "source_changed")
	}
	for _, change := range op.DesiredFields {
		if lockedField(change.Key, book.book) || change.Key == "identifiers" && change.State == "cleared" && book.library.ImportBarcodeIsbn {
			return s.syncFailure(ctx, op, "field_locked_or_barcode_enabled")
		}
	}
	client, err := s.connectEdit(ctx)
	if err != nil {
		return s.syncFailure(ctx, op, errorCode(err))
	}
	before := book.book
	if err := client.AnalyzeBook(ctx, op.BookID); err != nil {
		return s.syncFailure(ctx, op, errorCode(err))
	}
	op.State = StateSyncPending
	op.LastErrorCode = "analysis_pending"
	op, err = s.persist(ctx, op)
	if err != nil {
		return op, err
	}
	// The async analyzer may finish after the request returns. Poll only one
	// book for a bounded interval; retry uses the same operation without a file
	// rewrite. READY or 202 alone is never treated as proof of import.
	const attempts = 8
	clearedOnce := false
	for i := 0; i < attempts; i++ {
		if i != 0 {
			if err := waitForSync(ctx, 750*time.Millisecond); err != nil {
				return s.syncFailure(ctx, op, errorCode(err))
			}
		}
		current, err := client.Book(ctx, op.BookID)
		if err != nil {
			return s.syncFailure(ctx, op, errorCode(err))
		}
		if current.LibraryID != op.LibraryID || current.URL != book.book.URL {
			return s.markRestoreNeeded(ctx, op, "path_changed")
		}
		if current.Media.Status != "READY" {
			continue
		}
		currentLibrary, err := client.Library(ctx, op.LibraryID)
		if err != nil {
			return s.syncFailure(ctx, op, errorCode(err))
		}
		if currentLibrary.Root != book.library.Root {
			return s.markRestoreNeeded(ctx, op, "path_changed")
		}
		if !currentLibrary.ImportComicInfoBook {
			return s.syncFailure(ctx, op, "comicinfo_import_disabled")
		}
		for _, change := range op.DesiredFields {
			if lockedField(change.Key, current) || change.Key == "identifiers" && change.State == "cleared" && currentLibrary.ImportBarcodeIsbn {
				return s.syncFailure(ctx, op, "field_locked_or_barcode_enabled")
			}
		}
		clears := clearProjectionFields(inspection.ComicInfo, op.DesiredFields)
		if len(clears) > 0 && !clearedOnce {
			pending, safe := pendingClears(before, current, clears)
			if !safe {
				return s.syncFailure(ctx, op, "komga_metadata_changed")
			}
			// PATCH only clears stale book-level projections; no lock values or
			// unrelated metadata are sent. Import has already been enqueued.
			if len(pending) > 0 {
				if err := client.ClearBookMetadata(ctx, op.BookID, pending...); err != nil {
					return s.syncFailure(ctx, op, errorCode(err))
				}
				current, err = client.Book(ctx, op.BookID)
				if err != nil {
					return s.syncFailure(ctx, op, errorCode(err))
				}
			}
			clearedOnce = true
		}
		if projectionMatches(current, inspection.ComicInfo, op.DesiredFields) {
			op.State = StateCurrentValueConsistent
			op.ProjectionConsistent = true
			op.AnalyzeVerified = observableNonClearChanged(before, current, op.DesiredFields)
			op.LastErrorCode = ""
			return s.persist(ctx, op)
		}
	}
	op.State = StateSyncPending
	op.LastErrorCode = "analysis_pending"
	return s.persist(ctx, op)
}

func (s *EditService) syncFailure(ctx context.Context, op Operation, code string) (Operation, error) {
	op.State = StateSyncFailed
	op.LastErrorCode = code
	op.ProjectionConsistent = false
	op.AnalyzeVerified = false
	return s.persist(ctx, op)
}

func (s *EditService) connectEdit(ctx context.Context) (editGateway, error) {
	base, key, err := s.catalog.connection.Resolve(ctx)
	if err != nil {
		return nil, err
	}
	if s.editFactory == nil {
		return nil, ErrDisabled
	}
	return s.editFactory(base, key)
}

func waitForSync(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func validOperationID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			if c < 'a' || c > 'f' {
				return false
			}
		}
	}
	return true
}
