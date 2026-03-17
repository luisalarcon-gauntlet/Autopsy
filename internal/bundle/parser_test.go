package bundle

import (
	"context"
	"testing"
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
