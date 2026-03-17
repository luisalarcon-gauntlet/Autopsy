package analysis

import (
	"fmt"
	"strings"

	"github.com/yourusername/autopsy/internal/bundle"
)

// maxContextChars is the maximum number of characters returned by FindRelevantContext.
const maxContextChars = 2000

// FindRelevantContext searches the BundleData for content relevant to the query
// and returns a compact summary (at most maxContextChars) for injection into the
// chat prompt. It tokenises the query into keywords and scores pod summaries,
// events and log excerpts by keyword overlap.
func FindRelevantContext(query string, data *bundle.BundleData) string {
	if data == nil {
		return ""
	}

	keywords := tokenize(query)
	if len(keywords) == 0 {
		return buildFallbackContext(data)
	}

	var matched []string

	// Score and collect pod summaries.
	for _, p := range data.PodSummaries {
		if matchesAny(keywords, p.Name, p.Namespace, p.Reason, p.Message, p.Phase) {
			matched = append(matched, fmt.Sprintf("Pod %s/%s: phase=%s reason=%s restarts=%d",
				p.Namespace, p.Name, p.Phase, p.Reason, p.RestartCount))
		}
	}

	// Score and collect warning events.
	for _, e := range data.Events {
		if e.Type == "Warning" && matchesAny(keywords, e.Name, e.Namespace, e.Reason, e.Message) {
			msg := e.Message
			if len(msg) > 120 {
				msg = msg[:120] + "..."
			}
			matched = append(matched, fmt.Sprintf("Event [%s] %s: %s", e.Namespace, e.Reason, msg))
		}
	}

	// Score and collect log excerpts.
	for _, l := range data.LogExcerpts {
		if matchesAny(keywords, l.PodName, l.Namespace, l.Container) {
			for _, line := range l.Lines {
				if matchesAny(keywords, line) {
					short := line
					if len(short) > 120 {
						short = short[:120] + "..."
					}
					matched = append(matched, fmt.Sprintf("Log [%s/%s] %s", l.Namespace, l.PodName, short))
				}
			}
		}
	}

	if len(matched) == 0 {
		return buildFallbackContext(data)
	}

	// Cap at top 5 matches.
	const maxMatches = 5
	if len(matched) > maxMatches {
		matched = matched[:maxMatches]
	}

	result := "Relevant bundle data:\n" + strings.Join(matched, "\n")
	if len(result) > maxContextChars {
		result = result[:maxContextChars] + "\n...(truncated)"
	}
	return result
}

// buildFallbackContext returns a summary of all pods when no keywords matched.
func buildFallbackContext(data *bundle.BundleData) string {
	var sb strings.Builder
	sb.WriteString("Pod summary:\n")
	count := 0
	for _, p := range data.PodSummaries {
		if count >= 10 {
			break
		}
		fmt.Fprintf(&sb, "  %s/%s: phase=%s reason=%s restarts=%d\n",
			p.Namespace, p.Name, p.Phase, p.Reason, p.RestartCount)
		count++
	}
	result := sb.String()
	if len(result) > maxContextChars {
		result = result[:maxContextChars]
	}
	return result
}

// tokenize splits a query string into lowercase keywords, filtering short words.
func tokenize(query string) []string {
	words := strings.Fields(strings.ToLower(query))
	keywords := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.Trim(w, ".,?!;:'\"()")
		if len(w) >= 3 {
			keywords = append(keywords, w)
		}
	}
	return keywords
}

// matchesAny returns true if any keyword appears as a substring in any field.
func matchesAny(keywords []string, fields ...string) bool {
	for _, kw := range keywords {
		for _, f := range fields {
			if strings.Contains(strings.ToLower(f), kw) {
				return true
			}
		}
	}
	return false
}
