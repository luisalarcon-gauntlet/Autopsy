//go:build evals

package evals_test

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/yourusername/autopsy/internal/analysis"
)

// toyotaBrokenPods are the 5 broken pods present in the Toyota fixture bundle.
var toyotaBrokenPods = []string{
	"memory-hog-7d9f8b6c5-xk2pq",
	"bad-entrypoint-6b8d7f9c4-m3nvw",
	"bad-image-5c7f6d8b3-p9rtx",
	"resource-hog-4a6e5c7b2-q8swz",
	"missing-config-3f5d4b6a1-r7tvx",
}

// ── helpers ──────────────────────────────────────────────────────────────────

// parseRelativeSeconds parses a timeline RelativeTime string like "T+3:42"
// into a total number of seconds for chronological comparison.
func parseRelativeSeconds(t *testing.T, rel string) int {
	t.Helper()
	trimmed := strings.TrimPrefix(rel, "T+")
	parts := strings.SplitN(trimmed, ":", 2)
	if len(parts) != 2 {
		t.Fatalf("unexpected relativeTime format: %q", rel)
	}
	minutes, err1 := strconv.Atoi(parts[0])
	seconds, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		t.Fatalf("cannot parse relativeTime %q: %v / %v", rel, err1, err2)
	}
	return minutes*60 + seconds
}

// extractSection returns the text between header and the next "## " heading
// (or end of string). Returns "" if header is not found.
func extractSection(text, header string) string {
	idx := strings.Index(text, header)
	if idx == -1 {
		return ""
	}
	rest := text[idx+len(header):]
	if nextIdx := strings.Index(rest, "\n## "); nextIdx != -1 {
		return rest[:nextIdx]
	}
	return rest
}

// extractCodeBlocks returns the contents of all fenced code blocks in s.
func extractCodeBlocks(s string) []string {
	re := regexp.MustCompile("(?s)```(?:yaml|yml)?\n(.*?)```")
	var blocks []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		blocks = append(blocks, m[1])
	}
	return blocks
}

// ── Triage evals (8) ─────────────────────────────────────────────────────────

func TestEval_Triage_ToyotaBundle_SeverityIsHigh(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	result, err := analysis.RunTriage(context.Background(), client, data, false)
	if err != nil {
		t.Fatalf("RunTriage: %v", err)
	}

	logResult(t, "SeverityScore", fmt.Sprintf("%d", result.SeverityScore))
	if result.SeverityScore < 70 {
		t.Errorf("SeverityScore = %d, want >= 70", result.SeverityScore)
	}
}

func TestEval_Triage_ToyotaBundle_AllBrokenPodsInTopIssues(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	result, err := analysis.RunTriage(context.Background(), client, data, false)
	if err != nil {
		t.Fatalf("RunTriage: %v", err)
	}

	// Concatenate every issue's title and affectedPod into one searchable string.
	var sb strings.Builder
	for _, issue := range result.TopIssues {
		sb.WriteString(issue.Title + " " + issue.AffectedPod + " ")
	}
	issueText := sb.String()

	found := 0
	for _, pod := range toyotaBrokenPods {
		if strings.Contains(issueText, pod) {
			found++
		}
	}
	logResult(t, "BrokenPodsInTopIssues", fmt.Sprintf("%d/%d", found, len(toyotaBrokenPods)))
	if found < len(toyotaBrokenPods) {
		t.Errorf("%d/%d broken pods found in topIssues; issue text: %s",
			found, len(toyotaBrokenPods), issueText)
	}
}

func TestEval_Triage_ToyotaBundle_ClusterHealthIsCritical(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	result, err := analysis.RunTriage(context.Background(), client, data, false)
	if err != nil {
		t.Fatalf("RunTriage: %v", err)
	}

	logResult(t, "ClusterHealth", result.ClusterHealth)
	if result.ClusterHealth != "critical" {
		t.Errorf("ClusterHealth = %q, want %q", result.ClusterHealth, "critical")
	}
}

