package promonitor

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ExportFormat represents the output format for export.
type ExportFormat string

const (
	FormatPatch    ExportFormat = "patch"
	FormatManifest ExportFormat = "manifest"
	FormatDiff     ExportFormat = "diff"
	FormatJSON     ExportFormat = "json"
)

// Export generates output in the requested format.
// currentJSON is required only for FormatManifest (the full K8s object as JSON).
func Export(rec *AlignmentRecommendation, format ExportFormat, currentJSON []byte) (string, error) {
	if rec == nil {
		return "", fmt.Errorf("no recommendation to export")
	}

	switch format {
	case FormatPatch:
		return exportPatch(rec)
	case FormatManifest:
		if len(currentJSON) == 0 {
			return "", fmt.Errorf("manifest format requires the current workload object")
		}
		return exportManifest(rec, currentJSON)
	case FormatDiff:
		return exportDiff(rec), nil
	case FormatJSON:
		return exportJSON(rec)
	default:
		return "", fmt.Errorf("unsupported export format: %q (supported: patch, manifest, diff, json)", format)
	}
}

// ExportToFile writes the export output to a file and returns the path.
func ExportToFile(rec *AlignmentRecommendation, workload WorkloadRef) (string, error) {
	output, err := Export(rec, FormatPatch, nil)
	if err != nil {
		return "", err
	}

	ts := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("kubenow-patch-%s-%s-%s-%s.yaml",
		strings.ToLower(workload.Kind), workload.Namespace, workload.Name, ts)
	if err := os.WriteFile(filename, []byte(output), 0o600); err != nil {
		return "", fmt.Errorf("failed to write export file: %w", err)
	}
	return filename, nil
}

// --- Patch format ---

// patchDoc is the struct-based YAML output for ordered fields.
type patchDoc struct {
	APIVersion string        `yaml:"apiVersion"`
	Kind       string        `yaml:"kind"`
	Metadata   patchMetadata `yaml:"metadata"`
	Spec       patchSpec     `yaml:"spec"`
}

type patchMetadata struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
}

type patchSpec struct {
	Template patchTemplate `yaml:"template"`
}

type patchTemplate struct {
	Spec patchPodSpec `yaml:"spec"`
}

type patchPodSpec struct {
	Containers []patchContainer `yaml:"containers"`
}

type patchContainer struct {
	Name      string         `yaml:"name"`
	Resources patchResources `yaml:"resources"`
}

type patchResources struct {
	Requests map[string]string `yaml:"requests"`
	Limits   map[string]string `yaml:"limits"`
}

func exportPatch(rec *AlignmentRecommendation) (string, error) {
	var b strings.Builder

	b.WriteString(evidenceComments(rec))

	containers := make([]patchContainer, len(rec.Containers))
	for i, c := range rec.Containers {
		containers[i] = patchContainer{
			Name: c.Name,
			Resources: patchResources{
				Requests: map[string]string{
					"cpu":    formatCPUResource(c.Recommended.CPURequest),
					"memory": formatMemResource(c.Recommended.MemoryRequest),
				},
				Limits: map[string]string{
					"cpu":    formatCPUResource(c.Recommended.CPULimit),
					"memory": formatMemResource(c.Recommended.MemoryLimit),
				},
			},
		}
	}

	doc := patchDoc{
		APIVersion: "apps/v1",
		Kind:       rec.Workload.Kind,
		Metadata: patchMetadata{
			Name:      rec.Workload.Name,
			Namespace: rec.Workload.Namespace,
		},
		Spec: patchSpec{
			Template: patchTemplate{
				Spec: patchPodSpec{Containers: containers},
			},
		},
	}

	data, err := yaml.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("marshal patch YAML: %w", err)
	}
	b.Write(data)
	return b.String(), nil
}

// --- Manifest format ---

func exportManifest(rec *AlignmentRecommendation, currentJSON []byte) (string, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(currentJSON, &obj); err != nil {
		return "", fmt.Errorf("failed to parse workload JSON: %w", err)
	}

	stripVolatileFields(obj)

	if err := updateContainerResources(obj, rec.Containers); err != nil {
		return "", fmt.Errorf("failed to update resources: %w", err)
	}

	data, err := yaml.Marshal(obj)
	if err != nil {
		return "", fmt.Errorf("failed to marshal YAML: %w", err)
	}

	var b strings.Builder
	b.WriteString(evidenceComments(rec))
	b.Write(data)
	return b.String(), nil
}

func stripVolatileFields(obj map[string]interface{}) {
	if metadata, ok := obj["metadata"].(map[string]interface{}); ok {
		delete(metadata, "resourceVersion")
		delete(metadata, "generation")
		delete(metadata, "managedFields")
		delete(metadata, "uid")
		delete(metadata, "creationTimestamp")
	}
	delete(obj, "status")
}

