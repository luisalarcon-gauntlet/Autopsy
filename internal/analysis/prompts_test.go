package analysis

import (
	"strings"
	"testing"

	"github.com/yourusername/autopsy/internal/bundle"
)

// sampleBundleData returns a realistic BundleData for prompt tests.
func sampleBundleData() *bundle.BundleData {
	return &bundle.BundleData{
		ClusterVersion: "v1.28.4",
		NodeSummaries: []bundle.NodeSummary{
			{Name: "kind-control-plane", Ready: true, Capacity: map[string]string{"cpu": "4", "memory": "8192Mi"}},
		},
		PodSummaries: []bundle.PodSummary{
			{Namespace: "broken-app", Name: "memory-hog-7d9f8b6c5-xk2pq", Phase: "Running", Reason: "CrashLoopBackOff", RestartCount: 14, NodeName: "kind-control-plane"},
			{Namespace: "broken-app", Name: "bad-image-5c7f6d8b3-p9rtx", Phase: "Pending", Reason: "ImagePullBackOff", RestartCount: 0},
			{Namespace: "broken-app", Name: "resource-hog-4a6e5c7b2-q8swz", Phase: "Pending", Reason: "Unschedulable", RestartCount: 0},
		},
		Events: []bundle.ClusterEvent{
			{Namespace: "broken-app", Reason: "OOMKilling", Message: "Memory cgroup out of memory: Killed process 12345", Type: "Warning", Count: 14},
			{Namespace: "broken-app", Reason: "FailedScheduling", Message: "0/1 nodes are available: 1 Insufficient memory", Type: "Warning", Count: 57},
			{Namespace: "broken-app", Reason: "BackOff", Message: "Back-off pulling image nginx:doesnotexist-v999", Type: "Warning", Count: 18},
		},
		LogExcerpts: []bundle.LogExcerpt{
			{
				Namespace: "broken-app",
				PodName:   "memory-hog-7d9f8b6c5-xk2pq",
				Container: "memory-hog",
				Lines:     []string{"stress: FAIL: [1] (415) <-- worker 7 got signal 9", "stress: FAIL: [1] (451) failed run completed in 1s"},
			},
		},
		TokenEstimate: 1200,
	}
}

func TestBuildTriagePrompt_UnderBudget(t *testing.T) {
	data := sampleBundleData()
	prompt := BuildTriagePrompt(data)

	const maxChars = 40_000
	if len(prompt) > maxChars {
		t.Errorf("triage prompt too large: %d chars (max %d)", len(prompt), maxChars)
	}
}

func TestBuildTriagePrompt_ContainsRequiredFields(t *testing.T) {
	data := sampleBundleData()
	prompt := BuildTriagePrompt(data)

	required := []string{
		"severityScore",
		"clusterHealth",
		"topIssues",
		"JSON",
	}
	for _, field := range required {
		if !strings.Contains(prompt, field) {
			t.Errorf("triage prompt missing required field/keyword: %q", field)
		}
	}
}

func TestBuildTriagePrompt_IncludesPodData(t *testing.T) {
	data := sampleBundleData()
	prompt := BuildTriagePrompt(data)

	// The pod names from our sample data must appear in the prompt
	pods := []string{"memory-hog-7d9f8b6c5-xk2pq", "bad-image-5c7f6d8b3-p9rtx"}
	for _, pod := range pods {
		if !strings.Contains(prompt, pod) {
			t.Errorf("triage prompt missing pod name: %q", pod)
		}
	}
}

func TestBuildTimelinePrompt_UnderBudget(t *testing.T) {
	data := sampleBundleData()
	prompt := BuildTimelinePrompt(data)

	const maxChars = 40_000
	if len(prompt) > maxChars {
		t.Errorf("timeline prompt too large: %d chars (max %d)", len(prompt), maxChars)
	}
}

func TestBuildTimelinePrompt_ContainsEvents(t *testing.T) {
	data := sampleBundleData()
	prompt := BuildTimelinePrompt(data)

	// Event reasons should appear in the prompt
	if !strings.Contains(prompt, "OOMKilling") {
		t.Error("timeline prompt missing OOMKilling event")
	}
	if !strings.Contains(prompt, "FailedScheduling") {
		t.Error("timeline prompt missing FailedScheduling event")
	}
}

func TestBuildRCAPrompt_UnderBudget(t *testing.T) {
	data := sampleBundleData()
	prompt := BuildRCAPrompt(data)

	const maxChars = 60_000
	if len(prompt) > maxChars {
		t.Errorf("RCA prompt too large: %d chars (max %d)", len(prompt), maxChars)
	}
}

func TestBuildRCAPrompt_ContainsLogExcerpts(t *testing.T) {
	data := sampleBundleData()
	prompt := BuildRCAPrompt(data)

	// Log content should appear in RCA prompt
	if !strings.Contains(prompt, "signal 9") {
		t.Error("RCA prompt missing log excerpt content")
	}
}

func TestSystemPrompts_NotEmpty(t *testing.T) {
	prompts := map[string]string{
		"TriageSystemPrompt":   TriageSystemPrompt,
		"TimelineSystemPrompt": TimelineSystemPrompt,
		"RCASystemPrompt":      RCASystemPrompt,
	}
	for name, p := range prompts {
		if strings.TrimSpace(p) == "" {
			t.Errorf("%s is empty", name)
		}
		if len(p) < 50 {
			t.Errorf("%s is suspiciously short (%d chars)", name, len(p))
		}
	}
}

func TestSystemPrompts_ContainJSONInstruction(t *testing.T) {
	// Triage and timeline prompts must instruct Claude to return JSON
	for name, p := range map[string]string{
		"TriageSystemPrompt":   TriageSystemPrompt,
		"TimelineSystemPrompt": TimelineSystemPrompt,
	} {
		if !strings.Contains(strings.ToUpper(p), "JSON") {
			t.Errorf("%s should instruct Claude to return JSON", name)
		}
	}
}

func TestRCASystemPrompt_ContainsRequiredSections(t *testing.T) {
	// RCA prompt must mention the required markdown sections
	required := []string{"Root Cause", "Evidence", "Fix Steps", "Prevention"}
	for _, section := range required {
		if !strings.Contains(RCASystemPrompt, section) {
			t.Errorf("RCASystemPrompt missing required section: %q", section)
		}
	}
}
