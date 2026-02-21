package services

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/railpush/api/models"
)

// validDockerImageRef matches valid Docker image references: registry/repo:tag or repo@sha256:...
// Rejects shell metacharacters ($, `, \, ;, |, &, newlines, etc.) to prevent injection
// in generated Dockerfiles.
var validDockerImageRef = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/:@-]{0,255}$`)

func int32Ptr(v int32) *int32 { return &v }
func int64Ptr(v int64) *int64 { return &v }

func cleanRelPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "/")
	raw = path.Clean(raw)
	raw = strings.TrimPrefix(raw, "./")
	if raw == "." {
		return "", nil
	}
	if raw == ".." || strings.HasPrefix(raw, "../") {
		return "", fmt.Errorf("invalid path: %q", raw)
	}
	return raw, nil
}

// BuildImageWithKaniko builds and pushes a Docker image for a git repo using a Kubernetes Job.
// MVP constraints:
// - requires a Dockerfile to exist in the repo (no generated Dockerfiles yet)
// - pushes to an insecure (HTTP) registry is supported via --insecure-registry
func (k *KubeDeployer) BuildImageWithKaniko(deployID string, svc *models.Service, repoURL string, branch string, commitSHA string, dockerContext string, dockerfilePath string, destImage string, githubToken string, dockerfileOverride string, appendLog func(string)) error {
	if k == nil || k.Client == nil || k.Config == nil {
		return fmt.Errorf("kube deployer not initialized")
	}
	if svc == nil {
		return fmt.Errorf("missing service")
	}
	deployID = strings.TrimSpace(deployID)
	if deployID == "" {
		return fmt.Errorf("missing deploy id")
	}
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return fmt.Errorf("missing repo url")
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = "main"
	}
	destImage = strings.TrimSpace(destImage)
	if destImage == "" {
		return fmt.Errorf("missing destination image")
	}

	ns := k.namespace()
	jobName := "rp-build-" + strings.ToLower(deployID)
	jobName = kubeNameInvalidChars.ReplaceAllString(jobName, "-")
	jobName = strings.Trim(jobName, "-")
	if len(jobName) > 63 {
		jobName = jobName[:63]
		jobName = strings.Trim(jobName, "-")
	}
	if jobName == "" {
		return fmt.Errorf("invalid job name")
	}

	labels := kubeServiceLabels(svc)
	labels["app.kubernetes.io/component"] = "build"
	labels["railpush.com/deploy-id"] = deployID

	registryHost := strings.SplitN(destImage, "/", 2)[0]

	repoRoot := "/workspace/repo"
	effectiveDir := repoRoot
	ctxRel, err := cleanRelPath(dockerContext)
	if err != nil {
		return fmt.Errorf("invalid docker context: %w", err)
	}
	if ctxRel != "" {
		effectiveDir = repoRoot + "/" + ctxRel
	}

	dfRelFromRepo := ""
	dfInput, err := cleanRelPath(dockerfilePath)
	if err != nil {
		return fmt.Errorf("invalid dockerfile path: %w", err)
	}
	if dfInput != "" {
		dfRelFromRepo = dfInput
	} else if ctxRel != "" {
		dfRelFromRepo = ctxRel + "/Dockerfile"
	} else {
		dfRelFromRepo = "Dockerfile"
	}

	// Kaniko expects the dockerfile path to be within the build context. Provide it relative to the context.
	dfRelForKaniko := dfRelFromRepo
	if ctxRel != "" {
		prefix := ctxRel + "/"
		if !strings.HasPrefix(dfRelFromRepo, prefix) {
			return fmt.Errorf("dockerfile %q must be within docker context %q", dfRelFromRepo, ctxRel)
		}
		dfRelForKaniko = strings.TrimPrefix(dfRelFromRepo, prefix)
	}
	dfAbs := repoRoot + "/" + dfRelFromRepo

	serviceType := strings.ToLower(strings.TrimSpace(svc.Type))
	runtime := strings.ToLower(strings.TrimSpace(svc.Runtime))
	// Match the non-Kubernetes deploy behavior: static sites always use the static build pipeline,
	// regardless of what the service "runtime" field says.
	if serviceType == "static" {
		runtime = "static"
	}
	port := svc.Port
	if port <= 0 {
		port = 10000
	}

	if appendLog != nil {
		appendLog(fmt.Sprintf("==> Kubernetes build: job=%s", jobName))
		appendLog(fmt.Sprintf("==> Repo: %s (branch=%s)", repoURL, branch))
		appendLog(fmt.Sprintf("==> Docker context: %s", effectiveDir))
		appendLog(fmt.Sprintf("==> Dockerfile: %s", dfAbs))
		appendLog(fmt.Sprintf("==> Destination: %s", destImage))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()

	// Enforce multi-tenant network isolation (default-deny ingress between workspaces).
	if err := k.EnsureTenantNetworkPolicies(ctx, svc.WorkspaceID); err != nil {
		return fmt.Errorf("ensure tenant networkpolicies: %w", err)
	}

	// Store GitHub token in a short-lived Secret (avoid embedding sensitive values in Job specs).
	// The Secret MUST be created before the Job, otherwise the pod's SecretKeyRef will fail
	// and the Job dies immediately (BackoffLimit=0).
	githubToken = strings.TrimSpace(githubToken)
	secretName := ""
	if githubToken != "" {
		secretName = jobName + "-git"
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: ns,
				Labels:    labels,
			},
			Type:       corev1.SecretTypeOpaque,
			StringData: map[string]string{"token": githubToken},
		}
		if existing, err := k.Client.CoreV1().Secrets(ns).Get(ctx, secretName, metav1.GetOptions{}); err == nil && existing != nil {
			sec.ResourceVersion = existing.ResourceVersion
			if _, err := k.Client.CoreV1().Secrets(ns).Update(ctx, sec, metav1.UpdateOptions{}); err != nil {
				return fmt.Errorf("update git token secret: %w", err)
			}
		} else if apierrors.IsNotFound(err) {
			if _, err := k.Client.CoreV1().Secrets(ns).Create(ctx, sec, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("create git token secret: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("get git token secret: %w", err)
		}
	}

	// Create the job if it doesn't exist. If it exists, we attach to it.
	if existing, err := k.Client.BatchV1().Jobs(ns).Get(ctx, jobName, metav1.GetOptions{}); err == nil && existing != nil {
		if existing.Status.Succeeded > 0 {
			if appendLog != nil {
				appendLog("==> Build already completed (job succeeded).")
			}
			return nil
		}
		if appendLog != nil {
			appendLog("==> Build job already exists; waiting for completion...")
		}
	} else if apierrors.IsNotFound(err) {
		cloneScript := strings.TrimSpace(`
