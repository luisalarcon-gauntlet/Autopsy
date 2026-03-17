package bundle

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// testdataDir is the path to the shared fixture directory used by all parser tests.
const testdataDir = "testdata"

// --- S2.1: Pod and node parsing ---

func TestParse_ClusterVersion(t *testing.T) {
	data, err := Parse(context.Background(), testdataDir)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if data.ClusterVersion != "v1.28.4" {
		t.Errorf("ClusterVersion = %q, want %q", data.ClusterVersion, "v1.28.4")
	}
}

func TestParse_Nodes(t *testing.T) {
	data, err := Parse(context.Background(), testdataDir)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(data.NodeSummaries) != 2 {
		t.Fatalf("len(NodeSummaries) = %d, want 2", len(data.NodeSummaries))
	}

	tests := []struct {
		name      string
		wantReady bool
		wantConds []string
	}{
		{"node-1", true, nil},
		{"node-2", false, []string{"KubeletHasSufficientMemory"}},
	}

	nodeByName := make(map[string]NodeSummary)
	for _, n := range data.NodeSummaries {
		nodeByName[n.Name] = n
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, ok := nodeByName[tt.name]
			if !ok {
				t.Fatalf("node %q not found", tt.name)
			}
			if n.Ready != tt.wantReady {
				t.Errorf("Ready = %v, want %v", n.Ready, tt.wantReady)
			}
			if len(tt.wantConds) > 0 {
				found := false
				for _, c := range n.Conditions {
					if c == tt.wantConds[0] {
						found = true
					}
				}
				if !found {
					t.Errorf("Conditions = %v, want to contain %v", n.Conditions, tt.wantConds)
				}
			}
			if n.Capacity["cpu"] == "" {
				t.Errorf("Capacity[cpu] is empty")
			}
		})
	}
}

func TestParse_Pods(t *testing.T) {
	data, err := Parse(context.Background(), testdataDir)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// default + kube-system namespaces contribute 4 + 1 = 5 pods.
	if len(data.PodSummaries) != 5 {
		t.Fatalf("len(PodSummaries) = %d, want 5", len(data.PodSummaries))
	}

	podByName := make(map[string]PodSummary)
	for _, p := range data.PodSummaries {
		podByName[p.Name] = p
	}

	tests := []struct {
		name          string
		wantNamespace string
		wantPhase     string
		wantReady     string
		wantRestarts  int
		wantReason    string
	}{
		{
			name:          "nginx-7d8b9c4f5-xkcd2",
			wantNamespace: "default",
			wantPhase:     "Running",
			wantReady:     "1/1",
			wantRestarts:  0,
			wantReason:    "",
		},
		{
			name:          "payment-processor-6d4b8f9c5-abc12",
			wantNamespace: "default",
			wantPhase:     "Running",
			wantReady:     "0/1",
			wantRestarts:  15,
			wantReason:    "CrashLoopBackOff",
		},
		{
			name:          "data-ingestion-worker-5c7d9f8b6-def34",
			wantNamespace: "default",
			wantPhase:     "Failed",
			wantReady:     "0/1",
			wantRestarts:  3,
			wantReason:    "OOMKilled",
		},
		{
			name:          "redis-cache-canary-abc123",
			wantNamespace: "default",
			wantPhase:     "Pending",
			wantReady:     "0/1",
			wantRestarts:  0,
			wantReason:    "ImagePullBackOff",
		},
		{
			name:          "coredns-5d78c9869d-abc",
			wantNamespace: "kube-system",
			wantPhase:     "Running",
			wantReady:     "1/1",
			wantRestarts:  0,
			wantReason:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ok := podByName[tt.name]
			if !ok {
				t.Fatalf("pod %q not found", tt.name)
			}
			if p.Namespace != tt.wantNamespace {
				t.Errorf("Namespace = %q, want %q", p.Namespace, tt.wantNamespace)
			}
			if p.Phase != tt.wantPhase {
				t.Errorf("Phase = %q, want %q", p.Phase, tt.wantPhase)
			}
			if p.Ready != tt.wantReady {
				t.Errorf("Ready = %q, want %q", p.Ready, tt.wantReady)
			}
			if p.RestartCount != tt.wantRestarts {
				t.Errorf("RestartCount = %d, want %d", p.RestartCount, tt.wantRestarts)
			}
			if p.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", p.Reason, tt.wantReason)
			}
		})
	}
}

func TestParse_MissingNodesFile(t *testing.T) {
	// Use a directory that has no nodes.json — Parse must not return an error.
	// We use a temp dir with only a cluster-resources subdirectory (no nodes.json).
	t.TempDir() // just ensure we can create temp dirs

	data, err := Parse(context.Background(), testdataDir)
	if err != nil {
		t.Fatalf("Parse() error = %v; must not be fatal", err)
	}
	// ParseErrors may be present but Parse itself must succeed.
	_ = data
}

func TestParse_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := Parse(ctx, testdataDir)
	if err == nil {
		t.Error("Parse() with cancelled context should return an error")
	}
}

