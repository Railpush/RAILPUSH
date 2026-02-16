package services

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	rpLabelManagedBy   = "app.kubernetes.io/managed-by"
	rpLabelComponent   = "app.kubernetes.io/component"
	rpLabelWorkspaceID = "railpush.com/workspace-id"
	rpManagedByValue   = "railpush"
)

func kubeNetpolNameIngressFromIngressNginx() string {
	return "rp-allow-ingress-nginx"
}

func kubeNetpolNameWorkspaceIsolation(workspaceID string) string {
	id := strings.ToLower(strings.TrimSpace(workspaceID))
	if id == "" {
		id = "unknown"
	}
	// metadata.name must be a DNS label; workspace IDs are typically UUIDs, but be conservative.
	id = strings.NewReplacer("_", "-", ".", "-", " ", "-").Replace(id)
	id = kubeNameInvalidChars.ReplaceAllString(id, "-")
	id = strings.Trim(id, "-")
	if id == "" {
		id = "unknown"
	}
	name := "rp-ws-" + id + "-isolation"
	if len(name) > 63 {
		name = name[:63]
		name = strings.Trim(name, "-")
	}
	if name == "" {
		name = "rp-ws-unknown-isolation"
	}
	return name
}

func (k *KubeDeployer) upsertNetworkPolicy(ctx context.Context, ns string, np *networkingv1.NetworkPolicy) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	if np == nil || strings.TrimSpace(np.Name) == "" {
		return fmt.Errorf("missing networkpolicy")
	}
	if strings.TrimSpace(ns) == "" {
		ns = k.namespace()
	}

	existing, err := k.Client.NetworkingV1().NetworkPolicies(ns).Get(ctx, np.Name, metav1.GetOptions{})
	if err == nil && existing != nil {
		np.ResourceVersion = existing.ResourceVersion
		if _, err := k.Client.NetworkingV1().NetworkPolicies(ns).Update(ctx, np, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("update networkpolicy %s: %w", np.Name, err)
		}
		return nil
	}
	if apierrors.IsNotFound(err) {
		if _, err := k.Client.NetworkingV1().NetworkPolicies(ns).Create(ctx, np, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create networkpolicy %s: %w", np.Name, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get networkpolicy %s: %w", np.Name, err)
	}
	return nil
}

func podCIDRBaseIPs(podCIDRs []string) []net.IP {
	set := map[string]net.IP{}
	for _, raw := range podCIDRs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		ip, ipnet, err := net.ParseCIDR(raw)
		if err != nil || ip == nil || ipnet == nil {
			continue
		}
		// The flannel VXLAN interface uses the base/network IP of the node's podCIDR (e.g. 10.42.3.0/32).
		base := ipnet.IP
		if base == nil {
			base = ip
		}
		k := base.String()
		if k == "" {
			continue
		}
		set[k] = base
	}

	out := make([]net.IP, 0, len(set))
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, set[k])
	}
	return out
}

func (k *KubeDeployer) ensureTenantNetpolGlobal(ctx context.Context) error {
	ns := k.namespace()
	// ingress-nginx runs with hostNetwork=true on this k3s cluster. When it proxies to backends on other nodes,
	// the kernel picks the flannel VXLAN interface address (the base IP of that node's podCIDR, e.g. 10.42.0.0)
	// as the source IP. NetworkPolicies that only allow the ingress-nginx Pod IPs will block these cross-node
	// connections. Allow the node flannel "base" IPs (/32) in addition to the ingress-nginx pod selector.
	//
	// This stays narrow (one /32 per node) and doesn't open workspace-to-workspace pod traffic.
	var nodeFlannelIPs []string
	if k != nil && k.Client != nil {
		if nodes, err := k.Client.CoreV1().Nodes().List(ctx, metav1.ListOptions{}); err == nil && nodes != nil {
			for _, n := range nodes.Items {
				cidrs := n.Spec.PodCIDRs
				if len(cidrs) == 0 && strings.TrimSpace(n.Spec.PodCIDR) != "" {
					cidrs = []string{n.Spec.PodCIDR}
				}
				for _, ip := range podCIDRBaseIPs(cidrs) {
					if ip == nil {
						continue
					}
					ones := 32
					if ip.To4() == nil {
						ones = 128
					}
					nodeFlannelIPs = append(nodeFlannelIPs, fmt.Sprintf("%s/%d", ip.String(), ones))
				}
			}
		}
	}
	sort.Strings(nodeFlannelIPs)

	from := []networkingv1.NetworkPolicyPeer{
		{
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					// Present by default on modern clusters.
					"kubernetes.io/metadata.name": "ingress-nginx",
				},
			},
			PodSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":      "ingress-nginx",
					"app.kubernetes.io/component": "controller",
				},
			},
		},
	}
	for _, cidr := range nodeFlannelIPs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		from = append(from, networkingv1.NetworkPolicyPeer{
			IPBlock: &networkingv1.IPBlock{CIDR: cidr},
		})
	}

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubeNetpolNameIngressFromIngressNginx(),
			Namespace: ns,
			Labels: map[string]string{
				rpLabelManagedBy: rpManagedByValue,
				rpLabelComponent: "tenant-isolation",
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					rpLabelManagedBy: rpManagedByValue,
					rpLabelComponent: "service",
				},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: from,
				},
			},
		},
	}
	return k.upsertNetworkPolicy(ctx, ns, np)
}

