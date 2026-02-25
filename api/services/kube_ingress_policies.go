package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/railpush/api/models"
)

var ingressPolicyManagedAnnotationKeys = []string{
	"nginx.ingress.kubernetes.io/whitelist-source-range",
	"nginx.ingress.kubernetes.io/denylist-source-range",
	"nginx.ingress.kubernetes.io/blacklist-source-range",
	"nginx.ingress.kubernetes.io/limit-rps",
	"nginx.ingress.kubernetes.io/limit-rpm",
	"nginx.ingress.kubernetes.io/limit-connections",
	"nginx.ingress.kubernetes.io/limit-burst-multiplier",
	"nginx.ingress.kubernetes.io/enable-cors",
	"nginx.ingress.kubernetes.io/cors-allow-origin",
	"nginx.ingress.kubernetes.io/cors-allow-methods",
	"nginx.ingress.kubernetes.io/cors-allow-headers",
	"nginx.ingress.kubernetes.io/cors-expose-headers",
	"nginx.ingress.kubernetes.io/cors-allow-credentials",
	"nginx.ingress.kubernetes.io/cors-max-age",
}

func cloneAnnotations(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func annotationMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		if bv, ok := b[k]; !ok || av != bv {
			return false
		}
	}
	return true
}

func applyIngressPolicyAnnotations(existing, policy map[string]string) map[string]string {
	out := cloneAnnotations(existing)
	for _, key := range ingressPolicyManagedAnnotationKeys {
		delete(out, key)
	}
	for key, value := range policy {
		k := strings.TrimSpace(key)
		v := strings.TrimSpace(value)
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func isIngressPolicyManagedComponent(component string) bool {
	switch strings.TrimSpace(component) {
	case "", "service", "custom-domain", "rewrite-rule":
		return true
	default:
		return false
	}
}

// ReconcileServiceIngressPolicies applies the current ingress policy annotations
// (IP allow/deny lists, rate limits, CORS) to all ingress resources belonging to
// the service without triggering a new deploy.
func (k *KubeDeployer) ReconcileServiceIngressPolicies(svc *models.Service) error {
	if k == nil || k.Client == nil || k.Config == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	if svc == nil {
		return fmt.Errorf("missing service")
	}

	svcType := strings.ToLower(strings.TrimSpace(svc.Type))
	if svcType != "web" && svcType != "static" {
		return nil
	}

	ns := k.namespace()
	policyAnnotations := k.serviceIngressPolicyAnnotations(svc)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	selector := fmt.Sprintf("railpush.com/service-id=%s", strings.TrimSpace(svc.ID))
	ingList, err := k.Client.NetworkingV1().Ingresses(ns).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}

	for i := range ingList.Items {
		ing := &ingList.Items[i]
		component := ""
		if ing.Labels != nil {
			component = ing.Labels["app.kubernetes.io/component"]
		}
		if !isIngressPolicyManagedComponent(component) {
			continue
		}

		current := ing.Annotations
		next := applyIngressPolicyAnnotations(current, policyAnnotations)
		if annotationMapsEqual(current, next) {
			continue
		}

		ingCopy := ing.DeepCopy()
		ingCopy.Annotations = next
		if _, err := k.Client.NetworkingV1().Ingresses(ns).Update(ctx, ingCopy, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("update ingress %s policy annotations: %w", ing.Name, err)
		}
	}

	return nil
}
