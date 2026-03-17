package bundle

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// maxLogLinesPerContainer is the hard cap on lines kept per container.
	maxLogLinesPerContainer = 50

	// maxLastLines is the number of trailing lines always included.
	maxLastLines = 30

	// maxErrorLines is the maximum number of additional error/panic lines added.
	maxErrorLines = 20

	// maxContainersForLogs is the maximum number of containers to process.
	maxContainersForLogs = 10
)

// extractLogs reads log files for unhealthy pods (RestartCount > 0 or Phase ==
// "Failed") and returns smart excerpts. Missing log files are appended to
// parseErrors and do not cause an error to be returned.
func extractLogs(ctx context.Context, bundleDir string, pods []PodSummary, parseErrors *[]string) ([]LogExcerpt, error) {
	var excerpts []LogExcerpt
	containerCount := 0

	for _, pod := range pods {
		if containerCount >= maxContainersForLogs {
			break
		}
		select {
		case <-ctx.Done():
			return excerpts, ctx.Err()
		default:
		}

		if pod.RestartCount == 0 && pod.Phase != "Failed" {
			continue
		}

		podExcerpts, err := extractPodLogs(ctx, bundleDir, pod, parseErrors)
		if err != nil {
			return excerpts, err
		}
		for _, ex := range podExcerpts {
			if containerCount >= maxContainersForLogs {
				break
			}
			excerpts = append(excerpts, ex)
			containerCount++
		}
	}
	return excerpts, nil
}

// extractPodLogs reads all container log files for a single pod.
// It looks for files at logs/{namespace}/{pod}/{container}.log.
func extractPodLogs(ctx context.Context, bundleDir string, pod PodSummary, parseErrors *[]string) ([]LogExcerpt, error) {
	logDir := filepath.Join(bundleDir, "logs", pod.Namespace, pod.Name)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		// Log directory absent is non-fatal — many pods simply have no logs in the bundle.
		*parseErrors = append(*parseErrors,
			fmt.Sprintf("logs/%s/%s: %v", pod.Namespace, pod.Name, err))
		return nil, nil
	}

	var excerpts []LogExcerpt
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return excerpts, ctx.Err()
		default:
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}

		containerName := strings.TrimSuffix(entry.Name(), ".log")
		logPath := filepath.Join(logDir, entry.Name())

		excerpt, err := readLogExcerpt(logPath, pod.Namespace, pod.Name, containerName)
		if err != nil {
			*parseErrors = append(*parseErrors, fmt.Sprintf("log %s: %v", logPath, err))
			continue
		}
		if excerpt != nil {
			excerpts = append(excerpts, *excerpt)
		}
	}
	return excerpts, nil
}

// readLogExcerpt extracts a smart subset of lines from a log file:
//   - All error/fatal/panic lines (up to maxErrorLines)
//   - The last maxLastLines lines of the file
//
// The two sets are merged and deduplicated, capped at maxLogLinesPerContainer.
func readLogExcerpt(path, namespace, podName, container string) (*LogExcerpt, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("readLogExcerpt: open: %w", err)
	}
	defer f.Close()

	var allLines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("readLogExcerpt: scan: %w", err)
	}

	if len(allLines) == 0 {
		return nil, nil
	}

	// Collect error-indicator lines.
	var errorLines []string
	errorCount := 0
	for _, line := range allLines {
		if isErrorLine(line) {
			errorCount++
			if len(errorLines) < maxErrorLines {
				errorLines = append(errorLines, line)
			}
		}
	}

	// Tail of the file.
	tail := allLines
	truncated := false
	if len(allLines) > maxLastLines {
		tail = allLines[len(allLines)-maxLastLines:]
		truncated = true
	}

	// Merge error lines and tail, deduplicating, capped at maxLogLinesPerContainer.
	seen := make(map[string]bool, len(errorLines)+len(tail))
	selected := make([]string, 0, maxLogLinesPerContainer)

	for _, line := range errorLines {
		if !seen[line] {
			seen[line] = true
			selected = append(selected, line)
		}
	}
	for _, line := range tail {
		if !seen[line] {
			seen[line] = true
			selected = append(selected, line)
		}
	}
	if len(selected) > maxLogLinesPerContainer {
		selected = selected[:maxLogLinesPerContainer]
		truncated = true
	}

	return &LogExcerpt{
		Namespace:  namespace,
		PodName:    podName,
		Container:  container,
		Lines:      selected,
		Truncated:  truncated,
		ErrorCount: errorCount,
	}, nil
}

// isErrorLine reports whether a log line contains an error-severity keyword.
func isErrorLine(line string) bool {
	upper := strings.ToUpper(line)
	for _, kw := range []string{"ERROR", "FATAL", "PANIC", "EXCEPTION", "CRITICAL"} {
		if strings.Contains(upper, kw) {
			return true
		}
	}
	return false
}
