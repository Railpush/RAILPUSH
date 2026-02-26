package handlers

import (
	"context"
	"strings"
	"time"

	"github.com/railpush/api/config"
	"github.com/railpush/api/services"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type managedStorageEncryptionPosture struct {
	AtRest       *bool
	Status       string
	Algorithm    string
	KeyManagement string
	Scope        string
	StorageClass string
	Evidence     string
}

func managedStorageEncryptionInfo(cfg *config.Config, worker *services.Worker) managedStorageEncryptionPosture {
	posture := managedStorageEncryptionPosture{
		AtRest:        nil,
		Status:        "unknown",
		Algorithm:     "",
		KeyManagement: "unknown",
		Scope:         "volume",
		StorageClass:  "",
		Evidence:      "runtime_not_kubernetes",
	}
	if cfg == nil || !cfg.Kubernetes.Enabled {
		return posture
	}

	scName := strings.TrimSpace(cfg.Kubernetes.StorageClass)
	posture.StorageClass = scName
	posture.Evidence = "storage_class_lookup_failed"
	if scName == "" {
		posture.Evidence = "storage_class_not_configured"
		return posture
	}
	if worker == nil {
		posture.Evidence = "worker_unavailable"
		return posture
	}

	kd, err := worker.GetKubeDeployer()
	if err != nil || kd == nil || kd.Client == nil {
		posture.Evidence = "kube_deployer_unavailable"
		return posture
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sc, err := kd.Client.StorageV1().StorageClasses().Get(ctx, scName, metav1.GetOptions{})
	if err != nil || sc == nil {
		return posture
	}

	params := sc.Parameters
	enabled := false
	for _, key := range []string{"encrypted", "encryption", "encryption_enabled", "luks_encrypted", "volume_encryption"} {
		if truthyParam(params[key]) {
			enabled = true
			break
		}
	}
	if !enabled {
		for key := range params {
			lower := strings.ToLower(strings.TrimSpace(key))
			if strings.Contains(lower, "encrypted") || strings.Contains(lower, "encryption") {
				if truthyParam(params[key]) {
					enabled = true
					break
				}
			}
		}
	}

	if enabled {
		posture.Status = "enabled"
		posture.AtRest = boolPtr(true)
		algo := strings.TrimSpace(params["encryption_algorithm"])
		if algo == "" {
			algo = strings.TrimSpace(params["encryptionAlgorithm"])
		}
		posture.Algorithm = algo
		secretName := strings.TrimSpace(params["csi.storage.k8s.io/node-stage-secret-name"])
		if secretName != "" {
			posture.KeyManagement = "kubernetes-secret"
		} else {
			posture.KeyManagement = "platform-managed"
		}
		posture.Evidence = "storage_class_parameter"
		return posture
	}

	posture.Status = "disabled"
	posture.AtRest = boolPtr(false)
	posture.KeyManagement = "not_configured"
	posture.Evidence = "storage_class_parameter"
	return posture
}

func truthyParam(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}

func boolPtr(v bool) *bool {
	return &v
}
