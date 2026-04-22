package httpapi

import (
	"testing"

	app "github.com/ryancheng/telegram-downloader/internal/app/taskcore"
	domain "github.com/ryancheng/telegram-downloader/internal/domain/taskcore"
)

func TestPresentTaskCoreViewUsesDomainActions(t *testing.T) {
	t.Parallel()

	view := presentTaskCoreView(app.TaskView{
		Task:     app.Task{ID: "task-1", Kind: domain.KindURL, Status: domain.StatusSucceeded},
		Progress: domain.NewProgress(domain.PhaseDone, 1, 1, domain.UnitSteps, ""),
		Result:   &app.Result{TaskID: "task-1", ArtifactPath: "/tmp/a.cbz", ArtifactName: "a.cbz", ArtifactSize: 1},
	}, true)

	if view.StatusLabel != "成功" {
		t.Fatalf("status label = %q, want 成功", view.StatusLabel)
	}
	if view.PhaseLabel != "已完成" {
		t.Fatalf("phase label = %q, want 已完成", view.PhaseLabel)
	}
	if !containsAction(view.AvailableActions, "download") || !containsAction(view.AvailableActions, "copy_to_komga") {
		t.Fatalf("actions = %#v, want download and copy_to_komga", view.AvailableActions)
	}
}

func TestPresentTaskCoreViewExposesRetryForFailedAndCanceledTasks(t *testing.T) {
	t.Parallel()

	for _, status := range []domain.Status{domain.StatusFailed, domain.StatusCanceled} {
		status := status
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()

			view := presentTaskCoreView(app.TaskView{
				Task: app.Task{ID: "task-1", Status: status},
			}, true)

			if !containsAction(view.AvailableActions, "retry") {
				t.Fatalf("actions = %#v, want retry", view.AvailableActions)
			}
		})
	}
}

func TestPresentTaskCoreViewExposesCancelForActiveTasks(t *testing.T) {
	t.Parallel()

	for _, status := range []domain.Status{domain.StatusCreated, domain.StatusReady, domain.StatusRunning} {
		status := status
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()

			view := presentTaskCoreView(app.TaskView{
				Task: app.Task{ID: "task-1", Status: status},
			}, true)

			if !containsAction(view.AvailableActions, "cancel") {
				t.Fatalf("actions = %#v, want cancel", view.AvailableActions)
			}
		})
	}
}
