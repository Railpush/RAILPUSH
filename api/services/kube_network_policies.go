package services

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/railpush/api/models"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	rpLabelManagedBy   = "app.kubernetes.io/managed-by"
	rpLabelComponent   = "app.kubernetes.io/component"
	rpLabelWorkspaceID = "railpush.com/workspace-id"
	rpLabelProjectID   = "railpush.com/project-id"
	rpLabelServiceID   = "railpush.com/service-id"
	rpLabelDatabaseID  = "railpush.com/database-id"
	rpLabelMTLSStrict  = "railpush.com/mtls-strict"
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

func kubeNetpolNameProjectIsolation(workspaceID, projectID string) string {
	ws := strings.ToLower(strings.TrimSpace(workspaceID))
	if ws == "" {
		ws = "unknown"
	}
	pid := strings.ToLower(strings.TrimSpace(projectID))
	if pid == "" {
		pid = "unknown"
	}
	replacer := strings.NewReplacer("_", "-", ".", "-", " ", "-")
	ws = kubeNameInvalidChars.ReplaceAllString(replacer.Replace(ws), "-")
	ws = strings.Trim(ws, "-")
	if ws == "" {
		ws = "unknown"
	}
	pid = kubeNameInvalidChars.ReplaceAllString(replacer.Replace(pid), "-")
	pid = strings.Trim(pid, "-")
	if pid == "" {
		pid = "unknown"
	}
	name := "rp-ws-" + ws + "-proj-" + pid + "-isolation"
	if len(name) > 63 {
		name = name[:63]
		name = strings.Trim(name, "-")
	}
	if name == "" {
		name = "rp-ws-proj-isolation"
	}
	return name
}

func kubeNetpolNameDatabaseAccess(databaseID string) string {
	id := strings.ToLower(strings.TrimSpace(databaseID))
	if id == "" {
		id = "unknown"
	}
	id = strings.NewReplacer("_", "-", ".", "-", " ", "-").Replace(id)
	id = kubeNameInvalidChars.ReplaceAllString(id, "-")
	id = strings.Trim(id, "-")
	if id == "" {
		id = "unknown"
	}
	name := "rp-db-" + id + "-access"
	if len(name) > 63 {
		name = strings.Trim(name[:63], "-")
	}
	if name == "" {
		name = "rp-db-access"
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

// ensureTenantNetpolDatabaseExternal allows ingress-nginx controller pods and flannel
// node IPs to reach managed-database pods on port 5432 (TCP proxy for external access).
// This is the database counterpart of ensureTenantNetpolGlobal (which covers only
// component=service). Workspace isolation still applies — this only opens the nginx→db
// path so the TCP proxy can forward connections.
func (k *KubeDeployer) ensureTenantNetpolDatabaseExternal(ctx context.Context) error {
	ns := k.namespace()
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

	pgPort := intstr.FromInt(5432)
	tcp := corev1.ProtocolTCP
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rp-allow-ingress-nginx-databases",
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
					rpLabelComponent: "managed-database",
				},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: from,
					Ports: []networkingv1.NetworkPolicyPort{
						{Port: &pgPort, Protocol: &tcp},
					},
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
					rpLabelComponent:   "service",
				},
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      rpLabelProjectID,
					Operator: metav1.LabelSelectorOpDoesNotExist,
				}, {
					Key:      rpLabelMTLSStrict,
					Operator: metav1.LabelSelectorOpNotIn,
					Values:   []string{"true"},
				}},
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
									rpLabelComponent:   "service",
								},
								MatchExpressions: []metav1.LabelSelectorRequirement{{
									Key:      rpLabelProjectID,
									Operator: metav1.LabelSelectorOpDoesNotExist,
								}},
							},
						},
					},
				},
			},
		},
	}

	return k.upsertNetworkPolicy(ctx, ns, np)
}

