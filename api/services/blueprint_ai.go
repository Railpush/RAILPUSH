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
	timeout := 120 * time.Second
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

// configFileNames are files whose full content is sent to the AI.
// These are the only files needed to determine runtime, dependencies, build commands, etc.
var configFileNames = map[string]bool{
	"dockerfile":          true,
	"docker-compose.yml":  true,
	"docker-compose.yaml": true,
	"compose.yml":         true,
	"compose.yaml":        true,
	"package.json":        true,
	"requirements.txt":    true,
	"pyproject.toml":      true,
	"go.mod":              true,
	"go.sum":              false, // tree only
	"cargo.toml":          true,
	"gemfile":             true,
	"composer.json":       true,
	"pom.xml":             true,
	"build.gradle":        true,
	"procfile":            true,
	"railway.toml":        true,
	"vercel.json":         true,
	"next.config.js":      true,
	"next.config.ts":      true,
	"next.config.mjs":     true,
	"nuxt.config.js":      true,
	"nuxt.config.ts":      true,
	"vite.config.js":      true,
	"vite.config.ts":      true,
	"tsconfig.json":       true,
	"webpack.config.js":   true,
	"angular.json":        true,
	"nest-cli.json":       true,
	"mix.exs":             true,
	"elixir.exs":          true,
	"config.exs":          true,
	"makefile":            true,
	"cmakelists.txt":      true,
	"railpush.yaml":       true,
	"railpush.yml":        true,
	"render.yaml":         true,
	"render.yml":          true,
	".env.example":        true,
	"dot_env":             true,
	"nixpacks.toml":       true,
}