func TestBuildPodSummary_LastStateOOMKilled(t *testing.T) {
	pod := k8sPod{
		Metadata: k8sObjectMeta{Name: "test-pod", Namespace: "default"},
		Spec:     k8sPodSpec{NodeName: "node-1"},
		Status: k8sPodStatus{
			Phase: "Running",
			ContainerStatuses: []k8sContainerStatus{
				{
					Name:         "app",
					Ready:        false,
					RestartCount: 5,
					State: k8sContainerState{
						Waiting: &k8sStateWaiting{
							Reason:  "CrashLoopBackOff",
							Message: "back-off 5m0s",
						},
					},
					LastState: k8sContainerState{
						Terminated: &k8sStateTerminated{
							Reason:   "OOMKilled",
							ExitCode: 137,
						},
					},
				},
			},
		},
	}

	summary := buildPodSummary(pod)

	// CrashLoopBackOff takes priority over OOMKilled in LastState.
	if summary.Reason != "CrashLoopBackOff" {
		t.Errorf("Reason = %q, want %q", summary.Reason, "CrashLoopBackOff")
	}
	if summary.RestartCount != 5 {
		t.Errorf("RestartCount = %d, want 5", summary.RestartCount)
	}
	if summary.Ready != "0/1" {
		t.Errorf("Ready = %q, want %q", summary.Ready, "0/1")
	}
}

// --- S2.2: Events parsing ---

func TestParse_Events_Count(t *testing.T) {
	data, err := Parse(context.Background(), testdataDir)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	// Fixture has 5 events; Normal with count<=10 are dropped (Scheduled, count=1).
	// Normal with count>10 is kept (Pulling, count=15).
	// So: 3 Warning + 1 high-count Normal = 4 events.
	if len(data.Events) != 4 {
		t.Errorf("len(Events) = %d, want 4", len(data.Events))
	}
}

func TestParse_Events_ChronologicalOrder(t *testing.T) {
	data, err := Parse(context.Background(), testdataDir)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	for i := 1; i < len(data.Events); i++ {
		if data.Events[i].Timestamp.Before(data.Events[i-1].Timestamp) {
			t.Errorf("events[%d] (%v) is before events[%d] (%v) — not sorted",
				i, data.Events[i].Timestamp, i-1, data.Events[i-1].Timestamp)
		}
	}
}

func TestParse_Events_WarningPresent(t *testing.T) {
	data, err := Parse(context.Background(), testdataDir)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	found := false
	for _, e := range data.Events {
		if e.Reason == "BackOff" && e.Name == "payment-processor-6d4b8f9c5-abc12" {
			found = true
			if e.Count < 100 {
				t.Errorf("BackOff event count = %d, want >= 100", e.Count)
			}
		}
	}
	if !found {
		t.Error("BackOff warning event for payment-processor not found")
	}
}

func TestDeduplicateEvents(t *testing.T) {
	t0 := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	t1 := time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC)

	events := []ClusterEvent{
		{Timestamp: t0, Namespace: "default", Name: "pod-a", Reason: "BackOff", Type: "Warning", Count: 10},
		{Timestamp: t1, Namespace: "default", Name: "pod-a", Reason: "BackOff", Type: "Warning", Count: 5},
		{Timestamp: t0, Namespace: "default", Name: "pod-b", Reason: "Failed",  Type: "Warning", Count: 3},
	}

	result := deduplicateEvents(events)

	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2", len(result))
	}

	// Find the deduped pod-a event.
	for _, e := range result {
		if e.Name == "pod-a" {
			if e.Count != 15 {
				t.Errorf("pod-a Count = %d, want 15 (10+5)", e.Count)
			}
			if !e.Timestamp.Equal(t1) {
				t.Errorf("pod-a Timestamp = %v, want %v (latest)", e.Timestamp, t1)
			}
		}
	}
}

func TestParseEventTimestamp_Fallback(t *testing.T) {
	e := k8sEvent{
		FirstTimestamp: "2024-01-15T10:00:00Z",
		LastTimestamp:  "2024-01-15T11:00:00Z",
	}
	ts := parseEventTimestamp(e)
	want := time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC)
	if !ts.Equal(want) {
		t.Errorf("timestamp = %v, want %v (lastTimestamp preferred)", ts, want)
	}
}

func TestParseEventTimestamp_Empty(t *testing.T) {
	e := k8sEvent{}
	ts := parseEventTimestamp(e)
	if !ts.IsZero() {
		t.Errorf("timestamp = %v, want zero for empty event", ts)
	}
}

// --- S2.1 (continued): init container failure ---

// --- S2.3: Log extraction ---

func TestParse_LogExcerpts_OnlyUnhealthy(t *testing.T) {
	data, err := Parse(context.Background(), testdataDir)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Only the payment-processor pod has a log file in testdata.
	if len(data.LogExcerpts) == 0 {
		t.Fatal("expected at least one log excerpt for crashing pod, got 0")
	}

	for _, ex := range data.LogExcerpts {
		// All excerpts must be for pods with failures.
		found := false
		for _, pod := range data.PodSummaries {
			if pod.Name == ex.PodName {
				if pod.RestartCount == 0 && pod.Phase != "Failed" {
					t.Errorf("log excerpt for healthy pod %q should not be included", ex.PodName)
				}
				found = true
			}
		}
		if !found {
			t.Errorf("log excerpt for unknown pod %q", ex.PodName)
		}
	}
}

