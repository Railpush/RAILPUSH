package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/railpush/api/config"
)

type BlueprintAIGenerator struct {
	Config     *config.Config
	HTTPClient *http.Client
}

func NewBlueprintAIGenerator(cfg *config.Config) *BlueprintAIGenerator {
	timeout := 45 * time.Second
	if cfg != nil && cfg.BlueprintAI.RequestTimeoutSeconds > 0 {
		timeout = time.Duration(cfg.BlueprintAI.RequestTimeoutSeconds) * time.Second
	}
	return &BlueprintAIGenerator{
		Config: cfg,
		HTTPClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (g *BlueprintAIGenerator) Available() bool {
	if g == nil || g.Config == nil {
		return false
	}
	return g.Config.BlueprintAI.Enabled &&
		strings.TrimSpace(g.Config.BlueprintAI.OpenRouterAPIKey) != "" &&
		strings.TrimSpace(g.Config.BlueprintAI.OpenRouterModel) != "" &&
		strings.TrimSpace(g.Config.BlueprintAI.OpenRouterURL) != ""
}

type blueprintAISnippet struct {
	Path    string
	Content string
	Score   int
	Size    int
}

type openRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openRouterChatRequest struct {
	Model       string              `json:"model"`
	Messages    []openRouterMessage `json:"messages"`
	Temperature float64             `json:"temperature,omitempty"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
}

type openRouterChatResponse struct {
	Choices []struct {
		Message struct {
			Content any `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (g *BlueprintAIGenerator) GenerateRenderYAMLFromRepo(repoDir string, repoURL string, branch string) (string, error) {
	if g == nil || !g.Available() {
		return "", fmt.Errorf("blueprint ai unavailable")
	}
	repoDir = strings.TrimSpace(repoDir)
	if repoDir == "" {
		return "", fmt.Errorf("missing repository path")
	}

	maxFiles := g.Config.BlueprintAI.MaxScanFiles
	if maxFiles <= 0 {
		maxFiles = 120
	}
	maxFileBytes := g.Config.BlueprintAI.MaxFileBytes
	if maxFileBytes <= 0 {
		maxFileBytes = 20000
	}
	maxPromptBytes := g.Config.BlueprintAI.MaxPromptBytes
	if maxPromptBytes <= 0 {
		maxPromptBytes = 180000
	}

	snippets, err := collectBlueprintAISnippets(repoDir, maxFiles, maxFileBytes)
	if err != nil {
		return "", err
	}
	if len(snippets) == 0 {
		return "", fmt.Errorf("no relevant source files found")
	}

	var fileList strings.Builder
	var snippetBlock strings.Builder
	remaining := maxPromptBytes

	for _, s := range snippets {
		fileList.WriteString("- ")
		fileList.WriteString(s.Path)
		fileList.WriteString("\n")
	}

	for _, s := range snippets {
		block := "## " + s.Path + "\n```\n" + s.Content + "\n```\n\n"
		if len(block) > remaining {
			continue
		}
		snippetBlock.WriteString(block)
		remaining -= len(block)
		if remaining <= maxPromptBytes/5 {
			break
		}
	}

	systemPrompt := `You generate RailPush render.yaml files from repository source.
Return ONLY valid YAML, no markdown fences and no explanation.

Full schema:

services:
  - name: <required string>
    type: web|worker|cron|static|pserv
    runtime: docker|node|python|go|ruby|rust|elixir|image
    repo: <repo-url>
    branch: <branch>
    buildCommand: <optional string>
    startCommand: <optional string>
    dockerfilePath: <optional, path to Dockerfile>
    dockerContext: <optional, docker build context directory>
    dockerCommand: <optional, overrides container CMD>
    rootDir: <optional, monorepo subdirectory containing the app>
    staticPublishPath: <required for type=static, e.g. ./dist or ./build>
    schedule: <required for type=cron, cron expression e.g. "*/5 * * * *">
    port: <int, default 10000, required for web/pserv/static>
    autoDeploy: true|false
    plan: free|starter|standard|pro
    numInstances: <int, default 1>
    healthCheckPath: <optional, e.g. /healthz>
    preDeployCommand: <optional, runs before deploy e.g. "npm run migrate">
    domains:
      - <custom domain string, e.g. app.example.com>
    disk:
      name: <string>
      mountPath: <string, e.g. /data>
      sizeGB: <int, default 10>
    buildFilter:
      paths:
        - <paths that trigger a build, e.g. src/**>
      ignoredPaths:
        - <paths to ignore, e.g. docs/**>
    image:
      url: <pre-built image URL, use with runtime=image>
    envVars:
      - key: <ENV_NAME>
        value: <literal value>
      - key: <ENV_NAME>
        generateValue: true
      - key: DATABASE_URL
        fromDatabase:
          name: <database name from databases section>
          property: connectionString|host|port|user|password|database
      - key: <ENV_NAME>
        fromService:
          name: <service name from services section>
          type: <service type>
          property: host|port|hostport|connectionString
          envVarKey: <optional, read an env var from the referenced service>
      - key: <ENV_NAME>
        fromGroup: <env var group name from envVarGroups section>

databases:
  - name: <required string>
    plan: free|starter|standard|pro
    postgresMajorVersion: <int, default 16>
    databaseName: <optional, defaults to name>
    user: <optional, defaults to name>

keyValues:
  - name: <required string>
    plan: free|starter|standard|pro
    maxmemoryPolicy: <optional, default allkeys-lru>

envVarGroups:
  - name: <required string>
    envVars:
      - key: <ENV_NAME>
        value: <value>

Rules:
- Infer a practical, deployable configuration from the detected source files.
- Include at least one service.
- Plan values MUST be one of: free, starter, standard, pro. If unsure use starter.
- Never invent custom plan names.
- Prefer runtime=image only when a pre-built image URL is explicitly present; otherwise infer the standard runtime from source (node, python, go, etc.).
- If the project uses a database (e.g. DATABASE_URL, pg, prisma, sqlalchemy, gorm, sequelize, typeorm, knex, diesel), add a databases entry and wire it via fromDatabase with property connectionString.
- If the project uses Redis (e.g. REDIS_URL, ioredis, redis, bull, celery broker), add a keyValues entry.
- Use generateValue: true for secrets like SESSION_SECRET, JWT_SECRET, API_KEY, SECRET_KEY_BASE.
- Use preDeployCommand for migration commands when detected (e.g. "npx prisma migrate deploy", "python manage.py migrate", "bundle exec rake db:migrate").
- For monorepos with multiple apps, set rootDir for each service.
- For static sites (React, Vue, Svelte, Next.js export), use type=static with the correct staticPublishPath.
- For cron jobs, use type=cron with a valid cron schedule expression.
- Keep values conservative and production-safe.`

	userPrompt := fmt.Sprintf(
		"Repository URL: %s\nBranch: %s\n\nRepository file list:\n%s\n\nRelevant file contents:\n%s",
		strings.TrimSpace(repoURL),
		strings.TrimSpace(branch),
		fileList.String(),
		snippetBlock.String(),
	)

	reqBody := openRouterChatRequest{
		Model: g.Config.BlueprintAI.OpenRouterModel,
		Messages: []openRouterMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.1,
		MaxTokens:   4000,
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal openrouter request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), g.HTTPClient.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.Config.BlueprintAI.OpenRouterURL, bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("build openrouter request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+g.Config.BlueprintAI.OpenRouterAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://railpush.com")
	req.Header.Set("X-Title", "RailPush Blueprint Autogen")

	resp, err := g.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openrouter request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("openrouter error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed openRouterChatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode openrouter response: %w", err)
	}
	if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return "", fmt.Errorf("openrouter error: %s", strings.TrimSpace(parsed.Error.Message))
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("openrouter returned no choices")
	}
	content := extractOpenRouterContent(parsed.Choices[0].Message.Content)
	content = extractYAMLFromMarkdown(content)
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf("openrouter returned empty content")
	}
	return content + "\n", nil
}

