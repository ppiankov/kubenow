package promonitor

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// helmResources holds CPU/memory requests and limits for Helm values output.
type helmResources struct {
	Requests map[string]string `yaml:"requests"`
	Limits   map[string]string `yaml:"limits"`
}

// exportHelm generates a Helm values.yaml fragment with resource overrides.
// Single-container workloads get a flat resources: block.
// Multi-container workloads get a containers: map keyed by container name.
func exportHelm(rec *AlignmentRecommendation) (string, error) {
	var b strings.Builder

	b.WriteString("# kubenow helm values override\n")
	b.WriteString(fmt.Sprintf("# Workload: %s/%s/%s\n",
		rec.Workload.Namespace, strings.ToLower(rec.Workload.Kind), rec.Workload.Name))

	if len(rec.Containers) == 1 {
		b.WriteString("# Place these values at the appropriate path in your chart's values.yaml\n")
		res := containerHelmResources(&rec.Containers[0])
		data, err := yaml.Marshal(map[string]helmResources{"resources": res})
		if err != nil {
			return "", fmt.Errorf("marshal helm values: %w", err)
		}
		b.Write(data)
	} else {
		b.WriteString("# Multi-container: place each block at the appropriate chart path\n")
		containers := make(map[string]map[string]helmResources, len(rec.Containers))
		for i := range rec.Containers {
			c := &rec.Containers[i]
			containers[c.Name] = map[string]helmResources{
				"resources": containerHelmResources(c),
			}
		}
		data, err := yaml.Marshal(map[string]interface{}{"containers": containers})
		if err != nil {
			return "", fmt.Errorf("marshal helm values: %w", err)
		}
		b.Write(data)
	}

	return b.String(), nil
}

func containerHelmResources(c *ContainerAlignment) helmResources {
	return helmResources{
		Requests: map[string]string{
			"cpu":    formatCPUResource(c.Recommended.CPURequest),
			"memory": formatMemResource(c.Recommended.MemoryRequest),
		},
		Limits: map[string]string{
			"cpu":    formatCPUResource(c.Recommended.CPULimit),
			"memory": formatMemResource(c.Recommended.MemoryLimit),
		},
	}
}