func (g *BlueprintAIGenerator) GenerateRenderYAMLFromRepo(repoDir string, repoURL string, branch string) (string, error) {
	if g == nil || !g.Available() {
		return "", fmt.Errorf("blueprint ai unavailable")
	}
	repoDir = strings.TrimSpace(repoDir)
	if repoDir == "" {
		return "", fmt.Errorf("missing repository path")
	}

	maxFileBytes := g.Config.BlueprintAI.MaxFileBytes
	if maxFileBytes <= 0 {
		maxFileBytes = 20000
	}

	// Collect the file tree and read only config files.
	fileTree, configContents, err := collectRepoStructure(repoDir, maxFileBytes)
	if err != nil {
		return "", err
	}
	if len(fileTree) == 0 {
		return "", fmt.Errorf("no files found in repository")
	}

	// Build the config files block.
	var configBlock strings.Builder
	for _, cf := range configContents {
		configBlock.WriteString("## " + cf.Path + "\n```\n" + cf.Content + "\n```\n\n")
	}

	systemPrompt := `You generate RailPush railpush.yaml (blueprint) files from repository structure.
You will receive:
1. The full file tree (paths only) — use this to understand the project structure, detect monorepos, and identify frameworks.
2. The full content of key config files (package.json, Dockerfile, requirements.txt, etc.) — use these to determine runtime, dependencies, build/start commands, env vars, and database usage.

Return ONLY valid YAML, no markdown fences, no explanation.

Schema:

services:
  - name: <required string>
    type: web|worker|cron|static|pserv
    runtime: docker|node|python|go|ruby|rust|elixir|static|image
    repo: <repo-url>
    branch: <branch>
    buildCommand: <optional string>
    startCommand: <optional string>
    dockerfilePath: <optional, path to Dockerfile>
    dockerContext: <optional, docker build context directory>
    dockerCommand: <optional, overrides container CMD>
    rootDir: <optional, monorepo subdirectory containing the app>
    staticPublishPath: <required for type=static, e.g. ./dist or ./build or ./out>
    schedule: <required for type=cron, cron expression>
    port: <int, default 10000>
    autoDeploy: true|false
    plan: free|starter|standard|pro
    numInstances: <int, default 1>
    healthCheckPath: <optional, e.g. /healthz>
    preDeployCommand: <optional, runs before deploy e.g. "npx prisma migrate deploy">
    disk:
      name: <string>
      mountPath: <string>
      sizeGB: <int, default 10>
    envVars:
      - key: <ENV_NAME>
        value: <literal value>
      - key: <ENV_NAME>
        generateValue: true
      - key: DATABASE_URL
        fromDatabase:
          name: <database name>
          property: connectionString
      - key: <ENV_NAME>
        fromGroup: <env var group name>

databases:
  - name: <required string>
    plan: free|starter|standard|pro
    postgresMajorVersion: <int, default 16>

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
- Infer runtime from config files: package.json=node, requirements.txt/pyproject.toml=python, go.mod=go, Cargo.toml=rust, Gemfile=ruby, mix.exs=elixir.
- If a Dockerfile exists, prefer runtime=docker unless it's a simple single-stage build for a known runtime.
- For Next.js: use runtime=node (NOT static), buildCommand="npm install && npm run build", startCommand="npm start", port=3000.
- For static sites (React CRA, Vue, plain HTML): use type=static with the correct staticPublishPath.
- Detect database usage from deps (pg, prisma, sqlalchemy, typeorm, knex, diesel, gorm, sequelize, mongoose) and add a databases entry with fromDatabase env var.
- Detect Redis usage from deps (ioredis, redis, bull, celery) and add a keyValues entry.
- Use generateValue: true for secrets (SESSION_SECRET, JWT_SECRET, API_KEY, SECRET_KEY_BASE, ENCRYPTION_KEY).
- Look at .env.example/dot_env for required env var names. Use generateValue for secrets, leave others as placeholder comments.
- Plan values MUST be one of: free, starter, standard, pro. Default to starter.
- Keep configuration conservative and production-safe.
- Include at least one service.`

	userPrompt := fmt.Sprintf(
		"Repository: %s (branch: %s)\n\n## File Tree\n```\n%s```\n\n## Config File Contents\n%s",
		strings.TrimSpace(repoURL),
		strings.TrimSpace(branch),
		strings.Join(fileTree, "\n")+"\n",
		configBlock.String(),
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

type configFile struct {
	Path    string
	Content string
}

// collectRepoStructure walks the repo and returns:
// 1. fileTree: a list of all file paths (for the AI to understand structure)
// 2. configContents: full content of config files (package.json, Dockerfile, etc.)
func collectRepoStructure(repoDir string, maxFileBytes int) ([]string, []configFile, error) {
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
		"__pycache__":  {},
		".tox":         {},
		".venv":        {},
		"venv":         {},
		"target":       {},  // Rust/Java
		"bin":          {},
		"obj":          {},  // .NET
	}

	// Files to skip from the tree entirely (noise).
	skipFiles := map[string]bool{
		"package-lock.json": true,
		"pnpm-lock.yaml":   true,
		"yarn.lock":        true,
		"go.sum":           true,
		"cargo.lock":       true,
		"composer.lock":    true,
		"gemfile.lock":     true,
		"poetry.lock":      true,
	}

	var fileTree []string
	var configs []configFile

	err := filepath.WalkDir(repoDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if _, skip := skipDirs[strings.ToLower(name)]; skip {
				return filepath.SkipDir
			}
			return nil
		}

		lowerName := strings.ToLower(name)

		// Skip secret files entirely.
		if strings.HasPrefix(lowerName, ".env") && lowerName != ".env.example" {
			return nil
		}
		if strings.Contains(lowerName, "id_rsa") ||
			strings.HasSuffix(lowerName, ".pem") ||
			strings.HasSuffix(lowerName, ".key") ||
			strings.HasSuffix(lowerName, ".crt") {
			return nil
		}

		// Skip lock files from tree (they just add noise).
		if skipFiles[lowerName] {
			return nil
		}

		rel, rerr := filepath.Rel(repoDir, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		// Add to file tree (path only).
		fileTree = append(fileTree, rel)

		// If it's a config file, read its content.
		if include, ok := configFileNames[lowerName]; ok && include {
			info, ierr := d.Info()
			if ierr != nil || info.Size() <= 0 || info.Size() > int64(maxFileBytes) {
				return nil
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil
			}
			content := strings.TrimSpace(string(data))
			if content != "" {
				configs = append(configs, configFile{Path: rel, Content: content})
			}
		}

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// Sort configs: Dockerfile and compose files first, then package managers, then others.
	configPriority := map[string]int{
		"dockerfile":          1,
		"docker-compose.yml":  2,
		"docker-compose.yaml": 2,
		"compose.yml":         2,
		"compose.yaml":        2,
		"package.json":        3,
		"requirements.txt":    3,
		"pyproject.toml":      3,
		"go.mod":              3,
		"cargo.toml":          3,
		"gemfile":             3,
		"composer.json":       3,
		"mix.exs":             3,
		"pom.xml":             3,
		"build.gradle":        3,
		"procfile":            4,
		".env.example":        5,
		"dot_env":             5,
	}
	sort.Slice(configs, func(i, j int) bool {
		pi := configPriority[strings.ToLower(filepath.Base(configs[i].Path))]
		pj := configPriority[strings.ToLower(filepath.Base(configs[j].Path))]
		if pi == 0 {
			pi = 10
		}
		if pj == 0 {
			pj = 10
		}
		return pi < pj
	})

	return fileTree, configs, nil
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
