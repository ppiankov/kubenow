// This file gathers pods, logs, events, node conditions.

package snapshot

import (
    "context"
    "encoding/json"
    "fmt"
    "strings"
    "time"

    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
)

type ContainerInfo struct {
    Name         string       `json:"name"`
    Image        string       `json:"image"`
    LastState    string       `json:"lastState"`
    LastReason   string       `json:"lastReason"`
    RestartCount int32        `json:"restartCount"`
    Ready        bool         `json:"ready"`
    Resources    ResourceInfo `json:"resources"`
    LastLogs     []string     `json:"lastLogs"`
}

type ResourceInfo struct {
    Requests map[string]string `json:"requests"`
    Limits   map[string]string `json:"limits"`
}

type EventInfo struct {
    Type    string `json:"type"`
    Reason  string `json:"reason"`
    Message string `json:"message"`
}

type ProblemPod struct {
    Namespace  string          `json:"namespace"`
    Name       string          `json:"name"`
    Node       string          `json:"node"`
    Phase      string          `json:"phase"`
    Reason     string          `json:"reason"`
    Restarts   int32           `json:"restarts"`
    IssueType  string          `json:"issueType"` // e.g. ImagePullError, CrashLoop, OOMKilled, PendingScheduling, etc.
    Containers []ContainerInfo `json:"containers"`
    Events     []EventInfo     `json:"events"`
}

type NodeCondition struct {
    Node       string            `json:"node"`
    Conditions map[string]string `json:"conditions"`
}

type Snapshot struct {
    Cluster        string          `json:"cluster"`
    Timestamp      string          `json:"timestamp"`
    Namespaces     []string        `json:"namespaces"`
    ProblemPods    []ProblemPod    `json:"problemPods"`
    NodeConditions []NodeCondition `json:"nodeConditions"`
}

