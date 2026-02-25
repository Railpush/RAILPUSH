package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/railpush/api/config"
	"github.com/railpush/api/services"
)

func hydrateDeployBuildLogFromLoki(cfg *config.Config, deployID string, buildLog string, startedAt, finishedAt *time.Time) string {
	if cfg == nil || !cfg.Kubernetes.Enabled {
		return buildLog
	}

	ns := strings.TrimSpace(cfg.Kubernetes.Namespace)
	if ns == "" {
		ns = "railpush"
	}
	lokiURL := strings.TrimSpace(cfg.Logging.LokiURL)
	if lokiURL == "" {
		lokiURL = "http://loki-gateway.logging.svc.cluster.local"
	}
	if lokiURL == "" {
		return buildLog
	}

	start := time.Now().UTC().Add(-30 * time.Minute)
	if startedAt != nil {
		start = startedAt.Add(-2 * time.Minute)
	}
	end := time.Now().UTC()
	if finishedAt != nil {
		end = finishedAt.Add(5 * time.Minute)
	}
	if end.Sub(start) > 6*time.Hour {
		start = end.Add(-6 * time.Hour)
	}

	hasInlineDBLogs := strings.Contains(buildLog, "\n    ") || strings.HasPrefix(buildLog, "    ")

	buildJobName := services.KubeBuildJobName(deployID)
	if buildJobName != "" && !hasInlineDBLogs && !strings.Contains(buildLog, "==> Build logs (Loki):") {
		buildLog = appendLokiSection(buildLog, lokiURL, fmt.Sprintf(`{namespace=%q, app=%q, component="build", container=~"clone|kaniko"}`,
			ns, buildJobName), start, end, "==> Build logs (Loki):")
	}

	preDeployJobName := services.KubePreDeployJobName(deployID)
	runsPreDeploy := strings.Contains(strings.ToLower(buildLog), "running pre-deploy command")
	hasPreDeployLines := strings.Contains(buildLog, "[predeploy]") || strings.Contains(buildLog, "==> Pre-deploy logs (Loki):")
	if preDeployJobName != "" && runsPreDeploy && !hasPreDeployLines {
		buildLog = appendLokiSection(buildLog, lokiURL, fmt.Sprintf(`{namespace=%q, app=%q, component="predeploy", container=~"predeploy|wait-deps"}`,
			ns, preDeployJobName), start, end, "==> Pre-deploy logs (Loki):")
	}

	return buildLog
}

func appendLokiSection(buildLog string, lokiURL string, logQL string, start, end time.Time, header string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	lines, err := services.LokiQueryRange(ctx, lokiURL, logQL, start, end, 5000)
	cancel()
	if err != nil || len(lines) == 0 {
		return buildLog
	}

	if buildLog != "" && !strings.HasSuffix(buildLog, "\n") {
		buildLog += "\n"
	}
	buildLog += header + "\n"

	const maxBytes = 512 * 1024
	bytes := 0
	for _, ln := range lines {
		container := ""
		if ln.Labels != nil {
			container = strings.TrimSpace(ln.Labels["container"])
		}
		prefix := "    "
		if container != "" {
			prefix = "    [" + container + "] "
		}
		line := prefix + strings.TrimRight(ln.Line, "\r\n")
		if line == "" {
			continue
		}
		if bytes+len(line)+1 > maxBytes {
			buildLog += "    (truncated; view full logs in Grafana Loki)\n"
			break
		}
		buildLog += line + "\n"
		bytes += len(line) + 1
	}

	return buildLog
}
