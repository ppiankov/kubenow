package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveWorkloadName_AppLabel(t *testing.T) {
	labels := map[string]string{"app": "payment-api"}
	assert.Equal(t, "payment-api", ResolveWorkloadName("payment-api-7d8f9c4b6-abc12", labels))
}

func TestResolveWorkloadName_K8sNameLabel(t *testing.T) {
	labels := map[string]string{"app.kubernetes.io/name": "payment-api", "app": "wrong"}
	assert.Equal(t, "payment-api", ResolveWorkloadName("payment-api-7d8f9c4b6-abc12", labels))
}

func TestResolveWorkloadName_CNPGLabel(t *testing.T) {
	labels := map[string]string{"cnpg.io/cluster": "payments-main-db"}
	assert.Equal(t, "payments-main-db", ResolveWorkloadName("payments-main-db-2", labels))
}

func TestResolveWorkloadName_NoLabels(t *testing.T) {
	assert.Equal(t, "payment-api", ResolveWorkloadName("payment-api-7d8f9c4b6-abc12", nil))
}

func TestResolveWorkloadName_EmptyLabels(t *testing.T) {
	assert.Equal(t, "payment-api", ResolveWorkloadName("payment-api-7d8f9c4b6-abc12", map[string]string{}))
}

func TestExtractWorkloadNameHeuristic_Deployment(t *testing.T) {
	assert.Equal(t, "payment-api", extractWorkloadNameHeuristic("payment-api-7d8f9c4b6-abc12"))
}

func TestExtractWorkloadNameHeuristic_Short(t *testing.T) {
	assert.Equal(t, "api-abc", extractWorkloadNameHeuristic("api-abc"))
}

func TestExtractWorkloadNameHeuristic_SingleSegment(t *testing.T) {
	assert.Equal(t, "api", extractWorkloadNameHeuristic("api"))
}
