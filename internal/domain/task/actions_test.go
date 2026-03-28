package task

import "testing"

func TestCanRetryAllowsFailedAndCanceledTasks(t *testing.T) {
	t.Parallel()

	if !CanRetry(TaskTypeURL, StatusFailed, true, false) {
		t.Fatalf("expected failed url task with url to be retryable")
	}
	if !CanRetry(TaskTypeURL, StatusCanceled, true, false) {
		t.Fatalf("expected canceled url task with url to be retryable")
	}
	if !CanRetry(TaskTypeUpload, StatusFailed, false, true) {
		t.Fatalf("expected failed upload task with source archive to be retryable")
	}
	if CanRetry(TaskTypeUpload, StatusUploading, false, true) {
		t.Fatalf("expected uploading task to remain non-retryable")
	}
}

func TestCanCancelOnlyAllowsActiveTasks(t *testing.T) {
	t.Parallel()

	active := []string{StatusUploading, StatusQueued, StatusRunning}
	for _, status := range active {
		if !CanCancel(status) {
			t.Fatalf("expected %q to be cancelable", status)
		}
	}

	inactive := []string{StatusCancelRequested, StatusSuccess, StatusFailed, StatusCanceled}
	for _, status := range inactive {
		if CanCancel(status) {
			t.Fatalf("expected %q to be non-cancelable", status)
		}
	}
}

