// This file gathers pods, logs, events, node conditions.

package snapshot

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ContainerSnapshot describes a single container in a pod.
type ContainerSnapshot struct {
	Name            string `json:"name"`
	Image           string `json:"image"`
	Ready           bool   `json:"ready"`
	RestartCount    int32  `json:"restartCount"`
	State           string `json:"state,omitempty"`       // Waiting|Running|Terminated
	StateReason     string `json:"stateReason,omitempty"` // e.g. ImagePullBackOff
	LastState       string `json:"lastState,omitempty"`
	LastStateReason string `json:"lastStateReason,omitempty"`
}

// EventSnapshot is a simplified event view.
type EventSnapshot struct {
	Type      string    `json:"type,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	Message   string    `json:"message,omitempty"`
	Count     int32     `json:"count,omitempty"`
	FirstTime time.Time `json:"firstTimestamp,omitempty"`
	LastTime  time.Time `json:"lastTimestamp,omitempty"`
}

// PodSnapshot is what we send to the LLM per “problem pod”.
type PodSnapshot struct {
	Namespace  string              `json:"namespace"`
	Name       string              `json:"name"`
	Phase      string              `json:"phase"`
	Reason     string              `json:"reason,omitempty"`
	Restarts   int32               `json:"restarts"`
	Ready      bool                `json:"ready"`
	NodeName   string              `json:"nodeName,omitempty"`
	Containers []ContainerSnapshot `json:"containers"`
	Events     []EventSnapshot     `json:"events,omitempty"`
	Logs       string              `json:"logs,omitempty"`
}

// NodeConditionSnapshot flattens node conditions.
type NodeConditionSnapshot struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// NodeSnapshot is a node + its conditions.
type NodeSnapshot struct {
	Name       string                  `json:"name"`
	Conditions []NodeConditionSnapshot `json:"conditions"`
}

// Snapshot is the whole thing the model sees.
type Snapshot struct {
	GeneratedAt    time.Time      `json:"generatedAt"`
	Namespace      string         `json:"namespace,omitempty"`
	ProblemPods    []PodSnapshot  `json:"problemPods"`
	NodeConditions []NodeSnapshot `json:"nodeConditions"`
}

// Filters controls what pods and content to include/exclude.
type Filters struct {
	IncludePods       string // comma-separated patterns with wildcard support
	ExcludePods       string
	IncludeNamespaces string
	ExcludeNamespaces string
	IncludeKeywords   string // comma-separated keywords to search in logs/events
	ExcludeKeywords   string
}

// BuildSnapshot collects:
// - non-Running pods / pods with restarts / not-ready
// - last N log lines for each bad pod
// - all node conditions
// - applies include/exclude filters
func BuildSnapshot(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
	maxPods int,
	logLines int,
	maxConcurrent int,
	filters Filters,
) (*Snapshot, error) {
	if maxPods <= 0 {
		maxPods = 20
	}
	if logLines <= 0 {
		logLines = 50
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 5
	}

	snap := &Snapshot{
		GeneratedAt: time.Now().UTC(),
		Namespace:   namespace,
	}

	// --- Nodes ---
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	for _, n := range nodes.Items {
		ns := NodeSnapshot{Name: n.Name}
		for _, c := range n.Status.Conditions {
			ns.Conditions = append(ns.Conditions, NodeConditionSnapshot{
				Type:    string(c.Type),
				Status:  string(c.Status),
				Reason:  c.Reason,
				Message: c.Message,
			})
		}
		snap.NodeConditions = append(snap.NodeConditions, ns)
	}

	// --- Pods ---
	podOpts := metav1.ListOptions{}
	var podList *corev1.PodList
	if namespace != "" {
		podList, err = clientset.CoreV1().Pods(namespace).List(ctx, podOpts)
	} else {
		podList, err = clientset.CoreV1().Pods("").List(ctx, podOpts)
	}
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}

	for _, p := range podList.Items {
		if len(snap.ProblemPods) >= maxPods {
			break
		}

		// Apply namespace filters
		if !matchesFilter(p.Namespace, filters.IncludeNamespaces, filters.ExcludeNamespaces) {
			continue
		}

		// Apply pod name filters
		if !matchesFilter(p.Name, filters.IncludePods, filters.ExcludePods) {
			continue
		}

		status := p.Status
		phase := string(status.Phase)

		var restarts int32
		allReady := true
		for _, cs := range status.ContainerStatuses {
			restarts += cs.RestartCount
			if !cs.Ready {
				allReady = false
			}
		}

		// "Problem pod" heuristic
		if phase == "Running" && restarts == 0 && allReady {
			continue
		}

		ps := PodSnapshot{
			Namespace: p.Namespace,
			Name:      p.Name,
			Phase:     phase,
			NodeName:  p.Spec.NodeName,
			Ready:     allReady,
			Restarts:  restarts,
			Reason:    status.Reason,
		}

		// Containers
		for _, cs := range status.ContainerStatuses {
			cSnap := ContainerSnapshot{
				Name:         cs.Name,
				Image:        cs.Image,
				Ready:        cs.Ready,
				RestartCount: cs.RestartCount,
			}

			if cs.State.Waiting != nil {
				cSnap.State = "Waiting"
				cSnap.StateReason = cs.State.Waiting.Reason
			} else if cs.State.Running != nil {
				cSnap.State = "Running"
			} else if cs.State.Terminated != nil {
				cSnap.State = "Terminated"
				cSnap.StateReason = cs.State.Terminated.Reason
			}

			if cs.LastTerminationState.Terminated != nil {
				cSnap.LastState = "Terminated"
				cSnap.LastStateReason = cs.LastTerminationState.Terminated.Reason
			} else if cs.LastTerminationState.Waiting != nil {
				cSnap.LastState = "Waiting"
				cSnap.LastStateReason = cs.LastTerminationState.Waiting.Reason
			}

			ps.Containers = append(ps.Containers, cSnap)
		}

		// Events (Warning events for this pod)
		evts, err := clientset.CoreV1().Events(p.Namespace).List(ctx, metav1.ListOptions{
			FieldSelector: fmt.Sprintf("involvedObject.kind=Pod,involvedObject.name=%s", p.Name),
		})
		if err == nil {
			for _, e := range evts.Items {
				// Old API has Type; K8s may emit Normal/Warning etc.
				if e.Type != "Warning" && e.Type != "" {
					continue
				}
				// Apply keyword filters to event messages
				if !containsKeywords(e.Message, filters.IncludeKeywords, filters.ExcludeKeywords) {
					continue
				}
				ps.Events = append(ps.Events, EventSnapshot{
					Type:      e.Type,
					Reason:    e.Reason,
					Message:   e.Message,
					Count:     e.Count,
					FirstTime: e.FirstTimestamp.Time,
					LastTime:  e.LastTimestamp.Time,
				})
			}
		}

		snap.ProblemPods = append(snap.ProblemPods, ps)
	}

	// Fetch logs concurrently with controlled parallelism to avoid API throttling
	// Use a semaphore pattern to limit concurrent requests
	var wg sync.WaitGroup
	var mu sync.Mutex
	semaphore := make(chan struct{}, maxConcurrent)

	for i := range snap.ProblemPods {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }() // Release semaphore

			pod := &snap.ProblemPods[idx]
			var tail int64 = int64(logLines)
			logReq := clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
				TailLines: &tail,
			})
			logBytes, err := logReq.DoRaw(ctx)

			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				logs := string(logBytes)
				// Apply keyword filters to logs
				if containsKeywords(logs, filters.IncludeKeywords, filters.ExcludeKeywords) {
					pod.Logs = logs
				} else {
					pod.Logs = "<filtered out by keyword filters>"
				}
			} else {
				pod.Logs = "<unable to fetch logs>"
			}
		}(i)
	}
	wg.Wait()

	return snap, nil
}

// matchesFilter checks if a string matches the include/exclude patterns.
// Patterns are comma-separated and support wildcard matching.
func matchesFilter(value, includePatterns, excludePatterns string) bool {
	// If exclude patterns are specified and match, reject
	if excludePatterns != "" {
		patterns := splitAndTrim(excludePatterns)
		for _, pattern := range patterns {
			if matchesPattern(value, pattern) {
				return false
			}
		}
	}

	// If include patterns are specified, must match at least one
	if includePatterns != "" {
		patterns := splitAndTrim(includePatterns)
		for _, pattern := range patterns {
			if matchesPattern(value, pattern) {
				return true
			}
		}
		return false
	}

	// No filters or passed exclude check
	return true
}

// containsKeywords checks if content contains include keywords and doesn't contain exclude keywords.
func containsKeywords(content, includeKeywords, excludeKeywords string) bool {
	content = strings.ToLower(content)

	// If exclude keywords are specified and match, reject
	if excludeKeywords != "" {
		keywords := splitAndTrim(excludeKeywords)
		for _, keyword := range keywords {
			if strings.Contains(content, strings.ToLower(keyword)) {
				return false
			}
		}
	}

	// If include keywords are specified, must match at least one
	if includeKeywords != "" {
		keywords := splitAndTrim(includeKeywords)
		for _, keyword := range keywords {
			if strings.Contains(content, strings.ToLower(keyword)) {
				return true
			}
		}
		return false
	}

	// No keyword filters specified or passed exclude check
	return true
}

// matchesPattern checks if a string matches a pattern with wildcard support.
func matchesPattern(str, pattern string) bool {
	matched, err := filepath.Match(pattern, str)
	if err != nil {
		// If pattern is invalid, fall back to exact match
		return str == pattern
	}
	return matched
}

// splitAndTrim splits a comma-separated string and trims whitespace.
func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
