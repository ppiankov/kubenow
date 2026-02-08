package output

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ppiankov/kubenow/internal/analyzer"
	"github.com/ppiankov/kubenow/internal/monitor"
)

// SARIF represents the SARIF 2.1.0 format for static analysis results
// Spec: https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html
type SARIF struct {
	Schema  string `json:"$schema"`
	Version string `json:"version"`
	Runs    []Run  `json:"runs"`
}

type Run struct {
	Tool    Tool     `json:"tool"`
	Results []Result `json:"results"`
}

type Tool struct {
	Driver Driver `json:"driver"`
}

type Driver struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	InformationUri  string `json:"informationUri"`
	SemanticVersion string `json:"semanticVersion"`
	Rules           []Rule `json:"rules"`
}

type Rule struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	ShortDescription MessageString          `json:"shortDescription"`
	FullDescription  MessageString          `json:"fullDescription"`
	Help             MessageString          `json:"help"`
	DefaultLevel     string                 `json:"defaultConfiguration.level"`
	Properties       map[string]interface{} `json:"properties,omitempty"`
}

type Result struct {
	RuleID     string                 `json:"ruleId"`
	Level      string                 `json:"level"`
	Message    MessageString          `json:"message"`
	Locations  []Location             `json:"locations,omitempty"`
	Properties map[string]interface{} `json:"properties,omitempty"`
}

type Location struct {
	PhysicalLocation PhysicalLocation `json:"physicalLocation"`
}

type PhysicalLocation struct {
	ArtifactLocation ArtifactLocation `json:"artifactLocation"`
	Region           Region           `json:"region,omitempty"`
}

type ArtifactLocation struct {
	URI string `json:"uri"`
}

type Region struct {
	StartLine int `json:"startLine,omitempty"`
}

type MessageString struct {
	Text string `json:"text"`
}

// GenerateSARIFFromRequestsSkew converts requests-skew analysis to SARIF format
func GenerateSARIFFromRequestsSkew(result *analyzer.RequestsSkewResult, version string) ([]byte, error) {
	sarif := SARIF{
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		Version: "2.1.0",
		Runs: []Run{
			{
				Tool: Tool{
					Driver: Driver{
						Name:            "kubenow",
						Version:         version,
						InformationUri:  "https://github.com/ppiankov/kubenow",
						SemanticVersion: version,
						Rules:           generateRequestsSkewRules(),
					},
				},
				Results: convertRequestsSkewToResults(result),
			},
		},
	}

	return json.MarshalIndent(sarif, "", "  ")
}

// GenerateSARIFFromMonitor converts monitor problems to SARIF format
func GenerateSARIFFromMonitor(problems []monitor.Problem, version string) ([]byte, error) {
	sarif := SARIF{
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		Version: "2.1.0",
		Runs: []Run{
			{
				Tool: Tool{
					Driver: Driver{
						Name:            "kubenow",
						Version:         version,
						InformationUri:  "https://github.com/ppiankov/kubenow",
						SemanticVersion: version,
						Rules:           generateMonitorRules(),
					},
				},
				Results: convertMonitorToResults(problems),
			},
		},
	}

	return json.MarshalIndent(sarif, "", "  ")
}

func generateRequestsSkewRules() []Rule {
	return []Rule{
		{
			ID:   "over-provisioned-cpu",
			Name: "Over-provisioned CPU Resources",
			ShortDescription: MessageString{
				Text: "Workload CPU requests significantly exceed actual usage",
			},
			FullDescription: MessageString{
				Text: "This workload has CPU requests that are much higher than actual usage, leading to resource waste and potentially higher costs.",
			},
			Help: MessageString{
				Text: "Review P99 CPU usage and consider reducing CPU requests. Ensure safety margin is appropriate for workload characteristics.",
			},
			DefaultLevel: "warning",
		},
		{
			ID:   "unsafe-reduction",
			Name: "Unsafe Resource Reduction",
			ShortDescription: MessageString{
				Text: "Reducing resources would be unsafe based on usage patterns",
			},
			FullDescription: MessageString{
				Text: "This workload shows usage patterns that make resource reduction unsafe, such as OOMKills, high spike ratios, or usage near limits.",
			},
			Help: MessageString{
				Text: "Do not reduce resources for this workload. Investigate OOMKills, spikes, or high restart counts before making changes.",
			},
			DefaultLevel: "error",
		},
	}
}

