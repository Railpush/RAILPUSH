package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/railpush/api/config"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type WebhookHandler struct {
	Config *config.Config
	Worker *services.Worker
}

func NewWebhookHandler(cfg *config.Config, worker *services.Worker) *WebhookHandler {
	return &WebhookHandler{Config: cfg, Worker: worker}
}

func bearerTokenFromAuthHeader(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parts := strings.SplitN(raw, " ", 2)
	if len(parts) != 2 {
		return ""
	}
	if !strings.EqualFold(strings.TrimSpace(parts[0]), "bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func constantTimeEquals(a, b string) bool {
	// Avoid leaking length via timing; also avoid allocations.
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// AlertmanagerWebhook receives Alertmanager v4 webhook payloads and stores them in Postgres.
//
// This is intended as a self-hosted "receiver of record" so alerts don't depend on external services.
// Alert delivery is authenticated via `Authorization: Bearer <ALERT_WEBHOOK_TOKEN>`.
func (h *WebhookHandler) AlertmanagerWebhook(w http.ResponseWriter, r *http.Request) {
	want := strings.TrimSpace(h.Config.Ops.AlertWebhookToken)
	if want == "" {
		// Fail-closed: don't expose an unauthenticated sink.
		utils.RespondError(w, http.StatusServiceUnavailable, "alert webhook not configured")
		return
	}
	got := bearerTokenFromAuthHeader(r.Header.Get("Authorization"))
	if !constantTimeEquals(want, got) {
		utils.RespondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Defensive limit: webhook payloads can be large (many alerts). Keep this reasonable for MVP.
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024) // 1 MiB
	body, err := io.ReadAll(r.Body)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	var payload struct {
		Status       string            `json:"status"`
		Receiver     string            `json:"receiver"`
		GroupKey     string            `json:"groupKey"`
		CommonLabels map[string]string `json:"commonLabels"`
		Alerts       []struct {
			Status string            `json:"status"`
			Labels map[string]string `json:"labels"`
		} `json:"alerts"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	alertName := ""
	severity := ""
	namespace := ""
	if payload.CommonLabels != nil {
		alertName = strings.TrimSpace(payload.CommonLabels["alertname"])
		severity = strings.TrimSpace(payload.CommonLabels["severity"])
		namespace = strings.TrimSpace(payload.CommonLabels["namespace"])
	}
	// Some Alertmanager groupings omit alertname from commonLabels (e.g. grouping by severity/namespace only).
	// For display + indexing, fall back to the alerts list if needed.
	if strings.TrimSpace(alertName) == "" && len(payload.Alerts) > 0 {
		seen := make(map[string]struct{}, 4)
		for _, a := range payload.Alerts {
			if a.Labels == nil {
				continue
			}
			n := strings.TrimSpace(a.Labels["alertname"])
			if n == "" {
				continue
			}
			seen[n] = struct{}{}
			if len(seen) > 1 {
				break
			}
		}
		if len(seen) == 1 {
			for n := range seen {
				alertName = n
			}
		} else if len(seen) > 1 {
			alertName = "multiple"
		}
	}
	if strings.TrimSpace(severity) == "" && len(payload.Alerts) > 0 && payload.Alerts[0].Labels != nil {
		severity = strings.TrimSpace(payload.Alerts[0].Labels["severity"])
	}
	if strings.TrimSpace(namespace) == "" && len(payload.Alerts) > 0 && payload.Alerts[0].Labels != nil {
		namespace = strings.TrimSpace(payload.Alerts[0].Labels["namespace"])
	}

	ev := &models.AlertEvent{
		Status:    strings.TrimSpace(payload.Status),
		Receiver:  strings.TrimSpace(payload.Receiver),
		GroupKey:  strings.TrimSpace(payload.GroupKey),
		AlertName: alertName,
		Severity:  severity,
		Namespace: namespace,
		Payload:   body,
	}
	if err := models.CreateAlertEvent(ev); err != nil {
		log.Printf("Alertmanager webhook: failed to store event: %v", err)
		utils.RespondError(w, http.StatusInternalServerError, "failed to store alert")
		return
	}

	log.Printf("Alertmanager webhook stored: id=%s status=%s receiver=%s alert=%s severity=%s namespace=%s alerts=%d",
		ev.ID, ev.Status, ev.Receiver, ev.AlertName, ev.Severity, ev.Namespace, len(payload.Alerts))
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *WebhookHandler) GitHubWebhook(w http.ResponseWriter, r *http.Request) {
	// Defensive limit: GitHub payloads can be large, but cap for abuse safety.
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024) // 1 MiB
	body, err := io.ReadAll(r.Body)
	if err != nil {
		// http.MaxBytesReader returns an error when the limit is exceeded.
		if strings.Contains(strings.ToLower(err.Error()), "too large") {
			utils.RespondError(w, http.StatusRequestEntityTooLarge, "payload too large")
			return
		}
		utils.RespondError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	if h.Config.GitHub.WebhookSecret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if !verifyGitHubSignature(body, sig, h.Config.GitHub.WebhookSecret) {
			utils.RespondError(w, http.StatusUnauthorized, "invalid signature")
			return
		}
	}
	event := r.Header.Get("X-GitHub-Event")
	switch event {
	case "push":
		var payload struct {
			Ref        string `json:"ref"`
			HeadCommit struct {
				ID      string `json:"id"`
				Message string `json:"message"`
			} `json:"head_commit"`
			Repository struct {
				CloneURL string `json:"clone_url"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid push payload")
			return
		}
		branch := strings.TrimPrefix(payload.Ref, "refs/heads/")
		repoURL := strings.TrimSpace(payload.Repository.CloneURL)
		if repoURL == "" || branch == "" {
			utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
			return
		}
		svcs, _ := models.ListAutoDeployServicesByRepoBranch(repoURL, branch)
		for _, svc := range svcs {
			deploy := &models.Deploy{
				ServiceID:     svc.ID,
				Trigger:       "github_push",
				CommitSHA:     payload.HeadCommit.ID,
				CommitMessage: payload.HeadCommit.Message,
				Branch:        branch,
			}
			if err := models.CreateDeploy(deploy); err != nil {
				log.Printf("Failed to create deploy for service %s: %v", svc.ID, err)
				continue
			}
			// Look up service owner's GitHub token for private repo cloning
			var ghToken string
			if ws, err := models.GetWorkspace(svc.WorkspaceID); err == nil && ws != nil {
				if encToken, err := models.GetUserGitHubToken(ws.OwnerID); err == nil && encToken != "" {
					if t, err := utils.Decrypt(encToken, h.Config.Crypto.EncryptionKey); err == nil {
						ghToken = t
					}
				}
			}
			// Enqueue the deploy job to the background worker
			svcCopy := svc
			h.Worker.Enqueue(services.DeployJob{
				Deploy:      deploy,
				Service:     &svcCopy,
				GitHubToken: ghToken,
			})
			log.Printf("Auto-deploy triggered for service %s (branch: %s)", svc.Name, branch)
		}
		utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case "pull_request":
		var payload struct {
			Action      string `json:"action"`
			Number      int    `json:"number"`
			PullRequest struct {
				Title string `json:"title"`
				Head  struct {
					Ref string `json:"ref"`
					SHA string `json:"sha"`
				} `json:"head"`
				Base struct {
					Ref string `json:"ref"`
				} `json:"base"`
			} `json:"pull_request"`
			Repository struct {
				CloneURL string `json:"clone_url"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid pull_request payload")
			return
		}

		action := strings.ToLower(strings.TrimSpace(payload.Action))
		repoURL := strings.TrimSpace(payload.Repository.CloneURL)
		if repoURL == "" || payload.Number == 0 {
			utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
			return
		}

		baseSvcs, _ := models.ListBaseServicesForPreview(repoURL, payload.PullRequest.Base.Ref)
		var baseService *models.Service
		if len(baseSvcs) > 0 {
			baseService = &baseSvcs[0]
		}
		if baseService == nil {
			utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "no matching base service"})
			return
		}

		existing, _ := models.GetPreviewEnvironmentByRepoPR(baseService.WorkspaceID, repoURL, payload.Number)

		if action == "closed" {
			if existing != nil && existing.ServiceID != nil {
				if previewSvc, _ := models.GetService(*existing.ServiceID); previewSvc != nil {
					if previewSvc.ContainerID != "" {
						_ = h.Worker.Deployer.RemoveContainer(previewSvc.ContainerID)
					}
					if insts, err := models.ListServiceInstances(previewSvc.ID); err == nil {
						for _, inst := range insts {
							_ = h.Worker.Deployer.RemoveContainer(inst.ContainerID)
						}
					}
					_ = models.DeleteServiceInstancesByService(previewSvc.ID)
					if h.Config.Deploy.Domain != "" && h.Config.Deploy.Domain != "localhost" {
						domain := utils.ServiceHostLabel(previewSvc.Name, previewSvc.Subdomain) + "." + h.Config.Deploy.Domain
						_ = h.Worker.Router.RemoveRoute(domain)
					}
					_ = models.DeleteService(previewSvc.ID)
				}
				_ = models.MarkPreviewEnvironmentClosed(baseService.WorkspaceID, repoURL, payload.Number)
			}
			utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "preview closed"})
			return
		}

		if action != "opened" && action != "reopened" && action != "synchronize" {
			utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
			return
		}

		var previewSvc *models.Service
		if existing != nil && existing.ServiceID != nil {
			previewSvc, _ = models.GetService(*existing.ServiceID)
		}

		if previewSvc == nil {
			previewName := fmt.Sprintf("preview-%s-pr-%d", utils.ServiceHostLabel(baseService.Name, baseService.Subdomain), payload.Number)
			candidate := &models.Service{
				WorkspaceID:       baseService.WorkspaceID,
				ProjectID:         baseService.ProjectID,
				EnvironmentID:     baseService.EnvironmentID,
				Name:              previewName,
				Type:              baseService.Type,
				Runtime:           baseService.Runtime,
				RepoURL:           baseService.RepoURL,
				Branch:            payload.PullRequest.Head.Ref,
				BuildCommand:      baseService.BuildCommand,
				StartCommand:      baseService.StartCommand,
				DockerfilePath:    baseService.DockerfilePath,
				DockerContext:     baseService.DockerContext,
				ImageURL:          baseService.ImageURL,
				HealthCheckPath:   baseService.HealthCheckPath,
				Port:              baseService.Port,
				AutoDeploy:        false,
				MaxShutdownDelay:  baseService.MaxShutdownDelay,
				PreDeployCommand:  baseService.PreDeployCommand,
				StaticPublishPath: baseService.StaticPublishPath,
				Schedule:          baseService.Schedule,
				Plan:              baseService.Plan,
				Instances:         1,
				DockerAccess:      baseService.DockerAccess,
			}
			if err := models.CreateService(candidate); err != nil {
				log.Printf("preview create failed: %v", err)
				utils.RespondError(w, http.StatusInternalServerError, "failed to create preview service")
				return
			}
			previewSvc = candidate

			// Inherit environment variables from the base service for first preview creation.
			// This gives PR previews parity with production settings while allowing later
			// preview-specific overrides (we only copy on initial create).
			if baseVars, err := models.ListEnvVars("service", baseService.ID); err != nil {
				log.Printf("preview env inheritance: failed to list base env vars for %s: %v", baseService.ID, err)
			} else if len(baseVars) > 0 {
				vars := make([]models.EnvVar, 0, len(baseVars))
				for _, v := range baseVars {
					vars = append(vars, models.EnvVar{Key: v.Key, EncryptedValue: v.EncryptedValue, IsSecret: v.IsSecret})
				}
				if err := models.BulkUpsertEnvVars("service", candidate.ID, vars); err != nil {
					log.Printf("preview env inheritance: failed to copy env vars base=%s preview=%s: %v", baseService.ID, candidate.ID, err)
				}
			}
		} else {
			previewSvc.Branch = payload.PullRequest.Head.Ref
			_ = models.UpdateService(previewSvc)
		}

		pe := &models.PreviewEnvironment{
			WorkspaceID: baseService.WorkspaceID,
			ServiceID:   &previewSvc.ID,
			Repository:  repoURL,
			PRNumber:    payload.Number,
			PRTitle:     payload.PullRequest.Title,
			PRBranch:    payload.PullRequest.Head.Ref,
			BaseBranch:  payload.PullRequest.Base.Ref,
			CommitSHA:   payload.PullRequest.Head.SHA,
			Status:      "deploying",
		}
		if err := models.CreateOrUpdatePreviewEnvironment(pe); err != nil {
			log.Printf("preview metadata upsert failed: %v", err)
		}

		deploy := &models.Deploy{
			ServiceID:     previewSvc.ID,
			Trigger:       "preview",
			CommitSHA:     payload.PullRequest.Head.SHA,
			CommitMessage: "Preview deploy for PR #" + fmt.Sprintf("%d", payload.Number),
			Branch:        payload.PullRequest.Head.Ref,
		}
		if err := models.CreateDeploy(deploy); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to create preview deploy")
			return
		}

		var ghToken string
		if ws, err := models.GetWorkspace(baseService.WorkspaceID); err == nil && ws != nil {
			if encToken, err := models.GetUserGitHubToken(ws.OwnerID); err == nil && encToken != "" {
				if t, err := utils.Decrypt(encToken, h.Config.Crypto.EncryptionKey); err == nil {
					ghToken = t
				}
			}
		}
		previewSvcCopy := *previewSvc
		h.Worker.Enqueue(services.DeployJob{
			Deploy:      deploy,
			Service:     &previewSvcCopy,
			GitHubToken: ghToken,
		})
		log.Printf("Preview deploy triggered for service %s PR#%d", previewSvc.Name, payload.Number)
	}
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "received"})
}

func verifyGitHubSignature(payload []byte, signature, secret string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	sig, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hmac.Equal(sig, mac.Sum(nil))
}
