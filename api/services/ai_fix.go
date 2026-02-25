package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/railpush/api/config"
	"github.com/railpush/api/models"
)

type AIFixService struct {
	Config     *config.Config
	HTTPClient *http.Client
}

type AIFixDiagnosis struct {
	Summary       string `json:"summary"`
	ProbableCause string `json:"probable_cause"`
	SuggestedFix  string `json:"suggested_fix"`
	Confidence    string `json:"confidence"`
	Source        string `json:"source"`
}

func NewAIFixService(cfg *config.Config) *AIFixService {
	return &AIFixService{
		Config:     cfg,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
	}
}

func (s *AIFixService) Available() bool {
	if s == nil || s.Config == nil {
		return false
	}
	return s.Config.BlueprintAI.Enabled &&
		strings.TrimSpace(s.Config.BlueprintAI.OpenRouterAPIKey) != "" &&
		strings.TrimSpace(s.Config.BlueprintAI.OpenRouterModel) != "" &&
		strings.TrimSpace(s.Config.BlueprintAI.OpenRouterURL) != ""
}

func (s *AIFixService) DiagnoseDeployFailure(deploy *models.Deploy) (*AIFixDiagnosis, error) {
	if deploy == nil {
		return nil, fmt.Errorf("missing deploy")
	}

	buildLogs := strings.TrimSpace(deploy.BuildLog)
	if len(buildLogs) < 50 && s != nil && s.Config != nil && s.Config.Kubernetes.Enabled {
		if lokiLogs := strings.TrimSpace(s.fetchLokiLogs(deploy)); lokiLogs != "" {
			buildLogs = lokiLogs
		}
	}
	buildLogs = strings.TrimSpace(lastNLines(buildLogs, 240))

	if s.Available() && buildLogs != "" {
		diag, err := s.callOpenRouterDiagnosis(buildLogs)
		if err == nil && diag != nil {
			diag.Source = "ai"
			return sanitizeAIFixDiagnosis(diag), nil
		}
		if err != nil {
			log.Printf("ai_fix: diagnosis openrouter fallback: %v", err)
		}
	}

	diag := heuristicAIFixDiagnosis(buildLogs)
	diag.Source = "heuristic"
	return sanitizeAIFixDiagnosis(diag), nil
}

// AttemptFix gets the last failed deploy's build logs, sends them to OpenRouter
// to get a fixed Dockerfile, then creates a new deploy with the override.
func (s *AIFixService) AttemptFix(session *models.AIFixSession, worker *Worker) error {
	if session == nil {
		return fmt.Errorf("nil session")
	}

	svc, err := models.GetService(session.ServiceID)
	if err != nil || svc == nil {
		return fmt.Errorf("service not found: %v", err)
	}

	// Get last failed deploy
	failedDeploy, err := models.GetLastFailedDeploy(svc.ID)
	if err != nil || failedDeploy == nil {
		return fmt.Errorf("no failed deploy found")
	}

	buildLogs := failedDeploy.BuildLog

	// If build logs are sparse, try Loki
	if len(strings.TrimSpace(buildLogs)) < 50 && s.Config.Kubernetes.Enabled {
		lokiLogs := s.fetchLokiLogs(failedDeploy)
		if lokiLogs != "" {
			buildLogs = lokiLogs
		}
	}

	// Truncate to last 200 lines
	buildLogs = lastNLines(buildLogs, 200)

	// Determine the current Dockerfile content
	currentDockerfile := failedDeploy.DockerfileOverride
	if currentDockerfile == "" {
		currentDockerfile = "(auto-generated or from repository)"
	}

	// Call OpenRouter
	fixedDockerfile, summary, err := s.callOpenRouter(buildLogs, currentDockerfile)
	if err != nil {
		return fmt.Errorf("openrouter call failed: %w", err)
	}

	// Create new deploy with the fixed Dockerfile
	deploy := &models.Deploy{
		ServiceID:          svc.ID,
		Trigger:            "ai_fix",
		CommitSHA:          failedDeploy.CommitSHA,
		CommitMessage:      fmt.Sprintf("AI fix attempt %d/%d", session.CurrentAttempt+1, session.MaxAttempts),
		Branch:             failedDeploy.Branch,
		DockerfileOverride: fixedDockerfile,
	}
	if err := models.CreateDeploy(deploy); err != nil {
		return fmt.Errorf("create deploy: %w", err)
	}

	// Update session
	if err := models.UpdateAIFixSessionAttempt(session.ID, session.CurrentAttempt+1, deploy.ID, summary); err != nil {
		log.Printf("ai_fix: update session attempt failed: %v", err)
	}

	// Enqueue the deploy
	if worker != nil {
		ghToken := worker.resolveGitHubToken(deploy, svc)
		worker.Enqueue(DeployJob{
			Deploy:      deploy,
			Service:     svc,
			GitHubToken: ghToken,
		})
	}

	return nil
}

