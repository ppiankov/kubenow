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
	clientset  kubernetes.Interface
	config     Config
	problems   map[string]*Problem
	events     []RecentEvent
	stats      ClusterStats
	mu         sync.RWMutex
	updateChan chan struct{}
	connStatus ConnectionStatus
	lastErr    string
}

// NewWatcher creates a new cluster watcher
func NewWatcher(clientset kubernetes.Interface, config Config) *Watcher {
	return &Watcher{
		clientset:  clientset,
		config:     config,
		problems:   make(map[string]*Problem),
		events:     make([]RecentEvent, 0),
		updateChan: make(chan struct{}, 100),
	}
}

// Start begins watching cluster events and pods.
// Performs an initial connectivity probe before starting background watchers.
func (w *Watcher) Start(ctx context.Context) error {
	// Probe connectivity: a lightweight server version check
	_, err := w.clientset.Discovery().ServerVersion()
	if err != nil {
		w.mu.Lock()
		w.connStatus = ConnectionUnreachable
		w.lastErr = err.Error()
		w.mu.Unlock()
		// Still start watchers — they will retry and update status on recovery
	} else {
		w.mu.Lock()
		w.connStatus = ConnectionOK
		w.mu.Unlock()
	}

	// Start event watcher
	go w.watchEvents(ctx)

	// Start pod watcher
	go w.watchPods(ctx)

	// Start service mesh health monitor (unless disabled)
	if !w.config.DisableMesh {
		go w.watchServiceMesh(ctx)
	}

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

	stats := w.stats
	stats.Connection = w.connStatus
	stats.LastError = w.lastErr

	return problems, events, stats
}

// watchEvents watches Kubernetes events for problems
func (w *Watcher) watchEvents(ctx context.Context) {
	w.watchLoop(
		ctx,
		func() (watch.Interface, error) {
			return w.clientset.CoreV1().Events(w.config.Namespace).Watch(ctx, metav1.ListOptions{Watch: true})
		},
		func(event watch.Event) {
			if k8sEvent, ok := event.Object.(*corev1.Event); ok {
				w.processEvent(k8sEvent)
			}
		},
	)
}

// watchPods watches pod status changes
func (w *Watcher) watchPods(ctx context.Context) {
	w.watchLoop(
		ctx,
		func() (watch.Interface, error) {
			return w.clientset.CoreV1().Pods(w.config.Namespace).Watch(ctx, metav1.ListOptions{Watch: true})
		},
		func(event watch.Event) {
			if pod, ok := event.Object.(*corev1.Pod); ok {
				w.processPodStatus(pod)
			}
		},
	)
}

