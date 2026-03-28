package httpapi

import (
	"context"
	"errors"
	"strings"

	"github.com/ryancheng/telegram-downloader/internal/httpv2"
	"github.com/ryancheng/telegram-downloader/internal/service"
)

func (a *LegacyAdapter) OpenTaskArtifact(ctx context.Context, taskID string) (*service.OpenedV2Artifact, error) {
	if !a.SupportsArtifactDownload() {
		return nil, errors.New("legacy adapter artifact dependencies are not configured")
	}

	normalizedID := strings.TrimSpace(taskID)
	task, err := a.store.GetTask(ctx, normalizedID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, ErrLegacyAdapterTaskNotFound
	}
	if httpv2.NormalizeTaskStatus(task.Status) != httpv2.TaskStatusSuccess {
		return nil, ErrLegacyAdapterTaskNotReady
	}

	resultPath := ""
	if task.ResultZipPath != nil {
		resultPath = strings.TrimSpace(*task.ResultZipPath)
	}
	if resultPath == "" {
		return nil, ErrLegacyAdapterTaskOutputNotFound
	}

	artifact, err := a.artifacts.OpenArtifact(resultPath)
	if err != nil {
		if errors.Is(err, service.ErrV2ArtifactNotFound) || errors.Is(err, service.ErrV2ArtifactPathInvalid) {
			return nil, ErrLegacyAdapterArtifactUnavailable
		}
		return nil, err
	}
	return artifact, nil
}
