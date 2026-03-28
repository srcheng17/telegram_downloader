package tasks

import (
	"context"
	"errors"
	"strings"

	taskdomain "github.com/ryancheng/telegram-downloader/internal/domain/task"
)

var ErrTaskResultNotReady = errors.New("task result not ready")

type CopyTaskRecord struct {
	ID            string
	Status        string
	ResultZipPath *string
	SeriesName    *string
}

type ArtifactDescriptor struct {
	FileName string
}

type CopyTaskStore interface {
	GetTaskForCopy(ctx context.Context, taskID string) (*CopyTaskRecord, error)
}

type ArtifactDescriber interface {
	DescribeArtifact(resultPath string) (ArtifactDescriptor, error)
}

type ResultCopier interface {
	CopyFromPath(sourcePath, fileName, seriesName string) (string, error)
}

type CopyResultService struct {
	Store     CopyTaskStore
	Artifacts ArtifactDescriber
	Copier    ResultCopier
}

type CopyResult struct {
	TaskID     string
	TargetPath string
}

func NewCopyResultService(store CopyTaskStore, artifacts ArtifactDescriber, copier ResultCopier) *CopyResultService {
	return &CopyResultService{
		Store:     store,
		Artifacts: artifacts,
		Copier:    copier,
	}
}

func (s *CopyResultService) CopyToKomga(ctx context.Context, taskID string) (CopyResult, error) {
	if s == nil || s.Store == nil || s.Artifacts == nil || s.Copier == nil {
		return CopyResult{}, errors.New("copy result service dependencies are not configured")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return CopyResult{}, errors.New("task id is required")
	}

	task, err := s.Store.GetTaskForCopy(ctx, taskID)
	if err != nil {
		return CopyResult{}, err
	}
	if task == nil {
		return CopyResult{}, ErrTaskNotFound
	}
	if !taskdomain.CanAccessResult(task.Status, task.ResultZipPath != nil && strings.TrimSpace(*task.ResultZipPath) != "") {
		return CopyResult{}, ErrTaskResultNotReady
	}

	resultPath := strings.TrimSpace(*task.ResultZipPath)
	artifact, err := s.Artifacts.DescribeArtifact(resultPath)
	if err != nil {
		return CopyResult{}, err
	}
	targetPath, err := s.Copier.CopyFromPath(resultPath, artifact.FileName, stringValue(task.SeriesName))
	if err != nil {
		return CopyResult{}, err
	}
	return CopyResult{TaskID: taskID, TargetPath: targetPath}, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
