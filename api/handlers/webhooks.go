package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
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

func (h *WebhookHandler) GitHubWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
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
		json.Unmarshal(body, &payload)
		branch := strings.TrimPrefix(payload.Ref, "refs/heads/")
		svcs, _ := models.ListServices("")
		for _, svc := range svcs {
			if svc.RepoURL == payload.Repository.CloneURL && svc.Branch == branch && svc.AutoDeploy {
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
		}
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

		svcs, _ := models.ListServices("")
		var baseService *models.Service
		for i := range svcs {
			if svcs[i].RepoURL == repoURL && svcs[i].Branch == payload.PullRequest.Base.Ref && !strings.HasPrefix(strings.ToLower(svcs[i].Name), "preview-") {
				baseService = &svcs[i]
				break
			}
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
						domain := utils.ServiceDomainLabel(previewSvc.Name) + "." + h.Config.Deploy.Domain
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
			previewName := fmt.Sprintf("preview-%s-pr-%d", utils.ServiceDomainLabel(baseService.Name), payload.Number)
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
			}
			if err := models.CreateService(candidate); err != nil {
				log.Printf("preview create failed: %v", err)
				utils.RespondError(w, http.StatusInternalServerError, "failed to create preview service")
				return
			}
			previewSvc = candidate
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
