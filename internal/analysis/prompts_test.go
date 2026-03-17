package analysis

import (
	"context"
	"strings"
	"testing"

	"github.com/yourusername/autopsy/internal/bundle"
)

// makeSampleBundleData returns a BundleData with realistic content for prompt tests.
func makeSampleBundleData() *bundle.BundleData {
	return &bundle.BundleData{
		ClusterVersion: "v1.28.4",
		NodeSummaries: []bundle.NodeSummary{
			{Name: "node-1", Ready: true, Capacity: map[string]string{"cpu": "4", "memory": "16Gi"}},
			{Name: "node-2", Ready: false, Conditions: []string{"DiskPressure"}},
		},
		PodSummaries: []bundle.PodSummary{
			{Namespace: "payments", Name: "payment-processor-7d9f5b8c4-xk2pq", Phase: "Running",
				Ready: "0/1", RestartCount: 12, Reason: "CrashLoopBackOff",
				Message: "back-off 5m0s restarting failed container"},
			{Namespace: "data-pipeline", Name: "data-ingestion-worker-6b4d9c7f5-m3nrs", Phase: "Running",
				Ready: "0/1", RestartCount: 3, Reason: "OOMKilled",
				Message: "Container exceeded memory limit"},
		},
		Events: []bundle.ClusterEvent{
			{Namespace: "payments", Name: "payment-processor-7d9f5b8c4-xk2pq",
				Kind: "Pod", Reason: "BackOff", Type: "Warning",
				Message: "Back-off restarting failed container", Count: 47},
			{Namespace: "data-pipeline", Name: "data-ingestion-worker-6b4d9c7f5-m3nrs",
				Kind: "Pod", Reason: "OOMKilling", Type: "Warning",
				Message: "Memory cgroup out of memory", Count: 3},
		},
		LogExcerpts: []bundle.LogExcerpt{
			{Namespace: "payments", PodName: "payment-processor-7d9f5b8c4-xk2pq",
				Container: "payment-processor",
				Lines: []string{
					"FATAL: runtime error: invalid memory address or nil pointer dereference",
					"goroutine 1 [running]:",
					"pkg/db/pool.go:47 +0x1f4",
				},
				ErrorCount: 1},
		},
		ParseErrors:   []string{},
		TokenEstimate: 1500,
	}
}

func TestBuildTriagePromptUnderBudget(t *testing.T) {
	data := makeSampleBundleData()
	prompt := BuildTriagePrompt(data)

	if len(prompt) == 0 {
		t.Fatal("BuildTriagePrompt returned empty string")
	}

	if len(prompt) > maxTriagePromptChars {
		t.Errorf("triage prompt exceeds budget: %d chars > %d", len(prompt), maxTriagePromptChars)
	}
}

func TestBuildTimelinePromptUnderBudget(t *testing.T) {
	data := makeSampleBundleData()
	prompt := BuildTimelinePrompt(data)

	if len(prompt) == 0 {
		t.Fatal("BuildTimelinePrompt returned empty string")
	}

	if len(prompt) > maxTimelinePromptChars {
		t.Errorf("timeline prompt exceeds budget: %d chars > %d", len(prompt), maxTimelinePromptChars)
	}
}

func TestBuildRCAPromptUnderBudget(t *testing.T) {
	data := makeSampleBundleData()
	prompt := BuildRCAPrompt(data)

	if len(prompt) == 0 {
		t.Fatal("BuildRCAPrompt returned empty string")
	}

	if len(prompt) > maxRCAPromptChars {
		t.Errorf("RCA prompt exceeds budget: %d chars > %d", len(prompt), maxRCAPromptChars)
	}
}

func TestBuildTriagePromptContainsSchema(t *testing.T) {
	data := makeSampleBundleData()
	prompt := BuildTriagePrompt(data)

	requiredFields := []string{"severityScore", "clusterHealth", "topIssues", "affectedNamespaces"}
	for _, field := range requiredFields {
		if !containsStr(prompt, field) {
			t.Errorf("triage prompt missing schema field: %s", field)
		}
	}
}

