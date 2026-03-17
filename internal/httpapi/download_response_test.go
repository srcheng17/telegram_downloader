package httpapi

import (
	"net/http"
	"testing"

	"github.com/ryancheng/telegram-downloader/internal/domain"
)

func TestBuildClaimDownloadDecisionResponse(t *testing.T) {
	statusCode, payload, err := buildClaimDownloadDecisionResponse("task-created", domain.ClaimDecisionCreated, true)
	if err != nil {
		t.Fatalf("build claim response: %v", err)
	}
	if statusCode != http.StatusAccepted {
		t.Fatalf("expected created response status 202, got %d", statusCode)
	}
	if payload["task_id"] != "task-created" || payload["duplicate"] != false || payload["force_applied"] != true {
		t.Fatalf("unexpected created payload: %#v", payload)
	}

	statusCode, payload, err = buildClaimDownloadDecisionResponse("task-active", domain.ClaimDecisionReuseActive, false)
	if err != nil {
		t.Fatalf("build active response: %v", err)
	}
	if statusCode != http.StatusOK || payload["active"] != true || payload["logs_url"] != "/logs" {
		t.Fatalf("unexpected active payload: %#v", payload)
	}

	statusCode, payload, err = buildClaimDownloadDecisionResponse("task-success", domain.ClaimDecisionReuseSuccess, false)
	if err != nil {
		t.Fatalf("build success response: %v", err)
	}
	if statusCode != http.StatusOK || payload["needs_confirmation"] != true || payload["download_url"] != "/api/tasks/task-success/download" {
		t.Fatalf("unexpected reuse_success payload: %#v", payload)
	}
}

func TestBuildLegacyDownloadDecisionResponse(t *testing.T) {
	statusCode, payload, err := buildLegacyDownloadDecisionResponse("legacy-task", LegacyDownloadDecisionReuseSuccess, true)
	if err != nil {
		t.Fatalf("build legacy response: %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("expected legacy response status 200, got %d", statusCode)
	}
	if payload["download_url"] != "/api/tasks/legacy-task/download" || payload["force_applied"] != true {
		t.Fatalf("unexpected legacy payload: %#v", payload)
	}
}
