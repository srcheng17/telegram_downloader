package task

import "strings"

const (
	TaskTypeURL    = "url"
	TaskTypeUpload = "upload"
)

const (
	StatusUploading       = "UPLOADING"
	StatusQueued          = "QUEUED"
	StatusRunning         = "RUNNING"
	StatusCancelRequested = "CANCEL_REQUESTED"
	StatusSuccess         = "SUCCESS"
	StatusFailed          = "FAILED"
	StatusCanceled        = "CANCELED"
)

func NormalizeStatus(status string) string {
	return strings.ToUpper(strings.TrimSpace(status))
}

func NormalizeTaskType(taskType string) string {
	return strings.ToLower(strings.TrimSpace(taskType))
}

func IsTerminalStatus(status string) bool {
	switch NormalizeStatus(status) {
	case StatusSuccess, StatusFailed, StatusCanceled:
		return true
	default:
		return false
	}
}