set -e
mkdir -p /workspace/repo
cd /workspace/repo
git init -q
git remote add origin "$REPO_URL"

if [ -n "${GITHUB_TOKEN:-}" ]; then
  auth="$(printf 'x-access-token:%s' "$GITHUB_TOKEN" | base64 | tr -d '\n')"
  extraheader="AUTHORIZATION: basic $auth"
  git -c http.extraheader="$extraheader" fetch -q --depth=1 origin "$BRANCH"
  git -c http.extraheader="$extraheader" checkout -q FETCH_HEAD
else
  git fetch -q --depth=1 origin "$BRANCH"
  git checkout -q FETCH_HEAD
fi

# If the repo doesn't include a Dockerfile (common for static sites), generate one.
# This keeps the MVP flow working without asking users to commit Dockerfiles.
RUNTIME="$(printf '%s' "${RAILPUSH_RUNTIME:-}" | tr '[:upper:]' '[:lower:]' | tr -d ' ')"
DOCKERFILE_PATH="${RAILPUSH_DOCKERFILE_PATH:-}"
if [ -z "$DOCKERFILE_PATH" ]; then
  DOCKERFILE_PATH="Dockerfile"
fi

# Auto-detect runtime from repo contents when not explicitly set.
RAILPUSH_SUBDIR=""
if [ -z "$RUNTIME" ] && [ ! -f "$DOCKERFILE_PATH" ]; then
  if [ -f "package.json" ]; then
    RUNTIME="node"
    echo "RailPush: auto-detected runtime=node (found package.json)"
  elif [ -f "requirements.txt" ] || [ -f "Pipfile" ] || [ -f "pyproject.toml" ]; then
    RUNTIME="python"
    echo "RailPush: auto-detected runtime=python"
  elif [ -f "go.mod" ]; then
    RUNTIME="go"
    echo "RailPush: auto-detected runtime=go (found go.mod)"
  elif [ -f "Gemfile" ]; then
    RUNTIME="ruby"
    echo "RailPush: auto-detected runtime=ruby (found Gemfile)"
  elif [ -f "Cargo.toml" ]; then
    RUNTIME="rust"
    echo "RailPush: auto-detected runtime=rust (found Cargo.toml)"
  else
    # Check common subdirectories for monorepo layouts.
    for subdir in backend server api app src; do
      if [ -d "$subdir" ]; then
        if [ -f "$subdir/Dockerfile" ]; then
          echo "RailPush: found Dockerfile in $subdir/, promoting to build root"
          # Promote subdir to repo root so Dockerfile COPY paths resolve correctly.
          cp -a "$subdir" /workspace/_promote
          find . -maxdepth 1 ! -name . ! -name .. -exec rm -rf {} +
          cp -a /workspace/_promote/. .
          rm -rf /workspace/_promote
          break
        elif [ -f "$subdir/package.json" ]; then
          RUNTIME="node"; RAILPUSH_SUBDIR="$subdir"
          echo "RailPush: auto-detected runtime=node in $subdir/"
          break
        elif [ -f "$subdir/requirements.txt" ] || [ -f "$subdir/Pipfile" ] || [ -f "$subdir/pyproject.toml" ]; then
          RUNTIME="python"; RAILPUSH_SUBDIR="$subdir"
          echo "RailPush: auto-detected runtime=python in $subdir/"
          break
        elif [ -f "$subdir/go.mod" ]; then
          RUNTIME="go"; RAILPUSH_SUBDIR="$subdir"
          echo "RailPush: auto-detected runtime=go in $subdir/"
          break
        fi
      fi
    done
  fi
