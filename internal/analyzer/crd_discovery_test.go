package analyzer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func newCRDPod(name, namespace, ownerKind, ownerName string, labels map[string]string, created time.Time) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			Labels:            labels,
			CreationTimestamp: metav1.NewTime(created),
		},
	}
	if ownerKind != "" {
		pod.OwnerReferences = []metav1.OwnerReference{{
			Kind: ownerKind,
			Name: ownerName,
		}}
	}
	return pod
}

func TestDiscoverCRDWorkloads_CNPGPods(t *testing.T) {
	now := time.Now()
	client := fake.NewSimpleClientset(
		newCRDPod("mydb-1", "prod", "Cluster", "mydb", map[string]string{
			"cnpg.io/cluster": "mydb",
		}, now.Add(-48*time.Hour)),
		newCRDPod("mydb-2", "prod", "Cluster", "mydb", map[string]string{
			"cnpg.io/cluster": "mydb",
		}, now.Add(-72*time.Hour)),
	)

	a := &RequestsSkewAnalyzer{kubeClient: client}
	groups, err := a.discoverCRDWorkloads(context.Background(), "prod", nil)
	require.NoError(t, err)
	require.Len(t, groups, 1)

	assert.Equal(t, "mydb", groups[0].workloadName)
	assert.Equal(t, "CNPG", groups[0].displayType)
	assert.Equal(t, "StatefulSet", groups[0].promqlType)
	assert.Equal(t, 2, groups[0].podCount)
	// Oldest timestamp should be selected
	assert.Equal(t, now.Add(-72*time.Hour).Unix(), groups[0].creationTime.Unix())
}

func TestDiscoverCRDWorkloads_StrimziPods(t *testing.T) {
	now := time.Now()
	client := fake.NewSimpleClientset(
		newCRDPod("kafka-0", "messaging", "StrimziPodSet", "kafka", map[string]string{
			"strimzi.io/cluster":     "kafka",
			"app.kubernetes.io/name": "kafka",
		}, now.Add(-24*time.Hour)),
	)

	a := &RequestsSkewAnalyzer{kubeClient: client}
	groups, err := a.discoverCRDWorkloads(context.Background(), "messaging", nil)
	require.NoError(t, err)
	require.Len(t, groups, 1)

	assert.Equal(t, "kafka", groups[0].workloadName)
	assert.Equal(t, "Strimzi", groups[0].displayType)
}

func TestDiscoverCRDWorkloads_SkipsStandardOwners(t *testing.T) {
	now := time.Now()
	standardKinds := []string{"ReplicaSet", "StatefulSet", "DaemonSet", "Job", "Node"}

	client := fake.NewSimpleClientset()
	for _, kind := range standardKinds {
		pod := newCRDPod("pod-"+kind, "ns", kind, "owner-"+kind, map[string]string{
			"cnpg.io/cluster": "should-be-skipped",
		}, now)
		_, err := client.CoreV1().Pods("ns").Create(context.Background(), pod, metav1.CreateOptions{})
		require.NoError(t, err)
	}

	a := &RequestsSkewAnalyzer{kubeClient: client}
	groups, err := a.discoverCRDWorkloads(context.Background(), "ns", nil)
	require.NoError(t, err)
	assert.Empty(t, groups)
}

func TestDiscoverCRDWorkloads_SkipsStandalonePods(t *testing.T) {
	// Pod with no ownerReferences
	client := fake.NewSimpleClientset(
		newCRDPod("standalone-pod", "ns", "", "", map[string]string{
			"cnpg.io/cluster": "should-be-skipped",
		}, time.Now()),
	)

	a := &RequestsSkewAnalyzer{kubeClient: client}
	groups, err := a.discoverCRDWorkloads(context.Background(), "ns", nil)
	require.NoError(t, err)
	assert.Empty(t, groups)
}

