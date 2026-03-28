package task

func CanRetry(taskType, status string, hasURL, hasSourceArchive bool) bool {
	switch NormalizeStatus(status) {
	case StatusFailed, StatusCanceled:
	default:
		return false
	}

	switch NormalizeTaskType(taskType) {
	case TaskTypeUpload:
		return hasSourceArchive
	default:
		return hasURL
	}
}

func CanCancel(status string) bool {
	switch NormalizeStatus(status) {
	case StatusUploading, StatusQueued, StatusRunning:
		return true
	default:
		return false
	}
}

func CanAccessResult(status string, hasResultArtifact bool) bool {
	return NormalizeStatus(status) == StatusSuccess && hasResultArtifact
}

