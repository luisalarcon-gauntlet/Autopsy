package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/yourusername/autopsy/internal/bundle"
)

// maxPromptChars defines per-phase character budgets (roughly 4 chars per token).
const (
	maxTriagePromptChars   = 40_000
	maxTimelinePromptChars = 40_000
	maxRCAPromptChars      = 60_000

	stubStreamChunkSize = 50
	stubStreamDelayMS   = 30
)

// RunTriage executes Phase 1 analysis: structured JSON triage of cluster health.
// In stub mode it returns StubTriageResult after a short simulated delay.
func RunTriage(ctx context.Context, client *anthropic.Client, data *bundle.BundleData, stubMode bool) (*TriageResult, error) {
	if stubMode {
		time.Sleep(500 * time.Millisecond)
		return parseTriageJSON(StubTriageJSON)
	}

	prompt := BuildTriagePrompt(data)
	if err := checkPromptBudget(prompt, maxTriagePromptChars); err != nil {
		slog.Warn("triage prompt over budget", "err", err)
	}

	text, err := withRetry(ctx, func() (string, error) {
		msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.ModelClaudeSonnet4_5_20250929,
			MaxTokens: 1024,
			System: []anthropic.TextBlockParam{
				{Text: TriageSystemPrompt},
			},
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
			},
		})
		if err != nil {
			return "", fmt.Errorf("claude messages.new: %w", err)
		}
		if len(msg.Content) == 0 {
			return "", fmt.Errorf("empty response from Claude")
		}
		return msg.Content[0].Text, nil
	})
	if err != nil {
		return nil, classifyAPIError("RunTriage", err)
	}

	return parseTriageJSON(text)
}

// RunTimeline executes Phase 2 analysis: chronological failure timeline reconstruction.
// In stub mode it returns StubTimelineResult after a short simulated delay.
func RunTimeline(ctx context.Context, client *anthropic.Client, data *bundle.BundleData, stubMode bool) (*TimelineResult, error) {
	if stubMode {
		time.Sleep(500 * time.Millisecond)
		return buildStubTimelineResult(), nil
	}

	prompt := BuildTimelinePrompt(data)
	if err := checkPromptBudget(prompt, maxTimelinePromptChars); err != nil {
		slog.Warn("timeline prompt over budget", "err", err)
	}

	text, err := withRetry(ctx, func() (string, error) {
		msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.ModelClaudeSonnet4_5_20250929,
			MaxTokens: 2048,
			System: []anthropic.TextBlockParam{
				{Text: TimelineSystemPrompt},
			},
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
			},
		})
		if err != nil {
			return "", fmt.Errorf("claude messages.new: %w", err)
		}
		if len(msg.Content) == 0 {
			return "", fmt.Errorf("empty response from Claude")
		}
		return msg.Content[0].Text, nil
	})
	if err != nil {
		return nil, classifyAPIError("RunTimeline", err)
	}

	return parseTimelineJSON(text)
}

// RunRCA executes Phase 3 analysis: streaming root cause analysis written to w.
// In stub mode it writes StubRCAText to w in small chunks to simulate streaming.
func RunRCA(ctx context.Context, client *anthropic.Client, data *bundle.BundleData, stubMode bool, w io.Writer) error {
	if stubMode {
		return streamStubText(ctx, StubRCAText, w)
	}

	prompt := BuildRCAPrompt(data)
	if err := checkPromptBudget(prompt, maxRCAPromptChars); err != nil {
		slog.Warn("RCA prompt over budget", "err", err)
	}

	stream := client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_5_20250929,
		MaxTokens: 4096,
		System: []anthropic.TextBlockParam{
			{Text: RCASystemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})

	for stream.Next() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		event := stream.Current()
		if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" {
			if _, err := w.Write([]byte(event.Delta.Text)); err != nil {
				return fmt.Errorf("RunRCA write: %w", err)
			}
		}
	}

	if err := stream.Err(); err != nil {
		return classifyAPIError("RunRCA", err)
	}

	return nil
}

// parseTriageJSON unmarshals a Claude JSON response into a TriageResult.
// It handles optional markdown code fences wrapping the JSON.
func parseTriageJSON(text string) (*TriageResult, error) {
	var result TriageResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		cleaned := extractJSONFromMarkdown(text)
		if err2 := json.Unmarshal([]byte(cleaned), &result); err2 != nil {
			return nil, fmt.Errorf("parse triage JSON: %w (raw: %.200s)", err2, text)
		}
	}
	return &result, nil
}

// parseTimelineJSON unmarshals a Claude JSON response into a TimelineResult.
func parseTimelineJSON(text string) (*TimelineResult, error) {
	var result TimelineResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		cleaned := extractJSONFromMarkdown(text)
		if err2 := json.Unmarshal([]byte(cleaned), &result); err2 != nil {
			return nil, fmt.Errorf("parse timeline JSON: %w (raw: %.200s)", err2, text)
		}
	}
	return &result, nil
}

// buildStubTimelineResult converts the flat StubTimelineEvents slice to a TimelineResult.
func buildStubTimelineResult() *TimelineResult {
	events := make([]TimelineEvent, 0, len(StubTimelineEvents))
	for _, e := range StubTimelineEvents {
		events = append(events, TimelineEvent{
			RelativeTime: e["relativeTime"],
			Title:        e["title"],
			Detail:       e["detail"],
			Severity:     e["severity"],
			LinkedPod:    e["linkedPod"],
		})
	}
	return &TimelineResult{Events: events}
}

// streamStubText writes text to w in small fixed-size chunks with a small delay
// between each chunk to simulate a streaming response.
func streamStubText(ctx context.Context, text string, w io.Writer) error {
	runes := []rune(text)
	for i := 0; i < len(runes); i += stubStreamChunkSize {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		end := i + stubStreamChunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunk := string(runes[i:end])
		if _, err := w.Write([]byte(chunk)); err != nil {
			return fmt.Errorf("streamStubText write: %w", err)
		}

		time.Sleep(stubStreamDelayMS * time.Millisecond)
	}
	return nil
}

// checkPromptBudget returns an error if the prompt exceeds the character limit.
func checkPromptBudget(prompt string, maxChars int) error {
	if len(prompt) > maxChars {
		return fmt.Errorf("prompt too large: %d chars (max %d)", len(prompt), maxChars)
	}
	return nil
}

// withRetry calls fn once, and retries exactly once if the API returns a 529
// (overloaded) error, waiting 2 seconds before the retry.
func withRetry(ctx context.Context, fn func() (string, error)) (string, error) {
	result, err := fn()
	if err == nil {
		return result, nil
	}

	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode == 529 {
		slog.Warn("Claude overloaded (529), retrying in 2s")
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return "", ctx.Err()
		}
		return fn()
	}

	return "", err
}

// classifyAPIError maps Anthropic SDK errors to user-friendly error messages.
func classifyAPIError(op string, err error) error {
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		return fmt.Errorf("%s: could not reach analysis service: %w", op, err)
	}

	switch apiErr.StatusCode {
	case 429:
		return fmt.Errorf("%s: analysis rate limited — please retry in 60 seconds", op)
	case 529:
		return fmt.Errorf("%s: analysis temporarily unavailable — service overloaded", op)
	case 401:
		return fmt.Errorf("%s: API key invalid or missing", op)
	case 400:
		return fmt.Errorf("%s: analysis request was malformed", op)
	default:
		return fmt.Errorf("%s: analysis failed (HTTP %d)", op, apiErr.StatusCode)
	}
}
