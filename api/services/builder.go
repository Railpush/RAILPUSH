package services

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/railpush/api/config"
)

type Builder struct {
	Config *config.Config
}

func NewBuilder(cfg *config.Config) *Builder {
	return &Builder{Config: cfg}
}

func (b *Builder) CloneRepo(repoURL, branch, destDir, token string) error {
	os.MkdirAll(destDir, 0755)
	cloneURL := repoURL
	// Inject token for authenticated GitHub HTTPS cloning
	if token != "" && strings.Contains(cloneURL, "github.com") && strings.HasPrefix(cloneURL, "https://") {
		cloneURL = strings.Replace(cloneURL, "https://github.com/", "https://x-access-token:"+token+"@github.com/", 1)
	}
	cmd := exec.Command("git", "clone", "--depth", "1", "-b", branch, cloneURL, destDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, string(out))
	}
	return nil
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
		bc := "npm install"
		if buildCmd != "" {
			bc = fmt.Sprintf("npm install && %s", buildCmd)
		}
		sc := "npm start"
		if startCmd != "" {
			sc = startCmd
		}
		return fmt.Sprintf(`FROM node:20-alpine
WORKDIR /app
COPY package*.json ./
RUN npm install --production=false
COPY . .
RUN %s
ENV PORT=%d
EXPOSE %d
CMD ["/bin/sh", "-c", "%s"]
`, bc, port, port, sc)

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