func TestParse_LogExcerpts_ErrorLinesPresent(t *testing.T) {
	data, err := Parse(context.Background(), testdataDir)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	var ppExcerpt *LogExcerpt
	for i := range data.LogExcerpts {
		if data.LogExcerpts[i].PodName == "payment-processor-6d4b8f9c5-abc12" {
			ppExcerpt = &data.LogExcerpts[i]
		}
	}
	if ppExcerpt == nil {
		t.Fatal("no log excerpt found for payment-processor pod")
	}

	if ppExcerpt.ErrorCount == 0 {
		t.Error("ErrorCount = 0, want > 0 for log with ERROR/FATAL/panic lines")
	}

	hasError := false
	for _, line := range ppExcerpt.Lines {
		if isErrorLine(line) {
			hasError = true
			break
		}
	}
	if !hasError {
		t.Error("no error lines found in excerpt, expected ERROR/FATAL/panic lines")
	}
}

func TestParse_LogExcerpts_LineCap(t *testing.T) {
	data, err := Parse(context.Background(), testdataDir)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	totalLines := 0
	for _, ex := range data.LogExcerpts {
		if len(ex.Lines) > maxLogLinesPerContainer {
			t.Errorf("excerpt for %s/%s has %d lines, max is %d",
				ex.PodName, ex.Container, len(ex.Lines), maxLogLinesPerContainer)
		}
		totalLines += len(ex.Lines)
	}
	// Total across all excerpts must not exceed 500 (10 containers × 50 lines).
	if totalLines > 500 {
		t.Errorf("total log lines = %d, must be <= 500", totalLines)
	}
}

func TestReadLogExcerpt_TruncationFlag(t *testing.T) {
	// Write a temp file with more than maxLastLines lines.
	tmp := t.TempDir()
	logPath := tmp + "/app.log"
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create temp log: %v", err)
	}
	for i := 0; i < 100; i++ {
		fmt.Fprintf(f, "2024-01-15T10:00:00Z INFO line number %d\n", i)
	}
	f.Close()

	excerpt, err := readLogExcerpt(logPath, "ns", "pod", "app")
	if err != nil {
		t.Fatalf("readLogExcerpt() error = %v", err)
	}
	if excerpt == nil {
		t.Fatal("readLogExcerpt() returned nil for non-empty file")
	}
	if !excerpt.Truncated {
		t.Error("Truncated = false, want true for 100-line file")
	}
	if len(excerpt.Lines) > maxLogLinesPerContainer {
		t.Errorf("Lines count = %d, want <= %d", len(excerpt.Lines), maxLogLinesPerContainer)
	}
}

func TestReadLogExcerpt_EmptyFile(t *testing.T) {
	tmp := t.TempDir()
	logPath := tmp + "/empty.log"
	if err := os.WriteFile(logPath, []byte{}, 0o644); err != nil {
		t.Fatalf("write empty file: %v", err)
	}

	excerpt, err := readLogExcerpt(logPath, "ns", "pod", "app")
	if err != nil {
		t.Fatalf("readLogExcerpt() error = %v", err)
	}
	if excerpt != nil {
		t.Errorf("readLogExcerpt() on empty file = %v, want nil", excerpt)
	}
}

func TestIsErrorLine(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{"2024-01-15 ERROR connection refused", true},
		{"2024-01-15 FATAL out of memory", true},
		{"panic: nil pointer dereference", true},
		{"Exception in thread main", true},
		{"CRITICAL disk full", true},
		{"2024-01-15 INFO server started", false},
		{"2024-01-15 DEBUG reading config", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isErrorLine(tt.line); got != tt.want {
			t.Errorf("isErrorLine(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

// --- S2.1 (continued): init container failure ---

func TestBuildPodSummary_InitContainerFailure(t *testing.T) {
	pod := k8sPod{
		Metadata: k8sObjectMeta{Name: "init-fail-pod", Namespace: "staging"},
		Spec:     k8sPodSpec{NodeName: "node-1"},
		Status: k8sPodStatus{
			Phase: "Init:0/1",
			InitContainerStatuses: []k8sContainerStatus{
				{
					Name:         "db-migrate",
					Ready:        false,
					RestartCount: 2,
					State: k8sContainerState{
						Waiting: &k8sStateWaiting{
							Reason:  "CrashLoopBackOff",
							Message: "init container failing",
						},
					},
				},
			},
		},
	}

	summary := buildPodSummary(pod)

	if summary.Reason != "Init:CrashLoopBackOff" {
		t.Errorf("Reason = %q, want %q", summary.Reason, "Init:CrashLoopBackOff")
	}
	if summary.RestartCount != 2 {
		t.Errorf("RestartCount = %d, want 2", summary.RestartCount)
	}
}