func (s *AIFixService) callOpenRouter(buildLogs string, currentDockerfile string) (fixedDockerfile string, summary string, err error) {
	systemPrompt := `You are a DevOps expert. A Docker build failed on RailPush (a Render-like PaaS). Analyze the build logs and fix the Dockerfile.

Return ONLY a valid Dockerfile. No markdown fences, no explanation, no comments about what you changed.

CRITICAL RULES:
1. Fix ONLY the specific error shown in the logs. Minimal changes.
2. NEVER replace "npm install" with "npm ci" unless you can confirm package-lock.json exists. npm ci REQUIRES package-lock.json and will fail without it.
3. For Node.js: always use "npm install" as the safe default. Only use "npm ci" if the logs show package-lock.json was found.
4. If the error is "ENOENT: no such file or directory, open 'package.json'" — the Dockerfile's WORKDIR or COPY context is wrong, NOT a missing package. Adjust COPY source paths or WORKDIR.
5. For monorepos (frontend+backend in one repo), COPY only the relevant subdirectory. Check the docker context path in the logs.
6. Do NOT add unnecessary build dependencies (python3, make, g++) unless the logs specifically show native module compilation failures.
7. Do NOT set environment variables with empty values (ENV RESEND_API_KEY=). Runtime env vars are injected by the platform.
8. If the error is a runtime crash (not a build failure), the Dockerfile is probably fine — the issue is missing env vars or config. In this case, return the EXACT same Dockerfile unchanged.
9. Use standard base images: node:20-alpine, python:3.12-slim, golang:1.22-alpine, ruby:3.3-slim, etc.
10. For static sites (React, Vue, Next.js static export), use a multi-stage build: node for building, nginx:alpine or a lightweight server for serving.`

	userPrompt := fmt.Sprintf("Build logs (last 200 lines):\n```\n%s\n```\n\nCurrent Dockerfile:\n```\n%s\n```", buildLogs, currentDockerfile)

	reqBody := openRouterChatRequest{
		Model: s.Config.BlueprintAI.OpenRouterModel,
		Messages: []openRouterMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.2,
		MaxTokens:   4000,
	}

	raw, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", fmt.Errorf("marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.HTTPClient.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Config.BlueprintAI.OpenRouterURL, bytes.NewReader(raw))
	if err != nil {
		return "", "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.Config.BlueprintAI.OpenRouterAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://railpush.com")
	req.Header.Set("X-Title", "RailPush AI Fix")

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("openrouter error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed openRouterChatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", "", fmt.Errorf("decode response: %w", err)
	}
	if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return "", "", fmt.Errorf("openrouter error: %s", strings.TrimSpace(parsed.Error.Message))
	}
	if len(parsed.Choices) == 0 {
		return "", "", fmt.Errorf("openrouter returned no choices")
	}

	content := extractOpenRouterContent(parsed.Choices[0].Message.Content)
	content = normalizeDockerfileContent(content)
	if content == "" {
		return "", "", fmt.Errorf("openrouter returned empty content")
	}
	if !looksLikeDockerfile(content) {
		return "", "", fmt.Errorf("openrouter returned content without a Dockerfile")
	}

	// Build a short summary from the first line of the Dockerfile change
	summaryLine := "AI-generated Dockerfile fix"
	lines := strings.SplitN(content, "\n", 3)
	if len(lines) > 0 {
		summaryLine = fmt.Sprintf("Fixed Dockerfile (FROM %s...)", strings.TrimPrefix(lines[0], "FROM "))
		if len(summaryLine) > 120 {
			summaryLine = summaryLine[:120]
		}
	}

	return content, summaryLine, nil
}

