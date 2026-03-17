package bundle

import (
	"context"
	"testing"
)

const testdataDir = "testdata"

func TestParse_FullBundle(t *testing.T) {
	data, err := Parse(context.Background(), testdataDir)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	// Cluster version
	if data.ClusterVersion == "" {
		t.Error("ClusterVersion should not be empty")
	}
	if data.ClusterVersion != "v1.28.4" {
		t.Errorf("ClusterVersion = %q, want %q", data.ClusterVersion, "v1.28.4")
	}

	// Nodes
	if len(data.NodeSummaries) == 0 {
		t.Error("NodeSummaries should not be empty")
	}
	node := data.NodeSummaries[0]
	if node.Name != "kind-control-plane" {
		t.Errorf("node.Name = %q, want %q", node.Name, "kind-control-plane")
	}
	if !node.Ready {
		t.Error("node should be Ready=true")
	}
}

func TestParse_PodStatuses(t *testing.T) {
	data, err := Parse(context.Background(), testdataDir)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	// Build a lookup map for easy assertions
	pods := make(map[string]PodSummary)
	for _, p := range data.PodSummaries {
		pods[p.Name] = p
	}

	tests := []struct {
		podName       string
		wantReason    string
		wantRestarts  int
		wantNamespace string
		wantPhase     string
	}{
		{
			podName:       "memory-hog-7d9f8b6c5-xk2pq",
			wantReason:    "CrashLoopBackOff",
			wantRestarts:  14,
			wantNamespace: "broken-app",
			wantPhase:     "Running",
		},
		{
			podName:       "bad-entrypoint-6b8d7f9c4-m3nvw",
			wantReason:    "CrashLoopBackOff",
			wantRestarts:  8,
			wantNamespace: "broken-app",
			wantPhase:     "Running",
		},
		{
			podName:       "bad-image-5c7f6d8b3-p9rtx",
			wantReason:    "ImagePullBackOff",
			wantRestarts:  0,
			wantNamespace: "broken-app",
			wantPhase:     "Pending",
		},
		{
			podName:       "resource-hog-4a6e5c7b2-q8swz",
			wantReason:    "Unschedulable",
			wantRestarts:  0,
			wantNamespace: "broken-app",
			wantPhase:     "Pending",
		},
		{
			podName:       "missing-config-3f5d4b6a1-r7tvx",
			wantReason:    "CreateContainerConfigError",
			wantRestarts:  0,
			wantNamespace: "broken-app",
			wantPhase:     "Pending",
		},
	}

	for _, tt := range tests {
		t.Run(tt.podName, func(t *testing.T) {
			pod, ok := pods[tt.podName]
			if !ok {
				t.Fatalf("pod %q not found in parsed summaries", tt.podName)
			}
			if pod.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", pod.Reason, tt.wantReason)
			}
			if pod.RestartCount != tt.wantRestarts {
				t.Errorf("RestartCount = %d, want %d", pod.RestartCount, tt.wantRestarts)
			}
			if pod.Namespace != tt.wantNamespace {
				t.Errorf("Namespace = %q, want %q", pod.Namespace, tt.wantNamespace)
			}
			if pod.Phase != tt.wantPhase {
				t.Errorf("Phase = %q, want %q", pod.Phase, tt.wantPhase)
			}
		})
	}
}

func TestParse_HealthyPodNotFlagged(t *testing.T) {
	data, err := Parse(context.Background(), testdataDir)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	for _, p := range data.PodSummaries {
		if p.Name == "healthy-nginx-2e4c3a5z0-k5mnp" {
			// Healthy pod should have no reason and zero restarts
			if p.Reason != "" {
				t.Errorf("healthy pod Reason = %q, want empty", p.Reason)
			}
			if p.RestartCount != 0 {
				t.Errorf("healthy pod RestartCount = %d, want 0", p.RestartCount)
			}
			return
		}
	}
	// It's fine if the healthy pod is omitted from summaries entirely
}

