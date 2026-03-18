package bundle

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
)

// findBundleRoot searches up to 3 directory levels deep for the actual bundle
// root — the directory that contains cluster-resources/ or cluster-info/.
// This handles flat bundles, single-wrapped bundles (support-bundle-xxx/
// wrapper), and archives that place extra files (like logs/) at the archive
// root alongside the wrapper directory.
func findBundleRoot(bundleDir string) string {
	found := walkForRoot(bundleDir, 3)
	if found != bundleDir {
		slog.Info("bundle root unwrapped", "from", bundleDir, "to", found)
	}
	return found
}

// walkForRoot is the recursive helper for findBundleRoot. It returns the
// deepest directory (up to maxDepth levels below dir) that satisfies
// looksLikeBundleRoot, or dir itself if none is found.
func walkForRoot(dir string, maxDepth int) string {
	if looksLikeBundleRoot(dir) {
		return dir
	}
	if maxDepth <= 0 {
		return dir
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dir
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "__MACOSX" {
			continue
		}
		if found := walkForRoot(filepath.Join(dir, e.Name()), maxDepth-1); looksLikeBundleRoot(found) {
			return found
		}
	}
	return dir
}

// looksLikeBundleRoot reports whether dir contains cluster-resources/ or
// cluster-info/ — the unambiguous markers of a Troubleshoot bundle root.
// "logs" is intentionally excluded: it appears in many archives at the wrong
// level and would cause false positives.
func looksLikeBundleRoot(dir string) bool {
	for _, marker := range []string{"cluster-resources", "cluster-info"} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	return false
}

// logBundleTree walks the extracted directory and logs every subdirectory
// (up to 4 levels deep) plus files at the top 2 levels. This makes the
// bundle structure visible in server logs for debugging root-detection issues.
func logBundleTree(root string) {
	slog.Info("bundle extracted", "root", root)
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > 4 {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			rel, _ := filepath.Rel(root, filepath.Join(dir, e.Name()))
			if e.IsDir() {
				slog.Info("bundle tree", "dir", rel)
				walk(filepath.Join(dir, e.Name()), depth+1)
			} else if depth <= 1 {
				slog.Info("bundle tree", "file", rel)
			}
		}
	}
	walk(root, 0)
}

// Parse walks the extracted bundle directory and returns structured BundleData.
// Non-fatal errors (missing files, parse failures) are recorded in ParseErrors
// and do not cause Parse to return an error. Only unrecoverable failures (e.g.
// context cancellation) return a non-nil error.
func Parse(ctx context.Context, bundleDir string) (*BundleData, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Log the extracted tree before root detection so the structure is visible
	// in server logs when debugging parsing issues.
	logBundleTree(bundleDir)

	// Troubleshoot bundles typically contain a single top-level directory
	// (e.g. support-bundle-2024-01-15T11-00-00/). Unwrap it so all sub-parsers
	// operate on the directory that actually contains cluster-resources/, logs/, etc.
	bundleDir = findBundleRoot(bundleDir)
	slog.Info("bundle root resolved", "dir", bundleDir)

	data := &BundleData{}

	parseClusterVersion(bundleDir, data)
	parseNodes(bundleDir, data)
	parsePods(ctx, bundleDir, data)

	events, err := parseEvents(ctx, bundleDir)
	if err != nil {
		data.ParseErrors = append(data.ParseErrors, fmt.Sprintf("events: %v", err))
	}
	data.Events = events

	excerpts, err := extractLogs(ctx, bundleDir, data.PodSummaries, &data.ParseErrors)
	if err != nil {
		data.ParseErrors = append(data.ParseErrors, fmt.Sprintf("logs: %v", err))
	}
	data.LogExcerpts = excerpts

	parseHelm(bundleDir, data)

	data.TokenEstimate = EstimateTokens(data)

	slog.Info("bundle parsed",
		"pods", len(data.PodSummaries),
		"nodes", len(data.NodeSummaries),
		"events", len(data.Events),
		"logExcerpts", len(data.LogExcerpts),
		"parseErrors", len(data.ParseErrors),
		"tokenEstimate", data.TokenEstimate,
	)
	for _, e := range data.ParseErrors {
		log.Printf("parse error: %s", e)
	}

	return data, nil
}

// parseClusterVersion reads the Kubernetes server version from known bundle locations.
// Tries several candidate paths since bundle formats differ between tools.
func parseClusterVersion(bundleDir string, data *BundleData) {
	candidates := []string{
		filepath.Join(bundleDir, "cluster-info", "cluster_version.json"),
		filepath.Join(bundleDir, "version", "version.json"),
		filepath.Join(bundleDir, "cluster-resources", "version.json"),
	}
	for _, path := range candidates {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		// Format: {"gitVersion":"v1.28.0"}
		var flat struct {
			GitVersion string `json:"gitVersion"`
		}
		if err := json.Unmarshal(raw, &flat); err == nil && flat.GitVersion != "" {
			data.ClusterVersion = flat.GitVersion
			return
		}
		// Format: {"serverVersion":{"gitVersion":"v1.28.0"}}
		var nested struct {
			ServerVersion struct {
				GitVersion string `json:"gitVersion"`
			} `json:"serverVersion"`
		}
		if err := json.Unmarshal(raw, &nested); err == nil && nested.ServerVersion.GitVersion != "" {
			data.ClusterVersion = nested.ServerVersion.GitVersion
			return
		}
	}
}