func TestEval_Triage_ToyotaBundle_IssueCountMatchesParser(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	brokenPodCount := 0
	for _, p := range data.PodSummaries {
		if p.Reason != "" {
			brokenPodCount++
		}
	}

	result, err := analysis.RunTriage(context.Background(), client, data, false)
	if err != nil {
		t.Fatalf("RunTriage: %v", err)
	}

	logResult(t, "IssueCount",
		fmt.Sprintf("topIssues=%d brokenPods=%d", len(result.TopIssues), brokenPodCount))
	if len(result.TopIssues) < brokenPodCount {
		t.Errorf("len(TopIssues) = %d, want >= %d (broken pod count)",
			len(result.TopIssues), brokenPodCount)
	}
}

func TestEval_Triage_ToyotaBundle_OOMCategoryPresent(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	result, err := analysis.RunTriage(context.Background(), client, data, false)
	if err != nil {
		t.Fatalf("RunTriage: %v", err)
	}

	for _, issue := range result.TopIssues {
		if issue.Category == "oom" {
			return
		}
	}
	var cats []string
	for _, issue := range result.TopIssues {
		cats = append(cats, issue.Category)
	}
	t.Errorf("no issue with Category == %q; categories seen: %v", "oom", cats)
}

func TestEval_Triage_ToyotaBundle_ImagePullCategoryPresent(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	result, err := analysis.RunTriage(context.Background(), client, data, false)
	if err != nil {
		t.Fatalf("RunTriage: %v", err)
	}

	for _, issue := range result.TopIssues {
		if issue.Category == "image-pull" {
			return
		}
	}
	var cats []string
	for _, issue := range result.TopIssues {
		cats = append(cats, issue.Category)
	}
	t.Errorf("no issue with Category == %q; categories seen: %v", "image-pull", cats)
}

func TestEval_Triage_ToyotaBundle_SchemaIsValid(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	result, err := analysis.RunTriage(context.Background(), client, data, false)
	if err != nil {
		t.Fatalf("RunTriage: %v", err)
	}

	if result.SeverityScore < 0 || result.SeverityScore > 100 {
		t.Errorf("SeverityScore %d out of range [0, 100]", result.SeverityScore)
	}

	validHealth := map[string]bool{
		"critical": true, "degraded": true, "warning": true, "healthy": true,
	}
	if !validHealth[result.ClusterHealth] {
		t.Errorf("ClusterHealth %q is not one of: critical, degraded, warning, healthy",
			result.ClusterHealth)
	}

	if len(result.TopIssues) == 0 {
		t.Error("TopIssues must not be empty")
	}
	for i, issue := range result.TopIssues {
		if issue.Title == "" {
			t.Errorf("issue[%d].Title is empty", i)
		}
		if issue.Severity == "" {
			t.Errorf("issue[%d].Severity is empty", i)
		}
		if issue.Category == "" {
			t.Errorf("issue[%d].Category is empty", i)
		}
	}
}

func TestEval_Triage_ToyotaBundle_SummaryMentionsNamespace(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	result, err := analysis.RunTriage(context.Background(), client, data, false)
	if err != nil {
		t.Fatalf("RunTriage: %v", err)
	}

	logResult(t, "Summary", result.Summary)
	if !strings.Contains(result.Summary, "broken-app") {
		t.Errorf("Summary does not mention %q; got: %s", "broken-app", result.Summary)
	}
}

// ── Timeline evals (5) ───────────────────────────────────────────────────────

func TestEval_Timeline_ToyotaBundle_IsChronological(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	result, err := analysis.RunTimeline(context.Background(), client, data, false)
	if err != nil {
		t.Fatalf("RunTimeline: %v", err)
	}

	prev := -1
	for i, event := range result.Events {
		secs := parseRelativeSeconds(t, event.RelativeTime)
		if secs < prev {
			t.Errorf("event[%d] %q (%ds) precedes event[%d] (%ds) — not chronological",
				i, event.RelativeTime, secs, i-1, prev)
		}
		prev = secs
	}
}

func TestEval_Timeline_ToyotaBundle_ContainsOOMEvent(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	result, err := analysis.RunTimeline(context.Background(), client, data, false)
	if err != nil {
		t.Fatalf("RunTimeline: %v", err)
	}

	for _, event := range result.Events {
		combined := strings.ToLower(event.Title + " " + event.Detail)
		if strings.Contains(combined, "oom") ||
			strings.Contains(combined, "memory") ||
			strings.Contains(combined, "killed") {
			return
		}
	}
	t.Error("no timeline event mentions OOM, memory, or killed")
}