fi

# AI Fix: if an override Dockerfile is provided, write it directly.
if [ -n "${RAILPUSH_DOCKERFILE_CONTENT:-}" ]; then
  echo "RailPush: using AI-fixed Dockerfile"
  mkdir -p "$(dirname "$DOCKERFILE_PATH")" 2>/dev/null || true
  printf '%s\n' "$RAILPUSH_DOCKERFILE_CONTENT" > "$DOCKERFILE_PATH"
fi

# Per-service build context: generate .dockerignore from buildInclude/buildExclude.
# buildInclude is a newline-delimited list of files to KEEP (everything else is excluded).
# buildExclude is a newline-delimited list of files to EXCLUDE.
if [ -n "${RAILPUSH_BUILD_INCLUDE:-}" ]; then
  echo "RailPush: generating .dockerignore from buildInclude"
  # Start by ignoring everything, then whitelist the specified paths.
  printf '%s\n' "*" > .dockerignore
  echo "$RAILPUSH_BUILD_INCLUDE" | while IFS= read -r incl || [ -n "$incl" ]; do
    incl="$(printf '%s' "$incl" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
    [ -z "$incl" ] && continue
    printf '!%s\n' "$incl" >> .dockerignore
  done
  # Always keep Dockerfile itself.
  printf '!%s\n' "$DOCKERFILE_PATH" >> .dockerignore
  printf '!%s\n' "Dockerfile" >> .dockerignore
  echo "RailPush: .dockerignore contents:"
  cat .dockerignore | sed 's/^/  /'
elif [ -n "${RAILPUSH_BUILD_EXCLUDE:-}" ]; then
  echo "RailPush: generating .dockerignore from buildExclude"
  echo "$RAILPUSH_BUILD_EXCLUDE" | while IFS= read -r excl || [ -n "$excl" ]; do
    excl="$(printf '%s' "$excl" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
    [ -z "$excl" ] && continue
    printf '%s\n' "$excl" >> .dockerignore
  done
  echo "RailPush: .dockerignore contents:"
  cat .dockerignore | sed 's/^/  /'
fi

# Always exclude .git from builds (saves context size).
if [ ! -f .dockerignore ]; then
  printf '%s\n' ".git" > .dockerignore
else
  printf '%s\n' ".git" >> .dockerignore
fi

# Resolve base image override for auto-generated Dockerfiles.
# The Go layer validates RAILPUSH_BASE_IMAGE against a strict regex, but as defense
# in depth we also reject values containing shell metacharacters here.
_validate_image_ref() {
  case "$1" in
    *'$'*|*'`'*|*'\'*|*';'*|*'|'*|*'&'*|*'('*|*')'*|*'{'*|*'}'*|*'>'*|*'<'*|*'!'*|*"'"*)
      echo "RailPush: WARNING: invalid base image reference, using default" >&2
      return 1 ;;
  esac
  return 0
}
BASE_IMAGE_NODE="node:20-alpine"
BASE_IMAGE_PYTHON="python:3.12-slim"
BASE_IMAGE_GO="golang:1.24-alpine"
if [ -n "${RAILPUSH_BASE_IMAGE:-}" ] && _validate_image_ref "$RAILPUSH_BASE_IMAGE"; then
  BASE_IMAGE_NODE="$RAILPUSH_BASE_IMAGE"
  BASE_IMAGE_PYTHON="$RAILPUSH_BASE_IMAGE"
  BASE_IMAGE_GO="$RAILPUSH_BASE_IMAGE"
