//go:build evals

package analysis

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/yourusername/autopsy/internal/bundle"
)

// RunRCASync is a non-streaming variant of RunRCA for use in evals only.
// It calls client.Messages.New() and returns the full RCA text as a single string,
// avoiding the SSE timeout issues that affect the streaming version in test contexts.
func RunRCASync(ctx context.Context, client *anthropic.Client, data *bundle.BundleData, stubMode bool) (string, error) {
	if stubMode {
		return StubRCAText, nil
	}

	prompt := BuildRCAPrompt(data)
	if err := checkPromptBudget(prompt, maxRCAPromptChars); err != nil {
		slog.Warn("RCA prompt over budget", "err", err)
	}

	text, err := withRetry(ctx, func() (string, error) {
		msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.ModelClaudeSonnet4_5_20250929,
			MaxTokens: 4096,
			System: []anthropic.TextBlockParam{
				{Text: RCASystemPrompt},
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
		return "", classifyAPIError("RunRCASync", err)
	}

	return text, nil
}
