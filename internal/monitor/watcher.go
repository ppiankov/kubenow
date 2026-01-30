package monitor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

// Watcher watches Kubernetes events and pod status
type Watcher struct {
	clientset  *kubernetes.Clientset
	config     Config
	problems   map[string]*Problem
	events     []RecentEvent
	stats      ClusterStats
	mu         sync.RWMutex
	updateChan chan struct{}
}

// NewWatcher creates a new cluster watcher
func NewWatcher(clientset *kubernetes.Clientset, config Config) *Watcher {
	return &Watcher{
		clientset:  clientset,
		config:     config,
		problems:   make(map[string]*Problem),
		events:     make([]RecentEvent, 0),
		updateChan: make(chan struct{}, 100),
	}
}

// Start begins watching cluster events and pods
func (w *Watcher) Start(ctx context.Context) error {
	// Start event watcher
	go w.watchEvents(ctx)

	// Start pod watcher
	go w.watchPods(ctx)

	// Start stats updater
	go w.updateStats(ctx)

	return nil
}

// GetUpdateChannel returns channel for UI updates
func (w *Watcher) GetUpdateChannel() <-chan struct{} {
	return w.updateChan
}

// GetState returns current monitoring state
func (w *Watcher) GetState() ([]Problem, []RecentEvent, ClusterStats) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	problems := make([]Problem, 0, len(w.problems))
	for _, p := range w.problems {
		problems = append(problems, *p)
	}

	events := make([]RecentEvent, len(w.events))
	copy(events, w.events)

	return problems, events, w.stats
}

// watchEvents watches Kubernetes events for problems
func (w *Watcher) watchEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		watcher, err := w.clientset.CoreV1().Events(w.config.Namespace).Watch(ctx, metav1.ListOptions{
			Watch: true,
		})
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}

		for event := range watcher.ResultChan() {
			if event.Type == watch.Error {
				break
			}

			if event.Type == watch.Added || event.Type == watch.Modified {
				if k8sEvent, ok := event.Object.(*corev1.Event); ok {
					w.processEvent(k8sEvent)
				}
			}
		}

		watcher.Stop()
		time.Sleep(1 * time.Second)
	}
}

// watchPods watches pod status changes
func (w *Watcher) watchPods(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		watcher, err := w.clientset.CoreV1().Pods(w.config.Namespace).Watch(ctx, metav1.ListOptions{
			Watch: true,
		})
		if err != nil {
			time.Sleep(5 * time.Second)
			continue
		}

		for event := range watcher.ResultChan() {
			if event.Type == watch.Error {
				break
			}

			if event.Type == watch.Added || event.Type == watch.Modified {
				if pod, ok := event.Object.(*corev1.Pod); ok {
					w.processPodStatus(pod)
				}
			}
		}

		watcher.Stop()
		time.Sleep(1 * time.Second)
	}
}

// processEvent processes a Kubernetes event
func (w *Watcher) processEvent(event *corev1.Event) {
	severity := classifyEventSeverity(event.Reason, event.Type)
	if severity == "" {
		return // Not a problem event
	}

	// Add to recent events
	w.mu.Lock()
	recentEvent := RecentEvent{
		Timestamp: event.LastTimestamp.Time,
		Severity:  severity,
		Type:      event.Reason,
		Namespace: event.InvolvedObject.Namespace,
		Resource:  fmt.Sprintf("%s/%s", event.InvolvedObject.Kind, event.InvolvedObject.Name),
		Message:   event.Message,
	}
	w.events = append([]RecentEvent{recentEvent}, w.events...)
	if len(w.events) > 20 {
		w.events = w.events[:20]
	}

	// Create or update problem
	problemKey := fmt.Sprintf("%s/%s/%s", event.InvolvedObject.Namespace, event.InvolvedObject.Name, event.Reason)
	if problem, exists := w.problems[problemKey]; exists {
		problem.Count++
		problem.LastSeen = event.LastTimestamp.Time
		problem.Message = event.Message
	} else {
		w.problems[problemKey] = &Problem{
			Severity:  severity,
			Type:      event.Reason,
			Namespace: event.InvolvedObject.Namespace,
			PodName:   event.InvolvedObject.Name,
			Message:   event.Message,
			Reason:    event.Reason,
			FirstSeen: event.FirstTimestamp.Time,
			LastSeen:  event.LastTimestamp.Time,
			Count:     int(event.Count),
			Details:   make(map[string]string),
		}
	}
	w.mu.Unlock()

	w.updateChan <- struct{}{}
}