func (k *KubeDeployer) ensureTenantNetpolProject(ctx context.Context, workspaceID, projectID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	if workspaceID == "" || projectID == "" {
		return nil
	}
	ns := k.namespace()
	npName := kubeNetpolNameProjectIsolation(workspaceID, projectID)

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      npName,
			Namespace: ns,
			Labels: map[string]string{
				rpLabelManagedBy:   rpManagedByValue,
				rpLabelComponent:   "tenant-isolation",
				rpLabelWorkspaceID: workspaceID,
				rpLabelProjectID:   projectID,
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					rpLabelManagedBy:   rpManagedByValue,
					rpLabelComponent:   "service",
					rpLabelWorkspaceID: workspaceID,
					rpLabelProjectID:   projectID,
				},
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      rpLabelMTLSStrict,
					Operator: metav1.LabelSelectorOpNotIn,
					Values:   []string{"true"},
				}},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{
					PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
						rpLabelManagedBy:   rpManagedByValue,
						rpLabelWorkspaceID: workspaceID,
						rpLabelProjectID:   projectID,
					}},
				}},
			}},
		},
	}

	return k.upsertNetworkPolicy(ctx, ns, np)
}

func uniqueNonEmpty(items []string) []string {
	set := map[string]struct{}{}
	for _, raw := range items {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		set[v] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func (k *KubeDeployer) ensureTenantNetpolDatabaseAccess(ctx context.Context, workspaceID, databaseID string, serviceIDs []string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	databaseID = strings.TrimSpace(databaseID)
	if workspaceID == "" || databaseID == "" {
		return nil
	}
	ns := k.namespace()
	npName := kubeNetpolNameDatabaseAccess(databaseID)

	allowedServiceIDs := uniqueNonEmpty(serviceIDs)
	from := make([]networkingv1.NetworkPolicyPeer, 0, len(allowedServiceIDs))
	for _, serviceID := range allowedServiceIDs {
		from = append(from, networkingv1.NetworkPolicyPeer{
			PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
				rpLabelManagedBy:   rpManagedByValue,
				rpLabelWorkspaceID: workspaceID,
				rpLabelServiceID:   serviceID,
			}},
		})
	}

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      npName,
			Namespace: ns,
			Labels: map[string]string{
				rpLabelManagedBy:   rpManagedByValue,
				rpLabelComponent:   "tenant-isolation",
				rpLabelWorkspaceID: workspaceID,
				rpLabelDatabaseID:  databaseID,
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{
				rpLabelManagedBy:   rpManagedByValue,
				rpLabelComponent:   "managed-database",
				rpLabelWorkspaceID: workspaceID,
				rpLabelDatabaseID:  databaseID,
			}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress:     []networkingv1.NetworkPolicyIngressRule{{From: from}},
		},
	}

	return k.upsertNetworkPolicy(ctx, ns, np)
}

func (k *KubeDeployer) ensureTenantWorkspaceScopedPolicies(ctx context.Context, workspaceID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return fmt.Errorf("missing workspace id")
	}
	if err := k.ensureTenantNetpolWorkspace(ctx, workspaceID); err != nil {
		return err
	}

	services, err := models.ListServices(workspaceID)
	if err != nil {
		return fmt.Errorf("list workspace services: %w", err)
	}
	projectIDs := make([]string, 0, len(services))
	for _, svc := range services {
		if svc.ProjectID != nil {
			projectIDs = append(projectIDs, strings.TrimSpace(*svc.ProjectID))
		}
	}
	for _, projectID := range uniqueNonEmpty(projectIDs) {
		if err := k.ensureTenantNetpolProject(ctx, workspaceID, projectID); err != nil {
			return err
		}
	}

	dbs, err := models.ListManagedDatabasesByWorkspace(workspaceID)
	if err != nil {
		return fmt.Errorf("list workspace databases: %w", err)
	}
	for _, db := range dbs {
		links, lerr := models.ListServiceDatabaseLinksByDatabase(db.ID)
		if lerr != nil {
			return fmt.Errorf("list database links for %s: %w", db.ID, lerr)
		}
		serviceIDs := make([]string, 0, len(links))
		for _, link := range links {
			serviceIDs = append(serviceIDs, link.ServiceID)
		}
		if err := k.ensureTenantNetpolDatabaseAccess(ctx, workspaceID, db.ID, serviceIDs); err != nil {
			return err
		}
	}

	return nil
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
	if err := k.ensureTenantNetpolDatabaseExternal(ctx); err != nil {
		return err
	}
	return k.ensureTenantWorkspaceScopedPolicies(ctx, workspaceID)
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
	if err := k.ensureTenantNetpolDatabaseExternal(ctx); err != nil {
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
		if err := k.ensureTenantWorkspaceScopedPolicies(ctx, ws); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("reconcile tenant networkpolicies: %s", strings.Join(errs, "; "))
	}
	return nil
}
