package bundle

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// parseEvents walks cluster-resources/{namespace}/events.json files and returns
// a deduplicated, chronologically sorted slice of ClusterEvents. Warning events
// are always included; Normal events are included only when count > 10.
func parseEvents(ctx context.Context, bundleDir string) ([]ClusterEvent, error) {
	clusterDir := filepath.Join(bundleDir, "cluster-resources")
	entries, err := os.ReadDir(clusterDir)
	if err != nil {
		return nil, fmt.Errorf("parseEvents: read dir: %w", err)
	}

	var all []ClusterEvent
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if !entry.IsDir() {
			continue
		}
		eventsPath := filepath.Join(clusterDir, entry.Name(), "events.json")
		events, err := parseEventsFile(eventsPath)
		if err != nil {
			continue // missing or unreadable events.json is non-fatal
		}
		all = append(all, events...)
	}

	// Filter: Warning always included; Normal only when count > 10.
	filtered := all[:0]
	for _, e := range all {
		if e.Type == "Warning" || e.Count > 10 {
			filtered = append(filtered, e)
		}
	}
	all = filtered

	all = deduplicateEvents(all)

	sort.Slice(all, func(i, j int) bool {
		return all[i].Timestamp.Before(all[j].Timestamp)
	})

	return all, nil
}

// parseEventsFile reads and decodes a single events.json file.
func parseEventsFile(path string) ([]ClusterEvent, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("parseEventsFile: read: %w", err)
	}

	var items []k8sEvent
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("parseEventsFile: unmarshal: %w", err)
	}

	events := make([]ClusterEvent, 0, len(items))
	for _, item := range items {
		events = append(events, ClusterEvent{
			Timestamp: parseEventTimestamp(item),
			Namespace: item.Metadata.Namespace,
			Name:      item.InvolvedObject.Name,
			Kind:      item.InvolvedObject.Kind,
			Reason:    item.Reason,
			Message:   item.Message,
			Count:     int(item.Count),
			Type:      item.Type,
		})
	}
	return events, nil
}

// parseEventTimestamp returns the best available timestamp for a K8s event.
// Priority: lastTimestamp > eventTime > firstTimestamp.
func parseEventTimestamp(e k8sEvent) time.Time {
	for _, ts := range []string{e.LastTimestamp, e.EventTime, e.FirstTimestamp} {
		if ts == "" {
			continue
		}
		for _, layout := range []string{time.RFC3339, time.RFC3339Nano} {
			if t, err := time.Parse(layout, ts); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

// deduplicateEvents collapses repeated events that share the same
// (namespace, involved-object name, reason) key. For each group the entry
// with the latest timestamp is kept, and counts are summed.
func deduplicateEvents(events []ClusterEvent) []ClusterEvent {
	type key struct {
		namespace, name, reason string
	}
	indexByKey := make(map[key]int, len(events))
	result := make([]ClusterEvent, 0, len(events))

	for _, e := range events {
		k := key{e.Namespace, e.Name, e.Reason}
		if idx, exists := indexByKey[k]; exists {
			result[idx].Count += e.Count
			if e.Timestamp.After(result[idx].Timestamp) {
				savedCount := result[idx].Count
				result[idx] = e
				result[idx].Count = savedCount
			}
		} else {
			indexByKey[k] = len(result)
			result = append(result, e)
		}
	}
	return result
}
