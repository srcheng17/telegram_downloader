package tasks

import (
	"context"
	"errors"
	"math"
	"net/url"
	"strings"

	"github.com/ryancheng/telegram-downloader/internal/domain"
)

type LegacyDownloadDecision string

const (
	LegacyDownloadDecisionCreated      LegacyDownloadDecision = "created"
	LegacyDownloadDecisionReuseSuccess LegacyDownloadDecision = "reuse_success"
	LegacyDownloadDecisionReuseActive  LegacyDownloadDecision = "reuse_active"
)

type LegacySubmitInput struct {
	RawURL       string
	CanonicalURL string
	ReuseSuccess bool
	Metadata     MetadataInput
}

type LegacySubmitResult struct {
	Decision    LegacyDownloadDecision
	TaskID      string
	DownloadURL string
}

type LegacyClaimInput struct {
	RawURL       string
	CanonicalURL string
	ReuseSuccess bool
	Metadata     Metadata
}

type LegacyClaimResult struct {
	Decision LegacyDownloadDecision
	TaskID   string
}

type LegacyDownloadClaimer interface {
	ClaimOrReuse(ctx context.Context, in LegacyClaimInput) (LegacyClaimResult, error)
}

type LegacySummaryReader interface {
	GetStatusCounts(ctx context.Context) (map[string]int, error)
}

type LegacySummaryResult struct {
	Summary       domain.Summary
	StatusCatalog map[string]StatusMeta
}

type LegacyBridge struct {
	Downloader    LegacyDownloadClaimer
	SummaryReader LegacySummaryReader
}

func NewLegacyBridge(downloader LegacyDownloadClaimer, summaryReader LegacySummaryReader) *LegacyBridge {
	return &LegacyBridge{
		Downloader:    downloader,
		SummaryReader: summaryReader,
	}
}

func (b *LegacyBridge) Submit(ctx context.Context, input LegacySubmitInput) (LegacySubmitResult, error) {
	if b == nil || b.Downloader == nil {
		return LegacySubmitResult{}, errors.New("legacy bridge download dependencies are not configured")
	}

	rawURL := strings.TrimSpace(input.RawURL)
	if rawURL == "" {
		return LegacySubmitResult{}, errors.New("legacy bridge raw url is required")
	}

	claim, err := b.Downloader.ClaimOrReuse(ctx, LegacyClaimInput{
		RawURL:       rawURL,
		CanonicalURL: canonicalizeLegacyURL(input.CanonicalURL, rawURL),
		ReuseSuccess: input.ReuseSuccess,
		Metadata:     NormalizeMetadata(input.Metadata),
	})
	if err != nil {
		return LegacySubmitResult{}, err
	}

	result := LegacySubmitResult{
		Decision: claim.Decision,
		TaskID:   strings.TrimSpace(claim.TaskID),
	}
	if result.Decision == LegacyDownloadDecisionReuseSuccess && result.TaskID != "" {
		result.DownloadURL = "/api/tasks/" + url.PathEscape(result.TaskID) + "/download"
	}

	return result, nil
}

func (b *LegacyBridge) BuildSummary(ctx context.Context) (LegacySummaryResult, error) {
	if b == nil || b.SummaryReader == nil {
		return LegacySummaryResult{}, errors.New("legacy bridge summary dependencies are not configured")
	}

	counts, err := b.SummaryReader.GetStatusCounts(ctx)
	if err != nil {
		return LegacySummaryResult{}, err
	}

	legacyCounts := map[string]int{
		domain.StatusPending:         safeCount(counts[StatusQueued]),
		domain.StatusInProgress:      safeCount(counts[StatusRunning]),
		domain.StatusCancelRequested: safeCount(counts[StatusCancelRequested]),
		domain.StatusSuccess:         safeCount(counts[StatusSuccess]),
		domain.StatusFailed:          safeCount(counts[StatusFailed]),
		domain.StatusCanceled:        safeCount(counts[StatusCanceled]),
	}

	return LegacySummaryResult{
		Summary:       buildLegacySummaryFromCounts(legacyCounts, domain.DefaultStartupRecovery()),
		StatusCatalog: Catalog(),
	}, nil
}

func buildLegacySummaryFromCounts(counts map[string]int, recovery domain.StartupRecovery) domain.Summary {
	summary := domain.Summary{
		PendingTasks:         safeCount(counts[domain.StatusPending]),
		InProgressTasks:      safeCount(counts[domain.StatusInProgress]),
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

func canonicalizeLegacyURL(canonicalURL string, rawURL string) string {
	normalizedCanonical := strings.TrimSpace(canonicalURL)
	if normalizedCanonical != "" {
		return normalizedCanonical
	}
	return strings.TrimSpace(rawURL)
}

func safeCount(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
