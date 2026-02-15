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

type OpsTechnicalHandler struct {
	Config *config.Config
}

func NewOpsTechnicalHandler(cfg *config.Config) *OpsTechnicalHandler {
	return &OpsTechnicalHandler{Config: cfg}
}

func (h *OpsTechnicalHandler) ensureOps(w http.ResponseWriter, r *http.Request) bool {
	userID := middleware.GetUserID(r)
	if err := services.EnsureOpsAccess(userID); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

type kubeDeploymentSummary struct {
	Name           string `json:"name"`
	DesiredReplicas int32  `json:"desired_replicas"`
	UpdatedReplicas int32  `json:"updated_replicas"`
	ReadyReplicas   int32  `json:"ready_replicas"`
	AvailableReplicas int32 `json:"available_replicas"`
	AgeSeconds     int64  `json:"age_seconds"`
}

type kubePodSummary struct {
	Name       string `json:"name"`
	Phase      string `json:"phase"`
	Ready      bool   `json:"ready"`
	Restarts   int32  `json:"restarts"`
	NodeName   string `json:"node_name"`
	AgeSeconds int64  `json:"age_seconds"`
}

func (h *OpsTechnicalHandler) KubeSummary(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}

	cfg := h.Config
	if cfg == nil {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"enabled":   false,
			"namespace": "",
			"error":     "missing config",
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
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()

	deps, _ := kd.Client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	pods, _ := kd.Client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})

	now := time.Now()
	outDeps := []kubeDeploymentSummary{}
	if deps != nil {
		for _, d := range deps.Items {
			desired := int32(0)
			if d.Spec.Replicas != nil {
				desired = *d.Spec.Replicas
			}
			outDeps = append(outDeps, kubeDeploymentSummary{
				Name:             d.Name,
				DesiredReplicas:  desired,
				UpdatedReplicas:  d.Status.UpdatedReplicas,
				ReadyReplicas:    d.Status.ReadyReplicas,
				AvailableReplicas: d.Status.AvailableReplicas,
				AgeSeconds:       int64(now.Sub(d.CreationTimestamp.Time).Seconds()),
			})
		}
	}

	outPods := []kubePodSummary{}
	if pods != nil {
		for _, p := range pods.Items {
			ready := false
			restarts := int32(0)
			total := len(p.Status.ContainerStatuses)
			readyCount := 0
			for _, cs := range p.Status.ContainerStatuses {
				restarts += cs.RestartCount
				if cs.Ready {
					readyCount++
				}
			}
			if total > 0 && readyCount == total {
				ready = true
			}
			outPods = append(outPods, kubePodSummary{
				Name:       p.Name,
				Phase:      string(p.Status.Phase),
				Ready:      ready,
				Restarts:   restarts,
				NodeName:   p.Spec.NodeName,
				AgeSeconds: int64(now.Sub(p.CreationTimestamp.Time).Seconds()),
			})
		}
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":     true,
		"namespace":   ns,
		"deployments": outDeps,
		"pods":        outPods,
	})
}
