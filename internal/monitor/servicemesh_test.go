package monitor

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// generateTestCert creates a self-signed PEM certificate with the given expiry
func generateTestCert(t *testing.T, notAfter time.Time) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "test-cert",
		},
		NotBefore: time.Now().Add(-1 * time.Hour),
		NotAfter:  notAfter,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	return certPEM
}

func TestParseCertExpiry_Valid(t *testing.T) {
	expiry := time.Now().Add(30 * 24 * time.Hour) // 30 days from now
	certPEM := generateTestCert(t, expiry)

	gotExpiry, subject, err := parseCertExpiry(certPEM)
	require.NoError(t, err)
	assert.Equal(t, "test-cert", subject)
	// Allow 1 second tolerance for test execution time
	assert.WithinDuration(t, expiry, gotExpiry, time.Second)
}

func TestParseCertExpiry_NoPEMBlock(t *testing.T) {
	_, _, err := parseCertExpiry([]byte("not a PEM block"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no PEM block found")
}

func TestParseCertExpiry_InvalidCert(t *testing.T) {
	invalidPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: []byte("not a valid certificate"),
	})

	_, _, err := parseCertExpiry(invalidPEM)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "x509 parse failed")
}

func TestParseCertExpiry_EmptyData(t *testing.T) {
	_, _, err := parseCertExpiry([]byte{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no PEM block found")
}

func TestCertSeverity(t *testing.T) {
	tests := []struct {
		name      string
		want      Severity
		remaining time.Duration
	}{
		{
			name:      "healthy cert (30 days)",
			remaining: 30 * 24 * time.Hour,
			want:      "",
		},
		{
			name:      "healthy cert (8 days)",
			remaining: 8 * 24 * time.Hour,
			want:      "",
		},
		{
			name:      "warning threshold (exactly 7 days)",
			remaining: 7 * 24 * time.Hour,
			want:      "",
		},
		{
			name:      "warning (6 days)",
			remaining: 6 * 24 * time.Hour,
			want:      SeverityWarning,
		},
		{
			name:      "warning (3 days)",
			remaining: 3 * 24 * time.Hour,
			want:      SeverityWarning,
		},
		{
			name:      "critical threshold (exactly 48h)",
			remaining: 48 * time.Hour,
			want:      SeverityWarning,
		},
		{
			name:      "critical (47h)",
			remaining: 47 * time.Hour,
			want:      SeverityCritical,
		},
		{
			name:      "critical (25h)",
			remaining: 25 * time.Hour,
			want:      SeverityCritical,
		},
		{
			name:      "fatal threshold (exactly 24h)",
			remaining: 24 * time.Hour,
			want:      SeverityCritical,
		},
		{
			name:      "fatal (23h)",
			remaining: 23 * time.Hour,
			want:      SeverityFatal,
		},
		{
			name:      "fatal (1h)",
			remaining: 1 * time.Hour,
			want:      SeverityFatal,
		},
		{
			name:      "expired (0)",
			remaining: 0,
			want:      SeverityFatal,
		},
		{
			name:      "expired (negative)",
			remaining: -1 * time.Hour,
			want:      SeverityFatal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := certSeverity(tt.remaining)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatCertDuration(t *testing.T) {
	tests := []struct {
		name string
		want string
		d    time.Duration
	}{
		{"expired", "EXPIRED", -1 * time.Hour},
		{"zero", "EXPIRED", 0},
		{"minutes", "30m", 30 * time.Minute},
		{"hours", "12h", 12 * time.Hour},
		{"days", "5d", 5 * 24 * time.Hour},
		{"one day", "1d", 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCertDuration(tt.d)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCertHint(t *testing.T) {
	assert.Contains(t, certHint("linkerd"), "linkerd check --proxy")
	assert.Contains(t, certHint("istio"), "istioctl proxy-status")
	assert.Contains(t, certHint("unknown"), "certificate configuration")
}

func TestControlPlaneStatus_Fields(t *testing.T) {
	s := controlPlaneStatus{
		name:      "istiod",
		replicas:  3,
		available: 2,
	}

	assert.Equal(t, "istiod", s.name)
	assert.Equal(t, int32(3), s.replicas)
	assert.Equal(t, int32(2), s.available)
}

func TestServiceMeshCheck_Definitions(t *testing.T) {
	// Verify the mesh checks cover both linkerd and istio
	meshChecks := []serviceMeshCheck{
		{namespace: linkerdNamespace, meshName: "linkerd"},
		{namespace: istioNamespace, meshName: "istio"},
	}

	assert.Equal(t, "linkerd", meshChecks[0].namespace)
	assert.Equal(t, "linkerd", meshChecks[0].meshName)
	assert.Equal(t, "istio-system", meshChecks[1].namespace)
	assert.Equal(t, "istio", meshChecks[1].meshName)
}

func TestCertCheck_Definitions(t *testing.T) {
	// Verify cert checks cover known secret names
	certChecks := []certCheck{
		{
			namespace:  linkerdNamespace,
			secretName: linkerdCertSecret,
			certKeys:   []string{"tls.crt", "ca.crt"},
			meshName:   "linkerd",
		},
		{
			namespace:  istioNamespace,
			secretName: istioCertSecret,
			certKeys:   []string{"ca-cert.pem", "tls.crt", "cert-chain.pem"},
			meshName:   "istio",
		},
	}

	assert.Equal(t, "linkerd-identity-issuer", certChecks[0].secretName)
	assert.Contains(t, certChecks[0].certKeys, "tls.crt")
	assert.Equal(t, "istio-ca-secret", certChecks[1].secretName)
	assert.Contains(t, certChecks[1].certKeys, "ca-cert.pem")
}

func TestConstants(t *testing.T) {
	// Verify threshold ordering: fatal < critical < warning
	assert.Less(t, certFatalThreshold, certCriticalThreshold)
	assert.Less(t, certCriticalThreshold, certWarningThreshold)

	// Verify specific values
	assert.Equal(t, 24*time.Hour, certFatalThreshold)
	assert.Equal(t, 48*time.Hour, certCriticalThreshold)
	assert.Equal(t, 7*24*time.Hour, certWarningThreshold)

	// Verify poll interval
	assert.Equal(t, 30*time.Second, serviceMeshPollInterval)
}

func TestParseCertExpiry_ExtractsSubject(t *testing.T) {
	// Generate cert with a specific common name
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "identity.linkerd.cluster.local",
			Organization: []string{"linkerd"},
		},
		NotBefore: time.Now().Add(-1 * time.Hour),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour),
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	_, subject, err := parseCertExpiry(certPEM)
	require.NoError(t, err)
	assert.Equal(t, "identity.linkerd.cluster.local", subject)
}

func TestAddProblem_ServiceMeshType(t *testing.T) {
	w := &Watcher{
		problems:   make(map[string]*Problem),
		events:     make([]RecentEvent, 0),
		updateChan: make(chan struct{}, 100),
	}

	w.addProblem(
		SeverityFatal,
		"ServiceMeshControlPlaneDown",
		"linkerd",
		"linkerd-destination",
		"",
		"linkerd control plane deployment \"linkerd-destination\" has 0/2 replicas available",
		map[string]string{
			"mesh":       "linkerd",
			"deployment": "linkerd-destination",
		},
	)

	problems, _, _ := w.GetState()
	require.Len(t, problems, 1)
	assert.Equal(t, SeverityFatal, problems[0].Severity)
	assert.Equal(t, "ServiceMeshControlPlaneDown", problems[0].Type)
	assert.Equal(t, "linkerd", problems[0].Namespace)
	assert.Equal(t, "linkerd-destination", problems[0].PodName)
	assert.Equal(t, "linkerd", problems[0].Details["mesh"])
}

func TestAddProblem_CertExpiring(t *testing.T) {
	w := &Watcher{
		problems:   make(map[string]*Problem),
		events:     make([]RecentEvent, 0),
		updateChan: make(chan struct{}, 100),
	}

	w.addProblem(
		SeverityWarning,
		"CertExpiringSoon",
		"linkerd",
		"linkerd-identity-issuer",
		"",
		"linkerd certificate expires in 5d",
		map[string]string{
			"mesh":   "linkerd",
			"secret": "linkerd-identity-issuer",
		},
	)

	problems, _, _ := w.GetState()
	require.Len(t, problems, 1)
	assert.Equal(t, SeverityWarning, problems[0].Severity)
	assert.Equal(t, "CertExpiringSoon", problems[0].Type)
}

func TestAddProblem_ServiceMeshDedup(t *testing.T) {
	w := &Watcher{
		problems:   make(map[string]*Problem),
		events:     make([]RecentEvent, 0),
		updateChan: make(chan struct{}, 100),
	}

	// Add same problem twice — should dedup
	for i := 0; i < 2; i++ {
		w.addProblem(
			SeverityFatal,
			"ServiceMeshControlPlaneDown",
			"linkerd",
			"linkerd-destination",
			"",
			"control plane down",
			map[string]string{},
		)
	}

	problems, _, _ := w.GetState()
	require.Len(t, problems, 1)
	assert.Equal(t, 2, problems[0].Count)
}
