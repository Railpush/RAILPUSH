package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type OpsClusterHandler struct {
	Config *config.Config
}

func NewOpsClusterHandler(cfg *config.Config) *OpsClusterHandler {
	return &OpsClusterHandler{Config: cfg}
}

// ---------- response types ----------

type clusterTotals struct {
	Nodes         int   `json:"nodes"`
	Pods          int   `json:"pods"`
	RunningPods   int   `json:"running_pods"`
	Deployments   int   `json:"deployments"`
	DeploymentsReady int `json:"deployments_ready"`
	StatefulSets  int   `json:"statefulsets"`
	StatefulSetsReady int `json:"statefulsets_ready"`
	Services      int   `json:"services"`
}

type clusterNode struct {
	Name              string `json:"name"`
	Status            string `json:"status"`
	Roles             string `json:"roles"`
	CPUCapacity       int64  `json:"cpu_capacity"`
	CPUAllocatable    int64  `json:"cpu_allocatable"`
	MemCapacityMi     int64  `json:"mem_capacity_mi"`
	MemAllocatableMi  int64  `json:"mem_allocatable_mi"`
	PodCapacity       int64  `json:"pod_capacity"`
	PodCount          int    `json:"pod_count"`
	KubeletVersion    string `json:"kubelet_version"`
	OS                string `json:"os"`
	Arch              string `json:"arch"`
	AgeSeconds        int64  `json:"age_seconds"`
}

