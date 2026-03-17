package bundle

import "time"

// BundleData holds structured data extracted from a Kubernetes support bundle.
// It is populated by the parser and passed to the analysis pipeline.
// Fields will be populated fully in Epic 2.
type BundleData struct {
	// ClusterVersion is the Kubernetes server version string.
	ClusterVersion string

	// NodeSummaries contains per-node health information.
	NodeSummaries []NodeSummary

	// PodSummaries contains per-pod status information.
	PodSummaries []PodSummary

	// Events contains cluster-level events, sorted chronologically.
	Events []ClusterEvent

	// LogExcerpts contains sampled log lines from failing pods.
	LogExcerpts []LogExcerpt

	// HelmReleases contains detected Helm chart releases.
	HelmReleases []HelmRelease

	// ParseErrors records non-fatal errors encountered during parsing.
	ParseErrors []string

	// TokenEstimate is a rough estimate of the total token count for this data.
	TokenEstimate int
}

// PodSummary holds status information for a single pod.
type PodSummary struct {
	// Namespace is the Kubernetes namespace.
	Namespace string

	// Name is the pod name.
	Name string

	// Phase is the pod lifecycle phase (Running, Pending, Failed, etc.).
	Phase string

	// Ready is the readiness ratio string, e.g. "1/1" or "0/2".
	Ready string

	// RestartCount is the total container restart count.
	RestartCount int

	// Reason is the failure reason if any (CrashLoopBackOff, OOMKilled, etc.).
	Reason string

	// Message is a human-readable failure message.
	Message string

	// NodeName is the node the pod is (or was) scheduled on.
	NodeName string
}

// NodeSummary holds health information for a cluster node.
type NodeSummary struct {
	// Name is the node name.
	Name string

	// Ready indicates whether the node is in a Ready condition.
	Ready bool

	// Conditions lists any non-Ready condition reasons.
	Conditions []string

	// Capacity holds the node's resource capacity (cpu, memory, etc.).
	Capacity map[string]string
}

// ClusterEvent represents a Kubernetes Event object.
type ClusterEvent struct {
	// Timestamp is the event time, preferring lastTimestamp over firstTimestamp.
	Timestamp time.Time

	// Namespace is the event's namespace.
	Namespace string

	// Name is the name of the involved object (pod, node, etc.), not the event itself.
	Name string

	// Kind is the involved object kind (Pod, Node, etc.).
	Kind string

	// Reason is the short machine-readable reason (BackOff, OOMKilling, etc.).
	Reason string

	// Message is the human-readable event message.
	Message string

	// Count is the number of times this event has occurred.
	Count int

	// Type is "Warning" or "Normal".
	Type string
}

// LogExcerpt holds a sampled set of log lines from a container.
type LogExcerpt struct {
	// Namespace is the pod's namespace.
	Namespace string

	// PodName is the pod name.
	PodName string

	// Container is the container name within the pod.
	Container string

	// Lines contains the sampled log lines (max 50).
	Lines []string

	// Truncated is true if the log was longer than the line budget.
	Truncated bool

	// ErrorCount is the number of error/fatal lines detected.
	ErrorCount int
}

// HelmRelease represents a detected Helm chart release.
type HelmRelease struct {
	// Name is the release name.
	Name string

	// Namespace is the release namespace.
	Namespace string

	// Chart is the chart name and version string.
	Chart string

	// Status is the release status (deployed, failed, etc.).
	Status string
}