// parseNodes reads cluster-resources/nodes.json and populates NodeSummaries.
func parseNodes(bundleDir string, data *BundleData) {
	nodesPath := filepath.Join(bundleDir, "cluster-resources", "nodes.json")
	raw, err := os.ReadFile(nodesPath)
	if err != nil {
		data.ParseErrors = append(data.ParseErrors, fmt.Sprintf("nodes.json: %v", err))
		return
	}

	var items []k8sNode
	if err := json.Unmarshal(raw, &items); err != nil {
		data.ParseErrors = append(data.ParseErrors, fmt.Sprintf("nodes.json parse: %v", err))
		return
	}

	for _, item := range items {
		ns := NodeSummary{
			Name:     item.Metadata.Name,
			Capacity: item.Status.Capacity,
		}
		if ns.Capacity == nil {
			ns.Capacity = map[string]string{}
		}
		for _, cond := range item.Status.Conditions {
			if cond.Type == "Ready" {
				ns.Ready = cond.Status == "True"
			} else if cond.Status == "True" {
				// A non-Ready condition being True indicates pressure/issue.
				reason := cond.Reason
				if reason == "" {
					reason = cond.Type
				}
				ns.Conditions = append(ns.Conditions, reason)
			}
		}
		data.NodeSummaries = append(data.NodeSummaries, ns)
	}
}

// parsePods walks cluster-resources/{namespace}/pods.json files and appends
// to data.PodSummaries. Namespace directories without pods.json are skipped silently.
func parsePods(ctx context.Context, bundleDir string, data *BundleData) {
	clusterDir := filepath.Join(bundleDir, "cluster-resources")
	entries, err := os.ReadDir(clusterDir)
	if err != nil {
		data.ParseErrors = append(data.ParseErrors, fmt.Sprintf("cluster-resources dir: %v", err))
		return
	}

	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if !entry.IsDir() {
			continue
		}
		parsePodsFile(filepath.Join(clusterDir, entry.Name(), "pods.json"), data)
	}
}

// parsePodsFile reads and parses a single pods.json file.
func parsePodsFile(path string, data *BundleData) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return // Missing pods.json in a namespace dir is non-fatal; skip silently.
	}

	var pods []k8sPod
	if err := json.Unmarshal(raw, &pods); err != nil {
		data.ParseErrors = append(data.ParseErrors, fmt.Sprintf("%s: unmarshal: %v", path, err))
		return
	}

	for _, pod := range pods {
		data.PodSummaries = append(data.PodSummaries, buildPodSummary(pod))
	}
}

// buildPodSummary converts a k8sPod into a PodSummary, extracting the most
// actionable failure reason from container states. Priority order:
//  1. Container waiting reason (e.g. CrashLoopBackOff, ImagePullBackOff, CreateContainerConfigError)
//  2. Container terminated reason (e.g. OOMKilled, Error)
//  3. Last terminated reason
//  4. Init container failures (prefixed with "Init:")
//  5. Pod-level scheduling failure from conditions (e.g. Unschedulable)
//  6. Pod-level status reason (e.g. for evicted pods)
func buildPodSummary(pod k8sPod) PodSummary {
	s := PodSummary{
		Namespace: pod.Metadata.Namespace,
		Name:      pod.Metadata.Name,
		Phase:     pod.Status.Phase,
		NodeName:  pod.Spec.NodeName,
	}

	total := len(pod.Status.ContainerStatuses)
	ready := 0
	for _, cs := range pod.Status.ContainerStatuses {
		s.RestartCount += cs.RestartCount
		if cs.Ready {
			ready++
		}
		if s.Reason == "" {
			s.Reason, s.Message = extractContainerReason(cs)
		}
	}

	// Fall back to init container failures.
	for _, cs := range pod.Status.InitContainerStatuses {
		s.RestartCount += cs.RestartCount
		if s.Reason == "" {
			reason, msg := extractContainerReason(cs)
			if reason != "" {
				s.Reason = "Init:" + reason
				s.Message = msg
			}
		}
	}

	// For Pending pods with no container statuses, check pod conditions for
	// scheduling failure (e.g. Insufficient memory, Unschedulable).
	if s.Reason == "" && pod.Status.Phase == "Pending" {
		for _, cond := range pod.Status.Conditions {
			if cond.Type == "PodScheduled" && cond.Status == "False" {
				s.Reason = cond.Reason
				s.Message = cond.Message
				break
			}
		}
	}

	// Fall back to pod-level reason (set for evicted pods).
	if s.Reason == "" && pod.Status.Reason != "" {
		s.Reason = pod.Status.Reason
		s.Message = pod.Status.Message
	}

	if total > 0 {
		s.Ready = fmt.Sprintf("%d/%d", ready, total)
	} else {
		s.Ready = "0/0"
	}
	return s
}

// extractContainerReason returns the failure reason and message for a container
// status, checking current state then last state.
func extractContainerReason(cs k8sContainerStatus) (reason, message string) {
	if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
		return cs.State.Waiting.Reason, cs.State.Waiting.Message
	}
	if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" {
		return cs.State.Terminated.Reason, cs.State.Terminated.Message
	}
	if cs.LastState.Terminated != nil && cs.LastState.Terminated.Reason != "" {
		return cs.LastState.Terminated.Reason, cs.LastState.Terminated.Message
	}
	return "", ""
}

// parseHelm looks for Helm release information at known bundle locations.
func parseHelm(bundleDir string, data *BundleData) {
	candidates := []string{
		filepath.Join(bundleDir, "helm", "releases.json"),
		filepath.Join(bundleDir, "cluster-resources", "helm-releases.json"),
	}
	for _, path := range candidates {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var releases []struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
			Chart     string `json:"chart"`
			Status    string `json:"status"`
		}
		if err := json.Unmarshal(raw, &releases); err != nil {
			continue
		}
		for _, r := range releases {
			data.HelmReleases = append(data.HelmReleases, HelmRelease{
				Name:      r.Name,
				Namespace: r.Namespace,
				Chart:     r.Chart,
				Status:    r.Status,
			})
		}
		return
	}
}