func collectBlueprintAISnippets(repoDir string, maxFiles int, maxFileBytes int) ([]blueprintAISnippet, error) {
	skipDirs := map[string]struct{}{
		".git":         {},
		"node_modules": {},
		"vendor":       {},
		"dist":         {},
		"build":        {},
		".next":        {},
		".nuxt":        {},
		"coverage":     {},
		".cache":       {},
	}
	exactPriority := map[string]int{
		"dockerfile":          1,
		"render.yaml":         1,
		"render.yml":          1,
		"docker-compose.yml":  2,
		"docker-compose.yaml": 2,
		"package.json":        3,
		"requirements.txt":    3,
		"pyproject.toml":      3,
		"go.mod":              3,
		"cargo.toml":          3,
		"gemfile":             3,
		"composer.json":       3,
		"pom.xml":             3,
		"build.gradle":        3,
		"procfile":            4,
		"railway.toml":        4,
		"vercel.json":         4,
	}
	extPriority := map[string]int{
		".yaml": 5,
		".yml":  5,
		".toml": 6,
		".json": 6,
		".js":   7,
		".ts":   7,
		".jsx":  7,
		".tsx":  7,
		".py":   7,
		".go":   7,
		".rb":   7,
		".rs":   7,
		".php":  7,
		".java": 7,
		".cs":   7,
		".sh":   8,
		".md":   9,
	}

	var snippets []blueprintAISnippet
	err := filepath.WalkDir(repoDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if _, skip := skipDirs[name]; skip {
				return filepath.SkipDir
			}
			return nil
		}

		lowerName := strings.ToLower(name)
		if strings.HasPrefix(lowerName, ".env") ||
			strings.Contains(lowerName, "secret") ||
			strings.Contains(lowerName, "token") ||
			strings.Contains(lowerName, "id_rsa") ||
			strings.HasSuffix(lowerName, ".pem") ||
			strings.HasSuffix(lowerName, ".key") ||
			strings.HasSuffix(lowerName, ".crt") {
			return nil
		}

		info, ierr := d.Info()
		if ierr != nil || info.Size() <= 0 || info.Size() > int64(maxFileBytes) {
			return nil
		}

		rel, rerr := filepath.Rel(repoDir, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		ext := strings.ToLower(filepath.Ext(lowerName))

		score := 100
		if p, ok := exactPriority[lowerName]; ok {
			score = p
		} else if p, ok := extPriority[ext]; ok {
			score = p
		} else {
			return nil
		}

		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			return nil
		}

		snippets = append(snippets, blueprintAISnippet{
			Path:    rel,
			Content: content,
			Score:   score,
			Size:    len(content),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(snippets, func(i, j int) bool {
		if snippets[i].Score != snippets[j].Score {
			return snippets[i].Score < snippets[j].Score
		}
		if snippets[i].Size != snippets[j].Size {
			return snippets[i].Size < snippets[j].Size
		}
		return snippets[i].Path < snippets[j].Path
	})

	if len(snippets) > maxFiles {
		snippets = snippets[:maxFiles]
	}
	return snippets, nil
}

func extractOpenRouterContent(raw any) string {
	switch v := raw.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if txt, ok := m["text"].(string); ok && strings.TrimSpace(txt) != "" {
					b.WriteString(txt)
				}
			}
		}
		return b.String()
	default:
		return ""
	}
}

func extractYAMLFromMarkdown(in string) string {
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