func TestExtractJSONFromMarkdown(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain JSON",
			input: `{"key": "value"}`,
			want:  `{"key": "value"}`,
		},
		{
			name: "JSON in json fences",
			input: "```json\n{\"key\": \"value\"}\n```",
			want:  `{"key": "value"}`,
		},
		{
			name: "JSON in plain fences",
			input: "```\n{\"key\": \"value\"}\n```",
			want:  `{"key": "value"}`,
		},
		{
			name:  "whitespace trimmed",
			input: "  {\"key\": \"value\"}  ",
			want:  `{"key": "value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSONFromMarkdown(tt.input)
			if got != tt.want {
				t.Errorf("extractJSONFromMarkdown(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseTriageJSON(t *testing.T) {
	result, err := parseTriageJSON(StubTriageJSON)
	if err != nil {
		t.Fatalf("parseTriageJSON failed on StubTriageJSON: %v", err)
	}
	if result.SeverityScore == 0 {
		t.Error("expected non-zero SeverityScore")
	}
	if len(result.TopIssues) == 0 {
		t.Error("expected at least one issue in TopIssues")
	}
	if result.ClusterHealth == "" {
		t.Error("expected non-empty ClusterHealth")
	}
}

func TestBuildStubTimelineResult(t *testing.T) {
	result := buildStubTimelineResult()
	if len(result.Events) == 0 {
		t.Fatal("expected at least one timeline event")
	}

	hasCritical := false
	for _, e := range result.Events {
		if e.Severity == "critical" {
			hasCritical = true
		}
		if e.RelativeTime == "" {
			t.Errorf("timeline event missing RelativeTime: %+v", e)
		}
		if e.Title == "" {
			t.Errorf("timeline event missing Title: %+v", e)
		}
	}

	if !hasCritical {
		t.Error("expected at least one critical event in stub timeline")
	}
}

func TestParseTimelineJSON(t *testing.T) {
	sampleJSON := `{
		"events": [
			{
				"relativeTime": "T+0:00",
				"title": "First failure event",
				"detail": "Something went wrong with the pod startup.",
				"severity": "critical",
				"linkedPod": "payment-processor-7d9f5b8c4-xk2pq"
			},
			{
				"relativeTime": "T+5:00",
				"title": "Secondary OOM event",
				"detail": "Worker exceeded memory limit.",
				"severity": "warning",
				"linkedPod": "data-ingestion-worker-6b4d9c7f5-m3nrs"
			}
		]
	}`

	result, err := parseTimelineJSON(sampleJSON)
	if err != nil {
		t.Fatalf("parseTimelineJSON failed: %v", err)
	}
	if len(result.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(result.Events))
	}
	if result.Events[0].RelativeTime != "T+0:00" {
		t.Errorf("expected first event at T+0:00, got %q", result.Events[0].RelativeTime)
	}
	if result.Events[0].Severity != "critical" {
		t.Errorf("expected first event severity=critical, got %q", result.Events[0].Severity)
	}
}

func TestBuildTimelinePromptContainsSchema(t *testing.T) {
	data := makeSampleBundleData()
	prompt := BuildTimelinePrompt(data)

	requiredFields := []string{"relativeTime", "title", "severity", "linkedPod"}
	for _, field := range requiredFields {
		if !containsStr(prompt, field) {
			t.Errorf("timeline prompt missing schema field: %s", field)
		}
	}
}

func TestBuildRCAPromptContainsSections(t *testing.T) {
	data := makeSampleBundleData()
	prompt := BuildRCAPrompt(data)

	requiredSections := []string{"Root Cause", "Evidence", "Fix Steps", "Prevention"}
	for _, section := range requiredSections {
		if !containsStr(prompt, section) {
			t.Errorf("RCA prompt missing section: %s", section)
		}
	}
}

func TestStreamStubText(t *testing.T) {
	var buf strings.Builder
	ctx := context.Background()

	err := streamStubText(ctx, StubRCAText, &buf)
	if err != nil {
		t.Fatalf("streamStubText failed: %v", err)
	}

	got := buf.String()
	if got != StubRCAText {
		t.Errorf("streamStubText output mismatch: got %d bytes, want %d bytes", len(got), len(StubRCAText))
	}
}

func TestStreamStubTextContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	var buf strings.Builder
	err := streamStubText(ctx, StubRCAText, &buf)
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
}

func containsStr(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 &&
		(func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		})()
}
