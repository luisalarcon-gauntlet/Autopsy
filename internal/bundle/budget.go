package bundle

import (
	"encoding/json"
	"log/slog"
)

const (
	// MaxTokenBudget is the hard upper limit on tokens passed to Claude.
	MaxTokenBudget = 80_000

	// budgetLogLineCap is the per-container line count after the first trim pass.
	budgetLogLineCap = 20

	// budgetEventMsgCap is the maximum event message length after the fourth trim pass.
	budgetEventMsgCap = 100
)

// EstimateTokens returns a rough token-count estimate for data.
// It serializes the struct to JSON and divides by 4 (≈ 4 chars per token).
func EstimateTokens(data *BundleData) int {
	b, err := json.Marshal(data)
	if err != nil {
		return 0
	}
	return len(b) / 4
}

// EnforceBudget trims data in progressive passes until EstimateTokens(data)
// is at or below maxTokens. A warning is logged if any trimming is applied.
// The TokenEstimate field is always updated before returning.
//
// Trim order (each pass re-checks the budget before continuing):
//  1. Truncate log excerpts to 20 lines each.
//  2. Drop healthy pods (Reason == "").
//  3. Drop Normal events entirely.
//  4. Truncate event messages to 100 characters.
func EnforceBudget(data *BundleData, maxTokens int) *BundleData {
	if EstimateTokens(data) <= maxTokens {
		data.TokenEstimate = EstimateTokens(data)
		return data
	}

	slog.Warn("token budget exceeded, trimming BundleData",
		"estimate", EstimateTokens(data),
		"limit", maxTokens,
	)

	// Pass 1: truncate log excerpts.
	for i := range data.LogExcerpts {
		if len(data.LogExcerpts[i].Lines) > budgetLogLineCap {
			data.LogExcerpts[i].Lines = data.LogExcerpts[i].Lines[:budgetLogLineCap]
			data.LogExcerpts[i].Truncated = true
		}
	}
	if EstimateTokens(data) <= maxTokens {
		data.TokenEstimate = EstimateTokens(data)
		return data
	}

	// Pass 2: drop healthy pods.
	unhealthy := data.PodSummaries[:0]
	for _, p := range data.PodSummaries {
		if p.Reason != "" {
			unhealthy = append(unhealthy, p)
		}
	}
	data.PodSummaries = unhealthy
	if EstimateTokens(data) <= maxTokens {
		data.TokenEstimate = EstimateTokens(data)
		return data
	}

	// Pass 3: drop Normal events.
	warnings := data.Events[:0]
	for _, e := range data.Events {
		if e.Type == "Warning" {
			warnings = append(warnings, e)
		}
	}
	data.Events = warnings
	if EstimateTokens(data) <= maxTokens {
		data.TokenEstimate = EstimateTokens(data)
		return data
	}

	// Pass 4: truncate event messages.
	for i := range data.Events {
		if len(data.Events[i].Message) > budgetEventMsgCap {
			data.Events[i].Message = data.Events[i].Message[:budgetEventMsgCap]
		}
	}

	data.TokenEstimate = EstimateTokens(data)
	slog.Info("BundleData trimmed to fit token budget", "finalEstimate", data.TokenEstimate)
	return data
}
