// This file gathers pods, logs, events, node conditions.

package snapshot

import (
	"context"
	"fmt"
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

// BuildSnapshot collects:
// - non-Running pods / pods with restarts / not-ready
// - last N log lines for each bad pod
// - all node conditions
func BuildSnapshot(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
	maxPods int,
	logLines int,
) (*Snapshot, error) {
	if maxPods <= 0 {
		maxPods = 20
	}
	if logLines <= 0 {
		logLines = 50
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

		// “Problem pod” heuristic
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

		// Logs (last N lines)
		var tail int64 = int64(logLines)
		logReq := clientset.CoreV1().Pods(p.Namespace).GetLogs(p.Name, &corev1.PodLogOptions{
			TailLines: &tail,
		})
		logBytes, err := logReq.DoRaw(ctx)
		if err == nil {
			ps.Logs = string(logBytes)
		} else {
			ps.Logs = "<unable to fetch logs>"
		}

		snap.ProblemPods = append(snap.ProblemPods, ps)
	}

	return snap, nil
}