type podPhases struct {
	Running   int `json:"running"`
	Pending   int `json:"pending"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Unknown   int `json:"unknown"`
}

type nsInfo struct {
	Name     string `json:"name"`
	PodCount int    `json:"pod_count"`
}

type pvcInfo struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Status       string `json:"status"`
	Capacity     string `json:"capacity"`
	StorageClass string `json:"storage_class"`
}

func shortNodeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if idx := strings.Index(name, "."); idx > 0 {
		return name[:idx]
	}
	return name
}

func buildNodeNameLookup(nodes []clusterNode) map[string]string {
	lookup := map[string]string{}
	for _, n := range nodes {
		raw := strings.TrimSpace(n.Name)
		if raw == "" {
			continue
		}
		short := shortNodeName(raw)
		lookup[raw] = raw
		lookup[strings.ToLower(raw)] = raw
		lookup[short] = raw
		lookup[strings.ToLower(short)] = raw
	}
	return lookup
}

func resolveNodeName(lookup map[string]string, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if canonical, ok := lookup[name]; ok {
		return canonical
	}
	if canonical, ok := lookup[strings.ToLower(name)]; ok {
		return canonical
	}
	short := shortNodeName(name)
	if canonical, ok := lookup[short]; ok {
		return canonical
	}
	if canonical, ok := lookup[strings.ToLower(short)]; ok {
		return canonical
	}
	return name
}

// ---------- handler ----------

func (h *OpsClusterHandler) Summary(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	if err := services.EnsureOpsAccess(userID); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}

	cfg := h.Config
	if cfg == nil {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"enabled": false,
			"error":   "missing config",
		})
		return
	}
	if !cfg.Kubernetes.Enabled {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"enabled":   false,
			"namespace": strings.TrimSpace(cfg.Kubernetes.Namespace),
		})
		return
	}

	kd, err := services.NewKubeDeployer(cfg)
	if err != nil {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"enabled":   false,
			"namespace": strings.TrimSpace(cfg.Kubernetes.Namespace),
			"error":     "kubernetes client unavailable",
		})
		return
	}

	ns := strings.TrimSpace(cfg.Kubernetes.Namespace)
	if ns == "" {
		ns = "railpush"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	now := time.Now()

	// ---------- nodes ----------
	nodeList, _ := kd.Client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	var nodes []clusterNode
	if nodeList != nil {
		for _, n := range nodeList.Items {
			status := "NotReady"
			for _, c := range n.Status.Conditions {
				if c.Type == "Ready" && c.Status == "True" {
					status = "Ready"
				}
			}
			roles := ""
			for k := range n.Labels {
				if strings.HasPrefix(k, "node-role.kubernetes.io/") {
					role := strings.TrimPrefix(k, "node-role.kubernetes.io/")
					if role == "" {
						role = "worker"
					}
					if roles != "" {
						roles += ","
					}
					roles += role
				}
			}
			if roles == "" {
				roles = "worker"
			}

			cpuCap := n.Status.Capacity.Cpu().MilliValue()
			cpuAlloc := n.Status.Allocatable.Cpu().MilliValue()
			memCap := n.Status.Capacity.Memory().Value() / (1024 * 1024)
			memAlloc := n.Status.Allocatable.Memory().Value() / (1024 * 1024)
			podCap := n.Status.Capacity.Pods().Value()

			nodes = append(nodes, clusterNode{
				Name:             n.Name,
				Status:           status,
				Roles:            roles,
				CPUCapacity:      cpuCap,
				CPUAllocatable:   cpuAlloc,
				MemCapacityMi:    memCap,
				MemAllocatableMi: memAlloc,
				PodCapacity:      podCap,
				KubeletVersion:   n.Status.NodeInfo.KubeletVersion,
				OS:               n.Status.NodeInfo.OperatingSystem,
				Arch:             n.Status.NodeInfo.Architecture,
				AgeSeconds:       int64(now.Sub(n.CreationTimestamp.Time).Seconds()),
			})
		}
	}

	// ---------- pods ----------
	podScope := "all_namespaces"
	podWarnings := []string{}
	allPods, allPodsErr := kd.Client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if allPodsErr != nil {
		podScope = ns
		fallbackPods, fallbackErr := kd.Client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if fallbackErr != nil {
			allPods = nil
			podWarnings = append(podWarnings, "pod listing unavailable")
		} else {
			allPods = fallbackPods
			podWarnings = append(podWarnings, "cluster-wide pod listing unavailable; showing namespace pod counts")
		}
	}
	phases := podPhases{}
	nsPodCounts := map[string]int{}
	nodePodCounts := map[string]int{}
	nodeNameLookup := buildNodeNameLookup(nodes)
	totalPods := 0
	if allPods != nil {
		totalPods = len(allPods.Items)
		for _, p := range allPods.Items {
			switch p.Status.Phase {
			case "Running":
				phases.Running++
			case "Pending":
				phases.Pending++
			case "Succeeded":
				phases.Succeeded++
			case "Failed":
				phases.Failed++
			default:
				phases.Unknown++
			}
			nsPodCounts[p.Namespace]++
			if nodeName := resolveNodeName(nodeNameLookup, p.Spec.NodeName); nodeName != "" {
				nodePodCounts[nodeName]++
			}
		}
	}

	// fill in pod counts per node
	for i := range nodes {
		nodes[i].PodCount = nodePodCounts[nodes[i].Name]
	}

	// namespace list
	var namespaces []nsInfo
	for name, count := range nsPodCounts {
		namespaces = append(namespaces, nsInfo{Name: name, PodCount: count})
	}

	// ---------- deployments ----------
	depList, _ := kd.Client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	depTotal, depReady := 0, 0
	if depList != nil {
		depTotal = len(depList.Items)
		for _, d := range depList.Items {
			desired := int32(1)
			if d.Spec.Replicas != nil {
				desired = *d.Spec.Replicas
			}
			if d.Status.ReadyReplicas >= desired {
				depReady++
			}
		}
	}

	// ---------- statefulsets ----------
	ssList, _ := kd.Client.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})
	ssTotal, ssReady := 0, 0
	if ssList != nil {
		ssTotal = len(ssList.Items)
		for _, s := range ssList.Items {
			desired := int32(1)
			if s.Spec.Replicas != nil {
				desired = *s.Spec.Replicas
			}
			if s.Status.ReadyReplicas >= desired {
				ssReady++
			}
		}
	}

	// ---------- services ----------
	svcList, _ := kd.Client.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
	svcCount := 0
	if svcList != nil {
		svcCount = len(svcList.Items)
	}

	// ---------- PVCs ----------
	pvcList, _ := kd.Client.CoreV1().PersistentVolumeClaims(ns).List(ctx, metav1.ListOptions{})
	var pvcs []pvcInfo
	if pvcList != nil {
		for _, p := range pvcList.Items {
			cap := ""
			if storage, ok := p.Status.Capacity["storage"]; ok {
				cap = storage.String()
			}
			sc := ""
			if p.Spec.StorageClassName != nil {
				sc = *p.Spec.StorageClassName
			}
			pvcs = append(pvcs, pvcInfo{
				Name:         p.Name,
				Namespace:    p.Namespace,
				Status:       string(p.Status.Phase),
				Capacity:     cap,
				StorageClass: sc,
			})
		}
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":   true,
		"namespace": ns,
		"pod_scope": podScope,
		"cluster_totals": clusterTotals{
			Nodes:             len(nodes),
			Pods:              totalPods,
			RunningPods:       phases.Running,
			Deployments:       depTotal,
			DeploymentsReady:  depReady,
			StatefulSets:      ssTotal,
			StatefulSetsReady: ssReady,
			Services:          svcCount,
		},
		"nodes":      nodes,
		"pod_phases": phases,
		"namespaces": namespaces,
		"pvcs":       pvcs,
		"warnings":   podWarnings,
	})
}
