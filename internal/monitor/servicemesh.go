// Package monitor provides real-time Kubernetes cluster monitoring via a BubbleTea TUI.
package monitor

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Service mesh detection constants
const (
	// Polling interval for service mesh health checks
	serviceMeshPollInterval = 30 * time.Second

	// Certificate expiry thresholds
	certWarningThreshold  = 7 * 24 * time.Hour // 7 days
	certCriticalThreshold = 48 * time.Hour     // 48 hours
	certFatalThreshold    = 24 * time.Hour     // 24 hours

	// Known service mesh namespaces
	linkerdNamespace = "linkerd"
	istioNamespace   = "istio-system"

	// Known secret names for mesh certificates
	linkerdCertSecret = "linkerd-identity-issuer"
	istioCertSecret   = "istio-ca-secret"
)

// serviceMeshCheck defines a control plane deployment to monitor
type serviceMeshCheck struct {
	namespace string
	meshName  string // "linkerd" or "istio"
}

// certCheck defines a certificate secret to monitor
type certCheck struct {
	namespace  string
	secretName string
	meshName   string
	certKeys   []string // possible keys in the secret data (e.g., "tls.crt", "ca-cert.pem")
}

// watchServiceMesh polls service mesh control plane deployments and certificates.
// Runs regardless of --namespace filter because mesh failures affect all namespaces.
func (w *Watcher) watchServiceMesh(ctx context.Context) {
	// Initial check on startup
	w.checkServiceMeshHealth(ctx)

	ticker := time.NewTicker(serviceMeshPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.checkServiceMeshHealth(ctx)
		}
	}
}

// checkServiceMeshHealth checks all service mesh components
func (w *Watcher) checkServiceMeshHealth(ctx context.Context) {
	meshChecks := []serviceMeshCheck{
		{namespace: linkerdNamespace, meshName: "linkerd"},
		{namespace: istioNamespace, meshName: "istio"},
	}

	for _, check := range meshChecks {
		w.checkControlPlane(ctx, check)
	}

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

	for _, check := range certChecks {
		w.checkCertExpiry(ctx, check)
	}
}

// checkControlPlane checks if service mesh control plane deployments have available replicas
func (w *Watcher) checkControlPlane(ctx context.Context, check serviceMeshCheck) {
	deployments, err := w.clientset.AppsV1().Deployments(check.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		if errors.IsNotFound(err) || errors.IsForbidden(err) {
			return // Namespace doesn't exist or no access — mesh not installed
		}
		// Connection errors are transient, don't create problems for them
		return
	}

	if len(deployments.Items) == 0 {
		return // No deployments in namespace — mesh not installed
	}

	for i := range deployments.Items {
		deploy := &deployments.Items[i]
		if deploy.Status.AvailableReplicas == 0 && deploy.Status.Replicas > 0 {
			w.addProblem(
				SeverityFatal,
				"ServiceMeshControlPlaneDown",
				check.namespace,
				deploy.Name,
				"",
				fmt.Sprintf("%s control plane deployment %q has 0/%d replicas available",
					check.meshName, deploy.Name, deploy.Status.Replicas),
				map[string]string{
					"mesh":               check.meshName,
					"deployment":         deploy.Name,
					"desired_replicas":   fmt.Sprintf("%d", deploy.Status.Replicas),
					"available_replicas": "0",
					"hint":               fmt.Sprintf("kubectl get pods -n %s -l app=%s", check.namespace, deploy.Name),
				},
			)
		}
	}
}

// checkCertExpiry checks service mesh certificate secrets for approaching expiry
func (w *Watcher) checkCertExpiry(ctx context.Context, check certCheck) {
	secret, err := w.clientset.CoreV1().Secrets(check.namespace).Get(ctx, check.secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) || errors.IsForbidden(err) {
			return // Secret doesn't exist or no access — mesh may not be installed
		}
		return
	}

	// Try each possible cert key in the secret
	for _, key := range check.certKeys {
		certData, ok := secret.Data[key]
		if !ok {
			continue
		}

		expiry, subject, err := parseCertExpiry(certData)
		if err != nil {
			w.addProblem(
				SeverityWarning,
				"CertParseError",
				check.namespace,
				check.secretName,
				"",
				fmt.Sprintf("Failed to parse %s certificate from secret %q key %q: %v",
					check.meshName, check.secretName, key, err),
				map[string]string{
					"mesh":   check.meshName,
					"secret": check.secretName,
					"key":    key,
					"hint":   "Check cert-manager or manually inspect the secret",
				},
			)
			return
		}

		remaining := time.Until(expiry)
		severity := certSeverity(remaining)
		if severity == "" {
			return // Cert is healthy
		}

		w.addProblem(
			severity,
			"CertExpiringSoon",
			check.namespace,
			check.secretName,
			"",
			fmt.Sprintf("%s certificate expires in %s (at %s)",
				check.meshName, formatCertDuration(remaining), expiry.Format(time.RFC3339)),
			map[string]string{
				"mesh":       check.meshName,
				"secret":     check.secretName,
				"key":        key,
				"subject":    subject,
				"expires_at": expiry.Format(time.RFC3339),
				"remaining":  remaining.String(),
				"hint":       certHint(check.meshName),
			},
		)
		return // Only report the first cert found in this secret
	}
}

// parseCertExpiry extracts the expiry time and subject from PEM-encoded certificate data
func parseCertExpiry(data []byte) (time.Time, string, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return time.Time{}, "", fmt.Errorf("no PEM block found")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("x509 parse failed: %w", err)
	}

	return cert.NotAfter, cert.Subject.CommonName, nil
}

// certSeverity returns the severity for a given remaining duration, or empty if healthy
func certSeverity(remaining time.Duration) Severity {
	if remaining <= 0 {
		return SeverityFatal // Already expired
	}
	if remaining < certFatalThreshold {
		return SeverityFatal
	}
	if remaining < certCriticalThreshold {
		return SeverityCritical
	}
	if remaining < certWarningThreshold {
		return SeverityWarning
	}
	return "" // Healthy
}

// formatCertDuration formats a duration for human-readable certificate messages
func formatCertDuration(d time.Duration) string {
	if d <= 0 {
		return "EXPIRED"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// certHint returns remediation hints for a given mesh type
func certHint(meshName string) string {
	switch meshName {
	case "linkerd":
		return "Rotate certs: linkerd check --proxy; Renew: linkerd upgrade | kubectl apply -f -"
	case "istio":
		return "Check status: istioctl proxy-status; Rotate: istioctl create-remote-secret"
	default:
		return "Check service mesh certificate configuration"
	}
}

// controlPlaneStatus holds the status of a control plane deployment (used in testing)
type controlPlaneStatus struct {
	name      string
	replicas  int32
	available int32
}
