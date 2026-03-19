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

Format your response as structured markdown with these EXACT sections in this EXACT order:

## TL;DR
**Cluster health:** [one sentence describing overall cluster state]
**Critical pods:** [count] failing, [count] healthy
**Root cause confidence:** [High/Medium/Low]
**Estimated fix time:** [X minutes]

## Root Cause
## Evidence
## Fix Steps
## Patch Files
## Prevention

Always begin with the TL;DR section exactly as shown above before any other content.
In Fix Steps, always provide exact kubectl commands. Be specific about namespace and resource names.
In Evidence, cite specific pod names, event reasons, and log lines from the data provided.
In Patch Files, provide at least one Kubernetes YAML manifest per fix as a yaml code block that can be applied directly with kubectl apply -f.`

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
1. ## TL;DR — Four bold fields: Cluster health (one sentence), Critical pods (X failing, Y healthy), Root cause confidence (High/Medium/Low), Estimated fix time (X minutes).
2. ## Root Cause — What is the primary failure and why did it occur?
3. ## Evidence — Specific pod names, event reasons, log lines, and metrics from the bundle that support your conclusion.
4. ## Fix Steps — Numbered list of exact kubectl commands to remediate the issues.
5. ## Patch Files — At least one Kubernetes YAML manifest per fix, each in its own yaml code block, ready for kubectl apply -f.
6. ## Prevention — How to prevent recurrence.

Be specific. Reference actual pod names, namespaces, and error messages from the bundle.`, string(dataJSON))
}

// ChatSystemPrompt instructs Claude to stay grounded in the provided bundle context.
const ChatSystemPrompt = `You are an expert Kubernetes SRE analyzing a specific support bundle.
Only reference information that appears in the bundle data provided.
If asked about something not in the bundle, say "I don't see that in this bundle."
Be concise and actionable. When providing remediation steps, include exact kubectl commands.`

// buildChatBundleContext returns a compact (<2000 char) summary of BundleData
// suitable for injecting into the chat system prompt.
func buildChatBundleContext(data *bundle.BundleData) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Cluster version: %s\n", data.ClusterVersion)
	fmt.Fprintf(&sb, "Nodes: %d\n", len(data.NodeSummaries))

	issueCount := 0
	fmt.Fprintf(&sb, "Pods with issues:\n")
	for _, p := range data.PodSummaries {
		if (p.Reason != "" || p.RestartCount > 0) && issueCount < 15 {
			fmt.Fprintf(&sb, "  - %s/%s: phase=%s reason=%s restarts=%d\n",
				p.Namespace, p.Name, p.Phase, p.Reason, p.RestartCount)
			issueCount++
		}
	}

	eventCount := 0
	fmt.Fprintf(&sb, "Warning events:\n")
	for _, e := range data.Events {
		if e.Type == "Warning" && eventCount < 10 {
			msg := e.Message
			if len(msg) > 100 {
				msg = msg[:100] + "..."
			}
			fmt.Fprintf(&sb, "  - [%s] %s: %s\n", e.Namespace, e.Reason, msg)
			eventCount++
		}
	}

	result := sb.String()
	if len(result) > 2000 {
		result = result[:2000] + "\n...(truncated)"
	}
	return result
}

// BuildChatSystemPrompt creates the full system prompt for a chat turn,
// embedding a compact bundle context to ground responses.
func BuildChatSystemPrompt(data *bundle.BundleData) string {
	return fmt.Sprintf("%s\n\nBundle context:\n%s", ChatSystemPrompt, buildChatBundleContext(data))
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
