package services

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/railpush/api/config"
)

type Builder struct {
	Config *config.Config
}

func NewBuilder(cfg *config.Config) *Builder {
	return &Builder{Config: cfg}
}

func (b *Builder) CloneRepo(repoURL, branch, destDir, token string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}

	// Prefer git CLI when available (better compatibility: submodules, LFS, etc).
	// Our runtime image is distroless (no /usr/bin/git), so we fall back to a pure-go clone.
	if err := cloneRepoWithGitCLI(repoURL, branch, destDir, token); err == nil {
		return nil
	} else if !isGitNotFound(err) {
		return err
	}

	return cloneRepoWithGoGit(repoURL, branch, destDir, token)
}

func isGitNotFound(err error) bool {
	if err == nil {
		return false
	}
	var ee *exec.Error
	return errors.As(err, &ee) && errors.Is(ee.Err, exec.ErrNotFound)
}

func cloneRepoWithGitCLI(repoURL, branch, destDir, token string) error {
	cmd := exec.Command("git", "clone", "--depth", "1", "-b", branch, repoURL, destDir)
	cmd.Env = os.Environ()

	// Avoid embedding secrets in clone URLs and process args. Use GIT_ASKPASS instead.
	token = strings.TrimSpace(token)
	if token != "" && strings.Contains(repoURL, "github.com") && strings.HasPrefix(repoURL, "https://") {
		f, err := os.CreateTemp("", "railpush-git-askpass-*")
		if err != nil {
			return err
		}
		askpassPath := f.Name()
		f.Close()

		script := `#!/bin/sh
case "$1" in
  *Username*) echo "x-access-token" ;;
  *) echo "${GITHUB_TOKEN}" ;;
esac
`
		if err := os.WriteFile(askpassPath, []byte(script), 0700); err != nil {
			_ = os.Remove(askpassPath)
			return err
		}
		defer os.Remove(askpassPath)

		cmd.Env = append(cmd.Env,
			"GIT_ASKPASS="+askpassPath,
			"GIT_TERMINAL_PROMPT=0",
			"GITHUB_TOKEN="+token,
		)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		// Wrap the original error so callers can detect exec.ErrNotFound and fall back.
		return fmt.Errorf("git clone failed: %w: %s", err, string(out))
	}
	return nil
}

func cloneRepoWithGoGit(repoURL, branch, destDir, token string) error {
	token = strings.TrimSpace(token)
	cloneOpts := &git.CloneOptions{
		URL:           repoURL,
		Depth:         1,
		SingleBranch:  true,
		ReferenceName: plumbing.NewBranchReferenceName(branch),
	}
	if token != "" && strings.Contains(repoURL, "github.com") && strings.HasPrefix(repoURL, "https://") {
		cloneOpts.Auth = &http.BasicAuth{Username: "x-access-token", Password: token}
	}

	_, err := git.PlainClone(destDir, false, cloneOpts)
	return err
}

func (b *Builder) DetectRuntime(dir string) string {
	m := map[string]string{
		"package.json":     "node",
		"requirements.txt": "python",
		"Pipfile":          "python",
		"go.mod":           "go",
		"Gemfile":          "ruby",
		"Cargo.toml":       "rust",
		"mix.exs":          "elixir",
		"pom.xml":          "java",
		"build.gradle":     "java",
	}
	for f, r := range m {
		if _, e := os.Stat(filepath.Join(dir, f)); e == nil {
			return r
		}
	}
	return "docker"
}

func (b *Builder) BuildImage(name, dir string) (string, error) {
	tag := fmt.Sprintf("railpush/%s:latest", strings.ToLower(name))
	cmd := exec.Command("docker", "build", "-t", tag, dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return tag, fmt.Errorf("%v: %s", err, string(out))
	}
	log.Printf("Built: %s", tag)
	return tag, nil
}