func generateMonitorRules() []Rule {
	return []Rule{
		{
			ID:   "pod-crashloop",
			Name: "Pod CrashLoopBackOff",
			ShortDescription: MessageString{
				Text: "Pod is repeatedly crashing and restarting",
			},
			FullDescription: MessageString{
				Text: "The pod is in CrashLoopBackOff state, indicating repeated failures to start or run successfully.",
			},
			Help: MessageString{
				Text: "Check pod logs with kubectl logs. Common causes: configuration errors, missing dependencies, application bugs.",
			},
			DefaultLevel: "error",
		},
		{
			ID:   "pod-oomkilled",
			Name: "Container OOMKilled",
			ShortDescription: MessageString{
				Text: "Container was killed due to out of memory",
			},
			FullDescription: MessageString{
				Text: "The container exceeded its memory limit and was killed by the system.",
			},
			Help: MessageString{
				Text: "Increase memory limits or investigate memory leaks. Check memory usage patterns with monitoring tools.",
			},
			DefaultLevel: "error",
		},
		{
			ID:   "pod-imagepull",
			Name: "Image Pull Failure",
			ShortDescription: MessageString{
				Text: "Cannot pull container image",
			},
			FullDescription: MessageString{
				Text: "The pod cannot pull its container image, preventing it from starting.",
			},
			Help: MessageString{
				Text: "Verify image name, registry access, and authentication. Check imagePullSecrets if using private registry.",
			},
			DefaultLevel: "error",
		},
		{
			ID:   "pod-pending",
			Name: "Pod Stuck in Pending",
			ShortDescription: MessageString{
				Text: "Pod cannot be scheduled",
			},
			FullDescription: MessageString{
				Text: "The pod has been pending for an extended period, unable to be scheduled to a node.",
			},
			Help: MessageString{
				Text: "Check node resources, pod resource requests, node selectors, taints, and tolerations.",
			},
			DefaultLevel: "error",
		},
	}
}

func convertRequestsSkewToResults(result *analyzer.RequestsSkewResult) []Result {
	results := make([]Result, 0)

	for _, w := range result.Results {
		// Skip if no significant over-provisioning
		if w.SkewCPU < 2.0 {
			continue
		}

		level := "warning"
		ruleID := "over-provisioned-cpu"

		// Check if reduction would be unsafe
		if w.Safety != nil && w.Safety.Rating == "UNSAFE" {
			level = "error"
			ruleID = "unsafe-reduction"
		}

		message := fmt.Sprintf("Workload %s/%s: CPU requests (%.2f) exceed P99 usage (%.2f) by %.1fx",
			w.Namespace, w.Workload, w.RequestedCPU, w.P99UsedCPU, w.SkewCPU)

		if w.Safety != nil {
			message += fmt.Sprintf(" | Safety: %s", w.Safety.Rating)
		}

		result := Result{
			RuleID: ruleID,
			Level:  level,
			Message: MessageString{
				Text: message,
			},
			Locations: []Location{
				{
					PhysicalLocation: PhysicalLocation{
						ArtifactLocation: ArtifactLocation{
							URI: fmt.Sprintf("kubernetes://%s/%s/%s", w.Namespace, w.Type, w.Workload),
						},
					},
				},
			},
			Properties: map[string]interface{}{
				"namespace":     w.Namespace,
				"workload":      w.Workload,
				"type":          w.Type,
				"requested_cpu": w.RequestedCPU,
				"p99_used_cpu":  w.P99UsedCPU,
				"skew_ratio":    w.SkewCPU,
				"impact_score":  w.ImpactScore,
				"runtime":       w.Runtime,
			},
		}

		if w.Safety != nil {
			result.Properties["safety_rating"] = w.Safety.Rating
			result.Properties["oom_kills"] = w.Safety.OOMKills
			result.Properties["restarts"] = w.Safety.Restarts
		}

		results = append(results, result)
	}

	return results
}

func convertMonitorToResults(problems []monitor.Problem) []Result {
	results := make([]Result, 0)

	for _, p := range problems {
		ruleID := getRuleIDForProblemType(p.Type)
		level := getSARIFLevelForSeverity(p.Severity)

		message := fmt.Sprintf("%s in %s/%s", p.Type, p.Namespace, p.PodName)
		if p.ContainerName != "" {
			message += fmt.Sprintf(" (container: %s)", p.ContainerName)
		}
		if p.Message != "" {
			message += fmt.Sprintf(": %s", p.Message)
		}

		result := Result{
			RuleID: ruleID,
			Level:  level,
			Message: MessageString{
				Text: message,
			},
			Locations: []Location{
				{
					PhysicalLocation: PhysicalLocation{
						ArtifactLocation: ArtifactLocation{
							URI: fmt.Sprintf("kubernetes://%s/pod/%s", p.Namespace, p.PodName),
						},
					},
				},
			},
			Properties: map[string]interface{}{
				"namespace":  p.Namespace,
				"pod":        p.PodName,
				"container":  p.ContainerName,
				"severity":   string(p.Severity),
				"type":       p.Type,
				"count":      p.Count,
				"first_seen": p.FirstSeen.Format(time.RFC3339),
				"last_seen":  p.LastSeen.Format(time.RFC3339),
			},
		}

		results = append(results, result)
	}

	return results
}

func getRuleIDForProblemType(problemType string) string {
	switch problemType {
	case "CrashLoopBackOff":
		return "pod-crashloop"
	case "OOMKilled":
		return "pod-oomkilled"
	case "ImagePullBackOff", "ErrImagePull":
		return "pod-imagepull"
	case "PodPending":
		return "pod-pending"
	default:
		return "pod-problem"
	}
}

func getSARIFLevelForSeverity(severity monitor.Severity) string {
	switch severity {
	case monitor.SeverityFatal:
		return "error"
	case monitor.SeverityCritical:
		return "error"
	case monitor.SeverityWarning:
		return "warning"
	default:
		return "note"
	}
}