func updateContainerResources(obj map[string]interface{}, containers []ContainerAlignment) error {
	spec, ok := obj["spec"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("missing spec")
	}
	template, ok := spec["template"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("missing spec.template")
	}
	templateSpec, ok := template["spec"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("missing spec.template.spec")
	}
	containerList, ok := templateSpec["containers"].([]interface{})
	if !ok {
		return fmt.Errorf("missing spec.template.spec.containers")
	}

	for _, rec := range containers {
		for _, item := range containerList {
			container, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := container["name"].(string)
			if name != rec.Name {
				continue
			}

			container["resources"] = map[string]interface{}{
				"requests": map[string]interface{}{
					"cpu":    formatCPUResource(rec.Recommended.CPURequest),
					"memory": formatMemResource(rec.Recommended.MemoryRequest),
				},
				"limits": map[string]interface{}{
					"cpu":    formatCPUResource(rec.Recommended.CPULimit),
					"memory": formatMemResource(rec.Recommended.MemoryLimit),
				},
			}
		}
	}
	return nil
}

// --- Diff format ---

func exportDiff(rec *AlignmentRecommendation) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("--- %s/%s (current)\n", strings.ToLower(rec.Workload.Kind), rec.Workload.Name))
	b.WriteString(fmt.Sprintf("+++ %s/%s (recommended)\n", strings.ToLower(rec.Workload.Kind), rec.Workload.Name))

	for _, c := range rec.Containers {
		b.WriteString(fmt.Sprintf("\n  Container: %s\n", c.Name))
		b.WriteString("    requests:\n")
		writeDiffLine(&b, "cpu", formatCPUResource(c.Current.CPURequest), formatCPUResource(c.Recommended.CPURequest))
		writeDiffLine(&b, "memory", formatMemResource(c.Current.MemoryRequest), formatMemResource(c.Recommended.MemoryRequest))
		b.WriteString("    limits:\n")
		writeDiffLine(&b, "cpu", formatCPUResource(c.Current.CPULimit), formatCPUResource(c.Recommended.CPULimit))
		writeDiffLine(&b, "memory", formatMemResource(c.Current.MemoryLimit), formatMemResource(c.Recommended.MemoryLimit))
	}

	// Warnings
	for _, w := range rec.Warnings {
		b.WriteString(fmt.Sprintf("\n# %s\n", w))
	}

	return b.String()
}

func writeDiffLine(b *strings.Builder, label, current, recommended string) {
	if current == recommended {
		b.WriteString(fmt.Sprintf("      %s: %s\n", label, current))
	} else {
		b.WriteString(fmt.Sprintf("-     %s: %s\n", label, current))
		b.WriteString(fmt.Sprintf("+     %s: %s\n", label, recommended))
	}
}

// --- JSON format ---

func exportJSON(rec *AlignmentRecommendation) (string, error) {
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return string(data) + "\n", nil
}

// --- Helpers ---

func evidenceComments(rec *AlignmentRecommendation) string {
	var b strings.Builder

	b.WriteString("# kubenow alignment patch\n")
	b.WriteString(fmt.Sprintf("# Generated: %s\n", rec.Timestamp.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("# Workload: %s/%s/%s\n",
		rec.Workload.Namespace, strings.ToLower(rec.Workload.Kind), rec.Workload.Name))
	b.WriteString(fmt.Sprintf("# Confidence: %s  Safety: %s\n", rec.Confidence, rec.Safety))

	if rec.Evidence != nil {
		b.WriteString(fmt.Sprintf("# Latch: %s (%d samples)\n",
			rec.Evidence.Duration.String(), rec.Evidence.SampleCount))
	}

	// HPA warning
	if rec.Policy != nil && rec.Policy.HPADetected {
		b.WriteString(fmt.Sprintf("# WARNING: HPA %q targets this workload\n", rec.Policy.HPAName))
	}

	b.WriteString("#\n")
	b.WriteString("# Apply with: kubectl apply --server-side -f <this-file>\n")

	return b.String()
}

// formatCPUResource converts CPU cores to a K8s resource string.
// Examples: 0.1 → "100m", 1.0 → "1", 0.5 → "500m"
func formatCPUResource(cores float64) string {
	m := int(math.Round(cores * 1000))
	if m <= 0 {
		return "0m"
	}
	if m%1000 == 0 {
		return fmt.Sprintf("%d", m/1000)
	}
	return fmt.Sprintf("%dm", m)
}

// formatMemResource converts memory bytes to a K8s resource string.
// Examples: 134217728 → "128Mi", 1073741824 → "1Gi"
func formatMemResource(bytes float64) string {
	b := int64(math.Round(bytes))
	if b <= 0 {
		return "0"
	}
	gi := b / (1024 * 1024 * 1024)
	if gi > 0 && b%(1024*1024*1024) == 0 {
		return fmt.Sprintf("%dGi", gi)
	}
	mi := b / (1024 * 1024)
	if mi > 0 {
		return fmt.Sprintf("%dMi", mi)
	}
	return fmt.Sprintf("%d", b)
}