func TestEval_Timeline_ToyotaBundle_HasCriticalEvents(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	result, err := analysis.RunTimeline(context.Background(), client, data, false)
	if err != nil {
		t.Fatalf("RunTimeline: %v", err)
	}

	for _, event := range result.Events {
		if event.Severity == "critical" {
			return
		}
	}
	var severities []string
	for _, e := range result.Events {
		severities = append(severities, e.Severity)
	}
	t.Errorf("no event with Severity == %q; severities seen: %v", "critical", severities)
}

func TestEval_Timeline_ToyotaBundle_StartsAtTZero(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	result, err := analysis.RunTimeline(context.Background(), client, data, false)
	if err != nil {
		t.Fatalf("RunTimeline: %v", err)
	}
	if len(result.Events) == 0 {
		t.Fatal("no timeline events returned")
	}

	first := result.Events[0].RelativeTime
	logResult(t, "FirstEventTime", first)
	if first != "T+0:00" {
		t.Errorf("first event RelativeTime = %q, want %q", first, "T+0:00")
	}
}

func TestEval_Timeline_ToyotaBundle_SchemaIsValid(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	result, err := analysis.RunTimeline(context.Background(), client, data, false)
	if err != nil {
		t.Fatalf("RunTimeline: %v", err)
	}
	if len(result.Events) == 0 {
		t.Fatal("Events must not be empty")
	}

	validSeverity := map[string]bool{"info": true, "warning": true, "critical": true}
	for i, event := range result.Events {
		if event.RelativeTime == "" {
			t.Errorf("event[%d].RelativeTime is empty", i)
		}
		if event.Title == "" {
			t.Errorf("event[%d].Title is empty", i)
		}
		if !validSeverity[event.Severity] {
			t.Errorf("event[%d].Severity %q is not one of: info, warning, critical",
				i, event.Severity)
		}
	}
}

// ── RCA evals (7) ────────────────────────────────────────────────────────────

func TestEval_RCA_ToyotaBundle_MentionsAllBrokenPods(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	var sb strings.Builder
	if err := analysis.RunRCA(context.Background(), client, data, false, &sb); err != nil {
		t.Fatalf("RunRCA: %v", err)
	}
	text := sb.String()

	for _, pod := range toyotaBrokenPods {
		if !strings.Contains(text, pod) {
			t.Errorf("RCA output does not mention pod %q", pod)
		}
	}
}

func TestEval_RCA_ToyotaBundle_HasRootCauseSection(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	var sb strings.Builder
	if err := analysis.RunRCA(context.Background(), client, data, false, &sb); err != nil {
		t.Fatalf("RunRCA: %v", err)
	}
	if !strings.Contains(sb.String(), "## Root Cause") {
		t.Errorf("RCA output missing section %q", "## Root Cause")
	}
}

func TestEval_RCA_ToyotaBundle_HasEvidenceSection(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	var sb strings.Builder
	if err := analysis.RunRCA(context.Background(), client, data, false, &sb); err != nil {
		t.Fatalf("RunRCA: %v", err)
	}
	if !strings.Contains(sb.String(), "## Evidence") {
		t.Errorf("RCA output missing section %q", "## Evidence")
	}
}

func TestEval_RCA_ToyotaBundle_HasFixStepsSection(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	var sb strings.Builder
	if err := analysis.RunRCA(context.Background(), client, data, false, &sb); err != nil {
		t.Fatalf("RunRCA: %v", err)
	}
	if !strings.Contains(sb.String(), "## Fix Steps") {
		t.Errorf("RCA output missing section %q", "## Fix Steps")
	}
}

func TestEval_RCA_ToyotaBundle_HasPatchFilesSection(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	var sb strings.Builder
	if err := analysis.RunRCA(context.Background(), client, data, false, &sb); err != nil {
		t.Fatalf("RunRCA: %v", err)
	}
	if !strings.Contains(sb.String(), "## Patch Files") {
		t.Errorf("RCA output missing section %q", "## Patch Files")
	}
}

