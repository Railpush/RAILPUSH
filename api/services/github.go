package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/railpush/api/config"
)

type GitHub struct {
	Config *config.Config
}

func NewGitHub(cfg *config.Config) *GitHub {
	return &GitHub{Config: cfg}
}

func (g *GitHub) GetAuthURL(state string) string {
	return fmt.Sprintf("https://github.com/login/oauth/authorize?client_id=%s&state=%s&scope=user:email,repo",
		g.Config.GitHub.ClientID, state)
}

func (g *GitHub) ExchangeCode(code string) (string, error) {
	url := fmt.Sprintf("https://github.com/login/oauth/access_token?client_id=%s&client_secret=%s&code=%s",
		g.Config.GitHub.ClientID, g.Config.GitHub.ClientSecret, code)
	req, _ := http.NewRequest("POST", url, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.AccessToken, nil
}

func (g *GitHub) GetUser(token string) (map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var user map[string]interface{}
	json.Unmarshal(body, &user)
	return user, nil
}

func (g *GitHub) PostCommitStatus(token, owner, repo, sha, state, desc, targetURL string) error {
	payload := map[string]string{"state": state, "description": desc, "target_url": targetURL, "context": "railpush"}
	body, _ := json.Marshal(payload)
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/statuses/%s", owner, repo, sha)
	req, _ := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

type GitHubRepo struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	Name          string `json:"name"`
	Private       bool   `json:"private"`
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
	UpdatedAt     string `json:"updated_at"`
}

type GitHubBranch struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
}

type GitHubWorkflow struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	State string `json:"state"`
}

type GitHubWebhook struct {
	ID     int64    `json:"id"`
	Active bool     `json:"active"`
	Events []string `json:"events"`
	Config struct {
		URL string `json:"url"`
	} `json:"config"`
}

type GitHubAPIError struct {
	StatusCode int
	Body       string
}

func (e *GitHubAPIError) Error() string {
	return fmt.Sprintf("GitHub API error %d: %s", e.StatusCode, e.Body)
}

func readGitHubAPIError(resp *http.Response) *GitHubAPIError {
	body, _ := io.ReadAll(resp.Body)
	return &GitHubAPIError{StatusCode: resp.StatusCode, Body: string(body)}
}

func (g *GitHub) ListRepos(token string) ([]GitHubRepo, error) {
	var allRepos []GitHubRepo
	page := 1
	for {
		apiURL := fmt.Sprintf("https://api.github.com/user/repos?per_page=100&sort=updated&type=all&page=%d", page)
		req, _ := http.NewRequest("GET", apiURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, string(body))
		}
		var repos []GitHubRepo
		if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
			return nil, err
		}
		allRepos = append(allRepos, repos...)
		if len(repos) < 100 {
			break
		}
		page++
	}
	return allRepos, nil
}

func (g *GitHub) ListBranches(token, owner, repo string) ([]GitHubBranch, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/branches?per_page=100", owner, repo)
	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, string(body))
	}
	var branches []GitHubBranch
	if err := json.NewDecoder(resp.Body).Decode(&branches); err != nil {
		return nil, err
	}
	return branches, nil
}

func (g *GitHub) ListWorkflows(token, owner, repo string) ([]GitHubWorkflow, error) {
	var allWorkflows []GitHubWorkflow
	page := 1
	for {
		apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/workflows?per_page=100&page=%d", owner, repo, page)
		req, _ := http.NewRequest("GET", apiURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, string(body))
		}

		var result struct {
			Workflows []GitHubWorkflow `json:"workflows"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}

		allWorkflows = append(allWorkflows, result.Workflows...)
		if len(result.Workflows) < 100 {
			break
		}
		page++
	}
	return allWorkflows, nil
}

func (g *GitHub) ListWebhooks(token, owner, repo string) ([]GitHubWebhook, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/hooks?per_page=100", owner, repo)
	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readGitHubAPIError(resp)
	}

	var hooks []GitHubWebhook
	if err := json.NewDecoder(resp.Body).Decode(&hooks); err != nil {
		return nil, err
	}

	return hooks, nil
}

func (g *GitHub) CreateWebhook(token, owner, repo, webhookURL, secret string) error {
	requiredEvents := []string{"push", "workflow_run"}
	payload := map[string]interface{}{
		"config": map[string]string{
			"url":          webhookURL,
			"content_type": "json",
			"secret":       secret,
		},
		"events": requiredEvents,
		"active": true,
	}
	body, _ := json.Marshal(payload)
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/hooks", owner, repo)
	req, _ := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		return nil
	}

	if resp.StatusCode == http.StatusUnprocessableEntity {
		if err := g.ensureWebhookHasEvents(token, owner, repo, webhookURL, requiredEvents); err != nil {
			return fmt.Errorf("webhook exists but could not update required events: %w", err)
		}
		return nil
	}

	return readGitHubAPIError(resp)
}

func normalizeWebhookURL(url string) string {
	return strings.TrimRight(strings.TrimSpace(url), "/")
}

func mergeWebhookEvents(existing, required []string) []string {
	merged := make([]string, 0, len(existing)+len(required))
	seen := map[string]bool{}
	for _, event := range existing {
		e := strings.TrimSpace(event)
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		merged = append(merged, e)
	}
	for _, event := range required {
		e := strings.TrimSpace(event)
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		merged = append(merged, e)
	}
	return merged
}

func containsWebhookEvents(events, required []string) bool {
	if len(required) == 0 {
		return true
	}
	have := map[string]bool{}
	for _, event := range events {
		e := strings.TrimSpace(event)
		if e == "" {
			continue
		}
		have[e] = true
	}
	for _, event := range required {
		e := strings.TrimSpace(event)
		if e == "" {
			continue
		}
		if !have[e] {
			return false
		}
	}
	return true
}

func (g *GitHub) ensureWebhookHasEvents(token, owner, repo, webhookURL string, requiredEvents []string) error {
	hooks, err := g.ListWebhooks(token, owner, repo)
	if err != nil {
		return err
	}

	targetURL := normalizeWebhookURL(webhookURL)

	for _, hook := range hooks {
		if normalizeWebhookURL(hook.Config.URL) != targetURL {
			continue
		}
		if containsWebhookEvents(hook.Events, requiredEvents) {
			return nil
		}

		mergedEvents := mergeWebhookEvents(hook.Events, requiredEvents)
		payload := map[string]interface{}{
			"events": mergedEvents,
			"active": true,
		}
		body, _ := json.Marshal(payload)
		patchURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/hooks/%d", owner, repo, hook.ID)
		patchReq, _ := http.NewRequest("PATCH", patchURL, bytes.NewReader(body))
		patchReq.Header.Set("Authorization", "Bearer "+token)
		patchReq.Header.Set("Content-Type", "application/json")
		patchReq.Header.Set("Accept", "application/vnd.github+json")

		patchResp, err := http.DefaultClient.Do(patchReq)
		if err != nil {
			return err
		}
		defer patchResp.Body.Close()
		if patchResp.StatusCode < 200 || patchResp.StatusCode >= 300 {
			return readGitHubAPIError(patchResp)
		}
		return nil
	}

	return fmt.Errorf("existing webhook for %s not found", webhookURL)
}