fi

if [ "$RUNTIME" = "static" ] && [ ! -f "$DOCKERFILE_PATH" ]; then
  echo "RailPush: generating static Dockerfile at $DOCKERFILE_PATH"
  mkdir -p "$(dirname "$DOCKERFILE_PATH")" 2>/dev/null || true

  PORT="${RAILPUSH_PORT:-10000}"
  BUILD_COMMAND="${RAILPUSH_BUILD_COMMAND:-npm run build}"
  PUBLISH_PATH="${RAILPUSH_STATIC_PUBLISH_PATH:-dist}"
  PUBLISH_PATH="${PUBLISH_PATH#/}"

  # If the build command already installs deps (npm/yarn/pnpm), don't duplicate work.
  NEEDS_INSTALL="1"
  if [ -n "$BUILD_COMMAND" ]; then
    if echo "$BUILD_COMMAND" | grep -Eq '(^|[[:space:];&|])npm[[:space:]]+(ci|install)([[:space:];&|]|$)'; then
      NEEDS_INSTALL="0"
    elif echo "$BUILD_COMMAND" | grep -Eq '(^|[[:space:];&|])(yarn|pnpm)[[:space:]]+install([[:space:];&|]|$)'; then
      NEEDS_INSTALL="0"
    fi
  fi

  # If the build command uses yarn/pnpm, ensure the base image has shims available.
  # Node images ship corepack, but it is not enabled by default.
  NEEDS_COREPACK="0"
  if [ -n "$BUILD_COMMAND" ]; then
    if echo "$BUILD_COMMAND" | grep -Eq '(^|[[:space:];&|])(yarn|pnpm)([[:space:];&|]|$)'; then
      NEEDS_COREPACK="1"
    fi
  fi

  {
    printf '%s\n' "FROM $BASE_IMAGE_NODE AS build"
    printf '%s\n' "WORKDIR /app"
    printf '%s\n' "COPY . ."
    if [ "$NEEDS_COREPACK" = "1" ]; then
      printf '%s\n' "RUN corepack enable"
    fi
    if [ "$NEEDS_INSTALL" = "1" ]; then
      printf '%s\n' "RUN if [ -f package-lock.json ]; then npm ci; else npm install; fi"
    fi
    printf '%s\n' "RUN $BUILD_COMMAND"
    printf '\n'
    printf '%s\n' "FROM nginx:alpine"
    printf '%s\n' "COPY --from=build /app/$PUBLISH_PATH /usr/share/nginx/html"
    printf '%s\n' "RUN printf 'worker_processes auto;\\nerror_log /dev/stderr notice;\\npid /tmp/nginx.pid;\\n\\nevents {\\n  worker_connections 1024;\\n}\\n\\nhttp {\\n  include /etc/nginx/mime.types;\\n  default_type application/octet-stream;\\n\\n  access_log /dev/stdout;\\n  sendfile on;\\n  keepalive_timeout 65;\\n\\n  # Nginx does not create intermediate directories; keep temp paths directly under /tmp.\\n  client_body_temp_path /tmp/client_temp 1 2;\\n  proxy_temp_path       /tmp/proxy_temp 1 2;\\n  fastcgi_temp_path     /tmp/fastcgi_temp 1 2;\\n  uwsgi_temp_path       /tmp/uwsgi_temp 1 2;\\n  scgi_temp_path        /tmp/scgi_temp 1 2;\\n\\n  include /etc/nginx/conf.d/*.conf;\\n}\\n' > /etc/nginx/nginx.conf"
    printf '%s\n' "RUN printf 'server {\\n  listen ${PORT};\\n  server_name _;\\n  root /usr/share/nginx/html;\\n  index index.html;\\n\\n  location = /healthz {\\n    add_header Content-Type text/plain;\\n    return 200 ok;\\n  }\\n\\n  location / {\\n    try_files \$uri \$uri/ /index.html;\\n  }\\n}\\n' > /etc/nginx/conf.d/default.conf"
    printf '%s\n' "ENV HOME=/tmp"
    printf '%s\n' "RUN addgroup -g 65532 -S nonroot && adduser -u 65532 -S -G nonroot -h /tmp nonroot"
    printf '%s\n' "USER 65532"
    printf '%s\n' "EXPOSE ${PORT}"
    printf '%s\n' 'ENTRYPOINT ["nginx"]'
    printf '%s\n' 'CMD ["-g", "daemon off;"]'
  } > "$DOCKERFILE_PATH"