func TestParse_Events(t *testing.T) {
	data, err := Parse(context.Background(), testdataDir)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	if len(data.Events) == 0 {
		t.Fatal("Events should not be empty")
	}

	// Events must be sorted chronologically (oldest first)
	for i := 1; i < len(data.Events); i++ {
		if data.Events[i].Timestamp.Before(data.Events[i-1].Timestamp) {
			t.Errorf("events not sorted: event[%d] (%v) is before event[%d] (%v)",
				i, data.Events[i].Timestamp,
				i-1, data.Events[i-1].Timestamp)
		}
	}

	// Must contain Warning events
	warningCount := 0
	for _, e := range data.Events {
		if e.Type == "Warning" {
			warningCount++
		}
	}
	if warningCount == 0 {
		t.Error("expected at least one Warning event")
	}

	// Check for specific expected events
	reasons := make(map[string]bool)
	for _, e := range data.Events {
		reasons[e.Reason] = true
	}

	expectedReasons := []string{"OOMKilling", "BackOff", "Failed", "FailedScheduling"}
	for _, r := range expectedReasons {
		if !reasons[r] {
			t.Errorf("expected event reason %q not found in events", r)
		}
	}
}

func TestParse_LogExcerpts(t *testing.T) {
	data, err := Parse(context.Background(), testdataDir)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	// Log excerpts should only exist for unhealthy pods
	logPods := make(map[string]bool)
	for _, l := range data.LogExcerpts {
		logPods[l.PodName] = true
	}

	// OOMKilled pod should have logs
	if !logPods["memory-hog-7d9f8b6c5-xk2pq"] {
		t.Error("expected log excerpt for OOMKilled pod memory-hog")
	}

	// CrashLoopBackOff pod should have logs
	if !logPods["bad-entrypoint-6b8d7f9c4-m3nvw"] {
		t.Error("expected log excerpt for CrashLoopBackOff pod bad-entrypoint")
	}

	// Healthy pod should NOT have logs extracted (no restarts, Running)
	if logPods["healthy-nginx-2e4c3a5z0-k5mnp"] {
		t.Error("healthy pod should not have log excerpts")
	}

	// Lines must be within budget
	for _, l := range data.LogExcerpts {
		if len(l.Lines) > maxLogLines {
			t.Errorf("pod %q has %d log lines, exceeds max of %d",
				l.PodName, len(l.Lines), maxLogLines)
		}
	}
}

func TestParse_MissingDirectoryGraceful(t *testing.T) {
	// Pointing at a directory with no cluster-resources/ should not fatal
	// It should populate ParseErrors and return partial data
	data, err := Parse(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Parse() should not error on empty dir, got: %v", err)
	}

	// Should have parse errors noting missing files
	if len(data.ParseErrors) == 0 {
		t.Error("expected ParseErrors for missing directories, got none")
	}
}

func TestParse_TokenEstimate(t *testing.T) {
	data, err := Parse(context.Background(), testdataDir)
	if err != nil {
		t.Fatalf("Parse() unexpected error: %v", err)
	}

	if data.TokenEstimate == 0 {
		t.Error("TokenEstimate should be non-zero after parsing")
	}

	// For our small fixture, estimate should be well under the max budget
	if data.TokenEstimate > maxTokenBudget {
		t.Errorf("TokenEstimate %d exceeds maxTokenBudget %d for test fixture",
			data.TokenEstimate, maxTokenBudget)
	}
}

func TestEnforceBudget(t *testing.T) {
	// Build a BundleData that's intentionally over budget
	huge := &BundleData{}
	// Add many pods with long messages
	for i := 0; i < 200; i++ {
		huge.PodSummaries = append(huge.PodSummaries, PodSummary{
			Name:      "pod-" + string(rune('a'+i%26)),
			Namespace: "default",
			Phase:     "Running",
			Reason:    "CrashLoopBackOff",
			Message:   "This is a very long error message that takes up a lot of tokens when you have many pods all reporting errors simultaneously in a large cluster deployment scenario",
		})
	}
	// Add many log lines
	for i := 0; i < 100; i++ {
		lines := make([]string, 50)
		for j := range lines {
			lines[j] = "ERROR: some very detailed error log line with lots of information that consumes tokens"
		}
		huge.LogExcerpts = append(huge.LogExcerpts, LogExcerpt{
			PodName: "pod-" + string(rune('a'+i%26)),
			Lines:   lines,
		})
	}
	huge.TokenEstimate = EstimateTokens(huge)

	trimmed := EnforceBudget(huge, maxTokenBudget)

	trimmedEstimate := EstimateTokens(trimmed)
	if trimmedEstimate > maxTokenBudget {
		t.Errorf("EnforceBudget did not reduce to budget: got %d tokens, max %d",
			trimmedEstimate, maxTokenBudget)
	}
}
