package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/railpush/api/config"
	"github.com/railpush/api/models"
	"github.com/railpush/api/utils"
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

type AIFixOptions struct {
	Hint string
}

type AIFixEnvVarUpdate struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	IsSecret bool   `json:"is_secret"`
}

type AIFixPatch struct {
	Summary       string            `json:"summary"`
	Dockerfile    string            `json:"dockerfile,omitempty"`
	BuildCommand  string            `json:"build_command,omitempty"`
	StartCommand  string            `json:"start_command,omitempty"`
	EnvVars       []AIFixEnvVarUpdate `json:"env_vars,omitempty"`
	DockerfileDiff string           `json:"dockerfile_diff,omitempty"`
	Source        string            `json:"source"`
}

type aiFixLogContext struct {
	BuildLogs   string
	RuntimeLogs string
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

func (s *AIFixService) collectDeployFailureLogs(svc *models.Service, deploy *models.Deploy) aiFixLogContext {
	ctx := aiFixLogContext{}
	if deploy == nil {
		return ctx
	}

	buildLogs := strings.TrimSpace(deploy.BuildLog)
	if len(buildLogs) < 50 && s != nil && s.Config != nil && s.Config.Kubernetes.Enabled {
		if lokiLogs := strings.TrimSpace(s.fetchLokiBuildLogs(deploy)); lokiLogs != "" {
			buildLogs = lokiLogs
		}
	}
	ctx.BuildLogs = strings.TrimSpace(lastNLines(buildLogs, 220))

	if s != nil && s.Config != nil && s.Config.Kubernetes.Enabled && svc != nil {
		runtimeLogs := strings.TrimSpace(s.fetchLokiRuntimeLogs(svc.ID, deploy))
		ctx.RuntimeLogs = strings.TrimSpace(lastNLines(runtimeLogs, 220))
	}

	return ctx
}

func formatAIFixLogsForPrompt(logs aiFixLogContext) string {
	parts := make([]string, 0, 2)
	if strings.TrimSpace(logs.BuildLogs) != "" {
		parts = append(parts, "BUILD LOGS:\n"+strings.TrimSpace(logs.BuildLogs))
	}
	if strings.TrimSpace(logs.RuntimeLogs) != "" {
		parts = append(parts, "RUNTIME LOGS:\n"+strings.TrimSpace(logs.RuntimeLogs))
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func (s *AIFixService) DiagnoseDeployFailure(deploy *models.Deploy) (*AIFixDiagnosis, error) {
	if deploy == nil {
		return nil, fmt.Errorf("missing deploy")
	}

	var svc *models.Service
	if strings.TrimSpace(deploy.ServiceID) != "" {
		svc, _ = models.GetService(deploy.ServiceID)
	}
	logs := s.collectDeployFailureLogs(svc, deploy)
	logContext := strings.TrimSpace(formatAIFixLogsForPrompt(logs))

	if s.Available() && logContext != "" {
		diag, err := s.callOpenRouterDiagnosis(logContext)
		if err == nil && diag != nil {
			diag.Source = "ai"
			return sanitizeAIFixDiagnosis(diag), nil
		}
		if err != nil {
			log.Printf("ai_fix: diagnosis openrouter fallback: %v", err)
		}
	}

	diag := heuristicAIFixDiagnosis(logContext)
	diag.Source = "heuristic"
	return sanitizeAIFixDiagnosis(diag), nil
}

func (s *AIFixService) PreviewFix(svc *models.Service, failedDeploy *models.Deploy, hint string) (*AIFixPatch, error) {
	if svc == nil {
		return nil, fmt.Errorf("missing service")
	}
	if failedDeploy == nil {
		return nil, fmt.Errorf("missing failed deploy")
	}
	if !s.Available() {
		return nil, fmt.Errorf("AI not available")
	}

	logs := s.collectDeployFailureLogs(svc, failedDeploy)
	logContext := strings.TrimSpace(formatAIFixLogsForPrompt(logs))
	if logContext == "" {
		return nil, fmt.Errorf("failed deploy logs are empty")
	}

	currentDockerfile := strings.TrimSpace(failedDeploy.DockerfileOverride)
	if currentDockerfile == "" {
		currentDockerfile = "(auto-generated or from repository)"
	}

	return s.callOpenRouterPatch(logContext, currentDockerfile, svc, hint)
}

// AttemptFix creates a fix plan from failed deploy context and applies it.
func (s *AIFixService) AttemptFix(session *models.AIFixSession, worker *Worker) error {
	return s.AttemptFixWithOptions(session, worker, AIFixOptions{})
}

func (s *AIFixService) AttemptFixWithOptions(session *models.AIFixSession, worker *Worker, opts AIFixOptions) error {
	if session == nil {
		return fmt.Errorf("nil session")
	}

	svc, err := models.GetService(session.ServiceID)
	if err != nil || svc == nil {
		return fmt.Errorf("service not found: %v", err)
	}

	failedDeploy, err := models.GetLastFailedDeploy(svc.ID)
	if err != nil || failedDeploy == nil {
		return fmt.Errorf("no failed deploy found")
	}

	patch, err := s.PreviewFix(svc, failedDeploy, opts.Hint)
	if err != nil {
		return fmt.Errorf("generate fix plan: %w", err)
	}
	if patch == nil {
		return fmt.Errorf("generate fix plan: empty response")
	}

	updatedSvc := *svc
	serviceChanged := false
	applied := make([]string, 0, 4)

	if v := strings.TrimSpace(patch.BuildCommand); v != "" && v != strings.TrimSpace(updatedSvc.BuildCommand) {
		updatedSvc.BuildCommand = v
		serviceChanged = true
		applied = append(applied, "build_command")
	}
	if v := strings.TrimSpace(patch.StartCommand); v != "" && v != strings.TrimSpace(updatedSvc.StartCommand) {
		updatedSvc.StartCommand = v
		serviceChanged = true
		applied = append(applied, "start_command")
	}

	if serviceChanged {
		if err := models.UpdateService(&updatedSvc); err != nil {
			return fmt.Errorf("update service from AI fix: %w", err)
		}
		svc = &updatedSvc
	}

	envVars := normalizeAIFixEnvVarUpdates(patch.EnvVars)
	if len(envVars) > 0 {
		upsert := make([]models.EnvVar, 0, len(envVars))
		for _, ev := range envVars {
			encrypted, encErr := utils.Encrypt(ev.Value, s.Config.Crypto.EncryptionKey)
			if encErr != nil {
				return fmt.Errorf("encrypt env var %s: %w", ev.Key, encErr)
			}
			upsert = append(upsert, models.EnvVar{
				Key:            ev.Key,
				EncryptedValue: encrypted,
				IsSecret:       ev.IsSecret,
			})
		}
		if err := models.MergeUpsertEnvVars("service", svc.ID, upsert); err != nil {
			return fmt.Errorf("update env vars from AI fix: %w", err)
		}
		applied = append(applied, fmt.Sprintf("env_vars(%d)", len(upsert)))
	}

	dockerfileOverride := strings.TrimSpace(patch.Dockerfile)
	if dockerfileOverride == "" {
		dockerfileOverride = strings.TrimSpace(failedDeploy.DockerfileOverride)
	}

	hasActionableChange := serviceChanged || len(envVars) > 0 || dockerfileOverride != strings.TrimSpace(failedDeploy.DockerfileOverride)
	if !hasActionableChange {
		applied = append(applied, "redeploy_only")
	}

	summary := strings.TrimSpace(patch.Summary)
	if summary == "" {
		summary = "AI-generated deploy fix"
	}
	if strings.TrimSpace(patch.DockerfileDiff) != "" {
		summary += "\n\nDockerfile diff preview:\n" + strings.TrimSpace(patch.DockerfileDiff)
	}
	if !hasActionableChange {
		summary += "\n\nNo deterministic config patch was detected from AI output; triggering a verification redeploy with existing settings."
	}
	if len(applied) > 0 {
		summary += "\n\nApplied changes: " + strings.Join(applied, ", ")
	}
	if h := strings.TrimSpace(opts.Hint); h != "" {
		summary += "\n\nUser hint: " + truncateAIFixText(h, 280)
	}
	summary = truncateAIFixText(summary, 2000)

	deploy := &models.Deploy{
		ServiceID:          svc.ID,
		Trigger:            "ai_fix",
		CommitSHA:          failedDeploy.CommitSHA,
		CommitMessage:      fmt.Sprintf("AI fix attempt %d/%d", session.CurrentAttempt+1, session.MaxAttempts),
		Branch:             failedDeploy.Branch,
		DockerfileOverride: dockerfileOverride,
	}
	if err := models.CreateDeploy(deploy); err != nil {
		return fmt.Errorf("create deploy: %w", err)
	}

	if err := models.UpdateAIFixSessionAttempt(session.ID, session.CurrentAttempt+1, deploy.ID, summary); err != nil {
		log.Printf("ai_fix: update session attempt failed: %v", err)
	}

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

func (s *AIFixService) callOpenRouterPatch(logContext string, currentDockerfile string, svc *models.Service, hint string) (*AIFixPatch, error) {
	serviceRuntime := ""
	serviceType := ""
	buildCommand := ""
	startCommand := ""
	if svc != nil {
		serviceRuntime = strings.TrimSpace(svc.Runtime)
		serviceType = strings.TrimSpace(svc.Type)
		buildCommand = strings.TrimSpace(svc.BuildCommand)
		startCommand = strings.TrimSpace(svc.StartCommand)
	}

	systemPrompt := `You are a senior DevOps engineer fixing failed deployments on RailPush.
Analyze build + runtime logs and output ONLY valid JSON with this schema:
{
  "summary": "one concise sentence",
  "dockerfile": "optional full Dockerfile text, empty if unchanged",
  "build_command": "optional new build command",
  "start_command": "optional new start command",
  "env_vars": [
    {"key": "ENV_KEY", "value": "value", "is_secret": false}
  ]
}

Rules:
- Return JSON only (no markdown/code fences).
- Keep changes minimal and focused on the first real failure.
- If Dockerfile is unchanged, return empty dockerfile.
- Only suggest env vars when values are deterministic from logs/context.
- Never invent sensitive secret values (API keys/tokens/passwords).
- Avoid broad speculative rewrites.
- Prefer safe Node default: npm install over npm ci unless lockfile is clearly present.
- If logs indicate runtime failure (CrashLoop/startup), prioritize start_command/env fixes over Dockerfile.`

	userPrompt := fmt.Sprintf("Service type: %s\nRuntime: %s\nCurrent build_command: %s\nCurrent start_command: %s\n\nLogs:\n```\n%s\n```\n\nCurrent Dockerfile:\n```\n%s\n```\n\nUser hint (optional): %s",
		serviceType,
		serviceRuntime,
		buildCommand,
		startCommand,
		logContext,
		currentDockerfile,
		strings.TrimSpace(hint),
	)

	reqBody := openRouterChatRequest{
		Model: s.Config.BlueprintAI.OpenRouterModel,
		Messages: []openRouterMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.1,
		MaxTokens:   1800,
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
	req.Header.Set("X-Title", "RailPush AI Fix")

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
	patch, err := parseAIFixPatchJSON(content)
	if err != nil {
		patch = fallbackAIFixPatchFromText(content, currentDockerfile)
		if patch == nil {
			return nil, err
		}
	}
	patch = normalizeAIFixPatch(patch)
	patch.Source = "ai"

	if patch.Dockerfile != "" && patch.Dockerfile != currentDockerfile {
		patch.DockerfileDiff = buildTextDiffPreview(currentDockerfile, patch.Dockerfile, 40)
	}

	if strings.TrimSpace(patch.Summary) == "" {
		patch.Summary = "AI-generated deploy fix plan"
	}

	return patch, nil
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

func (s *AIFixService) fetchLokiBuildLogs(deploy *models.Deploy) string {
	if s == nil || s.Config == nil || !s.Config.Kubernetes.Enabled {
		return ""
	}
	if deploy == nil {
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

func (s *AIFixService) fetchLokiRuntimeLogs(serviceID string, deploy *models.Deploy) string {
	if s == nil || s.Config == nil || !s.Config.Kubernetes.Enabled {
		return ""
	}
	if strings.TrimSpace(serviceID) == "" {
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

	start := time.Now().UTC().Add(-30 * time.Minute)
	end := time.Now().UTC()
	if deploy != nil {
		if deploy.StartedAt != nil {
			start = deploy.StartedAt.Add(-2 * time.Minute)
		}
		if deploy.FinishedAt != nil {
			end = deploy.FinishedAt.Add(8 * time.Minute)
		}
	}

	servicePodPrefix := regexp.QuoteMeta(kubeServiceName(serviceID)) + ".*"
	logQL := fmt.Sprintf(`{namespace=%q,pod=~%q,container="service"}`, ns, servicePodPrefix)
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

func parseAIFixPatchJSON(raw string) (*AIFixPatch, error) {
	type payload struct {
		Summary      string             `json:"summary"`
		Dockerfile   string             `json:"dockerfile"`
		BuildCommand string             `json:"build_command"`
		StartCommand string             `json:"start_command"`
		EnvVars      []AIFixEnvVarUpdate `json:"env_vars"`
	}

	cleaned := strings.TrimSpace(stripMarkdownFences(raw))
	if cleaned == "" {
		return nil, fmt.Errorf("empty AI patch response")
	}

	decode := func(candidate string) (*AIFixPatch, error) {
		var p payload
		if err := json.Unmarshal([]byte(candidate), &p); err != nil {
			return nil, err
		}
		return &AIFixPatch{
			Summary:      p.Summary,
			Dockerfile:   p.Dockerfile,
			BuildCommand: p.BuildCommand,
			StartCommand: p.StartCommand,
			EnvVars:      p.EnvVars,
		}, nil
	}

	if patch, err := decode(cleaned); err == nil {
		return patch, nil
	}

	start := strings.Index(cleaned, "{")
	end := strings.LastIndex(cleaned, "}")
	if start >= 0 && end > start {
		if patch, err := decode(cleaned[start : end+1]); err == nil {
			return patch, nil
		}
	}

	return nil, fmt.Errorf("AI patch response is not valid JSON")
}

func fallbackAIFixPatchFromText(raw string, currentDockerfile string) *AIFixPatch {
	cleaned := strings.TrimSpace(stripMarkdownFences(raw))
	if cleaned == "" {
		return nil
	}

	patch := &AIFixPatch{
		Summary: firstNonEmptyLine(cleaned),
	}

	dockerfile := normalizeDockerfileContent(cleaned)
	if looksLikeDockerfile(dockerfile) && dockerfile != strings.TrimSpace(currentDockerfile) {
		patch.Dockerfile = dockerfile
	}

	patch.BuildCommand = extractLooseAIFixField(cleaned, "build_command")
	patch.StartCommand = extractLooseAIFixField(cleaned, "start_command")

	return patch
}

func extractLooseAIFixField(raw string, field string) string {
	field = strings.TrimSpace(field)
	if field == "" {
		return ""
	}
	re := regexp.MustCompile(`(?im)^\s*` + regexp.QuoteMeta(field) + `\s*[:=]\s*(.+)\s*$`)
	matches := re.FindStringSubmatch(raw)
	if len(matches) < 2 {
		return ""
	}
	value := strings.TrimSpace(matches[1])
	value = strings.Trim(value, "\"'")
	return value
}

func firstNonEmptyLine(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.Trim(line, "\"'")
		return truncateAIFixText(line, 220)
	}
	return ""
}

func normalizeAIFixPatch(patch *AIFixPatch) *AIFixPatch {
	if patch == nil {
		patch = &AIFixPatch{}
	}
	patch.Summary = strings.TrimSpace(patch.Summary)
	patch.BuildCommand = strings.TrimSpace(patch.BuildCommand)
	patch.StartCommand = strings.TrimSpace(patch.StartCommand)

	if patch.Dockerfile != "" {
		normalized := normalizeDockerfileContent(patch.Dockerfile)
		if looksLikeDockerfile(normalized) {
			patch.Dockerfile = normalized
		} else {
			patch.Dockerfile = ""
		}
	}

	patch.EnvVars = normalizeAIFixEnvVarUpdates(patch.EnvVars)
	if strings.TrimSpace(patch.Source) == "" {
		patch.Source = "heuristic"
	}
	return patch
}

func normalizeAIFixEnvVarUpdates(updates []AIFixEnvVarUpdate) []AIFixEnvVarUpdate {
	if len(updates) == 0 {
		return nil
	}

	normalized := make([]AIFixEnvVarUpdate, 0, len(updates))
	indexByKey := map[string]int{}

	for _, item := range updates {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			continue
		}
		value := strings.TrimSpace(item.Value)
		if value == "" {
			continue
		}

		entry := AIFixEnvVarUpdate{
			Key:      key,
			Value:    value,
			IsSecret: item.IsSecret,
		}

		if idx, ok := indexByKey[key]; ok {
			normalized[idx] = entry
			continue
		}
		indexByKey[key] = len(normalized)
		normalized = append(normalized, entry)
		if len(normalized) >= 25 {
			break
		}
	}

	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func buildTextDiffPreview(before string, after string, maxLines int) string {
	before = strings.TrimSpace(before)
	after = strings.TrimSpace(after)
	if before == "" || after == "" || before == after {
		return ""
	}
	if maxLines <= 0 {
		maxLines = 40
	}

	beforeLines := strings.Split(strings.ReplaceAll(before, "\r\n", "\n"), "\n")
	afterLines := strings.Split(strings.ReplaceAll(after, "\r\n", "\n"), "\n")

	changes := make([]string, 0, maxLines+1)
	i := 0
	j := 0
	for i < len(beforeLines) || j < len(afterLines) {
		if i < len(beforeLines) && j < len(afterLines) && beforeLines[i] == afterLines[j] {
			i++
			j++
			continue
		}
		if i < len(beforeLines) {
			changes = append(changes, "- "+beforeLines[i])
			i++
		}
		if j < len(afterLines) {
			changes = append(changes, "+ "+afterLines[j])
			j++
		}
		if len(changes) >= maxLines {
			if i < len(beforeLines) || j < len(afterLines) {
				changes = append(changes, "... (truncated)")
			}
			break
		}
	}

	return strings.Join(changes, "\n")
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

func truncateAIFixText(raw string, max int) string {
	raw = strings.TrimSpace(raw)
	if max <= 0 || len(raw) <= max {
		return raw
	}
	if max < 4 {
		return raw[:max]
	}
	return raw[:max-3] + "..."
}

func lastNLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
