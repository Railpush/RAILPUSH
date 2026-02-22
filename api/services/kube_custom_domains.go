package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/railpush/api/models"
)

func kubeCustomDomainHash(domain string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(domain))))
	// 12 hex chars (48-bit) keeps names <=63 chars while materially reducing collision risk.
	return hex.EncodeToString(sum[:])[:12]
}

func kubeCustomDomainIngressName(serviceID string, domain string) string {
	base := kubeServiceName(serviceID)
	name := fmt.Sprintf("%s-cd-%s", base, kubeCustomDomainHash(domain))
	if len(name) > 63 {
		name = name[:63]
		name = strings.Trim(name, "-")
	}
	if name == "" {
		return base + "-cd"
	}
	return name
}

func kubeCustomDomainTLSSecretName(serviceID string, domain string) string {
	base := kubeServiceName(serviceID)
	name := fmt.Sprintf("%s-cdtls-%s", base, kubeCustomDomainHash(domain))
	if len(name) > 63 {
		name = name[:63]
		name = strings.Trim(name, "-")
	}
	if name == "" {
		return base + "-cdtls"
	}
	return name
}

func (k *KubeDeployer) customDomainIssuer() string {
	if k == nil || k.Config == nil {
		return "letsencrypt-http01-prod"
	}
	issuer := strings.TrimSpace(k.Config.Kubernetes.CustomDomainIssuer)
	if issuer == "" {
		return "letsencrypt-http01-prod"
	}
	return issuer
}

