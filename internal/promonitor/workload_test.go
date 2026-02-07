package promonitor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseWorkloadRef_Valid(t *testing.T) {
	tests := []struct {
		input    string
		wantKind string
		wantName string
	}{
		{"deployment/payment-api", "Deployment", "payment-api"},
		{"deploy/payment-api", "Deployment", "payment-api"},
		{"deployments/api-server", "Deployment", "api-server"},
		{"statefulset/postgres", "StatefulSet", "postgres"},
		{"sts/redis", "StatefulSet", "redis"},
		{"statefulsets/etcd", "StatefulSet", "etcd"},
		{"daemonset/node-exporter", "DaemonSet", "node-exporter"},
		{"ds/fluentd", "DaemonSet", "fluentd"},
		{"daemonsets/kube-proxy", "DaemonSet", "kube-proxy"},
		{"Deployment/mixed-case", "Deployment", "mixed-case"},
		{"DEPLOYMENT/upper", "Deployment", "upper"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			ref, err := ParseWorkloadRef(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.wantKind, ref.Kind)
			assert.Equal(t, tt.wantName, ref.Name)
		})
	}
}

func TestParseWorkloadRef_Invalid(t *testing.T) {
	tests := []struct {
		input   string
		wantErr string
	}{
		{"", "invalid workload ref"},
		{"payment-api", "invalid workload ref"},
		{"/payment-api", "invalid workload ref"},
		{"deployment/", "invalid workload ref"},
		{"cronjob/backup", "unsupported workload kind"},
		{"job/migration", "unsupported workload kind"},
		{"replicaset/rs-abc", "unsupported workload kind"},
		{"pod/my-pod", "unsupported workload kind"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := ParseWorkloadRef(tt.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestWorkloadRef_String(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "payment-api", Namespace: "production"}
	assert.Equal(t, "deployment/payment-api", ref.String())
	assert.Equal(t, "production/deployment/payment-api", ref.FullString())
}

func TestNormalizeKind(t *testing.T) {
	tests := []struct {
		input string
		want  string
		err   bool
	}{
		{"deployment", "Deployment", false},
		{"deploy", "Deployment", false},
		{"deployments", "Deployment", false},
		{"DEPLOYMENT", "Deployment", false},
		{"statefulset", "StatefulSet", false},
		{"sts", "StatefulSet", false},
		{"daemonset", "DaemonSet", false},
		{"ds", "DaemonSet", false},
		{"cronjob", "", true},
		{"job", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := normalizeKind(tt.input)
			if tt.err {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestParseWorkloadRef_NameWithSlashes(t *testing.T) {
	// Only the first slash splits kind/name
	ref, err := ParseWorkloadRef("deployment/my-api")
	require.NoError(t, err)
	assert.Equal(t, "Deployment", ref.Kind)
	assert.Equal(t, "my-api", ref.Name)
}
