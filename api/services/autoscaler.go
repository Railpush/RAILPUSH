package services

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/railpush/api/config"
	"github.com/railpush/api/models"
	"github.com/railpush/api/utils"
)

type Autoscaler struct {
	Config *config.Config
	Worker *Worker
	stopCh chan struct{}
}

func NewAutoscaler(cfg *config.Config, worker *Worker) *Autoscaler {
	return &Autoscaler{
		Config: cfg,
		Worker: worker,
		stopCh: make(chan struct{}),
	}
}

func (a *Autoscaler) Start() {
	go func() {
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				a.check()
			case <-a.stopCh:
				return
			}
		}
	}()
	log.Println("Autoscaler started")
}

func (a *Autoscaler) Stop() {
	close(a.stopCh)
}

func parsePct(raw string) float64 {
	raw = strings.TrimSpace(strings.TrimSuffix(raw, "%"))
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return v
}

func (a *Autoscaler) readServiceMetrics(containerID string) (cpuPct float64, memPct float64, err error) {
	out, err := a.Worker.Deployer.ExecCommand(
		"docker", "stats", "--no-stream", "--format", "{{.CPUPerc}}\t{{.MemPerc}}", containerID,
	)
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Split(strings.TrimSpace(out), "\t")
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("unexpected metrics output")
	}
	return parsePct(parts[0]), parsePct(parts[1]), nil
}

func (a *Autoscaler) check() {
	policies, err := models.ListEnabledAutoscalingPolicies()
	if err != nil {
		log.Printf("autoscaler list policies failed: %v", err)
		return
	}

	for _, p := range policies {
		svc, err := models.GetService(p.ServiceID)
		if err != nil || svc == nil || svc.ContainerID == "" || svc.Status != "live" {
			continue
		}
		if p.MinInstances < 1 {
			p.MinInstances = 1
		}
		if p.MaxInstances < p.MinInstances {
			p.MaxInstances = p.MinInstances
		}
		if svc.Instances < p.MinInstances {
			svc.Instances = p.MinInstances
		}

		cpuPct, memPct, err := a.readServiceMetrics(svc.ContainerID)
		if err != nil {
			continue
		}

		desired := svc.Instances
		scaleDirection := ""
		if cpuPct > float64(p.CPUTargetPercent) || memPct > float64(p.MemoryTargetPercent) {
			if svc.Instances < p.MaxInstances {
				desired = svc.Instances + 1
				scaleDirection = "out"
			}
		} else if cpuPct < float64(p.CPUTargetPercent)*0.5 && memPct < float64(p.MemoryTargetPercent)*0.5 {
			if svc.Instances > p.MinInstances {
				desired = svc.Instances - 1
				scaleDirection = "in"
			}
		}

		if desired == svc.Instances || scaleDirection == "" {
			continue
		}
		now := time.Now()
		if p.LastScaledAt != nil {
			elapsed := now.Sub(*p.LastScaledAt)
			if scaleDirection == "out" && elapsed < time.Duration(p.ScaleOutCooldownSec)*time.Second {
				continue
			}
			if scaleDirection == "in" && elapsed < time.Duration(p.ScaleInCooldownSec)*time.Second {
				continue
			}
		}

		oldInstances := svc.Instances
		svc.Instances = desired
		if err := models.UpdateService(svc); err != nil {
			continue
		}

		// Deploy the new replica count.
		deploy := &models.Deploy{
			ServiceID:     svc.ID,
			Trigger:       "autoscale",
			CommitSHA:     "",
			CommitMessage: fmt.Sprintf("autoscale %s from %d to %d (cpu=%.1f%% mem=%.1f%%)", scaleDirection, oldInstances, desired, cpuPct, memPct),
			Branch:        svc.Branch,
		}
		if err := models.CreateDeploy(deploy); err != nil {
			continue
		}

		var ghToken string
		auditUserID := ""
		if ws, err := models.GetWorkspace(svc.WorkspaceID); err == nil && ws != nil {
			auditUserID = ws.OwnerID
			if encToken, err := models.GetUserGitHubToken(ws.OwnerID); err == nil && encToken != "" {
				if t, err := utils.Decrypt(encToken, a.Config.Crypto.EncryptionKey); err == nil {
					ghToken = t
				}
			}
		}
		a.Worker.Enqueue(DeployJob{
			Deploy:      deploy,
			Service:     svc,
			GitHubToken: ghToken,
		})
		_ = models.TouchAutoscalingScaledAt(svc.ID, now)
		Audit(svc.WorkspaceID, auditUserID, "autoscale.triggered", "service", svc.ID, map[string]interface{}{
			"from": oldInstances,
			"to":   desired,
			"cpu":  cpuPct,
			"mem":  memPct,
		})
		log.Printf("autoscaler triggered deploy for service=%s from=%d to=%d", svc.ID, oldInstances, desired)
	}
}
