package promonitor

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// kustomizationDoc represents a kustomization.yaml file.
type kustomizationDoc struct {
	APIVersion string              `yaml:"apiVersion"`
	Kind       string              `yaml:"kind"`
	Patches    []kustomizePatchRef `yaml:"patches"`
}

// kustomizePatchRef is a patch entry in kustomization.yaml.
type kustomizePatchRef struct {
	Path   string          `yaml:"path"`
	Target kustomizeTarget `yaml:"target"`
}

// kustomizeTarget identifies the resource a patch applies to.
type kustomizeTarget struct {
	Kind      string `yaml:"kind"`
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
}

// kustomizePatchFilename returns the conventional patch filename for a workload.
func kustomizePatchFilename(ref WorkloadRef) string {
	return fmt.Sprintf("%s-%s-resources.yaml",
		strings.ToLower(ref.Kind), ref.Name)
}

// exportKustomize generates a multi-document YAML containing a kustomization.yaml
// and a strategic merge patch. The two documents are separated by "---".
func exportKustomize(rec *AlignmentRecommendation) (string, error) {
	patchFile := kustomizePatchFilename(rec.Workload)

	// Build kustomization.yaml document
	kustomization := kustomizationDoc{
		APIVersion: "kustomize.config.k8s.io/v1beta1",
		Kind:       "Kustomization",
		Patches: []kustomizePatchRef{
			{
				Path: patchFile,
				Target: kustomizeTarget{
					Kind:      rec.Workload.Kind,
					Name:      rec.Workload.Name,
					Namespace: rec.Workload.Namespace,
				},
			},
		},
	}

	kustomizationYAML, err := yaml.Marshal(kustomization)
	if err != nil {
		return "", fmt.Errorf("marshal kustomization YAML: %w", err)
	}

	// Build the strategic merge patch (same structure as exportPatch, no evidence comments)
	containers := make([]patchContainer, len(rec.Containers))
	for i := range rec.Containers {
		c := &rec.Containers[i]
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

	patchYAML, err := yaml.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("marshal patch YAML: %w", err)
	}

	// Combine as multi-document YAML
	var b strings.Builder
	b.WriteString(evidenceComments(rec))
	b.WriteString("# kustomization.yaml\n")
	b.Write(kustomizationYAML)
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("# %s\n", patchFile))
	b.Write(patchYAML)

	return b.String(), nil
}

// SplitKustomizeOutput splits the combined kustomize output into the
// kustomization.yaml content and the patch file content. Returns
// (kustomization, patch, patchFilename).
func SplitKustomizeOutput(combined string, ref WorkloadRef) (kustomization, patch, patchFilename string) {
	parts := strings.SplitN(combined, "---\n", 2)
	if len(parts) < 2 {
		return combined, "", kustomizePatchFilename(ref)
	}
	return parts[0], parts[1], kustomizePatchFilename(ref)
}