fi

if [ "$RUNTIME" = "node" ] && [ ! -f "$DOCKERFILE_PATH" ]; then
  echo "RailPush: generating node Dockerfile at $DOCKERFILE_PATH"
  mkdir -p "$(dirname "$DOCKERFILE_PATH")" 2>/dev/null || true

  PORT="${RAILPUSH_PORT:-10000}"
  BUILD_COMMAND="${RAILPUSH_BUILD_COMMAND:-}"
  if [ -z "$BUILD_COMMAND" ]; then
    BUILD_COMMAND="npm run build --if-present"
  fi

  # If the build command already installs deps (npm/yarn/pnpm), don't duplicate work.
  NEEDS_INSTALL="1"
  if [ -n "$BUILD_COMMAND" ]; then
    if echo "$BUILD_COMMAND" | grep -Eq '(^|[[:space:];&|])npm[[:space:]]+(ci|install)([[:space:];&|]|$)'; then
      NEEDS_INSTALL="0"
    elif echo "$BUILD_COMMAND" | grep -Eq '(^|[[:space:];&|])(yarn|pnpm)[[:space:]]+install([[:space:];&|]|$)'; then
      NEEDS_INSTALL="0"
    fi
  fi

  # If the build command uses yarn/pnpm, ensure the base image has shims available.
  NEEDS_COREPACK="0"
  if [ -n "$BUILD_COMMAND" ]; then
    if echo "$BUILD_COMMAND" | grep -Eq '(^|[[:space:];&|])(yarn|pnpm)([[:space:];&|]|$)'; then
      NEEDS_COREPACK="1"
    fi
  fi

  {
    printf '%s\n' "FROM $BASE_IMAGE_NODE"
    printf '%s\n' "WORKDIR /app"
    if [ -n "$RAILPUSH_SUBDIR" ]; then
      printf 'COPY %s/ .\n' "$RAILPUSH_SUBDIR"
    else
      printf '%s\n' "COPY . ."
      printf '%s\n' "RUN rm -rf .git"
    fi
    if [ "$NEEDS_COREPACK" = "1" ]; then
      printf '%s\n' "RUN corepack enable"
    fi
    if [ "$NEEDS_INSTALL" = "1" ]; then
      printf '%s\n' "RUN if [ -f package-lock.json ]; then npm ci; else npm install; fi"
    fi
    printf '%s\n' "RUN $BUILD_COMMAND"
    printf '%s\n' "ENV NODE_ENV=production"
    printf '%s\n' "ENV PORT=${PORT}"
    # In strict tenant security mode the root filesystem is read-only; keep npm cache/logs under /tmp.
    printf '%s\n' "ENV NPM_CONFIG_CACHE=/tmp/.npm"
    printf '%s\n' "ENV HOME=/tmp"
    printf '%s\n' "ENV XDG_CACHE_HOME=/tmp/.cache"
    printf '%s\n' "ENV COREPACK_HOME=/tmp/.corepack"
    printf '%s\n' "RUN addgroup -g 65532 -S nonroot && adduser -u 65532 -S -G nonroot -h /tmp nonroot"
    printf '%s\n' "USER 65532"
    printf '%s\n' "EXPOSE ${PORT}"
    printf '%s\n' "CMD [\"sh\",\"-lc\",\"if [ -f dist/index.js ]; then node dist/index.js; elif [ -f server-index.js ]; then node server-index.js; elif [ -f server.js ]; then node server.js; elif [ -f index.js ]; then node index.js; else npm start; fi\"]"
  } > "$DOCKERFILE_PATH"
fi