func (b *Builder) BuildImageWithLogs(tag, buildContext, dockerfilePath string) (string, error) {
	args := []string{"build", "-t", tag, "-f", dockerfilePath, buildContext}
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (b *Builder) GenerateDockerfile(runtime, buildCmd, startCmd string, port int) string {
	if port == 0 {
		port = 10000
	}

	switch runtime {
	case "node":
		bc := strings.TrimSpace(buildCmd)
		if bc == "" {
			// Common for TS backends; safe no-op if the script doesn't exist.
			bc = "npm run build --if-present"
		}
		sc := strings.TrimSpace(startCmd)
		if sc == "" {
			// Reasonable defaults for typical backends.
			sc = "if [ -f dist/index.js ]; then node dist/index.js; elif [ -f server-index.js ]; then node server-index.js; elif [ -f server.js ]; then node server.js; elif [ -f index.js ]; then node index.js; else npm start; fi"
		}

		// Support pnpm/yarn when a repo uses them. Corepack is shipped with Node images but is disabled by default.
		needsCorepack := false
		lc := strings.ToLower(bc + " " + sc)
		if strings.Contains(lc, "pnpm") || strings.Contains(lc, "yarn") {
			needsCorepack = true
		}
		corepackLine := ""
		if needsCorepack {
			corepackLine = "RUN corepack enable\n"
		}

		return fmt.Sprintf(`FROM node:20-alpine
WORKDIR /app
COPY package*.json ./
%sRUN if [ -f package-lock.json ]; then npm ci; elif [ -f pnpm-lock.yaml ]; then pnpm install --frozen-lockfile; elif [ -f yarn.lock ]; then yarn install --frozen-lockfile; else npm install; fi
COPY . .
RUN rm -rf .git
RUN %s
ENV NODE_ENV=production
ENV PORT=%d
ENV HOME=/tmp
ENV XDG_CACHE_HOME=/tmp/.cache
ENV COREPACK_HOME=/tmp/.corepack
ENV NPM_CONFIG_CACHE=/tmp/.npm
EXPOSE %d
CMD ["sh","-lc","%s"]
`, corepackLine, bc, port, port, sc)

	case "python":
		bc := "pip install --no-cache-dir -r requirements.txt"
		if buildCmd != "" {
			bc = fmt.Sprintf("%s && %s", bc, buildCmd)
		}
		sc := "python app.py"
		if startCmd != "" {
			sc = startCmd
		}
		return fmt.Sprintf(`FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt* ./
RUN pip install --no-cache-dir -r requirements.txt 2>/dev/null || true
COPY . .
RUN %s
EXPOSE %d
CMD ["/bin/sh", "-c", "%s"]
`, bc, port, sc)

	case "go":
		bc := "go build -o /app/server ."
		if buildCmd != "" {
			bc = buildCmd
		}
		sc := "/app/server"
		if startCmd != "" {
			sc = startCmd
		}
		return fmt.Sprintf(`FROM golang:1.22-alpine
WORKDIR /app
COPY go.* ./
RUN go mod download
COPY . .
RUN %s
EXPOSE %d
CMD ["/bin/sh", "-c", "%s"]
`, bc, port, sc)

	case "ruby":
		bc := "bundle install"
		if buildCmd != "" {
			bc = fmt.Sprintf("%s && %s", bc, buildCmd)
		}
		sc := "bundle exec ruby app.rb"
		if startCmd != "" {
			sc = startCmd
		}
		return fmt.Sprintf(`FROM ruby:3.3-slim
WORKDIR /app
COPY Gemfile* ./
RUN %s
COPY . .
EXPOSE %d
CMD ["/bin/sh", "-c", "%s"]
`, bc, port, sc)

	case "rust":
		bc := "cargo build --release"
		if buildCmd != "" {
			bc = buildCmd
		}
		sc := "./target/release/app"
		if startCmd != "" {
			sc = startCmd
		}
		return fmt.Sprintf(`FROM rust:1.77-slim
WORKDIR /app
COPY . .
RUN %s
EXPOSE %d
CMD ["/bin/sh", "-c", "%s"]
`, bc, port, sc)

	case "static":
		bc := "npm run build"
		if buildCmd != "" {
			bc = buildCmd
		}
		publishPath := "/app/dist"
		if startCmd != "" {
			publishPath = "/app/" + startCmd
		}
		return fmt.Sprintf(`FROM node:20-alpine AS build
WORKDIR /app
COPY package*.json ./
RUN npm install
COPY . .
RUN %s

FROM nginx:alpine
COPY --from=build %s /usr/share/nginx/html
RUN printf 'server {\n    listen %d;\n    location / {\n        root /usr/share/nginx/html;\n        try_files $uri $uri/ /index.html;\n    }\n}\n' > /etc/nginx/conf.d/default.conf
EXPOSE %d
CMD ["nginx", "-g", "daemon off;"]
`, bc, publishPath, port, port)

	case "elixir":
		bc := "mix deps.get && mix compile"
		if buildCmd != "" {
			bc = buildCmd
		}
		sc := "mix phx.server"
		if startCmd != "" {
			sc = startCmd
		}
		return fmt.Sprintf(`FROM elixir:1.16-alpine
WORKDIR /app
COPY mix.exs mix.lock ./
RUN %s
COPY . .
EXPOSE %d
CMD ["/bin/sh", "-c", "%s"]
`, bc, port, sc)

	case "java":
		bc := "echo 'no build'"
		if buildCmd != "" {
			bc = buildCmd
		}
		sc := "java -jar target/*.jar"
		if startCmd != "" {
			sc = startCmd
		}
		return fmt.Sprintf(`FROM eclipse-temurin:21-jdk-alpine
WORKDIR /app
COPY . .
RUN %s
EXPOSE %d
CMD ["/bin/sh", "-c", "%s"]
`, bc, port, sc)

	default:
		bc := buildCmd
		if bc == "" {
			bc = "echo 'no build command'"
		}
		sc := startCmd
		if sc == "" {
			sc = "echo 'no start command'"
		}
		return fmt.Sprintf(`FROM ubuntu:22.04
WORKDIR /app
COPY . .
RUN %s
EXPOSE %d
CMD ["/bin/sh", "-c", "%s"]
`, bc, port, sc)
	}
}
