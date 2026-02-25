package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/railpush/api/models"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func kubeNetpolNameServiceMTLS(serviceID string) string {
	id := strings.ToLower(strings.TrimSpace(serviceID))
	if id == "" {
		id = "unknown"
	}
	id = strings.NewReplacer("_", "-", ".", "-", " ", "-").Replace(id)
	id = kubeNameInvalidChars.ReplaceAllString(id, "-")
	id = strings.Trim(id, "-")
	if id == "" {
		id = "unknown"
	}
	name := "rp-svc-" + id + "-mtls"
	if len(name) > 63 {
		name = strings.Trim(name[:63], "-")
	}
	if name == "" {
		name = "rp-svc-mtls"
	}
	return name
}

func normalizeAllowedServiceIDs(workspaceID, selfServiceID string, allowed []string) []string {
	out := []string{}
	seen := map[string]struct{}{}

	selfServiceID = strings.TrimSpace(selfServiceID)
	if selfServiceID != "" {
		seen[selfServiceID] = struct{}{}
		out = append(out, selfServiceID)
	}

	workspaceID = strings.TrimSpace(workspaceID)
	for _, item := range allowed {
		serviceID := strings.TrimSpace(item)
		if serviceID == "" {
			continue
		}
		if _, ok := seen[serviceID]; ok {
			continue
		}
		seen[serviceID] = struct{}{}
		out = append(out, serviceID)
	}
	return out
}

func (k *KubeDeployer) upsertServiceMTLSStrictPolicy(ctx context.Context, svc *models.Service, allowedServiceIDs []string) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	if svc == nil {
		return fmt.Errorf("missing service")
	}
	workspaceID := strings.TrimSpace(svc.WorkspaceID)
	serviceID := strings.TrimSpace(svc.ID)
	if workspaceID == "" || serviceID == "" {
		return fmt.Errorf("missing workspace or service id")
	}

	ns := k.namespace()
	name := kubeNetpolNameServiceMTLS(serviceID)
	allowed := normalizeAllowedServiceIDs(workspaceID, serviceID, allowedServiceIDs)

	from := make([]networkingv1.NetworkPolicyPeer, 0, len(allowed))
	for _, allowedID := range allowed {
		from = append(from, networkingv1.NetworkPolicyPeer{
			PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
				rpLabelManagedBy:   rpManagedByValue,
				rpLabelComponent:   "service",
				rpLabelWorkspaceID: workspaceID,
				rpLabelServiceID:   allowedID,
			}},
		})
	}

	port := int32(svc.Port)
	if port <= 0 {
		port = 10000
	}
	tcp := corev1.ProtocolTCP
	targetPort := intstr.FromInt(int(port))

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				rpLabelManagedBy:   rpManagedByValue,
				rpLabelComponent:   "service-mtls",
				rpLabelWorkspaceID: workspaceID,
				rpLabelServiceID:   serviceID,
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{
				rpLabelManagedBy:   rpManagedByValue,
				rpLabelComponent:   "service",
				rpLabelWorkspaceID: workspaceID,
				rpLabelServiceID:   serviceID,
				rpLabelMTLSStrict:  "true",
			}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From: from,
				Ports: []networkingv1.NetworkPolicyPort{{
					Protocol: &tcp,
					Port:     &targetPort,
				}},
			}},
		},
	}

	return k.upsertNetworkPolicy(ctx, ns, np)
}

func (k *KubeDeployer) deleteServiceMTLSStrictPolicy(ctx context.Context, serviceID string) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	name := kubeNetpolNameServiceMTLS(serviceID)
	err := k.Client.NetworkingV1().NetworkPolicies(k.namespace()).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (k *KubeDeployer) setServiceMTLSStrictLabel(ctx context.Context, svc *models.Service, strict bool) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	if svc == nil {
		return fmt.Errorf("missing service")
	}
	ns := k.namespace()
	depName := kubeServiceName(svc.ID)
	dep, err := k.Client.AppsV1().Deployments(ns).Get(ctx, depName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if dep.Spec.Template.Labels == nil {
		dep.Spec.Template.Labels = map[string]string{}
	}

	current := strings.TrimSpace(dep.Spec.Template.Labels[rpLabelMTLSStrict])
	if strict {
		if current == "true" {
			return nil
		}
		dep.Spec.Template.Labels[rpLabelMTLSStrict] = "true"
	} else {
		if current == "" {
			return nil
		}
		delete(dep.Spec.Template.Labels, rpLabelMTLSStrict)
	}

	_, err = k.Client.AppsV1().Deployments(ns).Update(ctx, dep, metav1.UpdateOptions{})
	return err
}

func (k *KubeDeployer) ReconcileServiceMTLSPolicy(svc *models.Service, cfg *ServiceMTLSConfig) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	if svc == nil {
		return fmt.Errorf("missing service")
	}
	now := time.Now().UTC()
	normalized, err := NormalizeServiceMTLSConfig(cfg, now)
	if err != nil {
		return err
	}
	strict := IsStrictServiceMTLS(normalized)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	if err := k.EnsureTenantNetworkPolicies(ctx, svc.WorkspaceID); err != nil {
		return err
	}
	if err := k.setServiceMTLSStrictLabel(ctx, svc, strict); err != nil {
		return err
	}
	if strict {
		return k.upsertServiceMTLSStrictPolicy(ctx, svc, normalized.AllowedServices)
	}
	return k.deleteServiceMTLSStrictPolicy(ctx, svc.ID)
}
