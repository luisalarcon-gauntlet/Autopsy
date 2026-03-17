package analysis

import (
	"strings"
	"testing"

	"github.com/yourusername/autopsy/internal/bundle"
)

func TestFindRelevantContext(t *testing.T) {
	data := &bundle.BundleData{
		PodSummaries: []bundle.PodSummary{
			{Namespace: "default", Name: "nginx-6d6f8b7c5-abc12", Phase: "Running", Reason: "", RestartCount: 0},
			{Namespace: "payments", Name: "payment-processor-xyz", Phase: "CrashLoopBackOff", Reason: "CrashLoopBackOff", RestartCount: 7},
			{Namespace: "data-pipeline", Name: "data-ingestion-worker-abc", Phase: "Running", Reason: "OOMKilled", RestartCount: 3},
		},
		Events: []bundle.ClusterEvent{
			{Type: "Warning", Namespace: "payments", Reason: "BackOff", Message: "Back-off restarting failed container payment-processor"},
			{Type: "Normal", Namespace: "default", Reason: "Scheduled", Message: "Successfully assigned nginx pod"},
		},
	}

	tests := []struct {
		name       string
		query      string
		wantPod    string
		wantAbsent string
	}{
		{
			name:    "nginx keyword returns nginx pod context",
			query:   "what is wrong with nginx",
			wantPod: "nginx",
		},
		{
			name:    "payment keyword returns payment pod context",
			query:   "why is the payment-processor failing",
			wantPod: "payment-processor",
		},
		{
			name:    "oom keyword returns data-ingestion-worker context",
			query:   "which pod is getting OOMKilled",
			wantPod: "data-ingestion-worker",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FindRelevantContext(tt.query, data)

			if len(result) > maxContextChars {
				t.Errorf("context too large: got %d chars, max %d", len(result), maxContextChars)
			}

			if tt.wantPod != "" && !strings.Contains(result, tt.wantPod) {
				t.Errorf("expected context to mention %q, got:\n%s", tt.wantPod, result)
			}
		})
	}
}

func TestFindRelevantContextNilData(t *testing.T) {
	result := FindRelevantContext("anything", nil)
	if result != "" {
		t.Errorf("expected empty result for nil data, got %q", result)
	}
}

func TestFindRelevantContextFallback(t *testing.T) {
	data := &bundle.BundleData{
		PodSummaries: []bundle.PodSummary{
			{Namespace: "default", Name: "my-app", Phase: "Running", Reason: ""},
		},
	}
	result := FindRelevantContext("zzz-nomatch-zzz", data)
	// Should return fallback (pod summary), not empty
	if result == "" {
		t.Error("expected non-empty fallback context, got empty")
	}
}

func TestTokenize(t *testing.T) {
	keywords := tokenize("Why is nginx-pod getting OOMKilled?")
	found := map[string]bool{}
	for _, kw := range keywords {
		found[kw] = true
	}

	for _, want := range []string{"why", "nginx-pod", "getting", "oomkilled"} {
		if !found[want] {
			t.Errorf("expected keyword %q in result %v", want, keywords)
		}
	}
}
