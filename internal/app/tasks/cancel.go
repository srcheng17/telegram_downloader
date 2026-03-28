package tasks

import (
	"context"
	"errors"
	"strings"

	taskdomain "github.com/ryancheng/telegram-downloader/internal/domain/task"
)

type CancelTaskRecord struct {
	ID     string
	Status string
}

type CancelStore interface {
	GetTaskForCancel(ctx context.Context, taskID string) (*CancelTaskRecord, error)
	CancelTask(ctx context.Context, taskID, fromStatus string) error
}

type CancelService struct {
	Store CancelStore
}

func NewCancelService(store CancelStore) *CancelService {
	return &CancelService{Store: store}
}

func (s *CancelService) Cancel(ctx context.Context, taskID string) (CancelResult, error) {
	if s == nil || s.Store == nil {
		return CancelResult{}, errors.New("cancel service store is not configured")
	}

	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return CancelResult{}, errors.New("task id is required")
	}

	task, err := s.Store.GetTaskForCancel(ctx, taskID)
	if err != nil {
		return CancelResult{}, err
	}
	if task == nil {
		return CancelResult{}, ErrTaskNotFound
	}

	status := normalizeStatus(task.Status)
	switch status {
	case StatusCanceled:
		return CancelResult{TaskID: taskID, Status: StatusCanceled}, nil
	case StatusCancelRequested:
		return CancelResult{TaskID: taskID, Status: StatusCancelRequested}, nil
	case StatusSuccess, StatusFailed:
		return CancelResult{}, ErrTaskNotCancelable
	}
	if !taskdomain.CanCancel(status) {
		return CancelResult{}, ErrTaskNotCancelable
	}

	if err := s.Store.CancelTask(ctx, taskID, status); err != nil {
		if errors.Is(err, ErrTaskStatusConflict) {
			latestTask, latestErr := s.Store.GetTaskForCancel(ctx, taskID)
			if latestErr != nil {
				return CancelResult{}, latestErr
			}
			if latestTask != nil {
				latestStatus := normalizeStatus(latestTask.Status)
				if latestStatus == StatusCanceled || latestStatus == StatusCancelRequested {
					return CancelResult{TaskID: taskID, Status: latestStatus}, nil
				}
			}
			return CancelResult{}, ErrTaskStatusConflict
		}
		return CancelResult{}, err
	}

	return CancelResult{TaskID: taskID, Status: StatusCancelRequested}, nil
}