func (s *AIFixService) callOpenRouterDiagnosis(buildLogs string) (*AIFixDiagnosis, error) {
	systemPrompt := `You are a senior SRE helping a developer understand a failed deployment.
Analyze the build/runtime logs and explain what most likely failed.

Return ONLY valid JSON with this exact shape:
{
  "summary": "one concise sentence",
  "probable_cause": "plain-English root cause",
  "suggested_fix": "specific actionable fix",
  "confidence": "high|medium|low"
}

Rules:
- Keep summary under 140 characters.
- suggested_fix must be practical and immediately actionable.
- If logs are ambiguous, say that and lower confidence.
- No markdown, no code fences, no extra keys.`

	userPrompt := fmt.Sprintf("Deploy logs (last 240 lines):\n```\n%s\n```", buildLogs)

	reqBody := openRouterChatRequest{
		Model: s.Config.BlueprintAI.OpenRouterModel,
		Messages: []openRouterMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.1,
		MaxTokens:   700,
	}

	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.HTTPClient.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Config.BlueprintAI.OpenRouterURL, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.Config.BlueprintAI.OpenRouterAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://railpush.com")
	req.Header.Set("X-Title", "RailPush AI Diagnostics")

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openrouter error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed openRouterChatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return nil, fmt.Errorf("openrouter error: %s", strings.TrimSpace(parsed.Error.Message))
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("openrouter returned no choices")
	}

	content := extractOpenRouterContent(parsed.Choices[0].Message.Content)
	diag, err := parseAIFixDiagnosisJSON(content)
	if err != nil {
		return nil, err
	}
	return sanitizeAIFixDiagnosis(diag), nil
}

func (s *AIFixService) fetchLokiLogs(deploy *models.Deploy) string {
	if s == nil || s.Config == nil || !s.Config.Kubernetes.Enabled {
		return ""
	}
	ns := strings.TrimSpace(s.Config.Kubernetes.Namespace)
	if ns == "" {
		ns = "railpush"
	}
	lokiURL := strings.TrimSpace(s.Config.Logging.LokiURL)
	if lokiURL == "" {
		lokiURL = "http://loki-gateway.logging.svc.cluster.local"
	}

	jobName := KubeBuildJobName(deploy.ID)
	if jobName == "" {
		return ""
	}

	start := time.Now().UTC().Add(-30 * time.Minute)
	if deploy.StartedAt != nil {
		start = deploy.StartedAt.Add(-2 * time.Minute)
	}
	end := time.Now().UTC()
	if deploy.FinishedAt != nil {
		end = deploy.FinishedAt.Add(5 * time.Minute)
	}

	logQL := fmt.Sprintf(`{namespace=%q, app=%q, component="build", container=~"clone|kaniko"}`, ns, jobName)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	lines, err := LokiQueryRange(ctx, lokiURL, logQL, start, end, 5000)
	if err != nil || len(lines) == 0 {
		return ""
	}

	var b strings.Builder
	for _, ln := range lines {
		b.WriteString(ln.Line)
		b.WriteString("\n")
	}
	return b.String()
}

func stripMarkdownFences(in string) string {
	s := strings.TrimSpace(in)
	if s == "" {
		return ""
	}
	start := strings.Index(s, "```")
	if start < 0 {
		return s
	}
	rest := s[start+3:]
	if idx := strings.Index(rest, "\n"); idx >= 0 {
		rest = rest[idx+1:]
	}
	end := strings.Index(rest, "```")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

func normalizeDockerfileContent(in string) string {
	s := stripMarkdownFences(in)
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	start := -1
	for i, ln := range lines {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(ln)), "FROM ") {
			start = i
			break
		}
	}
	if start > 0 {
		lines = lines[start:]
	}
	out := strings.TrimSpace(strings.Join(lines, "\n"))
	return out
}

func sanitizeAIFixDiagnosis(diag *AIFixDiagnosis) *AIFixDiagnosis {
	if diag == nil {
		diag = &AIFixDiagnosis{}
	}
	diag.Summary = strings.TrimSpace(diag.Summary)
	diag.ProbableCause = strings.TrimSpace(diag.ProbableCause)
	diag.SuggestedFix = strings.TrimSpace(diag.SuggestedFix)
	diag.Confidence = normalizeAIFixConfidence(diag.Confidence)

	if diag.Summary == "" {
		diag.Summary = "Deploy failed due to a build or runtime configuration issue."
	}
	if diag.ProbableCause == "" {
		diag.ProbableCause = "The logs do not provide a single definitive root cause."
	}
	if diag.SuggestedFix == "" {
		diag.SuggestedFix = "Review the failing step in the build logs, fix that change locally, and redeploy."
	}
	if strings.TrimSpace(diag.Source) == "" {
		diag.Source = "heuristic"
	}
	return diag
}

func normalizeAIFixConfidence(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "high":
		return "high"
	case "low":
		return "low"
	default:
		return "medium"
	}
}

