package httpapi

import (
	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

type taskCoreProgressView struct {
	Phase   string `json:"phase"`
	Current int64  `json:"current"`
	Total   int64  `json:"total"`
	Unit    string `json:"unit"`
	Message string `json:"message"`
}

type taskCoreView struct {
	ID               string               `json:"id"`
	Kind             string               `json:"task_type"`
	Status           string               `json:"status"`
	StatusLabel      string               `json:"status_label"`
	PhaseLabel       string               `json:"phase_label"`
	AvailableActions []string             `json:"available_actions"`
	Progress         taskCoreProgressView `json:"progress"`
	URL              string               `json:"url,omitempty"`
	CanonicalURL     string               `json:"canonical_url,omitempty"`
	ArtifactName     string               `json:"artifact_name,omitempty"`
	Error            string               `json:"error,omitempty"`
}

func presentTaskCoreView(view app.TaskView, komgaConfigured bool) taskCoreView {
	hasResult := view.Result != nil && view.Result.ArtifactPath != ""
	actions := domain.AvailableActions(view.Task.Status, hasResult, komgaConfigured)
	outActions := make([]string, 0, len(actions))
	for _, action := range actions {
		outActions = append(outActions, string(action))
	}
	out := taskCoreView{
		ID:               view.Task.ID,
		Kind:             string(view.Task.Kind),
		Status:           string(view.Task.Status),
		StatusLabel:      taskCoreStatusLabel(view.Task.Status),
		PhaseLabel:       view.Progress.Phase.Label(),
		AvailableActions: outActions,
		Progress: taskCoreProgressView{
			Phase:   string(view.Progress.Phase),
			Current: view.Progress.Current,
			Total:   view.Progress.Total,
			Unit:    string(view.Progress.Unit),
			Message: view.Progress.Message,
		},
		URL:          view.Input.URL,
		CanonicalURL: view.Input.CanonicalURL,
		Error:        view.Task.LastError,
	}
	if view.Result != nil {
		out.ArtifactName = view.Result.ArtifactName
	}
	return out
}

func taskCoreStatusLabel(status domain.Status) string {
	switch status {
	case domain.StatusCreated:
		return "待上传"
	case domain.StatusReady:
		return "排队中"
	case domain.StatusRunning:
		return "运行中"
	case domain.StatusCanceling:
		return "取消中"
	case domain.StatusSucceeded:
		return "成功"
	case domain.StatusFailed:
		return "失败"
	case domain.StatusCanceled:
		return "已取消"
	default:
		return "未知"
	}
}

func containsAction(actions []string, want string) bool {
	for _, action := range actions {
		if action == want {
			return true
		}
	}
	return false
}