func TestDiscoverCRDWorkloads_SkipsUnknownOperator(t *testing.T) {
	// Pod with custom owner but no operator labels
	client := fake.NewSimpleClientset(
		newCRDPod("mystery-pod-0", "ns", "CustomResource", "mystery", map[string]string{
			"app": "mystery",
		}, time.Now()),
	)

	a := &RequestsSkewAnalyzer{kubeClient: client}
	groups, err := a.discoverCRDWorkloads(context.Background(), "ns", nil)
	require.NoError(t, err)
	assert.Empty(t, groups)
}

func TestDiscoverCRDWorkloads_DeduplicatesKnownWorkloads(t *testing.T) {
	client := fake.NewSimpleClientset(
		newCRDPod("mydb-1", "prod", "Cluster", "mydb", map[string]string{
			"cnpg.io/cluster": "mydb",
		}, time.Now()),
	)

	known := map[string]bool{"mydb": true}

	a := &RequestsSkewAnalyzer{kubeClient: client}
	groups, err := a.discoverCRDWorkloads(context.Background(), "prod", known)
	require.NoError(t, err)
	assert.Empty(t, groups)
}

func TestDiscoverCRDWorkloads_MixedNamespace(t *testing.T) {
	now := time.Now()
	client := fake.NewSimpleClientset(
		// Standard deployment pod (owned by ReplicaSet) — should be skipped
		newCRDPod("web-abc-123", "mixed", "ReplicaSet", "web-abc", map[string]string{
			"app": "web",
		}, now),
		// CNPG pod
		newCRDPod("pgdb-1", "mixed", "Cluster", "pgdb", map[string]string{
			"cnpg.io/cluster": "pgdb",
		}, now.Add(-10*time.Hour)),
		// Strimzi pod
		newCRDPod("kafka-0", "mixed", "StrimziPodSet", "kafka", map[string]string{
			"strimzi.io/cluster":     "kafka",
			"app.kubernetes.io/name": "kafka",
		}, now.Add(-5*time.Hour)),
		// StatefulSet pod — should be skipped
		newCRDPod("redis-0", "mixed", "StatefulSet", "redis", map[string]string{
			"app": "redis",
		}, now),
	)

	a := &RequestsSkewAnalyzer{kubeClient: client}
	groups, err := a.discoverCRDWorkloads(context.Background(), "mixed", nil)
	require.NoError(t, err)
	assert.Len(t, groups, 2)

	// Verify both CRD groups are present (order is map-dependent, so check by name)
	names := make(map[string]string)
	for _, g := range groups {
		names[g.workloadName] = g.displayType
	}
	assert.Equal(t, "CNPG", names["pgdb"])
	assert.Equal(t, "Strimzi", names["kafka"])
}

func TestDiscoverCRDWorkloads_RabbitMQ(t *testing.T) {
	client := fake.NewSimpleClientset(
		newCRDPod("rmq-server-0", "events", "RabbitmqCluster", "rmq", map[string]string{
			"rabbitmq.com/cluster-operator": "true",
			"app.kubernetes.io/name":        "rmq",
		}, time.Now()),
	)

	a := &RequestsSkewAnalyzer{kubeClient: client}
	groups, err := a.discoverCRDWorkloads(context.Background(), "events", nil)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, "rmq", groups[0].workloadName)
	assert.Equal(t, "RabbitMQ", groups[0].displayType)
}

func TestDiscoverCRDWorkloads_ManagedByFallback(t *testing.T) {
	client := fake.NewSimpleClientset(
		newCRDPod("pg-instance-1", "db", "Cluster", "pg-instance", map[string]string{
			"app.kubernetes.io/managed-by": "cloudnative-pg",
			"app.kubernetes.io/name":       "pg-instance",
		}, time.Now()),
	)

	a := &RequestsSkewAnalyzer{kubeClient: client}
	groups, err := a.discoverCRDWorkloads(context.Background(), "db", nil)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, "pg-instance", groups[0].workloadName)
	assert.Equal(t, "CNPG", groups[0].displayType)
}
