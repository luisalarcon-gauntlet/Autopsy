package analysis

// Result holds the combined output of all three analysis phases.
// Individual phase results may be nil until that phase completes.
type Result struct {
	// Triage is the output of Phase 1 (structured JSON analysis).
	Triage *TriageResult

	// Timeline is the output of Phase 2 (chronological event reconstruction).
	Timeline *TimelineResult

	// RCAText is the accumulated streaming text from Phase 3.
	RCAText string
}

// TriageResult is the structured JSON output of Phase 1 analysis.
type TriageResult struct {
	// SeverityScore is a 0-100 score where 100 means completely broken.
	SeverityScore int `json:"severityScore"`

	// Summary is a 1-2 sentence overview of cluster health.
	Summary string `json:"summary"`

	// TopIssues is the ranked list of detected issues.
	TopIssues []Issue `json:"topIssues"`

	// AffectedNS is the list of affected Kubernetes namespaces.
	AffectedNS []string `json:"affectedNamespaces"`

	// ClusterHealth is one of: "critical", "degraded", "warning", "healthy".
	ClusterHealth string `json:"clusterHealth"`
}

// Issue represents a single detected problem in the cluster.
type Issue struct {
	// Title is a short human-readable description.
	Title string `json:"title"`

	// Severity is one of "critical", "high", "medium", "low".
	Severity string `json:"severity"`

	// AffectedPod is the pod name, if applicable.
	AffectedPod string `json:"affectedPod"`

	// Category classifies the issue type.
	// Valid values: "oom", "image-pull", "crash-loop", "config", "resource", "network", "unknown".
	Category string `json:"category"`
}

// TimelineResult is the structured output of Phase 2 analysis.
type TimelineResult struct {
	// Events is an ordered list of timeline events.
	Events []TimelineEvent `json:"events"`
}

// TimelineEvent represents a single entry in the failure timeline.
type TimelineEvent struct {
	// RelativeTime is a human-readable offset from the first event, e.g. "T+0:00".
	RelativeTime string `json:"relativeTime"`

	// Title is a one-line description of the event.
	Title string `json:"title"`

	// Detail is a 1-2 sentence elaboration.
	Detail string `json:"detail"`

	// Severity is one of "info", "warning", "critical".
	Severity string `json:"severity"`

	// LinkedPod is the affected pod name, if any.
	LinkedPod string `json:"linkedPod"`
}

// ChatMessage represents a single turn in the chat conversation.
type ChatMessage struct {
	// Role is "user" or "assistant".
	Role string

	// Content is the message text.
	Content string
}