func Collect(ctx context.Context, client *kubernetes.Clientset, namespace string, maxPods int, logLines int64) (*Snapshot, error) {
    snap := &Snapshot{
        Cluster:   client.RESTClient().Get().URL().Host,
        Timestamp: time.Now().Format(time.RFC3339),
    }

    // Which namespaces to scan
    nsList := []string{}
    if namespace != "" {
        nsList = append(nsList, namespace)
    } else {
        nss, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
        if err != nil {
            return nil, err
        }
        for _, n := range nss.Items {
            if n.Name == "kube-system" || n.Name == "kube-node-lease" {
                continue
            }
            nsList = append(nsList, n.Name)
        }
    }
    snap.Namespaces = nsList

    // Node conditions
    nodes, _ := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
    for _, n := range nodes.Items {
        nc := NodeCondition{
            Node:       n.Name,
            Conditions: map[string]string{},
        }
        for _, c := range n.Status.Conditions {
            nc.Conditions[string(c.Type)] = string(c.Status)
        }
        snap.NodeConditions = append(snap.NodeConditions, nc)
    }

    // Scan pods in each namespace
    for _, ns := range nsList {
        pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
        if err != nil {
            continue
        }

        for _, pod := range pods.Items {
            // Decide if this pod is actually interesting/problematic
            isProblem := false

            switch pod.Status.Phase {
            case corev1.PodFailed, corev1.PodUnknown, corev1.PodPending:
                isProblem = true

            case corev1.PodRunning:
                // Running but with restarts or waiting states
                for _, cs := range pod.Status.ContainerStatuses {
                    if cs.RestartCount > 0 {
                        isProblem = true
                        break
                    }
                    if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
                        isProblem = true
                        break
                    }
                }

            case corev1.PodSucceeded:
                // Completed job – usually not a problem for current-state incident views
                isProblem = false
            }

            if !isProblem {
                continue
            }

            problem := ProblemPod{
                Namespace: pod.Namespace,
                Name:      pod.Name,
                Node:      pod.Spec.NodeName,
                Phase:     string(pod.Status.Phase),
            }

            // Pod-level reason, use first container as representative
            if len(pod.Status.ContainerStatuses) > 0 {
                cs := pod.Status.ContainerStatuses[0]
                if cs.State.Waiting != nil {
                    problem.Reason = cs.State.Waiting.Reason
                } else if cs.LastTerminationState.Terminated != nil {
                    problem.Reason = cs.LastTerminationState.Terminated.Reason
                }
                problem.Restarts = cs.RestartCount
            }

            // All containers
            for _, cs := range pod.Status.ContainerStatuses {
                ci := ContainerInfo{
                    Name:         cs.Name,
                    RestartCount: cs.RestartCount,
                    Ready:        cs.Ready,
                    Resources: ResourceInfo{
                        Requests: map[string]string{},
                        Limits:   map[string]string{},
                    },
                }

                // State / reason
                if cs.State.Waiting != nil {
                    ci.LastState = "Waiting"
                    ci.LastReason = cs.State.Waiting.Reason
                } else if cs.LastTerminationState.Terminated != nil {
                    ci.LastState = "Terminated"
                    ci.LastReason = cs.LastTerminationState.Terminated.Reason
                } else if cs.State.Running != nil {
                    ci.LastState = "Running"
                }

                // Get resource requests/limits & image from spec
                for _, c := range pod.Spec.Containers {
                    if c.Name == cs.Name {
                        ci.Image = c.Image
                        if cpu := c.Resources.Requests.Cpu(); cpu != nil {
                            ci.Resources.Requests["cpu"] = cpu.String()
                        }
                        if mem := c.Resources.Requests.Memory(); mem != nil {
                            ci.Resources.Requests["memory"] = mem.String()
                        }
                        if cpu := c.Resources.Limits.Cpu(); cpu != nil {
                            ci.Resources.Limits["cpu"] = cpu.String()
                        }
                        if mem := c.Resources.Limits.Memory(); mem != nil {
                            ci.Resources.Limits["memory"] = mem.String()
                        }
                    }
                }

                // Fetch logs (best-effort; ignore errors)
                req := client.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
                    Container: cs.Name,
                    TailLines: &logLines,
                })
                raw, _ := req.Do(ctx).Raw()
                lines := strings.Split(string(raw), "\n")
                if len(lines) > 0 && lines[len(lines)-1] == "" {
                    lines = lines[:len(lines)-1]
                }
                ci.LastLogs = lines

                problem.Containers = append(problem.Containers, ci)
            }

            // Pod events (best-effort)
            evs, _ := client.CoreV1().Events(ns).List(ctx, metav1.ListOptions{
                FieldSelector: fmt.Sprintf("involvedObject.name=%s", pod.Name),
                Limit:         20,
            })
            for _, e := range evs.Items {
                problem.Events = append(problem.Events, EventInfo{
                    Type:    e.Type,
                    Reason:  e.Reason,
                    Message: e.Message,
                })
            }

            // Classify issue type
            problem.IssueType = classifyProblem(problem)

            snap.ProblemPods = append(snap.ProblemPods, problem)

            if maxPods > 0 && len(snap.ProblemPods) >= maxPods {
                return snap, nil
            }
        }
    }

    return snap, nil
}

func (s *Snapshot) JSON() string {
    b, _ := json.MarshalIndent(s, "", "  ")
    return string(b)
}

// classifyProblem assigns a coarse issue type label to a problematic pod,
// based on its reason, container states, and events.
func classifyProblem(p ProblemPod) string {
    r := strings.ToLower(p.Reason)

    // Events-based classification
    for _, ev := range p.Events {
        reason := strings.ToLower(ev.Reason)
        msg := strings.ToLower(ev.Message)

        if strings.Contains(reason, "imagepullbackoff") || strings.Contains(reason, "errimagepull") ||
            strings.Contains(msg, "imagepullbackoff") || strings.Contains(msg, "pulling image") {
            return "ImagePullError"
        }
        if strings.Contains(reason, "failedscheduling") {
            if strings.Contains(msg, "insufficient") {
                return "InsufficientResources"
            }
            return "PendingScheduling"
        }
    }

    // Reason-based classification
    if strings.Contains(r, "imagepullbackoff") || strings.Contains(r, "errimagepull") {
        return "ImagePullError"
    }
    if strings.Contains(r, "crashloopbackoff") {
        return "CrashLoop"
    }
    if strings.Contains(r, "oomkilled") {
        return "OOMKilled"
    }

    // Container state classification
    for _, c := range p.Containers {
        lr := strings.ToLower(c.LastReason)
        if strings.Contains(lr, "crashloopbackoff") {
            return "CrashLoop"
        }
        if strings.Contains(lr, "oomkilled") {
            return "OOMKilled"
        }
    }

    if p.Phase == string(corev1.PodPending) {
        return "Pending"
    }
    if p.Phase == string(corev1.PodFailed) {
        return "FailedPod"
    }

    return "Unknown"
}
