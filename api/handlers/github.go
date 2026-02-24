package handlers

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type GitHubHandler struct {
	Config *config.Config
	GitHub *services.GitHub
}

func NewGitHubHandler(cfg *config.Config, gh *services.GitHub) *GitHubHandler {
	return &GitHubHandler{Config: cfg, GitHub: gh}
}

func (h *GitHubHandler) ListRepos(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	token, err := h.getDecryptedToken(userID)
	if err != nil || token == "" {
		utils.RespondError(w, http.StatusBadRequest, "no GitHub account connected")
		return
	}
	repos, err := h.GitHub.ListRepos(token)
	if err != nil {
		utils.RespondError(w, http.StatusBadGateway, "failed to list repos: "+err.Error())
		return
	}
	utils.RespondJSON(w, http.StatusOK, repos)
}

func (h *GitHubHandler) ListBranches(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	vars := mux.Vars(r)
	owner := vars["owner"]
	repo := vars["repo"]
	if owner == "" || repo == "" {
		utils.RespondError(w, http.StatusBadRequest, "owner and repo are required")
		return
	}
	token, err := h.getDecryptedToken(userID)
	if err != nil || token == "" {
		utils.RespondError(w, http.StatusBadRequest, "no GitHub account connected")
		return
	}
	branches, err := h.GitHub.ListBranches(token, owner, repo)
	if err != nil {
		utils.RespondError(w, http.StatusBadGateway, "failed to list branches: "+err.Error())
		return
	}
	utils.RespondJSON(w, http.StatusOK, branches)
}

func (h *GitHubHandler) ListWorkflows(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	vars := mux.Vars(r)
	owner := vars["owner"]
	repo := vars["repo"]
	if owner == "" || repo == "" {
		utils.RespondError(w, http.StatusBadRequest, "owner and repo are required")
		return
	}
	token, err := h.getDecryptedToken(userID)
	if err != nil || token == "" {
		utils.RespondError(w, http.StatusBadRequest, "no GitHub account connected")
		return
	}
	workflows, err := h.GitHub.ListWorkflows(token, owner, repo)
	if err != nil {
		utils.RespondError(w, http.StatusBadGateway, "failed to list workflows: "+err.Error())
		return
	}
	utils.RespondJSON(w, http.StatusOK, workflows)
}

func (h *GitHubHandler) getDecryptedToken(userID string) (string, error) {
	encrypted, err := models.GetUserGitHubToken(userID)
	if err != nil || encrypted == "" {
		return "", err
	}
	return utils.Decrypt(encrypted, h.Config.Crypto.EncryptionKey)
}

// ParseGitHubOwnerRepo extracts owner and repo from a GitHub URL.
// e.g. "https://github.com/owner/repo.git" -> ("owner", "repo")
func ParseGitHubOwnerRepo(repoURL string) (string, string) {
	repoURL = strings.TrimSuffix(repoURL, ".git")
	// Handle https://github.com/owner/repo
	parts := strings.Split(repoURL, "github.com/")
	if len(parts) < 2 {
		return "", ""
	}
	segments := strings.SplitN(parts[1], "/", 3)
	if len(segments) < 2 {
		return "", ""
	}
	return segments[0], segments[1]
}