func (w *Watcher) watchLoop(
	ctx context.Context,
	newWatcher func() (watch.Interface, error),
	handle func(watch.Event),
) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		watcher, err := newWatcher()
		if err != nil {
			w.setConnectionError(err)
			time.Sleep(5 * time.Second)
			continue
		}
		w.setConnectionOK()

		for event := range watcher.ResultChan() {
			if event.Type == watch.Error {
				break
			}
			if event.Type == watch.Added || event.Type == watch.Modified {
				handle(event)
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
	problems := make([]Problem, 0)

	for i := range pod.Status.ContainerStatuses {
		containerStatus := &pod.Status.ContainerStatuses[i]
		problems = append(problems, w.checkCrashLoop(pod, containerStatus)...)
		problems = append(problems, w.checkOOMKill(pod, containerStatus)...)
		problems = append(problems, w.checkImagePull(pod, containerStatus)...)
		problems = append(problems, w.checkHighRestarts(pod, containerStatus)...)
	}

	problems = append(problems, w.checkPendingPod(pod)...)

	if pod.Status.Reason == "Evicted" {
		problems = append(problems, Problem{
			Severity:  SeverityCritical,
			Type:      "Evicted",
			Namespace: pod.Namespace,
			PodName:   pod.Name,
			Message:   fmt.Sprintf("Pod evicted: %s", pod.Status.Message),
			Details: map[string]string{
				"eviction_reason": pod.Status.Reason,
			},
		})
	}

	for i := range problems {
		problem := &problems[i]
		w.addProblem(
			problem.Severity,
			problem.Type,
			problem.Namespace,
			problem.PodName,
			problem.ContainerName,
			problem.Message,
			problem.Details,
		)
	}

	w.updateChan <- struct{}{}
}

func (w *Watcher) checkCrashLoop(pod *corev1.Pod, cs *corev1.ContainerStatus) []Problem {
	if cs.State.Waiting == nil || cs.State.Waiting.Reason != "CrashLoopBackOff" {
		return nil
	}

	return []Problem{{
		Severity:      SeverityFatal,
		Type:          "CrashLoopBackOff",
		Namespace:     pod.Namespace,
		PodName:       pod.Name,
		ContainerName: cs.Name,
		Message:       fmt.Sprintf("Container crashing repeatedly (restarts: %d)", cs.RestartCount),
		Details: map[string]string{
			"restarts": fmt.Sprintf("%d", cs.RestartCount),
		},
	}}
}

func (w *Watcher) checkOOMKill(pod *corev1.Pod, cs *corev1.ContainerStatus) []Problem {
	if cs.LastTerminationState.Terminated == nil || cs.LastTerminationState.Terminated.Reason != "OOMKilled" {
		return nil
	}

	terminatedAt := cs.LastTerminationState.Terminated.FinishedAt.Time
	if time.Since(terminatedAt) >= 1*time.Hour {
		return nil
	}

	return []Problem{{
		Severity:      SeverityFatal,
		Type:          "OOMKilled",
		Namespace:     pod.Namespace,
		PodName:       pod.Name,
		ContainerName: cs.Name,
		Message:       fmt.Sprintf("Container killed due to out of memory (%s ago)", formatDuration(time.Since(terminatedAt))),
		Details: map[string]string{
			"exit_code":     fmt.Sprintf("%d", cs.LastTerminationState.Terminated.ExitCode),
			"terminated_at": terminatedAt.Format(time.RFC3339),
		},
	}}
}

func (w *Watcher) checkImagePull(pod *corev1.Pod, cs *corev1.ContainerStatus) []Problem {
	if cs.State.Waiting == nil {
		return nil
	}

	reason := cs.State.Waiting.Reason
	if reason != "ImagePullBackOff" && reason != "ErrImagePull" {
		return nil
	}

	return []Problem{{
		Severity:      SeverityCritical,
		Type:          reason,
		Namespace:     pod.Namespace,
		PodName:       pod.Name,
		ContainerName: cs.Name,
		Message:       fmt.Sprintf("Cannot pull image: %s", cs.State.Waiting.Message),
		Details: map[string]string{
			"image": cs.Image,
		},
	}}
}

func (w *Watcher) checkHighRestarts(pod *corev1.Pod, cs *corev1.ContainerStatus) []Problem {
	if cs.RestartCount <= 5 {
		return nil
	}

	return []Problem{{
		Severity:      SeverityWarning,
		Type:          "HighRestarts",
		Namespace:     pod.Namespace,
		PodName:       pod.Name,
		ContainerName: cs.Name,
		Message:       fmt.Sprintf("Container has %d restarts (may indicate instability)", cs.RestartCount),
		Details: map[string]string{
			"restart_count": fmt.Sprintf("%d", cs.RestartCount),
		},
	}}
}

func (w *Watcher) checkPendingPod(pod *corev1.Pod) []Problem {
	if pod.Status.Phase != corev1.PodPending {
		return nil
	}

	podAge := time.Since(pod.CreationTimestamp.Time)
	if podAge <= 5*time.Minute {
		return nil
	}

	reason := "Unknown"
	message := "Pod stuck in Pending state"
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse {
			reason = condition.Reason
			message = condition.Message
			break
		}
	}

	return []Problem{{
		Severity:  SeverityCritical,
		Type:      "PodPending",
		Namespace: pod.Namespace,
		PodName:   pod.Name,
		Message:   fmt.Sprintf("Pod pending for %s: %s", formatDuration(podAge), message),
		Details: map[string]string{
			"reason":    reason,
			"pod_age":   podAge.String(),
			"node_name": pod.Spec.NodeName,
		},
	}}
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
			w.cleanupOldProblems()
		}
	}
}

// refreshStats refreshes cluster statistics
func (w *Watcher) refreshStats() {
	// Get pod stats
	pods, err := w.clientset.CoreV1().Pods(w.config.Namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		w.setConnectionError(err)
		return
	}

	running := 0
	problem := 0
	for i := range pods.Items {
		pod := &pods.Items[i]
		switch pod.Status.Phase {
		case corev1.PodRunning:
			running++
		case corev1.PodFailed, corev1.PodPending:
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

	// Get node stats
	nodes, err := w.clientset.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		w.setConnectionError(err)
		return
	}

	w.setConnectionOK()

	ready := 0
	for i := range nodes.Items {
		node := &nodes.Items[i]
		for j := range node.Status.Conditions {
			condition := &node.Status.Conditions[j]
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

// cleanupOldProblems removes problems that haven't been seen in a while
func (w *Watcher) cleanupOldProblems() {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	maxAge := 15 * time.Minute // Problems disappear after 15 minutes of not being seen

	for key, problem := range w.problems {
		if now.Sub(problem.LastSeen) > maxAge {
			delete(w.problems, key)
		}
	}
}

// setConnectionError records a connection failure and notifies the UI
func (w *Watcher) setConnectionError(err error) {
	w.mu.Lock()
	w.connStatus = ConnectionUnreachable
	w.lastErr = err.Error()
	w.mu.Unlock()
	w.updateChan <- struct{}{}
}

// setConnectionOK marks the connection as healthy
func (w *Watcher) setConnectionOK() {
	w.mu.Lock()
	changed := w.connStatus != ConnectionOK
	w.connStatus = ConnectionOK
	w.lastErr = ""
	w.mu.Unlock()
	if changed {
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
