package services

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/railpush/api/models"
)

func int32Ptr(v int32) *int32 { return &v }

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
func (k *KubeDeployer) BuildImageWithKaniko(deployID string, svc *models.Service, repoURL string, branch string, commitSHA string, dockerContext string, dockerfilePath string, destImage string, githubToken string, appendLog func(string)) error {
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

	runtime := strings.ToLower(strings.TrimSpace(svc.Runtime))
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
		defer func() {
			// Best-effort cleanup.
			cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer ccancel()
			_ = k.Client.CoreV1().Secrets(ns).Delete(cctx, secretName, metav1.DeleteOptions{})
		}()
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
if [ "$RUNTIME" = "static" ] && [ ! -f "$DOCKERFILE_PATH" ]; then
  echo "RailPush: generating static Dockerfile at $DOCKERFILE_PATH"
  mkdir -p "$(dirname "$DOCKERFILE_PATH")" 2>/dev/null || true

  PORT="${RAILPUSH_PORT:-10000}"
  BUILD_COMMAND="${RAILPUSH_BUILD_COMMAND:-npm run build}"
  PUBLISH_PATH="${RAILPUSH_STATIC_PUBLISH_PATH:-dist}"
  PUBLISH_PATH="${PUBLISH_PATH#/}"

  {
    printf '%s\n' "FROM node:20-alpine AS build"
    printf '%s\n' "WORKDIR /app"
    printf '%s\n' "COPY . ."
    printf '%s\n' "RUN $BUILD_COMMAND"
    printf '\n'
    printf '%s\n' "FROM nginx:alpine"
    printf '%s\n' "COPY --from=build /app/$PUBLISH_PATH /usr/share/nginx/html"
    printf '%s\n' "RUN printf 'server {\\n    listen ${PORT};\\n    location / {\\n        root /usr/share/nginx/html;\\n        try_files \$uri \$uri/ /index.html;\\n    }\\n}\\n' > /etc/nginx/conf.d/default.conf"
    printf '%s\n' "EXPOSE ${PORT}"
    printf '%s\n' 'CMD ["nginx", "-g", "daemon off;"]'
  } > "$DOCKERFILE_PATH"
fi

if [ "$RUNTIME" = "node" ] && [ ! -f "$DOCKERFILE_PATH" ]; then
  echo "RailPush: generating node Dockerfile at $DOCKERFILE_PATH"
  mkdir -p "$(dirname "$DOCKERFILE_PATH")" 2>/dev/null || true

  PORT="${RAILPUSH_PORT:-10000}"
  BUILD_COMMAND="${RAILPUSH_BUILD_COMMAND:-}"

  # If the build command already installs deps (npm/yarn/pnpm), don't duplicate work.
  NEEDS_INSTALL="1"
  if [ -n "$BUILD_COMMAND" ]; then
    if echo "$BUILD_COMMAND" | grep -Eq '(^|[[:space:];&|])npm[[:space:]]+(ci|install)([[:space:];&|]|$)'; then
      NEEDS_INSTALL="0"
    elif echo "$BUILD_COMMAND" | grep -Eq '(^|[[:space:];&|])(yarn|pnpm)[[:space:]]+install([[:space:];&|]|$)'; then
      NEEDS_INSTALL="0"
    fi
  fi

  mkdir -p .railpush
  if [ -n "$BUILD_COMMAND" ]; then
    printf '%s\n' '#!/bin/sh' 'set -e' "$BUILD_COMMAND" > .railpush/build.sh
    chmod +x .railpush/build.sh
  fi

  {
    printf '%s\n' "FROM node:20-alpine"
    printf '%s\n' "WORKDIR /app"
    printf '%s\n' "COPY . ."
    printf '%s\n' "RUN rm -rf .git"
    if [ "$NEEDS_INSTALL" = "1" ]; then
      printf '%s\n' "RUN if [ -f package-lock.json ]; then npm ci; else npm install; fi"
    fi
    printf '%s\n' "RUN if [ -f .railpush/build.sh ]; then sh .railpush/build.sh; fi"
    printf '%s\n' "ENV NODE_ENV=production"
    printf '%s\n' "EXPOSE ${PORT}"
    printf '%s\n' "CMD [\"sh\",\"-lc\",\"npm start\"]"
  } > "$DOCKERFILE_PATH"
fi

if [ ! -f "$DOCKERFILE_PATH" ]; then
  echo "RailPush: Dockerfile not found at $DOCKERFILE_PATH (runtime=$RUNTIME)"
  echo "RailPush: add a Dockerfile to your repo or set the Dockerfile path in RailPush."
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
				TTLSecondsAfterFinished: int32Ptr(3600),
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: labels},
					Spec: corev1.PodSpec{
						RestartPolicy: corev1.RestartPolicyNever,
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
								Env:   buildGitCloneEnvWithOpts(repoURL, branch, commitSHA, secretName, runtime, dfRelFromRepo, svc.BuildCommand, svc.StaticPublishPath, port),
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
	return buildGitCloneEnvWithOpts(repoURL, branch, commitSHA, tokenSecretName, "", "", "", "", 0)
}

func buildGitCloneEnvWithOpts(repoURL string, branch string, commitSHA string, tokenSecretName string, runtime string, dockerfileAbs string, buildCommand string, staticPublishPath string, port int) []corev1.EnvVar {
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
