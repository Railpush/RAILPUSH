package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/railpush/api/models"
	"github.com/railpush/api/utils"
)

func kubeRewriteRuleHash(ruleID string) string {
	sum := sha256.Sum256([]byte(ruleID))
	return hex.EncodeToString(sum[:])[:12]
}

// kubeRewriteRuleIngressName returns the K8s ingress name for a rewrite rule.
// Format: rp-svc-<serviceID>-rw-<hash12>
func kubeRewriteRuleIngressName(serviceID, ruleID string) string {
	base := kubeServiceName(serviceID)
	name := fmt.Sprintf("%s-rw-%s", base, kubeRewriteRuleHash(ruleID))
	if len(name) > 63 {
		name = name[:63]
		name = strings.Trim(name, "-")
	}
	return name
}

// UpsertRewriteRuleIngress creates or updates an Ingress that routes a specific
// path prefix from the source service's host to a destination service.
//
// For example, with source service host "app.example.com" and rule:
//   source_path=/api/  dest_service=backend  dest_path=/api/
//
// This creates an Ingress for host "app.example.com" with path "/api/(.*)"
// routing to the backend K8s service, with rewrite-target "/$1" to strip/remap
// the prefix as configured.
func (k *KubeDeployer) UpsertRewriteRuleIngress(svc *models.Service, rule *models.RewriteRule) error {
	if k == nil || k.Client == nil || k.Config == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	if svc == nil || rule == nil {
		return fmt.Errorf("missing service or rule")
	}

	ns := k.namespace()
	ingName := kubeRewriteRuleIngressName(svc.ID, rule.ID)
	ingClass := strings.TrimSpace(k.Config.Kubernetes.IngressClass)
	if ingClass == "" {
		ingClass = "nginx"
	}

	// Get the destination service to find its K8s service name and port.
	destSvc, err := models.GetService(rule.DestServiceID)
	if err != nil || destSvc == nil {
		return fmt.Errorf("destination service not found: %s", rule.DestServiceID)
	}
	destSvcName := kubeServiceName(destSvc.ID)
	destPort := int32(destSvc.Port)
	if destPort <= 0 {
		destPort = 10000
	}

	// Determine the host(s) for this ingress — use the source service's default host
	// plus any custom domains.
	deployDomain := strings.ToLower(strings.TrimSpace(k.Config.Deploy.Domain))
	host := utils.ServiceDefaultHost(svc.Type, svc.Name, svc.Subdomain, deployDomain)

	// Build the path pattern. nginx ingress requires regex for rewrite.
	// Source: /api/*  ->  Ingress path: /api/(.*)  rewrite-target: /api/$1
	// Source: /api/   ->  Ingress path: /api/(.*)  rewrite-target: <dest_path>$1
	sourcePath := strings.TrimRight(rule.SourcePath, "/")
	destPath := strings.TrimRight(rule.DestPath, "/")

	ingressPath := sourcePath + "/(.*)"
	rewriteTarget := destPath + "/$1"

	labels := kubeServiceLabels(svc)
	labels["app.kubernetes.io/component"] = "rewrite-rule"
	labels["railpush.com/rewrite-rule-id"] = rule.ID

	// Use the same TLS secret as the main service ingress (wildcard cert).
	tlsSecret := strings.TrimSpace(k.Config.Kubernetes.TLSSecret)
	if tlsSecret == "" {
		tlsSecret = "apps-wildcard-tls"
	}

	annotations := map[string]string{
		"nginx.ingress.kubernetes.io/use-regex":      "true",
		"nginx.ingress.kubernetes.io/rewrite-target":  rewriteTarget,
		"nginx.ingress.kubernetes.io/proxy-read-timeout": "3600",
		"nginx.ingress.kubernetes.io/proxy-send-timeout": "3600",
		"nginx.ingress.kubernetes.io/proxy-body-size":    "50m",
		"railpush.com/rewrite-rule-id":                   rule.ID,
		"railpush.com/source-path":                       rule.SourcePath,
		"railpush.com/dest-service":                      destSvc.Name,
	}
	for key, value := range k.serviceIngressPolicyAnnotations(svc) {
		annotations[key] = value
	}

	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        ingName,
			Namespace:   ns,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &ingClass,
			TLS: []networkingv1.IngressTLS{
				{
					Hosts:      []string{host},
					SecretName: tlsSecret,
				},
			},
			Rules: []networkingv1.IngressRule{
				{
					Host: host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     ingressPath,
									PathType: func() *networkingv1.PathType { pt := networkingv1.PathTypeImplementationSpecific; return &pt }(),
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: destSvcName,
											Port: networkingv1.ServiceBackendPort{Number: destPort},
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

	// Also add rules for each custom domain attached to this service.
	customDomains, _ := models.ListCustomDomains(svc.ID)
	for _, cd := range customDomains {
		if cd.RedirectTarget != "" {
			continue // Skip redirect-only domains; they redirect everything.
		}
		// Add the custom domain TLS entry.
		cdSecretName := kubeCustomDomainTLSSecretName(svc.ID, cd.Domain)
		ing.Spec.TLS = append(ing.Spec.TLS, networkingv1.IngressTLS{
			Hosts:      []string{cd.Domain},
			SecretName: cdSecretName,
		})
		// Add the custom domain rule.
		ing.Spec.Rules = append(ing.Spec.Rules, networkingv1.IngressRule{
			Host: cd.Domain,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{
						{
							Path:     ingressPath,
							PathType: func() *networkingv1.PathType { pt := networkingv1.PathTypeImplementationSpecific; return &pt }(),
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: destSvcName,
									Port: networkingv1.ServiceBackendPort{Number: destPort},
								},
							},
						},
					},
				},
			},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if existing, err := k.Client.NetworkingV1().Ingresses(ns).Get(ctx, ingName, metav1.GetOptions{}); err == nil && existing != nil {
		ing.ResourceVersion = existing.ResourceVersion
		if _, err := k.Client.NetworkingV1().Ingresses(ns).Update(ctx, ing, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("update rewrite rule ingress: %w", err)
		}
		return nil
	} else if apierrors.IsNotFound(err) {
		if _, err := k.Client.NetworkingV1().Ingresses(ns).Create(ctx, ing, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create rewrite rule ingress: %w", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("get rewrite rule ingress: %w", err)
	}
	return nil
}

// ReconcileRewriteRuleIngresses ensures all rewrite rules for a service have
// corresponding K8s ingress resources, and removes stale ones.
func (k *KubeDeployer) ReconcileRewriteRuleIngresses(svc *models.Service) error {
	if k == nil || k.Client == nil || k.Config == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	if svc == nil {
		return fmt.Errorf("missing service")
	}

	ns := k.namespace()
	rules, err := models.ListRewriteRules(svc.ID)
	if err != nil {
		return err
	}

	// Build a set of expected ingress names.
	expectedNames := map[string]bool{}
	for i := range rules {
		ingName := kubeRewriteRuleIngressName(svc.ID, rules[i].ID)
		expectedNames[ingName] = true
		if err := k.UpsertRewriteRuleIngress(svc, &rules[i]); err != nil {
			return err
		}
	}

	// Delete stale rewrite-rule ingresses that no longer have corresponding rules.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	labelSelector := fmt.Sprintf("railpush.com/service-id=%s,app.kubernetes.io/component=rewrite-rule", svc.ID)
	ingList, err := k.Client.NetworkingV1().Ingresses(ns).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return err
	}
	for _, ing := range ingList.Items {
		if !expectedNames[ing.Name] {
			_ = k.Client.NetworkingV1().Ingresses(ns).Delete(ctx, ing.Name, metav1.DeleteOptions{})
		}
	}
	return nil
}