func (k *KubeDeployer) UpsertCustomDomainIngress(svc *models.Service, domain string) (string, error) {
	if k == nil || k.Client == nil || k.Config == nil {
		return "", fmt.Errorf("kube deployer not initialized")
	}
	if svc == nil {
		return "", fmt.Errorf("missing service")
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return "", fmt.Errorf("missing domain")
	}
	switch strings.ToLower(strings.TrimSpace(svc.Type)) {
	case "web", "static":
		// ok
	default:
		return "", fmt.Errorf("custom domains are only supported for web/static services")
	}

	ns := k.namespace()
	svcName := kubeServiceName(svc.ID)
	ingName := kubeCustomDomainIngressName(svc.ID, domain)
	secretName := kubeCustomDomainTLSSecretName(svc.ID, domain)
	labels := kubeServiceLabels(svc)
	labels["app.kubernetes.io/component"] = "custom-domain"
	labels["railpush.com/custom-domain-hash"] = kubeCustomDomainHash(domain)

	port := int32(svc.Port)
	if port <= 0 {
		port = 10000
	}
	issuer := k.customDomainIssuer()
	ingClass := strings.TrimSpace(k.Config.Kubernetes.IngressClass)
	if ingClass == "" {
		ingClass = "nginx"
	}

	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ingName,
			Namespace: ns,
			Labels:    labels,
			Annotations: map[string]string{
				"nginx.ingress.kubernetes.io/proxy-read-timeout": "3600",
				"nginx.ingress.kubernetes.io/proxy-send-timeout": "3600",
				"nginx.ingress.kubernetes.io/proxy-body-size":    "50m",

				// Use cert-manager ingress-shim to create a per-domain Certificate.
				"cert-manager.io/cluster-issuer":            issuer,
				"acme.cert-manager.io/http01-ingress-class": ingClass,

				// Debugging/ops metadata.
				"railpush.com/custom-domain": domain,
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &ingClass,
			TLS: []networkingv1.IngressTLS{
				{
					Hosts:      []string{domain},
					SecretName: secretName,
				},
			},
			Rules: []networkingv1.IngressRule{
				{
					Host: domain,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: func() *networkingv1.PathType { pt := networkingv1.PathTypePrefix; return &pt }(),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: svcName,
											Port: networkingv1.ServiceBackendPort{Number: port},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if existing, err := k.Client.NetworkingV1().Ingresses(ns).Get(ctx, ingName, metav1.GetOptions{}); err == nil && existing != nil {
		ing.ResourceVersion = existing.ResourceVersion
		if _, err := k.Client.NetworkingV1().Ingresses(ns).Update(ctx, ing, metav1.UpdateOptions{}); err != nil {
			return secretName, fmt.Errorf("update custom domain ingress: %w", err)
		}
		return secretName, nil
	} else if apierrors.IsNotFound(err) {
		if _, err := k.Client.NetworkingV1().Ingresses(ns).Create(ctx, ing, metav1.CreateOptions{}); err != nil {
			return secretName, fmt.Errorf("create custom domain ingress: %w", err)
		}
		return secretName, nil
	} else if err != nil {
		return secretName, fmt.Errorf("get custom domain ingress: %w", err)
	}

	return secretName, nil
}

// UpsertCustomDomainRedirectIngress creates an Ingress that 301-redirects all
// traffic from `domain` to `redirectTarget` (e.g. apex -> www).
// It still uses cert-manager for TLS on the source domain.
func (k *KubeDeployer) UpsertCustomDomainRedirectIngress(svc *models.Service, domain, redirectTarget string) (string, error) {
	if k == nil || k.Client == nil || k.Config == nil {
		return "", fmt.Errorf("kube deployer not initialized")
	}
	if svc == nil {
		return "", fmt.Errorf("missing service")
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	redirectTarget = strings.ToLower(strings.TrimSpace(redirectTarget))
	if domain == "" || redirectTarget == "" {
		return "", fmt.Errorf("missing domain or redirect target")
	}

	ns := k.namespace()
	svcName := kubeServiceName(svc.ID)
	ingName := kubeCustomDomainIngressName(svc.ID, domain)
	secretName := kubeCustomDomainTLSSecretName(svc.ID, domain)
	labels := kubeServiceLabels(svc)
	labels["app.kubernetes.io/component"] = "custom-domain"
	labels["railpush.com/custom-domain-hash"] = kubeCustomDomainHash(domain)

	port := int32(svc.Port)
	if port <= 0 {
		port = 10000
	}
	issuer := k.customDomainIssuer()
	ingClass := strings.TrimSpace(k.Config.Kubernetes.IngressClass)
	if ingClass == "" {
		ingClass = "nginx"
	}

	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ingName,
			Namespace: ns,
			Labels:    labels,
			Annotations: map[string]string{
				// 301 permanent redirect to the target domain.
				"nginx.ingress.kubernetes.io/permanent-redirect":      "https://" + redirectTarget + "/$1",
				"nginx.ingress.kubernetes.io/permanent-redirect-code": "301",
				"nginx.ingress.kubernetes.io/use-regex":               "true",

				// cert-manager for TLS on the source domain.
				"cert-manager.io/cluster-issuer":            issuer,
				"acme.cert-manager.io/http01-ingress-class": ingClass,

				// Metadata.
				"railpush.com/custom-domain":  domain,
				"railpush.com/redirect-target": redirectTarget,
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &ingClass,
			TLS: []networkingv1.IngressTLS{
				{
					Hosts:      []string{domain},
					SecretName: secretName,
				},
			},
			Rules: []networkingv1.IngressRule{
				{
					Host: domain,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/(.*)",
									PathType: func() *networkingv1.PathType { pt := networkingv1.PathTypeImplementationSpecific; return &pt }(),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: svcName,
											Port: networkingv1.ServiceBackendPort{Number: port},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if existing, err := k.Client.NetworkingV1().Ingresses(ns).Get(ctx, ingName, metav1.GetOptions{}); err == nil && existing != nil {
		ing.ResourceVersion = existing.ResourceVersion
		if _, err := k.Client.NetworkingV1().Ingresses(ns).Update(ctx, ing, metav1.UpdateOptions{}); err != nil {
			return secretName, fmt.Errorf("update redirect ingress: %w", err)
		}
		return secretName, nil
	} else if apierrors.IsNotFound(err) {
		if _, err := k.Client.NetworkingV1().Ingresses(ns).Create(ctx, ing, metav1.CreateOptions{}); err != nil {
			return secretName, fmt.Errorf("create redirect ingress: %w", err)
		}
		return secretName, nil
	} else if err != nil {
		return secretName, fmt.Errorf("get redirect ingress: %w", err)
	}

	return secretName, nil
}

func (k *KubeDeployer) DeleteCustomDomainIngress(serviceID string, domain string) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	ns := k.namespace()
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return fmt.Errorf("missing domain")
	}
	ingName := kubeCustomDomainIngressName(serviceID, domain)
	secretName := kubeCustomDomainTLSSecretName(serviceID, domain)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = k.Client.NetworkingV1().Ingresses(ns).Delete(ctx, ingName, metav1.DeleteOptions{})
	_ = k.Client.CoreV1().Secrets(ns).Delete(ctx, secretName, metav1.DeleteOptions{})
	return nil
}

func (k *KubeDeployer) IsCustomDomainTLSReady(serviceID string, domain string) (bool, error) {
	if k == nil || k.Client == nil {
		return false, fmt.Errorf("kube deployer not initialized")
	}
	ns := k.namespace()
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return false, fmt.Errorf("missing domain")
	}
	secretName := kubeCustomDomainTLSSecretName(serviceID, domain)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sec, err := k.Client.CoreV1().Secrets(ns).Get(ctx, secretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if sec == nil || sec.Type != corev1.SecretTypeTLS {
		// cert-manager uses kubernetes.io/tls. If it's not there yet, treat as not ready.
		return false, nil
	}
	if len(sec.Data["tls.crt"]) == 0 || len(sec.Data["tls.key"]) == 0 {
		return false, nil
	}
	return true, nil
}

func (k *KubeDeployer) ReconcileCustomDomainIngresses(svc *models.Service) error {
	if k == nil || k.Client == nil || k.Config == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	if svc == nil {
		return fmt.Errorf("missing service")
	}
	switch strings.ToLower(strings.TrimSpace(svc.Type)) {
	case "web", "static":
		// ok
	default:
		return nil
	}

	domains, err := models.ListCustomDomains(svc.ID)
	if err != nil {
		return err
	}
	for _, d := range domains {
		if d.RedirectTarget != "" {
			if _, err := k.UpsertCustomDomainRedirectIngress(svc, d.Domain, d.RedirectTarget); err != nil {
				return err
			}
		} else {
			if _, err := k.UpsertCustomDomainIngress(svc, d.Domain); err != nil {
				return err
			}
		}
	}
	return nil
}