// processPodStatus processes pod status for problems
func (w *Watcher) processPodStatus(pod *corev1.Pod) {
	for _, containerStatus := range pod.Status.ContainerStatuses {
		// Check for CrashLoopBackOff
		if containerStatus.State.Waiting != nil && containerStatus.State.Waiting.Reason == "CrashLoopBackOff" {
			w.addProblem(
				SeverityFatal,
				"CrashLoopBackOff",
				pod.Namespace,
				pod.Name,
				containerStatus.Name,
				fmt.Sprintf("Container crashing repeatedly (restarts: %d)", containerStatus.RestartCount),
				map[string]string{
					"restarts": fmt.Sprintf("%d", containerStatus.RestartCount),
				},
			)
		}

		// Check for OOMKilled
		if containerStatus.LastTerminationState.Terminated != nil &&
			containerStatus.LastTerminationState.Terminated.Reason == "OOMKilled" {
			w.addProblem(
				SeverityFatal,
				"OOMKilled",
				pod.Namespace,
				pod.Name,
				containerStatus.Name,
				"Container killed due to out of memory",
				map[string]string{
					"exit_code": fmt.Sprintf("%d", containerStatus.LastTerminationState.Terminated.ExitCode),
				},
			)
		}
	}

	w.updateChan <- struct{}{}
}

// addProblem adds or updates a problem
func (w *Watcher) addProblem(severity Severity, typ, namespace, podName, containerName, message string, details map[string]string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	problemKey := fmt.Sprintf("%s/%s/%s/%s", namespace, podName, containerName, typ)
	now := time.Now()

	if problem, exists := w.problems[problemKey]; exists {
		problem.Count++
		problem.LastSeen = now
		problem.Message = message
		for k, v := range details {
			problem.Details[k] = v
		}
	} else {
		w.problems[problemKey] = &Problem{
			Severity:      severity,
			Type:          typ,
			Namespace:     namespace,
			PodName:       podName,
			ContainerName: containerName,
			Message:       message,
			Reason:        typ,
			FirstSeen:     now,
			LastSeen:      now,
			Count:         1,
			Details:       details,
		}
	}
}

// updateStats periodically updates cluster statistics
func (w *Watcher) updateStats(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.refreshStats()
		}
	}
}

// refreshStats refreshes cluster statistics
func (w *Watcher) refreshStats() {
	// Get pod stats
	pods, err := w.clientset.CoreV1().Pods(w.config.Namespace).List(context.Background(), metav1.ListOptions{})
	if err == nil {
		running := 0
		problem := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning {
				running++
			} else if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodPending {
				problem++
			}
		}

		w.mu.Lock()
		w.stats.TotalPods = len(pods.Items)
		w.stats.RunningPods = running
		w.stats.ProblemPods = problem
		w.stats.CriticalCount = len(w.problems)
		w.mu.Unlock()

		w.updateChan <- struct{}{}
	}

	// Get node stats
	nodes, err := w.clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err == nil {
		ready := 0
		for _, node := range nodes.Items {
			for _, condition := range node.Status.Conditions {
				if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
					ready++
					break
				}
			}
		}

		w.mu.Lock()
		w.stats.TotalNodes = len(nodes.Items)
		w.stats.ReadyNodes = ready
		w.stats.NotReadyNodes = len(nodes.Items) - ready
		w.mu.Unlock()

		w.updateChan <- struct{}{}
	}
}

// classifyEventSeverity classifies event severity based on reason and type
func classifyEventSeverity(reason, eventType string) Severity {
	reason = strings.ToLower(reason)

	// Fatal events
	if strings.Contains(reason, "oomkilled") ||
		strings.Contains(reason, "crashloop") ||
		strings.Contains(reason, "failed") && eventType == "Warning" {
		return SeverityFatal
	}

	// Critical events
	if strings.Contains(reason, "imagepull") ||
		strings.Contains(reason, "backoff") ||
		strings.Contains(reason, "evicted") ||
		strings.Contains(reason, "nodenotready") ||
		strings.Contains(reason, "failedscheduling") {
		return SeverityCritical
	}

	// Warning events
	if strings.Contains(reason, "unhealthy") ||
		strings.Contains(reason, "probe") && eventType == "Warning" ||
		strings.Contains(reason, "throttle") {
		return SeverityWarning
	}

	return "" // Not a problem event
}