if [ "$RUNTIME" = "python" ] && [ ! -f "$DOCKERFILE_PATH" ]; then
  echo "RailPush: generating python Dockerfile at $DOCKERFILE_PATH"
  mkdir -p "$(dirname "$DOCKERFILE_PATH")" 2>/dev/null || true

  PORT="${RAILPUSH_PORT:-10000}"
  BUILD_COMMAND="${RAILPUSH_BUILD_COMMAND:-}"
  COPY_FROM="."
  CHECK_DIR="."
  if [ -n "$RAILPUSH_SUBDIR" ]; then
    COPY_FROM="$RAILPUSH_SUBDIR/"
    CHECK_DIR="$RAILPUSH_SUBDIR"
  fi

  # Detect the best start command from deps or common entry points.
  START_CMD=""
  if grep -q "gunicorn" "$CHECK_DIR/requirements.txt" 2>/dev/null; then
    # Try to find the WSGI module: look for wsgi.py or app.py with app/application object.
    if [ -f "$CHECK_DIR/wsgi.py" ]; then
      START_CMD="gunicorn --bind 0.0.0.0:\${PORT} wsgi:application"
    else
      START_CMD="gunicorn --bind 0.0.0.0:\${PORT} app:app"
    fi
  elif grep -q "uvicorn" "$CHECK_DIR/requirements.txt" 2>/dev/null; then
    START_CMD="uvicorn app:app --host 0.0.0.0 --port \${PORT}"
  elif [ -f "$CHECK_DIR/manage.py" ]; then
    START_CMD="python manage.py runserver 0.0.0.0:\${PORT}"
  elif [ -f "$CHECK_DIR/app.py" ]; then
    START_CMD="python app.py"
  elif [ -f "$CHECK_DIR/main.py" ]; then
    START_CMD="python main.py"
  else
    START_CMD="python -m http.server \${PORT}"
  fi

  {
    printf '%s\n' "FROM $BASE_IMAGE_PYTHON"
    printf '%s\n' "WORKDIR /app"
    printf 'COPY %s .\n' "$COPY_FROM"
    if [ -z "$RAILPUSH_SUBDIR" ]; then
      printf '%s\n' "RUN rm -rf .git"
    fi
    printf '%s\n' "RUN if [ -f requirements.txt ]; then pip install --no-cache-dir -r requirements.txt; elif [ -f Pipfile ]; then pip install --no-cache-dir pipenv && pipenv install --deploy --system; elif [ -f pyproject.toml ]; then pip install --no-cache-dir .; fi"
    if [ -n "$BUILD_COMMAND" ]; then
      printf 'RUN %s\n' "$BUILD_COMMAND"
    fi
    printf 'ENV PORT=%s\n' "${PORT}"
    printf '%s\n' "EXPOSE ${PORT}"
    printf 'CMD ["sh", "-c", "%s"]\n' "$START_CMD"
  } > "$DOCKERFILE_PATH"
fi

if [ "$RUNTIME" = "go" ] && [ ! -f "$DOCKERFILE_PATH" ]; then
  echo "RailPush: generating go Dockerfile at $DOCKERFILE_PATH"
  mkdir -p "$(dirname "$DOCKERFILE_PATH")" 2>/dev/null || true

  PORT="${RAILPUSH_PORT:-10000}"
  BUILD_COMMAND="${RAILPUSH_BUILD_COMMAND:-}"
  COPY_FROM="."
  if [ -n "$RAILPUSH_SUBDIR" ]; then
    COPY_FROM="$RAILPUSH_SUBDIR/"
  fi

  {
    printf '%s\n' "FROM $BASE_IMAGE_GO AS build"
    printf '%s\n' "WORKDIR /src"
    printf '%s\n' "RUN apk add --no-cache git ca-certificates"
    printf 'COPY %s .\n' "$COPY_FROM"
    printf '%s\n' "RUN go mod download"
    if [ -n "$BUILD_COMMAND" ]; then
      printf 'RUN %s\n' "$BUILD_COMMAND"
    fi
    printf '%s\n' "RUN CGO_ENABLED=0 go build -o /out/server ."
    printf '\n'
    printf '%s\n' "FROM alpine:3.20"
    printf '%s\n' "RUN apk add --no-cache ca-certificates"
    printf '%s\n' "WORKDIR /app"
    printf '%s\n' "COPY --from=build /out/server ."
    printf 'ENV PORT=%s\n' "${PORT}"
    printf '%s\n' "EXPOSE ${PORT}"
    printf '%s\n' 'CMD ["./server"]'
  } > "$DOCKERFILE_PATH"
fi

# Show the Dockerfile in build logs so users can debug build failures.
if [ -f "$DOCKERFILE_PATH" ]; then
  echo ""
  echo "=== Dockerfile ($DOCKERFILE_PATH) ==="
  cat "$DOCKERFILE_PATH" | sed 's/^/  /'
  echo "=== End Dockerfile ==="
  echo ""