func TestEval_RCA_ToyotaBundle_PatchFilesAreValidYAML(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	var sb strings.Builder
	if err := analysis.RunRCA(context.Background(), client, data, false, &sb); err != nil {
		t.Fatalf("RunRCA: %v", err)
	}
	text := sb.String()

	patchSection := extractSection(text, "## Patch Files")
	if patchSection == "" {
		t.Fatal("## Patch Files section not found in RCA output")
	}

	blocks := extractCodeBlocks(patchSection)
	logResult(t, "PatchBlockCount", fmt.Sprintf("%d", len(blocks)))
	if len(blocks) < 3 {
		t.Errorf("want >= 3 YAML patch blocks in ## Patch Files, got %d", len(blocks))
	}

	for i, block := range blocks {
		var v interface{}
		if err := yaml.Unmarshal([]byte(block), &v); err != nil {
			t.Errorf("patch block[%d] is not valid YAML: %v\n---\n%s\n---", i, err, block)
		}
	}
}

func TestEval_RCA_ToyotaBundle_MentionsCorrectNamespace(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	var sb strings.Builder
	if err := analysis.RunRCA(context.Background(), client, data, false, &sb); err != nil {
		t.Fatalf("RunRCA: %v", err)
	}
	if !strings.Contains(sb.String(), "broken-app") {
		t.Errorf("RCA output does not mention namespace %q", "broken-app")
	}
}

// ── Chat evals (4) ───────────────────────────────────────────────────────────

func TestEval_Chat_OOMQuestion_MentionsMemoryLimit(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	resp, err := analysis.RunChat(
		context.Background(), client, data, nil,
		"Why is memory-hog crashing?", false,
	)
	if err != nil {
		t.Fatalf("RunChat: %v", err)
	}
	logResult(t, "ChatResponse", resp)

	lower := strings.ToLower(resp)
	if !strings.Contains(lower, "32mi") &&
		!strings.Contains(lower, "memory limit") &&
		!strings.Contains(lower, "oom") {
		t.Errorf("response does not mention memory limit or OOM; got:\n%s", resp)
	}
}

func TestEval_Chat_FixQuestion_MentionsPodName(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	resp, err := analysis.RunChat(
		context.Background(), client, data, nil,
		"What should I fix first?", false,
	)
	if err != nil {
		t.Fatalf("RunChat: %v", err)
	}
	logResult(t, "ChatResponse", resp)

	for _, pod := range toyotaBrokenPods {
		if strings.Contains(resp, pod) {
			return
		}
	}
	t.Errorf("response does not mention any broken pod name; got:\n%s", resp)
}

func TestEval_Chat_KubectlQuestion_ContainsKubectl(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	resp, err := analysis.RunChat(
		context.Background(), client, data, nil,
		"Give me all the kubectl commands to fix this", false,
	)
	if err != nil {
		t.Fatalf("RunChat: %v", err)
	}
	logResult(t, "ChatResponse", resp)

	if !strings.Contains(resp, "kubectl") {
		t.Errorf("response does not contain %q", "kubectl")
	}
	if count := strings.Count(resp, "kubectl"); count < 3 {
		t.Errorf("response contains %q %d time(s), want >= 3", "kubectl", count)
	}
}

func TestEval_Chat_HallucinationCheck_NoFakePods(t *testing.T) {
	requireAPIKey(t)
	client := newClient(t)
	data := loadToyotaBundle(t)

	resp, err := analysis.RunChat(
		context.Background(), client, data, nil,
		"List all the pods in this cluster", false,
	)
	if err != nil {
		t.Fatalf("RunChat: %v", err)
	}
	logResult(t, "ChatResponse", resp)

	// Build set of known pod names from parsed bundle data.
	known := make(map[string]bool, len(data.PodSummaries))
	for _, p := range data.PodSummaries {
		known[p.Name] = true
	}

	// Match typical Kubernetes pod name patterns: <name>-<replicaset>-<podhash>
	podPattern := regexp.MustCompile(`\b[a-z][a-z0-9-]+-[a-z0-9]{5,10}-[a-z0-9]{4,6}\b`)
	matches := podPattern.FindAllString(resp, -1)

	var suspicious []string
	for _, match := range matches {
		if !known[match] {
			suspicious = append(suspicious, match)
		}
	}

	if len(suspicious) > 0 {
		logResult(t, "SuspiciousPodNames", strings.Join(suspicious, ", "))
		t.Errorf("response mentions pod names not found in bundle: %v", suspicious)
	}
}
