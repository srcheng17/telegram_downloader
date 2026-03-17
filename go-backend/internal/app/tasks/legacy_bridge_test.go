package tasks

import (
	"context"
	"testing"
)

type fakeLegacyDownloadClaimer struct {
	result LegacyClaimResult
	call   LegacyClaimInput
}

func (f *fakeLegacyDownloadClaimer) ClaimOrReuse(_ context.Context, in LegacyClaimInput) (LegacyClaimResult, error) {
	f.call = in
	return f.result, nil
}

type fakeLegacySummaryReader struct {
	counts map[string]int
}

func (f *fakeLegacySummaryReader) GetStatusCounts(context.Context) (map[string]int, error) {
	return f.counts, nil
}

func TestLegacyBridgeReuseSuccessKeepsExistingDownload(t *testing.T) {
	claimer := &fakeLegacyDownloadClaimer{
		result: LegacyClaimResult{Decision: LegacyDownloadDecisionReuseSuccess, TaskID: "task-existing"},
	}
	bridge := NewLegacyBridge(claimer, &fakeLegacySummaryReader{})

	result, err := bridge.Submit(context.Background(), LegacySubmitInput{
		RawURL:       "https://telegra.ph/demo",
		CanonicalURL: "https://telegra.ph/demo",
		ReuseSuccess: true,
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if result.Decision != LegacyDownloadDecisionReuseSuccess {
		t.Fatalf("expected reuse_success decision, got %q", result.Decision)
	}
	if result.DownloadURL == "" {
		t.Fatalf("expected download url for reusable task")
	}
}

func TestLegacyBridgeSummaryUsesSharedStatusCatalog(t *testing.T) {
	bridge := NewLegacyBridge(&fakeLegacyDownloadClaimer{}, &fakeLegacySummaryReader{
		counts: map[string]int{StatusSuccess: 1, StatusRunning: 1},
	})

	summary, err := bridge.BuildSummary(context.Background())
	if err != nil {
		t.Fatalf("build summary: %v", err)
	}
	if summary.StatusCatalog[StatusSuccess].CanDownload != true {
		t.Fatalf("expected success status to be downloadable")
	}
	if summary.Summary.SuccessTasks != 1 {
		t.Fatalf("expected success_tasks=1, got %d", summary.Summary.SuccessTasks)
	}
}
