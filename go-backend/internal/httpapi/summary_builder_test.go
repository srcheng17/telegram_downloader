package httpapi

import (
	"testing"

	"github.com/ryancheng/telegram-downloader/go-backend/internal/domain"
)

func TestBuildSummaryFromCountsDerivesTotalsAndRate(t *testing.T) {
	summary := buildSummaryFromCounts(map[string]int{
		"PENDING":          2,
		"IN_PROGRESS":      3,
		"CANCEL_REQUESTED": 1,
		"SUCCESS":          9,
		"FAILED":           1,
		"CANCELED":         2,
	}, domain.DefaultStartupRecovery())

	if summary.TotalTasks != 18 {
		t.Fatalf("expected total_tasks=18, got %d", summary.TotalTasks)
	}
	if summary.ActiveTasks != 6 {
		t.Fatalf("expected active_tasks=6, got %d", summary.ActiveTasks)
	}
	if summary.FinishedTasks != 12 {
		t.Fatalf("expected finished_tasks=12, got %d", summary.FinishedTasks)
	}
	if summary.SuccessRate == nil || *summary.SuccessRate != 75 {
		t.Fatalf("expected success_rate=75, got %#v", summary.SuccessRate)
	}
	if summary.StartupRecovery.Happened {
		t.Fatalf("expected startup_recovery.happened=false, got %#v", summary.StartupRecovery)
	}
}