func (k *KubeDeployer) ensureTenantNetpolWorkspace(ctx context.Context, workspaceID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return fmt.Errorf("missing workspace id")
	}
	ns := k.namespace()
	npName := kubeNetpolNameWorkspaceIsolation(workspaceID)

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      npName,
			Namespace: ns,
			Labels: map[string]string{
				rpLabelManagedBy:   rpManagedByValue,
				rpLabelComponent:   "tenant-isolation",
				rpLabelWorkspaceID: workspaceID,
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					rpLabelManagedBy:   rpManagedByValue,
					rpLabelWorkspaceID: workspaceID,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									rpLabelManagedBy:   rpManagedByValue,
									rpLabelWorkspaceID: workspaceID,
								},
							},
						},
					},
				},
			},
		},
	}

	return k.upsertNetworkPolicy(ctx, ns, np)
}

// EnsureTenantNetworkPolicies ensures:
// - per-workspace default-deny ingress (only allow from same workspace)
// - global allow from ingress-nginx controller -> service pods
//
// This is the minimal network-level tenant isolation model while keeping all workloads
// in a shared namespace.
func (k *KubeDeployer) EnsureTenantNetworkPolicies(ctx context.Context, workspaceID string) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	if ctx == nil {
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return k.EnsureTenantNetworkPolicies(cctx, workspaceID)
	}
	if err := k.ensureTenantNetpolGlobal(ctx); err != nil {
		return err
	}
	return k.ensureTenantNetpolWorkspace(ctx, workspaceID)
}

// ReconcileTenantNetworkPolicies backfills policies for any existing workspaces found
// via the labels on Deployments/StatefulSets in the namespace.
func (k *KubeDeployer) ReconcileTenantNetworkPolicies(ctx context.Context) error {
	if k == nil || k.Client == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	if ctx == nil {
		cctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		return k.ReconcileTenantNetworkPolicies(cctx)
	}
	if err := k.ensureTenantNetpolGlobal(ctx); err != nil {
		return err
	}

	ns := k.namespace()
	wsIDs := map[string]struct{}{}

	deps, err := k.Client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{
		LabelSelector: rpLabelManagedBy + "=" + rpManagedByValue,
	})
	if err != nil {
		return fmt.Errorf("list deployments: %w", err)
	}
	for _, d := range deps.Items {
		if ws := strings.TrimSpace(d.Labels[rpLabelWorkspaceID]); ws != "" {
			wsIDs[ws] = struct{}{}
		}
	}

	sts, err := k.Client.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{
		LabelSelector: rpLabelManagedBy + "=" + rpManagedByValue,
	})
	if err != nil {
		return fmt.Errorf("list statefulsets: %w", err)
	}
	for _, s := range sts.Items {
		if ws := strings.TrimSpace(s.Labels[rpLabelWorkspaceID]); ws != "" {
			wsIDs[ws] = struct{}{}
		}
	}

	var errs []string
	for ws := range wsIDs {
		if err := k.ensureTenantNetpolWorkspace(ctx, ws); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("reconcile tenant networkpolicies: %s", strings.Join(errs, "; "))
	}
	return nil
}
