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

func TestResolveWorkloadIdentity_CNPG(t *testing.T) {
	labels := map[string]string{"cnpg.io/cluster": "payments-main-db"}
	name, op := ResolveWorkloadIdentity("payments-main-db-2", labels)
	assert.Equal(t, "payments-main-db", name)
	assert.Equal(t, "CNPG", op)
}

func TestResolveWorkloadIdentity_Strimzi(t *testing.T) {
	labels := map[string]string{"strimzi.io/cluster": "events-kafka", "app": "events-kafka"}
	name, op := ResolveWorkloadIdentity("events-kafka-0", labels)
	assert.Equal(t, "events-kafka", name)
	assert.Equal(t, "Strimzi", op)
}

func TestResolveWorkloadIdentity_ManagedBy(t *testing.T) {
	labels := map[string]string{
		"app.kubernetes.io/managed-by": "cloudnative-pg",
		"app.kubernetes.io/name":       "payments-db",
	}
	name, op := ResolveWorkloadIdentity("payments-db-1", labels)
	assert.Equal(t, "payments-db", name)
	assert.Equal(t, "CNPG", op)
}

func TestResolveWorkloadIdentity_StandardWorkload(t *testing.T) {
	labels := map[string]string{"app": "payment-api"}
	name, op := ResolveWorkloadIdentity("payment-api-7d8f9c4b6-abc12", labels)
	assert.Equal(t, "payment-api", name)
	assert.Equal(t, "", op)
}

func TestResolveWorkloadIdentity_NoLabels(t *testing.T) {
	name, op := ResolveWorkloadIdentity("payment-api-7d8f9c4b6-abc12", nil)
	assert.Equal(t, "payment-api", name)
	assert.Equal(t, "", op)
}

func TestDetectOperatorType_UnknownManagedBy(t *testing.T) {
	labels := map[string]string{"app.kubernetes.io/managed-by": "helm"}
	assert.Equal(t, "", detectOperatorType(labels))
}
