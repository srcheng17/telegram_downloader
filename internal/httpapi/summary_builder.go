package httpapi

import (
	"math"

	"github.com/ryancheng/telegram-downloader/internal/domain"
)

func buildSummaryFromCounts(counts map[string]int, recovery domain.StartupRecovery) domain.Summary {
	summary := domain.Summary{
		PendingTasks:         safeCount(counts[domain.StatusPending]),
		InProgressTasks:      safeCount(counts[domain.StatusUploading]) + safeCount(counts[domain.StatusInProgress]),
		CancelRequestedTasks: safeCount(counts[domain.StatusCancelRequested]),
		CanceledTasks:        safeCount(counts[domain.StatusCanceled]),
		SuccessTasks:         safeCount(counts[domain.StatusSuccess]),
		FailedTasks:          safeCount(counts[domain.StatusFailed]),
		StartupRecovery:      recovery,
	}

	for _, count := range counts {
		summary.TotalTasks += safeCount(count)
	}

	summary.ActiveTasks = summary.PendingTasks + summary.InProgressTasks + summary.CancelRequestedTasks
	summary.FinishedTasks = summary.SuccessTasks + summary.FailedTasks + summary.CanceledTasks
	if summary.FinishedTasks > 0 {
		rate := float64(summary.SuccessTasks) / float64(summary.FinishedTasks) * 100
		rounded := math.Round(rate*10) / 10
		summary.SuccessRate = &rounded
	}

	return summary
}