func parseAIFixDiagnosisJSON(raw string) (*AIFixDiagnosis, error) {
	type payload struct {
		Summary       string `json:"summary"`
		ProbableCause string `json:"probable_cause"`
		SuggestedFix  string `json:"suggested_fix"`
		Confidence    string `json:"confidence"`
	}

	cleaned := strings.TrimSpace(stripMarkdownFences(raw))
	if cleaned == "" {
		return nil, fmt.Errorf("empty diagnosis response")
	}

	decode := func(candidate string) (*AIFixDiagnosis, error) {
		var p payload
		if err := json.Unmarshal([]byte(candidate), &p); err != nil {
			return nil, err
		}
		return &AIFixDiagnosis{
			Summary:       p.Summary,
			ProbableCause: p.ProbableCause,
			SuggestedFix:  p.SuggestedFix,
			Confidence:    p.Confidence,
		}, nil
	}

	if diag, err := decode(cleaned); err == nil {
		return diag, nil
	}

	start := strings.Index(cleaned, "{")
	end := strings.LastIndex(cleaned, "}")
	if start >= 0 && end > start {
		if diag, err := decode(cleaned[start : end+1]); err == nil {
			return diag, nil
		}
	}

	return nil, fmt.Errorf("diagnosis response is not valid JSON")
}

func heuristicAIFixDiagnosis(buildLogs string) *AIFixDiagnosis {
	logs := strings.TrimSpace(buildLogs)
	if logs == "" {
		return &AIFixDiagnosis{
			Summary:       "No build logs were available for diagnosis.",
			ProbableCause: "RailPush could not read enough logs from the failed deploy.",
			SuggestedFix:  "Open deploy logs, verify the failing step, and rerun with verbose logging if needed.",
			Confidence:    "low",
		}
	}

	lower := strings.ToLower(logs)
	if strings.Contains(lower, "enoent") && strings.Contains(lower, "package.json") {
		return &AIFixDiagnosis{
			Summary:       "Build context is pointing at the wrong directory.",
			ProbableCause: "The build step cannot find package.json, which usually means docker context/rootDir is incorrect.",
			SuggestedFix:  "Set dockerContext/rootDir to the folder containing package.json (or update COPY paths), then redeploy.",
			Confidence:    "high",
		}
	}
	if strings.Contains(lower, "npm ci can only install") && strings.Contains(lower, "package-lock.json") {
		return &AIFixDiagnosis{
			Summary:       "npm ci failed because package-lock.json is missing.",
			ProbableCause: "The Dockerfile or build command uses npm ci without a lock file in the repo.",
			SuggestedFix:  "Commit package-lock.json or switch the build step to npm install.",
			Confidence:    "high",
		}
	}
	if strings.Contains(lower, "modulenotfounderror") || strings.Contains(lower, "no module named") {
		return &AIFixDiagnosis{
			Summary:       "Python dependency is missing during startup or build.",
			ProbableCause: "A required module is not installed from requirements.txt.",
			SuggestedFix:  "Add the missing package to requirements.txt and ensure pip install runs in the Docker build.",
			Confidence:    "high",
		}
	}
	if strings.Contains(lower, "eaddrinuse") || strings.Contains(lower, "address already in use") {
		return &AIFixDiagnosis{
			Summary:       "The app failed due to a port conflict.",
			ProbableCause: "The process is binding to a hardcoded port instead of the platform-provided PORT.",
			SuggestedFix:  "Use the PORT environment variable in your app and avoid hardcoded listen ports.",
			Confidence:    "medium",
		}
	}
	if strings.Contains(lower, "out of memory") || strings.Contains(lower, "javascript heap") || strings.Contains(lower, "enomem") {
		return &AIFixDiagnosis{
			Summary:       "The build likely ran out of memory.",
			ProbableCause: "Dependency install or compile step exceeded available memory.",
			SuggestedFix:  "Reduce build memory usage (or set NODE_OPTIONS), trim dependencies, or use a larger plan.",
			Confidence:    "medium",
		}
	}
	if strings.Contains(lower, "could not find or read dockerfile") || (strings.Contains(lower, "no such file") && strings.Contains(lower, "dockerfile")) {
		return &AIFixDiagnosis{
			Summary:       "Dockerfile path is invalid for this repository.",
			ProbableCause: "The configured dockerfile path does not exist in the selected branch/context.",
			SuggestedFix:  "Fix dockerfilePath (or place Dockerfile at repo root) and redeploy.",
			Confidence:    "high",
		}
	}

	return &AIFixDiagnosis{
		Summary:       "Deploy failed, but no single root cause was confidently detected.",
		ProbableCause: "The logs contain multiple failures or incomplete context.",
		SuggestedFix:  "Inspect the first failing command in build logs, fix it locally, and redeploy. Use AI Fix to attempt an automated patch.",
		Confidence:    "low",
	}
}

func looksLikeDockerfile(in string) bool {
	for _, ln := range strings.Split(in, "\n") {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(ln)), "FROM ") {
			return true
		}
	}
	return false
}

func lastNLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
