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
	ID                string               `json:"id"`
	Kind              string               `json:"task_type"`
	Status            string               `json:"status"`
	StatusLabel       string               `json:"status_label"`
	PhaseLabel        string               `json:"phase_label"`
	AvailableActions  []string             `json:"available_actions"`
	Progress          taskCoreProgressView `json:"progress"`
	URL               string               `json:"url,omitempty"`
	CanonicalURL      string               `json:"canonical_url,omitempty"`
	SourceArchiveName string               `json:"source_archive_name,omitempty"`
	UploadLoadedBytes int64                `json:"upload_loaded_bytes,omitempty"`
	UploadTotalBytes  int64                `json:"upload_total_bytes,omitempty"`
	Retryable         bool                 `json:"retryable,omitempty"`
	StartTime         float64              `json:"start_time,omitempty"`
	ArtifactName      string               `json:"artifact_name,omitempty"`
	Error             string               `json:"error,omitempty"`
	Author            string               `json:"author,omitempty"`
	SeriesName        string               `json:"series_name,omitempty"`
	ComicName         string               `json:"comic_name,omitempty"`
	Summary           string               `json:"summary,omitempty"`
	TagsRaw           string               `json:"tags_raw,omitempty"`
	TagsNormalized    string               `json:"tags_normalized,omitempty"`
	GenresRaw         string               `json:"genres_raw,omitempty"`
	GenresNormalized  string               `json:"genres_normalized,omitempty"`
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
		URL:               view.Input.URL,
		CanonicalURL:      view.Input.CanonicalURL,
		SourceArchiveName: view.Input.SourceArchiveName,
		UploadTotalBytes:  view.Input.SourceArchiveSize,
		Error:             view.Task.LastError,
	}
	out.Retryable = containsAction(outActions, string(domain.ActionRetry))
	if view.Input.SourceArchiveSize > 0 {
		out.UploadLoadedBytes = view.Input.SourceArchiveSize
	}
	if !view.Task.CreatedAt.IsZero() {
		out.StartTime = float64(view.Task.CreatedAt.UnixNano()) / float64(1_000_000_000)
	}
	out.Author = metadataViewValue(view.Input.Metadata, "author")
	out.SeriesName = metadataViewValue(view.Input.Metadata, "series_name")
	out.ComicName = metadataViewValue(view.Input.Metadata, "comic_name")
	out.Summary = metadataViewValue(view.Input.Metadata, "summary")
	out.TagsRaw = metadataViewValue(view.Input.Metadata, "tags")
	out.TagsNormalized = metadataViewValue(view.Input.Metadata, "tags_normalized")
	out.GenresRaw = metadataViewValue(view.Input.Metadata, "genres")
	out.GenresNormalized = metadataViewValue(view.Input.Metadata, "genres_normalized")
	if view.Result != nil {
		out.ArtifactName = view.Result.ArtifactName
	}
	return out
}

func metadataViewValue(metadata map[string]string, key string) string {
	if metadata == nil {
		return ""
	}
	return metadata[key]
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
