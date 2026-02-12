package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

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

func (g *GitHub) CreateWebhook(token, owner, repo, webhookURL, secret string) error {
	payload := map[string]interface{}{
		"config": map[string]string{
			"url":          webhookURL,
			"content_type": "json",
			"secret":       secret,
		},
		"events": []string{"push"},
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
	resp.Body.Close()
	// 422 = webhook already exists, treat as success
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != 422 {
		return fmt.Errorf("failed to create webhook: HTTP %d", resp.StatusCode)
	}
	return nil
}
