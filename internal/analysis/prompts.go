// Package analysis provides the multi-phase AI analysis pipeline for Autopsy.
package analysis

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yourusername/autopsy/internal/bundle"
)

// TriageSystemPrompt instructs Claude to return a structured JSON triage report.
const TriageSystemPrompt = `You are an expert Kubernetes SRE analyzing a support bundle.
You will receive structured data extracted from a Kubernetes cluster support bundle.
Your job is to identify and prioritize issues affecting the cluster.

IMPORTANT: Respond with ONLY valid JSON. No markdown, no explanation, no code blocks.
The JSON must exactly match the schema provided in the user message.`

// TimelineSystemPrompt instructs Claude to reconstruct a failure timeline.
const TimelineSystemPrompt = `You are an expert Kubernetes SRE performing failure timeline reconstruction.
You will receive structured data from a Kubernetes support bundle including events, pod statuses, and log excerpts.
Your job is to reconstruct a chronological narrative of what happened and when.

IMPORTANT: Respond with ONLY valid JSON — a single JSON object with an "events" array.
No markdown, no explanation, no code blocks. Match the schema exactly.`

// RCASystemPrompt instructs Claude to produce a root cause analysis in structured markdown.
const RCASystemPrompt = `You are an expert Kubernetes SRE performing root cause analysis.
You will receive structured data from a Kubernetes support bundle.
Your job is to identify the root cause of failures and provide actionable remediation steps.

Format your response as structured markdown with these EXACT sections:
## Root Cause
## Evidence
## Fix Steps
## Prevention

In Fix Steps, always provide exact kubectl commands. Be specific about namespace and resource names.
In Evidence, cite specific pod names, event reasons, and log lines from the data provided.`

// BuildTriagePrompt constructs the user-turn prompt for Phase 1 triage analysis.
// It serializes the BundleData as JSON and appends the required output schema.
func BuildTriagePrompt(data *bundle.BundleData) string {
	dataJSON, _ := json.MarshalIndent(data, "", "  ")

	return fmt.Sprintf(`Analyze this Kubernetes support bundle data and return a JSON triage report.

Bundle data:
%s

Return JSON matching exactly this schema:
{
  "severityScore": <0-100, where 100 is completely broken>,
  "summary": "<1-2 sentence overview of cluster health>",
  "clusterHealth": "<one of: critical|degraded|warning|healthy>",
  "affectedNamespaces": ["<namespace>"],
  "topIssues": [
    {
      "title": "<short title>",
      "severity": "<critical|high|medium|low>",
      "affectedPod": "<pod name or empty string>",
      "category": "<oom|image-pull|crash-loop|config|resource|network|unknown>"
    }
  ]
}

Return ONLY the JSON. No other text.`, string(dataJSON))
}

// BuildTimelinePrompt constructs the user-turn prompt for Phase 2 timeline reconstruction.
func BuildTimelinePrompt(data *bundle.BundleData) string {
	dataJSON, _ := json.MarshalIndent(data, "", "  ")

	return fmt.Sprintf(`Analyze this Kubernetes support bundle data and reconstruct a chronological failure timeline.

Bundle data:
%s

Return JSON matching exactly this schema:
{
  "events": [
    {
      "relativeTime": "<e.g. T+0:00, T+3:42>",
      "title": "<one-line description of the event>",
      "detail": "<1-2 sentences elaborating on what happened>",
      "severity": "<info|warning|critical>",
      "linkedPod": "<pod name or empty string>"
    }
  ]
}

Order events chronologically (earliest first). Use T+0:00 for the first event.
Include 4-8 events that tell the story of how the failures developed.
Return ONLY the JSON. No other text.`, string(dataJSON))
}

// BuildRCAPrompt constructs the user-turn prompt for Phase 3 root cause analysis.
func BuildRCAPrompt(data *bundle.BundleData) string {
	dataJSON, _ := json.MarshalIndent(data, "", "  ")

	return fmt.Sprintf(`Perform a root cause analysis on this Kubernetes support bundle.

Bundle data:
%s

Produce a structured markdown report with the following sections in order:
1. ## Root Cause — What is the primary failure and why did it occur?
2. ## Evidence — Specific pod names, event reasons, log lines, and metrics from the bundle that support your conclusion.
3. ## Fix Steps — Numbered list of exact kubectl commands to remediate the issues.
4. ## Prevention — How to prevent recurrence.

Be specific. Reference actual pod names, namespaces, and error messages from the bundle.`, string(dataJSON))
}

// extractJSONFromMarkdown strips markdown code fences from a string that may
// wrap JSON in ` + "```" + `json ... ` + "```" + ` or plain ` + "```" + ` ... ` + "```" + ` blocks.
func extractJSONFromMarkdown(s string) string {
	if start := strings.Index(s, "```json"); start != -1 {
		s = s[start+7:]
		if end := strings.Index(s, "```"); end != -1 {
			return strings.TrimSpace(s[:end])
		}
	}
	if start := strings.Index(s, "```"); start != -1 {
		s = s[start+3:]
		if end := strings.Index(s, "```"); end != -1 {
			return strings.TrimSpace(s[:end])
		}
	}
	return strings.TrimSpace(s)
}
