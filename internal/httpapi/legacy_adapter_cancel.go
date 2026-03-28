package httpapi

import (
	"context"
	"errors"
	"strings"

	"github.com/ryancheng/telegram-downloader/internal/domain"
	"github.com/ryancheng/telegram-downloader/internal/httpv2"
	"github.com/ryancheng/telegram-downloader/internal/store/postgres"
)

func (a *LegacyAdapter) CancelTask(ctx context.Context, taskID string) (LegacyCancelResult, error) {
	if !a.SupportsTaskActions() {
		return LegacyCancelResult{}, errors.New("legacy adapter cancel dependencies are not configured")
	}

	normalizedID := strings.TrimSpace(taskID)
	task, err := a.store.GetTask(ctx, normalizedID)
	if err != nil {
		return LegacyCancelResult{}, err
	}
	if task == nil {
		return LegacyCancelResult{Decision: LegacyCancelDecisionNotFound}, nil
	}

	status := httpv2.NormalizeTaskStatus(task.Status)
	switch status {
	case httpv2.TaskStatusCanceled:
		return LegacyCancelResult{Decision: LegacyCancelDecisionAlreadyRequested, Status: domain.StatusCanceled}, nil
	case httpv2.TaskStatusCancelRequested:
		return LegacyCancelResult{Decision: LegacyCancelDecisionAlreadyRequested, Status: domain.StatusCancelRequested}, nil
	case httpv2.TaskStatusSuccess, httpv2.TaskStatusFailed:
		return LegacyCancelResult{Decision: LegacyCancelDecisionAlreadyFinished, Status: mapV2StatusToLegacy(status)}, nil
	case httpv2.TaskStatusQueued, httpv2.TaskStatusRunning:
	default:
		return LegacyCancelResult{Decision: LegacyCancelDecisionAlreadyFinished, Status: mapV2StatusToLegacy(status)}, nil
	}

	if err := a.store.CancelTask(ctx, normalizedID, status); err != nil {
		if errors.Is(err, postgres.ErrV2TaskStatusMismatchOrNotFound) {
			latestTask, latestErr := a.store.GetTask(ctx, normalizedID)
			if latestErr != nil {
				return LegacyCancelResult{}, latestErr
			}
			return a.resolveCancelMismatchByLatestStatus(ctx, normalizedID, latestTask)
		}
		return LegacyCancelResult{}, err
	}

	return LegacyCancelResult{Decision: LegacyCancelDecisionRequested, Status: domain.StatusCancelRequested}, nil
}

func (a *LegacyAdapter) resolveCancelMismatchByLatestStatus(
	ctx context.Context,
	taskID string,
	latestTask *httpv2.Task,
) (LegacyCancelResult, error) {
	if latestTask == nil {
		return LegacyCancelResult{Decision: LegacyCancelDecisionNotFound}, nil
	}

	latestStatus := httpv2.NormalizeTaskStatus(latestTask.Status)
	switch latestStatus {
	case httpv2.TaskStatusCanceled:
		return LegacyCancelResult{Decision: LegacyCancelDecisionAlreadyRequested, Status: domain.StatusCanceled}, nil
	case httpv2.TaskStatusCancelRequested:
		return LegacyCancelResult{Decision: LegacyCancelDecisionAlreadyRequested, Status: domain.StatusCancelRequested}, nil
	case httpv2.TaskStatusSuccess, httpv2.TaskStatusFailed:
		return LegacyCancelResult{Decision: LegacyCancelDecisionAlreadyFinished, Status: mapV2StatusToLegacy(latestStatus)}, nil
	case httpv2.TaskStatusQueued, httpv2.TaskStatusRunning:
		retryErr := a.store.CancelTask(ctx, taskID, latestStatus)
		if retryErr == nil {
			return LegacyCancelResult{Decision: LegacyCancelDecisionRequested, Status: domain.StatusCancelRequested}, nil
		}
		if !errors.Is(retryErr, postgres.ErrV2TaskStatusMismatchOrNotFound) {
			return LegacyCancelResult{}, retryErr
		}

		refreshedTask, refreshedErr := a.store.GetTask(ctx, taskID)
		if refreshedErr != nil {
			return LegacyCancelResult{}, refreshedErr
		}
		if refreshedTask == nil {
			return LegacyCancelResult{Decision: LegacyCancelDecisionNotFound}, nil
		}

		refreshedStatus := httpv2.NormalizeTaskStatus(refreshedTask.Status)
		switch refreshedStatus {
		case httpv2.TaskStatusCanceled:
			return LegacyCancelResult{Decision: LegacyCancelDecisionAlreadyRequested, Status: domain.StatusCanceled}, nil
		case httpv2.TaskStatusCancelRequested:
			return LegacyCancelResult{Decision: LegacyCancelDecisionAlreadyRequested, Status: domain.StatusCancelRequested}, nil
		case httpv2.TaskStatusSuccess, httpv2.TaskStatusFailed:
			return LegacyCancelResult{Decision: LegacyCancelDecisionAlreadyFinished, Status: mapV2StatusToLegacy(refreshedStatus)}, nil
		default:
			return LegacyCancelResult{Decision: LegacyCancelDecisionAlreadyFinished, Status: mapV2StatusToLegacy(refreshedStatus)}, nil
		}
	default:
		return LegacyCancelResult{Decision: LegacyCancelDecisionAlreadyFinished, Status: mapV2StatusToLegacy(latestStatus)}, nil
	}
}