else
  echo "RailPush: Dockerfile not found at $DOCKERFILE_PATH (runtime=$RUNTIME)"
  echo "RailPush: add a Dockerfile to your repo, set a runtime (node/python/go), or set the Dockerfile path."
  echo "RailPush: repo root:"
  ls -la | sed -n '1,120p' || true
  exit 2
fi
`)

		// Build jobs must be resource-bounded to avoid starving the cluster.
		cloneRequests := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		}
		cloneLimits := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		}
		kanikoRequests := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		}
		kanikoLimits := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("4"),
			corev1.ResourceMemory: resource.MustParse("8Gi"),
		}

		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: ns,
				Labels:    labels,
			},
			Spec: batchv1.JobSpec{
				BackoffLimit:            int32Ptr(0),
				ActiveDeadlineSeconds:   int64Ptr(25 * 60),
				TTLSecondsAfterFinished: int32Ptr(3600),
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: labels},
					Spec: corev1.PodSpec{
						PriorityClassName: "railpush-critical",
						RestartPolicy:     corev1.RestartPolicyNever,
						Volumes: []corev1.Volume{
							{
								Name: "workspace",
								VolumeSource: corev1.VolumeSource{
									EmptyDir: &corev1.EmptyDirVolumeSource{},
								},
							},
						},
						InitContainers: []corev1.Container{
							{
								Name:  "clone",
								Image: "alpine/git",
								Env:   buildGitCloneEnvWithOpts(repoURL, branch, commitSHA, secretName, runtime, dfRelFromRepo, svc.BuildCommand, svc.StaticPublishPath, port, dockerfileOverride, svc.BaseImage, svc.BuildInclude, svc.BuildExclude),
								Command: []string{"sh", "-c", cloneScript},
								VolumeMounts: []corev1.VolumeMount{
									{Name: "workspace", MountPath: "/workspace"},
								},
								Resources: corev1.ResourceRequirements{
									Requests: cloneRequests,
									Limits:   cloneLimits,
								},
							},
						},
						Containers: []corev1.Container{
							{
								Name:  "kaniko",
								Image: "gcr.io/kaniko-project/executor:v1.23.2",
								Args: []string{
									"--context=dir://" + effectiveDir,
									"--dockerfile=" + dfRelForKaniko,
									"--destination=" + destImage,
									"--insecure-registry=" + registryHost,
								"--cache=true",
								"--cache-repo=" + registryHost + "/cache",
								},
								VolumeMounts: []corev1.VolumeMount{
									{Name: "workspace", MountPath: "/workspace"},
								},
								Resources: corev1.ResourceRequirements{
									Requests: kanikoRequests,
									Limits:   kanikoLimits,
								},
							},
						},
					},
				},
			},
		}

		if _, err := k.Client.BatchV1().Jobs(ns).Create(ctx, job, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create build job: %w", err)
		}
		if appendLog != nil {
			appendLog("==> Build job created")
		}
	} else if err != nil {
		return fmt.Errorf("get build job: %w", err)
	}

	// Attach the git token Secret (if any) to the build Job as an OwnerReference so GC cleans it up
	// even if the API crashes mid-build. Best-effort delete at the end is still kept.
	if secretName != "" {
		buildJob, err := k.Client.BatchV1().Jobs(ns).Get(ctx, jobName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get build job for git token secret: %w", err)
		}

		// Update the already-created Secret to add OwnerReference for GC.
		sec, err := k.Client.CoreV1().Secrets(ns).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get git token secret for owner ref: %w", err)
		}
		sec.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion: batchv1.SchemeGroupVersion.String(),
				Kind:       "Job",
				Name:       buildJob.Name,
				UID:        buildJob.UID,
			},
		}
		if _, err := k.Client.CoreV1().Secrets(ns).Update(ctx, sec, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("update git token secret owner ref: %w", err)
		}

		defer func() {
			// Best-effort cleanup (OwnerReference is the crash-safety net).
			cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer ccancel()
			_ = k.Client.CoreV1().Secrets(ns).Delete(cctx, secretName, metav1.DeleteOptions{})
		}()
	}

	// Wait for job to complete.
	for {
		select {
		case <-ctx.Done():
			_ = k.appendJobLogs(ns, jobName, appendLog)
			return fmt.Errorf("timeout waiting for build job %s", jobName)
		default:
		}

		j, err := k.Client.BatchV1().Jobs(ns).Get(ctx, jobName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get build job: %w", err)
		}
		if j.Status.Succeeded > 0 {
			_ = k.appendJobLogs(ns, jobName, appendLog)
			return nil
		}
		if j.Status.Failed > 0 {
			_ = k.appendJobLogs(ns, jobName, appendLog)
			return fmt.Errorf("build job failed")
		}
		time.Sleep(2 * time.Second)
	}
}

func buildGitCloneEnv(repoURL string, branch string, commitSHA string, tokenSecretName string) []corev1.EnvVar {
	return buildGitCloneEnvWithOpts(repoURL, branch, commitSHA, tokenSecretName, "", "", "", "", 0, "", "", "", "")
}

func buildGitCloneEnvWithOpts(repoURL string, branch string, commitSHA string, tokenSecretName string, runtime string, dockerfileAbs string, buildCommand string, staticPublishPath string, port int, dockerfileOverride string, baseImage string, buildInclude string, buildExclude string) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{Name: "REPO_URL", Value: strings.TrimSpace(repoURL)},
		{Name: "BRANCH", Value: strings.TrimSpace(branch)},
		{Name: "COMMIT_SHA", Value: strings.TrimSpace(commitSHA)},
	}
	if strings.TrimSpace(tokenSecretName) != "" {
		env = append(env, corev1.EnvVar{
			Name: "GITHUB_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: tokenSecretName},
					Key:                  "token",
				},
			},
		})
	}

	// Non-secret settings for optional Dockerfile generation (static runtime).
	runtime = strings.TrimSpace(runtime)
	if runtime != "" {
		env = append(env, corev1.EnvVar{Name: "RAILPUSH_RUNTIME", Value: runtime})
	}
	dockerfileAbs = strings.TrimSpace(dockerfileAbs)
	if dockerfileAbs != "" {
		env = append(env, corev1.EnvVar{Name: "RAILPUSH_DOCKERFILE_PATH", Value: dockerfileAbs})
	}
	buildCommand = strings.TrimSpace(buildCommand)
	if buildCommand != "" {
		env = append(env, corev1.EnvVar{Name: "RAILPUSH_BUILD_COMMAND", Value: buildCommand})
	}
	staticPublishPath = strings.TrimSpace(staticPublishPath)
	if staticPublishPath != "" {
		env = append(env, corev1.EnvVar{Name: "RAILPUSH_STATIC_PUBLISH_PATH", Value: staticPublishPath})
	}
	if port > 0 {
		env = append(env, corev1.EnvVar{Name: "RAILPUSH_PORT", Value: fmt.Sprintf("%d", port)})
	}
	dockerfileOverride = strings.TrimSpace(dockerfileOverride)
	if dockerfileOverride != "" {
		env = append(env, corev1.EnvVar{Name: "RAILPUSH_DOCKERFILE_CONTENT", Value: dockerfileOverride})
	}
	baseImage = strings.TrimSpace(baseImage)
	if baseImage != "" {
		// Validate to prevent shell injection in generated Dockerfiles (FROM $BASE_IMAGE_*).
		if validDockerImageRef.MatchString(baseImage) {
			env = append(env, corev1.EnvVar{Name: "RAILPUSH_BASE_IMAGE", Value: baseImage})
		}
		// Invalid values are silently dropped — the default base image will be used.
	}
	buildInclude = strings.TrimSpace(buildInclude)
	if buildInclude != "" {
		env = append(env, corev1.EnvVar{Name: "RAILPUSH_BUILD_INCLUDE", Value: buildInclude})
	}
	buildExclude = strings.TrimSpace(buildExclude)
	if buildExclude != "" {
		env = append(env, corev1.EnvVar{Name: "RAILPUSH_BUILD_EXCLUDE", Value: buildExclude})
	}
	return env
}

func (k *KubeDeployer) appendJobLogs(namespace string, jobName string, appendLog func(string)) error {
	if k == nil || k.Client == nil || appendLog == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pods, err := k.Client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + jobName,
	})
	if err != nil {
		return err
	}
	if len(pods.Items) == 0 {
		return nil
	}
	podName := pods.Items[0].Name

	// Best-effort logs from init + main containers.
	for _, c := range []string{"clone", "kaniko"} {
		logs, err := k.Client.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
			Container: c,
		}).DoRaw(ctx)
		if err != nil || len(logs) == 0 {
			continue
		}
		appendLog(fmt.Sprintf("==> [%s] logs:", c))
		for _, line := range strings.Split(string(logs), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			appendLog("    " + line)
		}
	}
	return nil
}
