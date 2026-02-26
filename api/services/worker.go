package services

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/railpush/api/config"
	"github.com/railpush/api/models"
	"github.com/railpush/api/utils"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DeployJob struct {
	Deploy      *models.Deploy
	Service     *models.Service
	GitHubToken string
}

type Worker struct {
	Config     *config.Config
	Builder    *Builder
	Deployer   *Deployer
	Router     *Router
	Logger     *Logger
	Emailer    Emailer
	Kube       *KubeDeployer
	kubeOnce   sync.Once
	kubeErr    error
	emailMu    sync.Mutex
	Owner      string
	stopCh     chan struct{}
	Jobs       chan DeployJob
	OnBuildLog func(deployID string, line string) // callback for WebSocket broadcasting
	wg         sync.WaitGroup
}

func (w *Worker) waitForServiceDependencies(svc *models.Service, appendLog func(string)) error {
	if w == nil || w.Config == nil || svc == nil {
		return nil
	}
	vars, err := models.ListEnvVars("service", svc.ID)
	if err != nil || len(vars) == 0 {
		return nil
	}
	dependsRaw := ""
	timeoutSec := 900
	for _, ev := range vars {
		k := strings.ToUpper(strings.TrimSpace(ev.Key))
		if k != "RAILPUSH_DEPENDS_ON" && k != "DEPENDS_ON" && k != "RAILPUSH_DEPENDS_ON_TIMEOUT_SECONDS" {
			continue
		}
		if strings.TrimSpace(ev.EncryptedValue) == "" {
			continue
		}
		val, derr := utils.Decrypt(ev.EncryptedValue, w.Config.Crypto.EncryptionKey)
		if derr != nil {
			continue
		}
		if k == "RAILPUSH_DEPENDS_ON_TIMEOUT_SECONDS" {
			if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil && n > 0 && n <= 7200 {
				timeoutSec = n
			}
			continue
		}
		dependsRaw = strings.TrimSpace(val)
	}
	if dependsRaw == "" {
		return nil
	}

	all, err := models.ListServices(svc.WorkspaceID)
	if err != nil {
		return nil
	}
	byID := map[string]models.Service{}
	byLabel := map[string]models.Service{}
	for _, s := range all {
		byID[strings.TrimSpace(s.ID)] = s
		label := strings.ToLower(strings.TrimSpace(utils.ServiceHostLabel(s.Name, s.Subdomain)))
		if label != "" {
			byLabel[label] = s
		}
		nameKey := strings.ToLower(strings.TrimSpace(s.Name))
		if nameKey != "" {
			byLabel[nameKey] = s
		}
	}

	deps := []models.Service{}
	seen := map[string]struct{}{}
	for _, t := range strings.Split(dependsRaw, ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		dep, ok := byID[t]
		if !ok {
			dep, ok = byLabel[strings.ToLower(t)]
		}
		if !ok {
			return fmt.Errorf("dependency %q not found in workspace", t)
		}
		if dep.ID == svc.ID {
			continue
		}
		if _, ok := seen[dep.ID]; ok {
			continue
		}
		seen[dep.ID] = struct{}{}
		deps = append(deps, dep)
	}
	if len(deps) == 0 {
		return nil
	}

	if appendLog != nil {
		names := make([]string, 0, len(deps))
		for _, d := range deps {
			names = append(names, d.Name)
		}
		appendLog("==> Waiting for dependencies: " + strings.Join(names, ", "))
	}

	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for {
		allReady := true
		for _, d := range deps {
			cur, err := models.GetService(d.ID)
			if err != nil || cur == nil {
				allReady = false
				continue
			}
			if strings.ToLower(strings.TrimSpace(cur.Status)) != "live" {
				allReady = false
			}
		}
		if allReady {
			if appendLog != nil {
				appendLog("==> Dependency check passed")
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("dependency wait timed out after %ds", timeoutSec)
		}
		time.Sleep(3 * time.Second)
	}
}

var internalDiscoveryEnvKeyRe = regexp.MustCompile(`[^A-Za-z0-9_]`)

func internalDiscoveryKey(label string) string {
	label = strings.TrimSpace(label)
	label = strings.ToUpper(label)
	label = strings.ReplaceAll(label, "-", "_")
	label = internalDiscoveryEnvKeyRe.ReplaceAllString(label, "_")
	label = strings.Trim(label, "_")
	if label == "" {
		return "SERVICE"
	}
	if label[0] >= '0' && label[0] <= '9' {
		return "SVC_" + label
	}
	return label
}

// injectKubeInternalDiscoveryEnv publishes stable, cluster-local service endpoints
// into runtime env vars so services can call each other without public ingress hops.
// This is intentionally additive and non-breaking.
func (w *Worker) injectKubeInternalDiscoveryEnv(svc *models.Service, env map[string]string) {
	if w == nil || w.Config == nil || !w.Config.Kubernetes.Enabled || svc == nil || env == nil {
		return
	}
	if strings.TrimSpace(svc.WorkspaceID) == "" || strings.TrimSpace(svc.ID) == "" {
		return
	}

	selfPort := svc.Port
	if selfPort <= 0 {
		selfPort = 10000
	}
	selfHost := kubeServiceName(svc.ID)
	env["RAILPUSH_INTERNAL_HOST"] = selfHost
	env["RAILPUSH_INTERNAL_PORT"] = fmt.Sprintf("%d", selfPort)
	env["RAILPUSH_INTERNAL_URL"] = fmt.Sprintf("http://%s:%d", selfHost, selfPort)
	env["RAILPUSH_INTERNAL_SERVICE_ID"] = strings.TrimSpace(svc.ID)
	if svc.ProjectID != nil && strings.TrimSpace(*svc.ProjectID) != "" {
		env["RAILPUSH_INTERNAL_PROJECT_ID"] = strings.TrimSpace(*svc.ProjectID)
	}
	if svc.EnvironmentID != nil && strings.TrimSpace(*svc.EnvironmentID) != "" {
		env["RAILPUSH_INTERNAL_ENVIRONMENT_ID"] = strings.TrimSpace(*svc.EnvironmentID)
	}

	services, err := models.ListServices(svc.WorkspaceID)
	if err != nil {
		log.Printf("worker: internal discovery list services failed workspace=%s: %v", svc.WorkspaceID, err)
		return
	}

	for _, peer := range services {
		if strings.TrimSpace(peer.ID) == "" {
			continue
		}
		peerPort := peer.Port
		if peerPort <= 0 {
			peerPort = 10000
		}
		peerHost := kubeServiceName(peer.ID)
		label := internalDiscoveryKey(utils.ServiceHostLabel(peer.Name, peer.Subdomain))

		hostKey := "RAILPUSH_SERVICE_" + label + "_HOST"
		portKey := "RAILPUSH_SERVICE_" + label + "_PORT"
		urlKey := "RAILPUSH_SERVICE_" + label + "_URL"
		idKey := "RAILPUSH_SERVICE_" + label + "_ID"
		projectIDKey := "RAILPUSH_SERVICE_" + label + "_PROJECT_ID"
		envIDKey := "RAILPUSH_SERVICE_" + label + "_ENVIRONMENT_ID"

		suffix := ""
		if _, exists := env[hostKey]; exists {
			suffix = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(peer.ID), "-", "_"))
			hostKey = hostKey + "_" + suffix
			portKey = portKey + "_" + suffix
			urlKey = urlKey + "_" + suffix
			idKey = idKey + "_" + suffix
			projectIDKey = projectIDKey + "_" + suffix
			envIDKey = envIDKey + "_" + suffix
		}
		env[hostKey] = peerHost
		env[portKey] = fmt.Sprintf("%d", peerPort)
		env[urlKey] = fmt.Sprintf("http://%s:%d", peerHost, peerPort)
		env[idKey] = strings.TrimSpace(peer.ID)

		if peer.ProjectID != nil && strings.TrimSpace(*peer.ProjectID) != "" {
			env[projectIDKey] = strings.TrimSpace(*peer.ProjectID)
		}
		if peer.EnvironmentID != nil && strings.TrimSpace(*peer.EnvironmentID) != "" {
			env[envIDKey] = strings.TrimSpace(*peer.EnvironmentID)
		}

		idLabel := internalDiscoveryKey(peer.ID)
		env["RAILPUSH_SERVICE_ID_"+idLabel+"_HOST"] = peerHost
		env["RAILPUSH_SERVICE_ID_"+idLabel+"_PORT"] = fmt.Sprintf("%d", peerPort)
		env["RAILPUSH_SERVICE_ID_"+idLabel+"_URL"] = fmt.Sprintf("http://%s:%d", peerHost, peerPort)
	}
}

func (w *Worker) syncDatabaseServiceLinks(databaseID string) {
	if w == nil || w.Config == nil || strings.TrimSpace(databaseID) == "" {
		return
	}
	db, err := models.GetManagedDatabase(databaseID)
	if err != nil || db == nil || strings.TrimSpace(db.EncryptedPassword) == "" {
		return
	}
	pw, err := utils.Decrypt(db.EncryptedPassword, w.Config.Crypto.EncryptionKey)
	if err != nil || strings.TrimSpace(pw) == "" {
		return
	}
	links, err := models.ListServiceDatabaseLinksByDatabase(db.ID)
	if err != nil || len(links) == 0 {
		return
	}
	for _, l := range links {
		svc, err := models.GetService(l.ServiceID)
		if err != nil || svc == nil || svc.WorkspaceID != db.WorkspaceID {
			continue
		}
		host := strings.TrimSpace(db.Host)
		port := db.Port
		if !l.UseInternalURL && db.ExternalPort > 0 {
			extHost := strings.TrimSpace(w.Config.ControlPlane.DBExternalHost)
			if extHost == "" {
				extHost = strings.TrimSpace(w.Config.ControlPlane.Domain)
			}
			if extHost != "" {
				host = extHost
				port = db.ExternalPort
			}
		}
		dbName := strings.TrimSpace(db.DBName)
		if dbName == "" {
			dbName = strings.TrimSpace(db.Name)
		}
		username := strings.TrimSpace(db.Username)
		if username == "" {
			username = strings.TrimSpace(db.Name)
		}
		if host == "" || port <= 0 || dbName == "" || username == "" {
			continue
		}
		url := fmt.Sprintf("postgresql://%s:%s@%s:%d/%s", username, pw, host, port, dbName)
		if !l.UseInternalURL && db.ExternalPort > 0 {
			url += "?sslmode=require"
		}

		items := []struct {
			key    string
			value  string
			secret bool
		}{
			{key: strings.ToUpper(strings.TrimSpace(l.EnvVarName)), value: url, secret: true},
		}
		if strings.EqualFold(strings.TrimSpace(l.EnvVarName), "DATABASE_URL") {
			items = append(items,
				struct {
					key    string
					value  string
					secret bool
				}{key: "DATABASE_HOST", value: host, secret: false},
				struct {
					key    string
					value  string
					secret bool
				}{key: "DATABASE_PORT", value: strconv.Itoa(port), secret: false},
				struct {
					key    string
					value  string
					secret bool
				}{key: "DATABASE_NAME", value: dbName, secret: false},
				struct {
					key    string
					value  string
					secret bool
				}{key: "DATABASE_USER", value: username, secret: false},
				struct {
					key    string
					value  string
					secret bool
				}{key: "DATABASE_PASSWORD", value: pw, secret: true},
			)
		}

		vars := make([]models.EnvVar, 0, len(items))
		for _, item := range items {
			encrypted, err := utils.Encrypt(item.value, w.Config.Crypto.EncryptionKey)
			if err != nil {
				vars = nil
				break
			}
			vars = append(vars, models.EnvVar{Key: item.key, EncryptedValue: encrypted, IsSecret: item.secret})
		}
		if len(vars) == 0 {
			continue
		}
		_ = models.MergeUpsertEnvVars("service", svc.ID, vars)
	}
}

func (w *Worker) getServiceEnvValue(svc *models.Service, key string) string {
	if w == nil || w.Config == nil || svc == nil {
		return ""
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	vars, err := models.ListEnvVars("service", svc.ID)
	if err != nil {
		return ""
	}
	for _, ev := range vars {
		if !strings.EqualFold(strings.TrimSpace(ev.Key), key) {
			continue
		}
		if strings.TrimSpace(ev.EncryptedValue) == "" {
			return ""
		}
		v, err := utils.Decrypt(ev.EncryptedValue, w.Config.Crypto.EncryptionKey)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(v)
	}
	return ""
}

func deployErrorMessage(logText string) string {
	logText = strings.TrimSpace(logText)
	if logText == "" {
		return ""
	}
	lines := strings.Split(logText, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		ln := strings.TrimSpace(lines[i])
		if ln == "" {
			continue
		}
		lower := strings.ToLower(ln)
		if strings.Contains(lower, "error") || strings.Contains(lower, "failed") || strings.Contains(lower, "panic") || strings.Contains(lower, "fatal") {
			if len(ln) > 500 {
				return ln[:500]
			}
			return ln
		}
	}
	if len(lines) > 0 {
		ln := strings.TrimSpace(lines[len(lines)-1])
		if len(ln) > 500 {
			return ln[:500]
		}
		return ln
	}
	return ""
}

func normalizeDeployWebhookEventKey(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "started", "deploy.started":
		return "started"
	case "success", "succeeded", "deploy.success", "deploy.succeeded":
		return "success"
	case "failed", "failure", "deploy.failed":
		return "failed"
	case "rollback", "rolled_back", "deploy.rollback", "deploy.rolled_back":
		return "rollback"
	default:
		return ""
	}
}

func (w *Worker) notifyDeployWebhook(svc *models.Service, deploy *models.Deploy, phase string, ok bool) {
	if w == nil || w.Config == nil || svc == nil || deploy == nil {
		return
	}

	url := w.getServiceEnvValue(svc, "RAILPUSH_EVENT_WEBHOOK_URL")
	if url == "" {
		url = w.getServiceEnvValue(svc, "DEPLOY_WEBHOOK_URL")
	}
	if url == "" {
		url = w.getServiceEnvValue(svc, "RAILPUSH_DEPLOY_WEBHOOK_URL")
	}
	if url == "" {
		return
	}

	eventsRaw := w.getServiceEnvValue(svc, "RAILPUSH_EVENT_WEBHOOK_EVENTS")
	if eventsRaw == "" {
		eventsRaw = w.getServiceEnvValue(svc, "DEPLOY_WEBHOOK_EVENTS")
	}
	allowed := map[string]bool{}
	if strings.TrimSpace(eventsRaw) != "" {
		for _, p := range strings.Split(eventsRaw, ",") {
			e := normalizeDeployWebhookEventKey(p)
			if e != "" {
				allowed[e] = true
			}
		}
	}
	event := normalizeDeployWebhookEventKey(phase)
	if event == "" {
		return
	}
	if len(allowed) > 0 && !allowed[event] {
		return
	}

	status := "started"
	success := false
	if event == "success" {
		status = "success"
		success = true
	} else if event == "failed" {
		status = "failed"
		success = false
	} else if event == "rollback" {
		status = "rollback"
		success = ok
	}

	durationMs := int64(0)
	if deploy.StartedAt != nil {
		end := time.Now().UTC()
		if deploy.FinishedAt != nil {
			end = *deploy.FinishedAt
		}
		if end.After(*deploy.StartedAt) {
			durationMs = end.Sub(*deploy.StartedAt).Milliseconds()
		}
	}

	payload := map[string]interface{}{
		"event":         "deploy." + event,
		"service_id":    svc.ID,
		"service_name":  svc.Name,
		"deploy_id":     deploy.ID,
		"trigger":       deploy.Trigger,
		"status":        status,
		"success":       success,
		"commit_sha":    deploy.CommitSHA,
		"branch":        deploy.Branch,
		"duration_ms":   durationMs,
		"error_message": deployErrorMessage(deploy.BuildLog),
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "railpush-deploy-webhook/1.0")
		secret := w.getServiceEnvValue(svc, "RAILPUSH_EVENT_WEBHOOK_SECRET")
		if secret == "" {
			secret = w.getServiceEnvValue(svc, "DEPLOY_WEBHOOK_SECRET")
		}
		if secret == "" {
			secret = w.getServiceEnvValue(svc, "RAILPUSH_DEPLOY_WEBHOOK_SECRET")
		}
		if secret != "" {
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write(body)
			req.Header.Set("X-RailPush-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("deploy webhook: request failed service=%s deploy=%s event=%s err=%v", svc.ID, deploy.ID, event, err)
			return
		}
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			log.Printf("deploy webhook: non-2xx response service=%s deploy=%s event=%s status=%d", svc.ID, deploy.ID, event, resp.StatusCode)
		}
	}()
}

func workspaceDeployLogURL(cfg *config.Config, serviceID string) string {
	base := strings.TrimSpace(controlPlaneBaseURL(cfg))
	serviceID = strings.TrimSpace(serviceID)
	if base == "" || serviceID == "" {
		return ""
	}
	return base + "/services/" + serviceID + "/logs?type=deploy"
}

func (w *Worker) workspaceDeployNotificationSummary(svc *models.Service, deploy *models.Deploy, event string) string {
	stateLabel := "UNKNOWN"
	switch models.NormalizeWorkspaceNotificationDeployEvent(event) {
	case "success":
		stateLabel = "SUCCESS"
	case "failed":
		stateLabel = "FAILED"
	case "rollback":
		stateLabel = "ROLLBACK"
	}

	serviceName := strings.TrimSpace(svc.Name)
	if serviceName == "" {
		serviceName = strings.TrimSpace(svc.ID)
	}

	lines := []string{fmt.Sprintf("RailPush deploy %s for %s", stateLabel, serviceName)}
	if url := utils.ServicePublicURL(svc.Type, svc.Name, svc.Subdomain, w.Config.Deploy.Domain, 0); strings.TrimSpace(url) != "" {
		lines = append(lines, "Service URL: "+strings.TrimSpace(url))
	}
	if branch := strings.TrimSpace(deploy.Branch); branch != "" {
		lines = append(lines, "Branch: "+branch)
	}
	if sha := strings.TrimSpace(deploy.CommitSHA); sha != "" {
		lines = append(lines, "Commit: "+shortSHA(sha))
	}
	if logsURL := workspaceDeployLogURL(w.Config, svc.ID); logsURL != "" {
		lines = append(lines, "Logs: "+logsURL)
	}
	if models.NormalizeWorkspaceNotificationDeployEvent(event) == "failed" {
		if errLine := deployErrorMessage(deploy.BuildLog); errLine != "" {
			lines = append(lines, "Error: "+errLine)
		}
	}
	return strings.Join(lines, "\n")
}

func (w *Worker) postWorkspaceChannelWebhook(channel string, webhookURL string, body []byte, svc *models.Service, deploy *models.Deploy) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("workspace notification: %s request build failed service=%s deploy=%s err=%v", channel, svc.ID, deploy.ID, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "railpush-workspace-notification/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("workspace notification: %s request failed service=%s deploy=%s err=%v", channel, svc.ID, deploy.ID, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("workspace notification: %s non-2xx service=%s deploy=%s status=%d", channel, svc.ID, deploy.ID, resp.StatusCode)
	}
}

func (w *Worker) notifyWorkspaceNotificationChannels(svc *models.Service, deploy *models.Deploy, phase string, ok bool) {
	if w == nil || w.Config == nil || svc == nil || deploy == nil {
		return
	}
	event := models.NormalizeWorkspaceNotificationDeployEvent(phase)
	if event == "" {
		return
	}

	cfg, err := models.GetWorkspaceNotificationChannels(svc.WorkspaceID)
	if err != nil || cfg == nil {
		return
	}
	if !models.IsWorkspaceNotificationDeployEventEnabled(cfg.DeployEvents, event) {
		return
	}

	slackURL := strings.TrimSpace(cfg.SlackWebhookURL)
	discordURL := strings.TrimSpace(cfg.DiscordWebhookURL)
	if slackURL == "" && discordURL == "" && len(cfg.EmailRecipients) == 0 {
		return
	}

	summary := w.workspaceDeployNotificationSummary(svc, deploy, event)
	if slackURL != "" {
		if payload, err := json.Marshal(map[string]string{"text": summary}); err == nil {
			go w.postWorkspaceChannelWebhook("slack", slackURL, payload, svc, deploy)
		}
	}
	if discordURL != "" {
		if payload, err := json.Marshal(map[string]string{"content": summary}); err == nil {
			go w.postWorkspaceChannelWebhook("discord", discordURL, payload, svc, deploy)
		}
	}

	if !w.Config.Email.Enabled() || len(cfg.EmailRecipients) == 0 {
		return
	}

	subject := ""
	text := ""
	html := ""
	if event == "rollback" {
		serviceName := strings.TrimSpace(svc.Name)
		if serviceName == "" {
			serviceName = strings.TrimSpace(svc.ID)
		}
		subject = "Deploy rollback: " + serviceName
		text = summary + "\n"
		html = emailHTMLShell(
			"Deploy rollback: "+serviceName,
			"RailPush completed a deploy rollback.",
			`<h1 style="margin:0 0 10px 0;font-size:22px;line-height:1.25;">Deploy rollback completed</h1><p style="margin:0;color:#374151;line-height:1.65;">`+strings.ReplaceAll(htmlEscape(summary), "\n", "<br/>")+`</p>`,
		)
	} else {
		subject, text, html = BuildDeployResultEmail(w.Config, svc, deploy, ok)
	}
	for _, recipient := range cfg.EmailRecipients {
		email := strings.ToLower(strings.TrimSpace(recipient))
		if email == "" {
			continue
		}
		dedupe := "deploy-workspace-notification:" + strings.TrimSpace(deploy.ID) + ":" + email
		if _, err := models.EnqueueEmail(dedupe, "deploy_workspace_notification", email, subject, text, html); err != nil {
			log.Printf("workspace notification: email enqueue failed deploy=%s err=%v", deploy.ID, err)
		}
	}
}

func (w *Worker) workspaceGitHubToken(workspaceID string) string {
	if w == nil || w.Config == nil || strings.TrimSpace(workspaceID) == "" {
		return ""
	}
	ws, err := models.GetWorkspace(workspaceID)
	if err != nil || ws == nil {
		return ""
	}
	encToken, err := models.GetUserGitHubToken(ws.OwnerID)
	if err != nil || strings.TrimSpace(encToken) == "" {
		return ""
	}
	ghToken, err := utils.Decrypt(encToken, w.Config.Crypto.EncryptionKey)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(ghToken)
}

func (w *Worker) postGitHubCommitStatus(svc *models.Service, deploy *models.Deploy, state, description string) {
	if w == nil || w.Config == nil || svc == nil || deploy == nil {
		return
	}
	sha := strings.TrimSpace(deploy.CommitSHA)
	if sha == "" {
		return
	}
	ownerRepo := extractGitHubOwnerRepo(svc.RepoURL)
	if ownerRepo == "" {
		return
	}
	ghToken := w.workspaceGitHubToken(svc.WorkspaceID)
	if ghToken == "" {
		return
	}
	state = strings.ToLower(strings.TrimSpace(state))
	switch state {
	case "pending", "success", "failure", "error":
	default:
		return
	}
	targetURL := utils.ServicePublicURL(svc.Type, svc.Name, svc.Subdomain, w.Config.Deploy.Domain, 0)
	payload := map[string]string{
		"state":       state,
		"context":     "railpush/deploy",
		"description": strings.TrimSpace(description),
		"target_url":  targetURL,
	}
	body, _ := json.Marshal(payload)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		url := fmt.Sprintf("https://api.github.com/repos/%s/statuses/%s", ownerRepo, sha)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Authorization", "Bearer "+ghToken)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("github status: request failed service=%s deploy=%s state=%s err=%v", svc.ID, deploy.ID, state, err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			log.Printf("github status: non-2xx service=%s deploy=%s state=%s status=%d", svc.ID, deploy.ID, state, resp.StatusCode)
		}
	}()
}

func NewWorker(cfg *config.Config) *Worker {
	hostname, _ := os.Hostname()
	if strings.TrimSpace(hostname) == "" {
		hostname = "railpush"
	}
	owner := fmt.Sprintf("%s-%d-%d", hostname, os.Getpid(), time.Now().UnixNano())

	return &Worker{
		Config:   cfg,
		Builder:  NewBuilder(cfg),
		Deployer: NewDeployer(cfg),
		Router:   NewRouter(cfg),
		Logger:   NewLogger(cfg),
		Kube:     nil,
		Owner:    owner,
		stopCh:   make(chan struct{}),
		Jobs:     make(chan DeployJob, 100),
	}
}

func (w *Worker) GetKubeDeployer() (*KubeDeployer, error) {
	if w == nil || w.Config == nil {
		return nil, fmt.Errorf("missing config")
	}

	w.kubeOnce.Do(func() {
		kd, err := NewKubeDeployer(w.Config)
		if err != nil {
			w.kubeErr = err
			return
		}
		w.Kube = kd
	})
	if w.Kube != nil {
		return w.Kube, nil
	}
	if w.kubeErr != nil {
		return nil, w.kubeErr
	}
	return nil, fmt.Errorf("kube deployer not initialized")
}

func (w *Worker) GetEmailer() (Emailer, error) {
	if w == nil || w.Config == nil {
		return nil, fmt.Errorf("missing config")
	}
	if w.Emailer != nil {
		return w.Emailer, nil
	}
	w.emailMu.Lock()
	defer w.emailMu.Unlock()
	if w.Emailer != nil {
		return w.Emailer, nil
	}
	if !w.Config.Email.Enabled() {
		return nil, fmt.Errorf("email disabled")
	}
	switch strings.ToLower(strings.TrimSpace(w.Config.Email.Provider)) {
	case "smtp":
		e, err := NewSMTPEmailer(&w.Config.Email)
		if err != nil {
			return nil, err
		}
		w.Emailer = e
		return w.Emailer, nil
	default:
		return nil, fmt.Errorf("unsupported email provider")
	}
}

func (w *Worker) ExternalDatabaseAccessEnabled() bool {
	return w != nil && w.Config != nil && w.Config.Kubernetes.Enabled && w.Config.ControlPlane.DBExternalAccessEnabled
}

func (w *Worker) ExternalDatabaseHost() string {
	if w == nil || w.Config == nil {
		return ""
	}
	host := strings.TrimSpace(w.Config.ControlPlane.DBExternalHost)
	if host != "" {
		return host
	}
	return strings.TrimSpace(w.Config.ControlPlane.Domain)
}

func (w *Worker) EnsureDatabaseExternalEndpoint(ctx context.Context, db *models.ManagedDatabase) (int, error) {
	if !w.ExternalDatabaseAccessEnabled() {
		return 0, fmt.Errorf("external database access is disabled")
	}
	if db == nil || strings.TrimSpace(db.ID) == "" {
		return 0, fmt.Errorf("missing database")
	}
	if strings.EqualFold(strings.TrimSpace(db.Status), "soft_deleted") {
		return 0, fmt.Errorf("database is soft-deleted")
	}
	if strings.TrimSpace(db.Host) == "" || db.Port <= 0 {
		return 0, fmt.Errorf("database endpoint is not ready yet")
	}

	kd, err := w.GetKubeDeployer()
	if err != nil {
		return 0, fmt.Errorf("kube deployer unavailable: %w", err)
	}

	externalPort := db.ExternalPort
	allocatedNow := false
	if externalPort <= 0 {
		port, allocErr := models.AllocateExternalPort(db.ID)
		if allocErr != nil {
			latest, latestErr := models.GetManagedDatabase(db.ID)
			if latestErr == nil && latest != nil && latest.ExternalPort > 0 {
				externalPort = latest.ExternalPort
			} else {
				return 0, fmt.Errorf("allocate external port: %w", allocErr)
			}
		} else {
			externalPort = port
			allocatedNow = true
		}
	}

	if ctx == nil {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ctx = cctx
	} else if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		ctx = cctx
	}

	if err := kd.SetTCPServiceEntry(ctx, externalPort, strings.TrimSpace(db.Host), db.Port); err != nil {
		if allocatedNow {
			_ = models.ClearExternalPort(db.ID)
		}
		return 0, fmt.Errorf("configure tcp proxy: %w", err)
	}

	db.ExternalPort = externalPort
	return externalPort, nil
}

func (w *Worker) reconcileDatabaseExternalEndpoints() {
	if !w.ExternalDatabaseAccessEnabled() {
		return
	}
	dbs, err := models.ListManagedDatabases()
	if err != nil {
		log.Printf("worker: reconcile external database endpoints: list failed: %v", err)
		return
	}
	var ensured int
	for _, item := range dbs {
		if !strings.EqualFold(strings.TrimSpace(item.Status), "available") {
			continue
		}
		full, err := models.GetManagedDatabase(item.ID)
		if err != nil || full == nil {
			continue
		}
		if strings.TrimSpace(full.Host) == "" || full.Port <= 0 {
			continue
		}
		if _, err := w.EnsureDatabaseExternalEndpoint(context.Background(), full); err != nil {
			log.Printf("worker: reconcile external database endpoint %s failed: %v", full.ID, err)
			continue
		}
		w.syncDatabaseServiceLinks(full.ID)
		ensured++
	}
	log.Printf("worker: reconcile external database endpoints ensured=%d", ensured)
}

func (w *Worker) Start(numWorkers int) {
	if w == nil {
		return
	}
	if numWorkers <= 0 {
		numWorkers = 1
	}
	for i := 0; i < numWorkers; i++ {
		w.wg.Add(1)
		go w.run(i)
	}
	w.wg.Add(1)
	go w.pollLoop(numWorkers)
	// Backfill/ensure per-workspace NetworkPolicies in Kubernetes mode (multi-tenant isolation).
	if w.Config != nil && w.Config.Kubernetes.Enabled {
		w.wg.Add(1)
		go w.tenantNetpolLoop()
		if w.ExternalDatabaseAccessEnabled() {
			go w.reconcileDatabaseExternalEndpoints()
		} else {
			// One-time hardening: remove legacy external database TCP proxy exposure.
			go w.revokeDatabaseExternalPorts()
		}
	}
	// Transactional email outbox sender (runs only in worker pods).
	if w.Config != nil && w.Config.Email.Enabled() {
		w.wg.Add(1)
		go w.emailOutboxLoop()
	}
	log.Printf("Deploy worker started with %d workers", numWorkers)
}

func (w *Worker) Stop() {
	if w == nil {
		return
	}
	select {
	case <-w.stopCh:
		// already stopped
	default:
		close(w.stopCh)
	}
	w.wg.Wait()
	log.Println("Deploy worker stopped")
}

func (w *Worker) Enqueue(job DeployJob) {
	// When WORKER_ENABLED=false (common for API/control-plane pods), the in-process
	// worker is intentionally disabled. In that mode, deploys must be picked up by
	// a separate worker Deployment via the durable DB poll loop.
	if w == nil || w.Config == nil || !w.Config.Worker.Enabled || job.Deploy == nil || job.Service == nil {
		return
	}
	// Durable queue: claim the deploy lease before processing so multiple workers/pods
	// won't duplicate work.
	ok, err := models.ClaimDeployLease(job.Deploy.ID, w.Owner, w.Config.Worker.LeaseSeconds, w.Config.Worker.MaxAttempts)
	if err != nil {
		log.Printf("worker enqueue: claim lease failed for deploy=%s: %v", job.Deploy.ID, err)
		return
	}
	if !ok {
		return
	}
	select {
	case w.Jobs <- job:
	case <-w.stopCh:
	}
}

func (w *Worker) run(id int) {
	defer w.wg.Done()
	for {
		select {
		case job := <-w.Jobs:
			if job.Deploy == nil || job.Service == nil {
				continue
			}
			log.Printf("[worker-%d] Processing deploy %s for service %s", id, job.Deploy.ID, job.Service.Name)
			w.processJob(job)
		case <-w.stopCh:
			return
		}
	}
}

func (w *Worker) processJob(job DeployJob) {
	deploy := job.Deploy
	svc := job.Service
	if deploy == nil || svc == nil {
		return
	}

	// Always release the lease after the job completes (success or failure).
	defer func() {
		_ = models.ReleaseDeployLease(deploy.ID, w.Owner)
	}()

	// Keep the lease alive while we process (builds can take longer than the initial lease).
	leaseStop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = models.ExtendDeployLease(deploy.ID, w.Owner, w.Config.Worker.LeaseSeconds)
			case <-leaseStop:
				return
			case <-w.stopCh:
				return
			}
		}
	}()
	defer close(leaseStop)

	// Batch build log persistence to avoid one DB UPDATE per line.
	var logMu sync.Mutex
	var logBuf []string
	var logBytes int
	flushLogs := func() {
		logMu.Lock()
		if len(logBuf) == 0 {
			logMu.Unlock()
			return
		}
		lines := logBuf
		logBuf = nil
		logBytes = 0
		logMu.Unlock()
		chunk := strings.Join(lines, "\n") + "\n"
		if err := models.AppendDeployBuildLogChunk(deploy.ID, chunk); err != nil {
			log.Printf("[deploy:%s] WARNING: failed to persist build logs: %v", deploy.ID[:8], err)
		}
	}
	logStop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				flushLogs()
			case <-logStop:
				flushLogs()
				return
			case <-w.stopCh:
				flushLogs()
				return
			}
		}
	}()
	defer close(logStop)

	appendLog := func(line string) {
		line = RedactSecretsInText(line)
		log.Printf("[deploy:%s] %s", deploy.ID[:8], line)
		// In Kubernetes mode, large build output lives in Loki (via Promtail).
		// Keep only high-level metadata in Postgres to avoid DB bloat.
		persist := true
		if w != nil && w.Config != nil && w.Config.Kubernetes.Enabled {
			// Build output lines are consistently indented (we prefix them with "    ").
			if strings.HasPrefix(line, "    ") {
				persist = false
			}
		}

		if persist {
			shouldFlush := false
			logMu.Lock()
			logBuf = append(logBuf, line)
			logBytes += len(line) + 1
			if len(logBuf) >= 50 || logBytes >= 64*1024 {
				shouldFlush = true
			}
			logMu.Unlock()
			if shouldFlush {
				flushLogs()
			}
		}
		if w.OnBuildLog != nil {
			w.OnBuildLog(deploy.ID, line)
		}
	}

	if err := w.waitForServiceDependencies(svc, appendLog); err != nil {
		appendLog(fmt.Sprintf("ERROR: Dependency wait failed: %v", err))
		w.failDeploy(deploy, svc)
		return
	}

	if w.Config.Kubernetes.Enabled {
		w.processJobKubernetes(job, appendLog)
		return
	}

	// 1. Mark as building
	models.UpdateDeployStatus(deploy.ID, "building")
	models.UpdateServiceStatus(svc.ID, "building", svc.ContainerID, svc.HostPort)
	appendLog("==> Starting build...")

	// Check if this is a rollback with an existing image (skip build)
	imageTag := ""
	skipBuild := false
	if deploy.ImageTag != "" && deploy.Trigger == "rollback" {
		if err := w.Deployer.ExecCommandNoOutput("docker", "image", "inspect", deploy.ImageTag); err == nil {
			appendLog(fmt.Sprintf("==> Rollback: image %s already exists, skipping build", deploy.ImageTag))
			imageTag = deploy.ImageTag
			skipBuild = true
		}
	}

	buildDir := filepath.Join(w.Config.Docker.BuildPath, deploy.ID)
	defer os.RemoveAll(buildDir)

	// Check if this is an image-based deploy (no build needed)
	if !skipBuild && svc.ImageURL != "" {
		appendLog(fmt.Sprintf("==> Using prebuilt image: %s", svc.ImageURL))
		appendLog("==> Pulling image...")
		pullOut, err := w.Deployer.ExecCommand("docker", "pull", svc.ImageURL)
		if err != nil {
			appendLog(fmt.Sprintf("ERROR: Failed to pull image: %v - %s", err, pullOut))
			w.failDeploy(deploy, svc)
			return
		}
		imageTag = svc.ImageURL
		skipBuild = true
		appendLog("==> Image pulled successfully")
	}

	if !skipBuild {
		// 2. Clone repo
		if svc.RepoURL == "" {
			appendLog("ERROR: No repository URL configured")
			w.failDeploy(deploy, svc)
			return
		}

		appendLog(fmt.Sprintf("==> Cloning %s (branch: %s)...", RedactRepoURLCredentials(svc.RepoURL), svc.Branch))
		if err := w.Builder.CloneRepo(svc.RepoURL, svc.Branch, buildDir, job.GitHubToken); err != nil {
			appendLog(fmt.Sprintf("ERROR: Clone failed: %v", err))
			w.failDeploy(deploy, svc)
			return
		}
		appendLog("==> Clone complete")

		// Determine effective source directory (rootDir / DockerContext)
		effectiveDir := buildDir
		if svc.DockerContext != "" {
			effectiveDir = filepath.Join(buildDir, svc.DockerContext)
		}

		// 3. Detect runtime
		runtime := svc.Runtime
		if runtime == "" {
			runtime = w.Builder.DetectRuntime(effectiveDir)
			appendLog(fmt.Sprintf("==> Detected runtime: %s", runtime))
		}
		// For static sites, override runtime
		if svc.Type == "static" {
			runtime = "static"
			appendLog("==> Service type is static, using static site build")
		}

		// 4. Generate Dockerfile if not present
		dockerfilePath := filepath.Join(effectiveDir, "Dockerfile")
		if svc.DockerfilePath != "" {
			dockerfilePath = filepath.Join(buildDir, svc.DockerfilePath)
		}

		// AI Fix: if a Dockerfile override is provided, write it directly.
		if strings.TrimSpace(deploy.DockerfileOverride) != "" {
			appendLog("==> Using AI-fixed Dockerfile")
			if dir := filepath.Dir(dockerfilePath); dir != "" {
				os.MkdirAll(dir, 0755)
			}
			if err := os.WriteFile(dockerfilePath, []byte(deploy.DockerfileOverride), 0644); err != nil {
				appendLog(fmt.Sprintf("ERROR: Failed to write AI-fixed Dockerfile: %v", err))
				w.failDeploy(deploy, svc)
				return
			}
		} else if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
			appendLog("==> No Dockerfile found, generating one...")
			startCmd := svc.StartCommand
			if svc.Type == "static" && svc.StaticPublishPath != "" {
				startCmd = svc.StaticPublishPath
			}
			content := w.Builder.GenerateDockerfile(runtime, svc.BuildCommand, startCmd, svc.Port)
			if err := os.WriteFile(dockerfilePath, []byte(content), 0644); err != nil {
				appendLog(fmt.Sprintf("ERROR: Failed to write Dockerfile: %v", err))
				w.failDeploy(deploy, svc)
				return
			}
			appendLog("==> Dockerfile generated")
		} else {
			appendLog("==> Using existing Dockerfile")
		}

		// 5. Build image
		imageTag = fmt.Sprintf("railpush/%s:%s", utils.ServiceHostLabel(svc.Name, svc.Subdomain), deploy.ID[:8])
		appendLog(fmt.Sprintf("==> Building image %s...", imageTag))

		buildContext := effectiveDir

		buildOutput, err := w.Builder.BuildImageWithLogs(imageTag, buildContext, dockerfilePath)
		if buildOutput != "" {
			for _, line := range strings.Split(buildOutput, "\n") {
				if line = strings.TrimSpace(line); line != "" {
					appendLog("    " + line)
				}
			}
		}
		if err != nil {
			appendLog(fmt.Sprintf("ERROR: Build failed: %v", err))
			appendBuildHints(deploy.BuildLog, appendLog)
			w.failDeploy(deploy, svc)
			return
		}
		appendLog("==> Build complete")
	}

	// 6. Update deploy with image tag
	deploy.ImageTag = imageTag
	models.UpdateDeployStarted(deploy.ID, imageTag)
	models.UpdateDeployStatus(deploy.ID, "deploying")
	models.UpdateServiceStatus(svc.ID, "deploying", svc.ContainerID, svc.HostPort)
	w.notifyDeployWebhook(svc, deploy, "started", true)
	w.postGitHubCommitStatus(svc, deploy, "pending", "RailPush deploy started")
	appendLog("==> Deploying...")

	// 7. Stop old container
	if svc.ContainerID != "" {
		appendLog(fmt.Sprintf("==> Stopping old container %s...", svc.ContainerID))
		w.Deployer.RemoveContainer(svc.ContainerID)
	}
	// Stop and clear any replica containers from previous deploy.
	if prevInstances, err := models.ListServiceInstances(svc.ID); err == nil {
		for _, inst := range prevInstances {
			if inst.ContainerID != "" {
				_ = w.Deployer.RemoveContainer(inst.ContainerID)
			}
		}
	}
	_ = models.DeleteServiceInstancesByService(svc.ID)

	// 8. Run new container with env vars
	envVars, _ := models.ListEnvVars("service", svc.ID)
	cid, port, err := w.Deployer.RunContainerWithEnv(svc, imageTag, envVars, w.Config.Crypto.EncryptionKey)
	if err != nil {
		appendLog(fmt.Sprintf("ERROR: Failed to start container: %v", err))
		w.failDeploy(deploy, svc)
		return
	}
	appendLog(fmt.Sprintf("==> Container started: %s on port %d", cid, port))
	if svc.Instances < 1 {
		svc.Instances = 1
	}
	startedContainerIDs := []string{cid}
	upstreamPorts := []int{port}

	// 8b. Start replica instances if requested.
	publishReplicaPorts := svc.Type == "web" || svc.Type == "static" || svc.Type == "pserv"
	for i := 2; i <= svc.Instances; i++ {
		replicaName := fmt.Sprintf("sr-%s-%02d", svc.ID[:8], i)
		rcid, rport, rerr := w.Deployer.RunContainerWithEnvNamed(
			svc,
			imageTag,
			envVars,
			w.Config.Crypto.EncryptionKey,
			replicaName,
			publishReplicaPorts,
		)
		if rerr != nil {
			appendLog(fmt.Sprintf("WARNING: failed to start replica #%d: %v", i, rerr))
			continue
		}
		_ = models.CreateServiceInstance(&models.ServiceInstance{
			ServiceID:   svc.ID,
			ContainerID: rcid,
			HostPort:    rport,
			Role:        "replica",
			Status:      "live",
		})
		if rport > 0 {
			upstreamPorts = append(upstreamPorts, rport)
		}
		startedContainerIDs = append(startedContainerIDs, rcid)
		appendLog(fmt.Sprintf("==> Replica #%d started: %s on port %d", i, rcid, rport))
	}

	// 9. Health check
	requiresHealthCheck := svc.Type == "web" || svc.Type == "static" || svc.Type == "pserv"
	if requiresHealthCheck {
		appendLog("==> Running health check...")
		healthPath := svc.HealthCheckPath
		if healthPath == "" {
			healthPath = "/"
		}
		if ok := w.Deployer.HealthCheck("localhost", port, healthPath); !ok {
			appendLog("ERROR: Health check failed; rolling back deploy")
			for _, startedCID := range startedContainerIDs {
				if strings.TrimSpace(startedCID) != "" {
					_ = w.Deployer.RemoveContainer(startedCID)
				}
			}
			_ = models.DeleteServiceInstancesByService(svc.ID)
			w.failDeployWithReason(deploy, svc, "health_check_failed")
			return
		}
		appendLog("==> Health check passed")
	} else {
		appendLog("==> Skipping health check for non-HTTP service type")
	}

	// 10. Update Caddy route if domain is configured
	if (svc.Type == "web" || svc.Type == "static" || svc.Type == "pserv") &&
		!w.Config.Deploy.DisableRouter &&
		w.Router != nil &&
		w.Config.Deploy.Domain != "" &&
		w.Config.Deploy.Domain != "localhost" {
		domain := fmt.Sprintf("%s.%s", utils.ServiceHostLabel(svc.Name, svc.Subdomain), w.Config.Deploy.Domain)
		appendLog(fmt.Sprintf("==> Adding route: %s -> ports=%v", domain, upstreamPorts))
		if err := w.Router.AddRouteUpstreams(domain, upstreamPorts); err != nil {
			appendLog(fmt.Sprintf("WARNING: Failed to add Caddy route: %v", err))
		}
	}

	// 10b. Update Caddy routes for any custom domains and flag TLS provisioning state.
	if !w.Config.Deploy.DisableRouter && w.Router != nil {
		customDomains, err := models.ListCustomDomains(svc.ID)
		if err != nil {
			appendLog(fmt.Sprintf("WARNING: Failed to load custom domains: %v", err))
		} else {
			for _, cd := range customDomains {
				host := strings.ToLower(strings.TrimSpace(cd.Domain))
				if host == "" {
					continue
				}

				appendLog(fmt.Sprintf("==> Adding custom domain route: %s -> ports=%v", host, upstreamPorts))
				if err := w.Router.AddRouteUpstreams(host, upstreamPorts); err != nil {
					appendLog(fmt.Sprintf("WARNING: Failed to add custom domain route for %s: %v", host, err))
					_ = models.SetCustomDomainTLSProvisioned(svc.ID, host, false)
					continue
				}

				if err := models.SetCustomDomainTLSProvisioned(svc.ID, host, true); err != nil {
					appendLog(fmt.Sprintf("WARNING: Route added but failed to mark TLS for %s: %v", host, err))
				}
			}
		}
	}

	// 11. Mark as live
	models.UpdateServiceStatus(svc.ID, "live", cid, port)
	models.UpdateDeployStatus(deploy.ID, "live")
	appendLog(fmt.Sprintf("==> Deploy complete! Service is live on port %d", port))
	w.notifyDeployResult(svc, deploy, true)

	// 12. Start log tailing in background
	go w.Logger.TailContainer(cid)
}

func (w *Worker) processJobKubernetes(job DeployJob, appendLog func(string)) {
	deploy := job.Deploy
	svc := job.Service

	if deploy == nil || svc == nil {
		return
	}

	// 1. Mark as building
	_ = models.UpdateDeployStatus(deploy.ID, "building")
	_ = models.UpdateServiceStatus(svc.ID, "building", svc.ContainerID, 0)
	appendLog("==> Kubernetes deploy: preparing...")

	// Determine image tag and build strategy.
	imageTag := ""
	needsBuild := false
	if strings.TrimSpace(deploy.ImageTag) != "" && deploy.Trigger == "rollback" {
		imageTag = strings.TrimSpace(deploy.ImageTag)
		appendLog(fmt.Sprintf("==> Rollback: using existing image %s", imageTag))
	} else if strings.TrimSpace(svc.ImageURL) != "" {
		imageTag = strings.TrimSpace(svc.ImageURL)
		appendLog(fmt.Sprintf("==> Using prebuilt image: %s", imageTag))
	} else {
		registry := strings.TrimSuffix(strings.TrimSpace(w.Config.Docker.Registry), "/")
		if registry == "" {
			appendLog("ERROR: Kubernetes git builds require DOCKER_REGISTRY (e.g. 91.98.183.19:5000/railpush)")
			w.failDeploy(deploy, svc)
			return
		}
		if strings.TrimSpace(svc.RepoURL) == "" {
			appendLog("ERROR: No repository URL configured")
			w.failDeploy(deploy, svc)
			return
		}
		repoName := "svc-" + strings.ToLower(strings.TrimSpace(svc.ID))
		imageTag = fmt.Sprintf("%s/%s:%s", registry, repoName, strings.ToLower(strings.TrimSpace(deploy.ID)))
		needsBuild = true
		appendLog(fmt.Sprintf("==> Kubernetes build: will build and push %s", imageTag))
	}

	// 2. Update deploy with image tag (and started_at)
	deploy.ImageTag = imageTag
	_ = models.UpdateDeployStarted(deploy.ID, imageTag)
	w.notifyDeployWebhook(svc, deploy, "started", true)
	w.postGitHubCommitStatus(svc, deploy, "pending", "RailPush deploy started")

	// 3. Resolve env vars (decrypt secrets) and always include PORT.
	//    Merge linked env group vars first (lower priority), then service vars (higher priority).
	env := map[string]string{}

	// 3a. Load env group vars (earlier-created groups win on key conflict).
	groupIDs, _ := models.ListLinkedEnvGroupIDs(svc.ID)
	for _, gid := range groupIDs {
		groupVars, err := models.ListEnvVars("env_group", gid)
		if err != nil {
			log.Printf("worker: list env group vars failed for group=%s service=%s: %v", gid, svc.ID, err)
			continue
		}
		for _, ev := range groupVars {
			key := strings.TrimSpace(ev.Key)
			if key == "" || strings.TrimSpace(ev.EncryptedValue) == "" {
				continue
			}
			if _, exists := env[key]; exists {
				continue // earlier group already set this key
			}
			decrypted, err := utils.Decrypt(ev.EncryptedValue, w.Config.Crypto.EncryptionKey)
			if err != nil {
				log.Printf("worker: decrypt env group var failed for group=%s key=%s: %v", gid, key, err)
				continue
			}
			env[key] = decrypted
		}
	}

	// 3b. Load service-level vars (override any group vars with the same key).
	rawEnv, _ := models.ListEnvVars("service", svc.ID)
	for _, ev := range rawEnv {
		key := strings.TrimSpace(ev.Key)
		if key == "" {
			continue
		}
		if strings.TrimSpace(ev.EncryptedValue) == "" {
			continue
		}
		decrypted, err := utils.Decrypt(ev.EncryptedValue, w.Config.Crypto.EncryptionKey)
		if err != nil {
			log.Printf("worker: decrypt env var failed for service=%s key=%s: %v", svc.ID, key, err)
			continue
		}
		env[key] = decrypted
	}
	if svc.Port <= 0 {
		svc.Port = 10000
	}
	env["PORT"] = fmt.Sprintf("%d", svc.Port)
	w.injectKubeInternalDiscoveryEnv(svc, env)

	// Warn about development-mode start commands that waste resources and crash in production.
	if cmd := strings.ToLower(strings.TrimSpace(svc.StartCommand)); cmd != "" {
		devPatterns := map[string]string{
			"next dev":          "next start",
			"nuxt dev":          "nuxt start",
			"vite dev":          "vite preview",
			"npm run dev":       "npm start (with a production start script)",
			"yarn dev":          "yarn start (with a production start script)",
			"pnpm dev":          "pnpm start (with a production start script)",
			"flask run --debug": "gunicorn",
			"nodemon":           "node",
		}
		for pattern, suggestion := range devPatterns {
			if strings.Contains(cmd, pattern) {
				appendLog(fmt.Sprintf("WARNING: Your start command contains '%s' which runs in development mode. "+
					"Dev mode uses significantly more CPU and memory, and is not suitable for production. "+
					"Consider using '%s' instead.", pattern, suggestion))
				break
			}
		}
	}

	kd, err := w.GetKubeDeployer()
	if err != nil {
		appendLog(fmt.Sprintf("ERROR: Failed to initialize Kubernetes client: %v", err))
		w.failDeploy(deploy, svc)
		return
	}

	if needsBuild {
		appendLog("==> Starting Kubernetes build job...")
		if err := kd.BuildImageWithKaniko(deploy.ID, svc, svc.RepoURL, svc.Branch, deploy.CommitSHA, svc.DockerContext, svc.DockerfilePath, imageTag, job.GitHubToken, deploy.DockerfileOverride, appendLog); err != nil {
			appendLog(fmt.Sprintf("ERROR: Build failed: %v", err))
			appendBuildHints(deploy.BuildLog, appendLog)
			w.failDeploy(deploy, svc)
			return
		}
		appendLog("==> Build complete")
	}

	// Run pre-deploy command (e.g. database migrations) before deploying.
	if cmd := strings.TrimSpace(svc.PreDeployCommand); cmd != "" {
		appendLog("==> Running pre-deploy command: " + cmd)
		if err := kd.RunPreDeployJob(deploy.ID, svc, imageTag, env, cmd, appendLog); err != nil {
			appendLog(fmt.Sprintf("ERROR: Pre-deploy command failed: %v", err))
			w.failDeploy(deploy, svc)
			return
		}
		appendLog("==> Pre-deploy command complete")
	}

	_ = models.UpdateDeployStatus(deploy.ID, "deploying")
	_ = models.UpdateServiceStatus(svc.ID, "deploying", svc.ContainerID, 0)
	appendLog("==> Applying Kubernetes resources...")

	switch strings.ToLower(strings.TrimSpace(svc.Type)) {
	case "cron", "cron_job":
		cronName, err := kd.DeployCronJob(job.Deploy.ID, svc, imageTag, env)
		if err != nil {
			appendLog(fmt.Sprintf("ERROR: Kubernetes cron deploy failed: %v", err))
			w.failDeploy(deploy, svc)
			return
		}
		_ = models.UpdateServiceStatus(svc.ID, "live", "k8s-cron:"+cronName, 0)
		_ = models.UpdateDeployStatus(deploy.ID, "live")
		appendLog("==> Deploy complete! CronJob is scheduled.")
		w.notifyDeployResult(svc, deploy, true)
	default:
		deploymentName, err := kd.DeployService(job.Deploy.ID, svc, imageTag, env)
		if err != nil {
			appendLog(fmt.Sprintf("ERROR: Kubernetes deploy failed: %v", err))
			w.failDeploy(deploy, svc)
			return
		}

		appendLog("==> Waiting for rollout...")
		if err := kd.WaitForServiceReady(deploymentName, svc); err != nil {
			appendLog(fmt.Sprintf("ERROR: Rollout failed: %v", err))
			w.failDeployWithReason(deploy, svc, "rollout_failed")
			return
		}

		_ = models.UpdateServiceStatus(svc.ID, "live", "k8s:"+deploymentName, 0)
		_ = models.UpdateDeployStatus(deploy.ID, "live")
		appendLog("==> Deploy complete! Service is live.")
		w.notifyDeployResult(svc, deploy, true)
	}
}

func (w *Worker) pollLoop(batchSize int) {
	defer w.wg.Done()
	if w == nil || w.Config == nil {
		return
	}

	pollEvery := time.Duration(w.Config.Worker.PollIntervalMS) * time.Millisecond
	if pollEvery <= 0 {
		pollEvery = 1 * time.Second
	}
	if pollEvery < 250*time.Millisecond {
		pollEvery = 250 * time.Millisecond
	}
	if batchSize <= 0 {
		batchSize = 1
	}

	pollTicker := time.NewTicker(pollEvery)
	defer pollTicker.Stop()
	staleTicker := time.NewTicker(60 * time.Second)
	defer staleTicker.Stop()
	managedTicker := time.NewTicker(60 * time.Second)
	defer managedTicker.Stop()

	for {
		select {
		case <-pollTicker.C:
			w.pollOnce(batchSize)
		case <-staleTicker.C:
			if n, err := models.MarkStaleDeploysFailed(w.Config.Worker.MaxAttempts); err != nil {
				log.Printf("worker: failed to mark stale deploys: %v", err)
			} else if n > 0 {
				log.Printf("worker: marked %d stale deploy(s) as failed (max attempts)", n)
			}
		case <-managedTicker.C:
			w.reconcileManagedResources()
		case <-w.stopCh:
			return
		}
	}
}

func (w *Worker) reconcileManagedResources() {
	if w == nil || w.Config == nil || !w.Config.Kubernetes.Enabled {
		return
	}
	// Best-effort reconciliation so users don't get stuck with broken managed DB/KV resources after
	// restarts or transient failures.
	w.reconcileManagedDatabases()
	w.reconcileManagedKeyValues()
}

func (w *Worker) tenantNetpolLoop() {
	defer w.wg.Done()
	if w == nil || w.Config == nil || !w.Config.Kubernetes.Enabled {
		return
	}

	// Run once shortly after startup to backfill existing workspaces.
	time.Sleep(2 * time.Second)
	w.reconcileTenantNetworkPoliciesOnce()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.reconcileTenantNetworkPoliciesOnce()
		case <-w.stopCh:
			return
		}
	}
}

func (w *Worker) reconcileTenantNetworkPoliciesOnce() {
	if w == nil || w.Config == nil || !w.Config.Kubernetes.Enabled {
		return
	}
	kd, err := w.GetKubeDeployer()
	if err != nil {
		log.Printf("worker: reconcile tenant networkpolicies: kube deployer init failed: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := kd.ReconcileTenantNetworkPolicies(ctx); err != nil {
		log.Printf("worker: reconcile tenant networkpolicies failed: %v", err)
	}
}

func (w *Worker) reconcileManagedDatabases() {
	kd, err := w.GetKubeDeployer()
	if err != nil {
		log.Printf("worker: reconcile databases: kube deployer init failed: %v", err)
		return
	}
	dbs, err := models.ListManagedDatabases()
	if err != nil {
		log.Printf("worker: reconcile databases: list failed: %v", err)
		return
	}
	for _, d := range dbs {
		status := strings.ToLower(strings.TrimSpace(d.Status))
		if status == "soft_deleted" {
			continue
		}
		name := kubeManagedDatabaseName(d.ID)
		tlsSecName := name + "-tls"
		shouldEnsure := status != "available"
		if !shouldEnsure {
			// Existing databases created before TLS support (or during partial rollouts)
			// may be "available" but still reject SSL connections. Detect drift and re-ensure.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, secErr := kd.Client.CoreV1().Secrets(kd.namespace()).Get(ctx, tlsSecName, metav1.GetOptions{})
			cancel()
			if secErr != nil {
				shouldEnsure = true
			} else {
				ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
				ss, ssErr := kd.Client.AppsV1().StatefulSets(kd.namespace()).Get(ctx, name, metav1.GetOptions{})
				cancel()
				if ssErr != nil || ss == nil {
					shouldEnsure = true
				} else {
					hasInit := false
					for _, c := range ss.Spec.Template.Spec.InitContainers {
						if c.Name == "init-postgres-tls" {
							hasInit = true
							break
						}
					}
					hasTLSVol := false
					hasTLSSrcVol := false
					for _, v := range ss.Spec.Template.Spec.Volumes {
						if v.Name == "tls" {
							hasTLSVol = true
						}
						if v.Name == "tls-src" {
							hasTLSSrcVol = true
						}
					}
					hasSSLOpt := false
					hasTLSMount := false
					for _, c := range ss.Spec.Template.Spec.Containers {
						if c.Name != "postgres" {
							continue
						}
						for _, a := range c.Args {
							if a == "ssl=on" {
								hasSSLOpt = true
								break
							}
						}
						for _, m := range c.VolumeMounts {
							if m.Name == "tls" {
								hasTLSMount = true
								break
							}
						}
						break
					}
					if !(hasInit && hasTLSVol && hasTLSSrcVol && hasTLSMount && hasSSLOpt) {
						shouldEnsure = true
					}
				}
			}
		}
		if !shouldEnsure {
			continue
		}

		full, err := models.GetManagedDatabase(d.ID)
		if err != nil || full == nil || strings.TrimSpace(full.EncryptedPassword) == "" {
			continue
		}
		pw, err := utils.Decrypt(full.EncryptedPassword, w.Config.Crypto.EncryptionKey)
		if err != nil || strings.TrimSpace(pw) == "" {
			continue
		}

		name, err = kd.EnsureManagedDatabase(full, pw)
		if err != nil {
			log.Printf("worker: reconcile databases: ensure %s failed: %v", full.ID, err)
			continue
		}
		_ = models.UpdateManagedDatabaseConnection(full.ID, 5432, name)
		full.Host = name
		full.Port = 5432
		if w.ExternalDatabaseAccessEnabled() {
			if _, err := w.EnsureDatabaseExternalEndpoint(context.Background(), full); err != nil {
				log.Printf("worker: reconcile databases: external endpoint ensure %s failed: %v", full.ID, err)
			}
		}
		w.syncDatabaseServiceLinks(full.ID)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ss, getErr := kd.Client.AppsV1().StatefulSets(kd.namespace()).Get(ctx, name, metav1.GetOptions{})
		cancel()
		if getErr == nil && ss != nil && ss.Status.ReadyReplicas >= 1 {
			_ = models.UpdateManagedDatabaseStatus(full.ID, "available", "k8s:"+name)
		}
	}
}

func (w *Worker) reconcileManagedKeyValues() {
	kd, err := w.GetKubeDeployer()
	if err != nil {
		log.Printf("worker: reconcile keyvalues: kube deployer init failed: %v", err)
		return
	}
	kvs, err := models.ListManagedKeyValues()
	if err != nil {
		log.Printf("worker: reconcile keyvalues: list failed: %v", err)
		return
	}
	for _, kv := range kvs {
		status := strings.ToLower(strings.TrimSpace(kv.Status))
		if status == "available" || status == "soft_deleted" {
			continue
		}

		full, err := models.GetManagedKeyValue(kv.ID)
		if err != nil || full == nil || strings.TrimSpace(full.EncryptedPassword) == "" {
			continue
		}
		pw, err := utils.Decrypt(full.EncryptedPassword, w.Config.Crypto.EncryptionKey)
		if err != nil || strings.TrimSpace(pw) == "" {
			continue
		}

		name, err := kd.EnsureManagedKeyValue(full, pw)
		if err != nil {
			log.Printf("worker: reconcile keyvalues: ensure %s failed: %v", full.ID, err)
			continue
		}
		_ = models.UpdateManagedKeyValueConnection(full.ID, 6379, name)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ss, getErr := kd.Client.AppsV1().StatefulSets(kd.namespace()).Get(ctx, name, metav1.GetOptions{})
		cancel()
		if getErr == nil && ss != nil && ss.Status.ReadyReplicas >= 1 {
			_ = models.UpdateManagedKeyValueStatus(full.ID, "available", "k8s:"+name)
		}
	}
}

func (w *Worker) pollOnce(batchSize int) {
	if w == nil || w.Config == nil {
		return
	}
	deploys, err := models.ClaimExpiredDeploys(w.Owner, batchSize, w.Config.Worker.LeaseSeconds, w.Config.Worker.MaxAttempts)
	if err != nil {
		log.Printf("worker: claim deploys failed: %v", err)
		return
	}
	for i := range deploys {
		d := deploys[i] // copy
		svc, err := models.GetService(d.ServiceID)
		if err != nil || svc == nil {
			_ = models.UpdateDeployStatus(d.ID, "failed")
			_ = models.ReleaseDeployLease(d.ID, w.Owner)
			continue
		}
		ghToken := w.resolveGitHubToken(&d, svc)
		job := DeployJob{Deploy: &d, Service: svc, GitHubToken: ghToken}
		select {
		case w.Jobs <- job:
		case <-w.stopCh:
			return
		}
	}
}

func (w *Worker) resolveGitHubToken(deploy *models.Deploy, svc *models.Service) string {
	if w == nil || w.Config == nil {
		return ""
	}
	userID := ""
	if deploy != nil && deploy.CreatedBy != nil {
		userID = strings.TrimSpace(*deploy.CreatedBy)
	}
	if userID == "" && svc != nil {
		if ws, err := models.GetWorkspace(svc.WorkspaceID); err == nil && ws != nil {
			userID = strings.TrimSpace(ws.OwnerID)
		}
	}
	if userID == "" {
		return ""
	}
	encToken, err := models.GetUserGitHubToken(userID)
	if err != nil || strings.TrimSpace(encToken) == "" {
		return ""
	}
	t, err := utils.Decrypt(encToken, w.Config.Crypto.EncryptionKey)
	if err != nil {
		return ""
	}
	return t
}

// appendBuildHints scans the build log for common error patterns and appends
// actionable fix suggestions. Helps users self-serve instead of filing support tickets.
func appendBuildHints(buildLog string, appendLog func(string)) {
	if appendLog == nil || buildLog == "" {
		return
	}
	lower := strings.ToLower(buildLog)
	var hints []string

	// Missing package.json — wrong docker context or rootDir
	if strings.Contains(lower, "enoent") && strings.Contains(lower, "package.json") {
		hints = append(hints, "HINT: package.json not found. Your build context may point to the wrong directory. Set 'rootDir' or 'dockerContext' in your railpush.yaml to the folder containing package.json.")
	}
	// npm ci without lock file
	if strings.Contains(lower, "npm ci can only install") && strings.Contains(lower, "package-lock.json") {
		hints = append(hints, "HINT: npm ci requires a package-lock.json file. Either add one to your repo (run 'npm install' locally and commit the lock file) or change your build command to 'npm install'.")
	}
	// Missing env var at runtime (common crash)
	if strings.Contains(lower, "missing") && (strings.Contains(lower, "api key") || strings.Contains(lower, "api_key") || strings.Contains(lower, "secret")) {
		hints = append(hints, "HINT: Your app is crashing because a required environment variable (API key/secret) is missing. Set it in the RailPush dashboard under Environment > Environment Variables.")
	}
	// Python missing module
	if strings.Contains(lower, "modulenotfounderror") || strings.Contains(lower, "no module named") {
		hints = append(hints, "HINT: A Python module is missing. Ensure your requirements.txt lists all dependencies, and your Dockerfile runs 'pip install -r requirements.txt'.")
	}
	// Port already in use / EADDRINUSE
	if strings.Contains(lower, "eaddrinuse") || strings.Contains(lower, "address already in use") {
		hints = append(hints, "HINT: Port conflict. Ensure your app listens on the port specified in your service config (default: 10000). Use the PORT environment variable: process.env.PORT or os.environ['PORT'].")
	}
	// Permission denied on file
	if strings.Contains(lower, "permission denied") && (strings.Contains(lower, "chmod") || strings.Contains(lower, "/app/") || strings.Contains(lower, "eacces")) {
		hints = append(hints, "HINT: Permission denied. If your Dockerfile uses a non-root user, ensure file ownership is correct (use 'COPY --chown=...' or 'RUN chown ...').")
	}
	// Out of memory
	if strings.Contains(lower, "out of memory") || strings.Contains(lower, "javascript heap") || strings.Contains(lower, "enomem") {
		hints = append(hints, "HINT: Build ran out of memory. Try adding 'NODE_OPTIONS=--max-old-space-size=4096' as a build environment variable, or upgrade to a larger plan.")
	}
	// Dockerfile not found
	if strings.Contains(lower, "error: could not find or read dockerfile") || (strings.Contains(lower, "no such file") && strings.Contains(lower, "dockerfile")) {
		hints = append(hints, "HINT: Dockerfile not found. Check that 'dockerfilePath' in your railpush.yaml points to the correct location relative to the repo root.")
	}

	for _, h := range hints {
		appendLog(h)
	}
}

func (w *Worker) failDeploy(deploy *models.Deploy, svc *models.Service) {
	w.failDeployWithReason(deploy, svc, "")
}

func (w *Worker) failDeployWithReason(deploy *models.Deploy, svc *models.Service, reason string) {
	models.UpdateDeployStatus(deploy.ID, "failed")
	models.UpdateServiceStatus(svc.ID, "deploy_failed", svc.ContainerID, svc.HostPort)
	w.notifyDeployResult(svc, deploy, false)
	w.maybeAutoRollback(svc, deploy, reason)

	// Auto-retry if this deploy was triggered by an AI fix session.
	if deploy.Trigger == "ai_fix" {
		go w.maybeRetryAIFix(svc, deploy)
	}
}

func shouldAutoRollbackReason(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "health_check_failed", "rollout_failed":
		return true
	default:
		return false
	}
}

func selectRollbackDeployCandidate(deploys []models.Deploy, failed *models.Deploy) *models.Deploy {
	if failed == nil {
		return nil
	}
	failedID := strings.TrimSpace(failed.ID)
	failedImage := strings.TrimSpace(failed.ImageTag)
	for i := range deploys {
		d := deploys[i]
		if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.ID) == failedID {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(d.Status), "live") {
			continue
		}
		candidateImage := strings.TrimSpace(d.ImageTag)
		if candidateImage == "" || candidateImage == failedImage {
			continue
		}
		candidate := d
		return &candidate
	}
	return nil
}

func (w *Worker) maybeAutoRollback(svc *models.Service, failed *models.Deploy, reason string) {
	if w == nil || svc == nil || failed == nil {
		return
	}
	if !shouldAutoRollbackReason(reason) {
		return
	}
	if strings.EqualFold(strings.TrimSpace(failed.Trigger), "rollback") {
		return
	}
	failedImage := strings.TrimSpace(failed.ImageTag)
	if failedImage == "" {
		if fresh, err := models.GetDeploy(failed.ID); err == nil && fresh != nil {
			failedImage = strings.TrimSpace(fresh.ImageTag)
			failed.ImageTag = failedImage
			if failed.CreatedBy == nil {
				failed.CreatedBy = fresh.CreatedBy
			}
		}
	}
	if failedImage == "" {
		return
	}

	deploys, err := models.ListDeploys(svc.ID)
	if err != nil || len(deploys) == 0 {
		return
	}
	candidate := selectRollbackDeployCandidate(deploys, failed)
	if candidate == nil {
		return
	}

	rollback := &models.Deploy{
		ServiceID:     svc.ID,
		Trigger:       "rollback",
		CommitSHA:     candidate.CommitSHA,
		CommitMessage: candidate.CommitMessage,
		Branch:        candidate.Branch,
		ImageTag:      candidate.ImageTag,
	}
	if failed.CreatedBy != nil {
		if createdBy := strings.TrimSpace(*failed.CreatedBy); createdBy != "" {
			rollback.CreatedBy = &createdBy
		}
	}
	if err := models.CreateDeploy(rollback); err != nil {
		log.Printf("worker: auto rollback create deploy failed service=%s failed_deploy=%s err=%v", svc.ID, failed.ID, err)
		return
	}

	_ = models.UpdateDeployBuildLog(failed.ID, fmt.Sprintf("==> Auto-rollback queued: deploy %s using image %s from deploy %s", rollback.ID, rollback.ImageTag, candidate.ID))

	actorID := ""
	if failed.CreatedBy != nil {
		actorID = strings.TrimSpace(*failed.CreatedBy)
	}
	Audit(svc.WorkspaceID, actorID, "deploy.auto_rollback_triggered", "deploy", rollback.ID, map[string]interface{}{
		"failed_deploy_id": failed.ID,
		"failed_image":     failedImage,
		"rollback_to":      candidate.ID,
		"rollback_image":   candidate.ImageTag,
		"reason":           strings.TrimSpace(reason),
	})

	w.Enqueue(DeployJob{Deploy: rollback, Service: svc, GitHubToken: ""})
}

func (w *Worker) maybeRetryAIFix(svc *models.Service, deploy *models.Deploy) {
	session, err := models.GetActiveAIFixSessionForService(svc.ID)
	if err != nil || session == nil {
		return
	}
	if session.CurrentAttempt >= session.MaxAttempts {
		_ = models.UpdateAIFixSessionStatus(session.ID, "exhausted")
		return
	}
	// Wait for logs to flush before retrying.
	time.Sleep(5 * time.Second)
	aiFixer := NewAIFixService(w.Config)
	if err := aiFixer.AttemptFix(session, w); err != nil {
		log.Printf("ai_fix: retry attempt failed for service %s: %v", svc.ID, err)
		_ = models.UpdateAIFixSessionStatus(session.ID, "error")
	}
}

func (w *Worker) notifyDeployResult(svc *models.Service, deploy *models.Deploy, ok bool) {
	if w == nil || w.Config == nil || svc == nil || deploy == nil {
		return
	}

	// Mark AI fix session as success when the deploy succeeds.
	if ok && deploy.Trigger == "ai_fix" {
		go func() {
			session, err := models.GetActiveAIFixSessionForService(svc.ID)
			if err == nil && session != nil {
				_ = models.UpdateAIFixSessionStatus(session.ID, "success")
			}
		}()
	}

	// Post a comment on the GitHub PR with the preview URL when a preview deploy succeeds.
	if ok && deploy.Trigger == "preview" {
		go w.postGitHubPRComment(svc, deploy)
	}

	if fresh, err := models.GetDeploy(deploy.ID); err == nil && fresh != nil {
		deploy = fresh
	}
	phase := "failed"
	ghState := "failure"
	ghDesc := "RailPush deploy failed"
	if ok {
		if strings.EqualFold(strings.TrimSpace(deploy.Trigger), "rollback") {
			phase = "rollback"
			ghState = "success"
			ghDesc = "RailPush rollback deployed"
		} else {
			phase = "success"
			ghState = "success"
			ghDesc = "RailPush deploy live"
		}
	}
	w.notifyDeployWebhook(svc, deploy, phase, ok)
	w.postGitHubCommitStatus(svc, deploy, ghState, ghDesc)
	w.notifyWorkspaceNotificationChannels(svc, deploy, phase, ok)

	if !w.Config.Email.Enabled() {
		return
	}
	// Best-effort only: never block deploy completion.
	go func() {
		ws, err := models.GetWorkspace(svc.WorkspaceID)
		if err != nil || ws == nil {
			return
		}
		u, err := models.GetUserByID(ws.OwnerID)
		if err != nil || u == nil || strings.TrimSpace(u.Email) == "" {
			return
		}

		// Reload deploy so email includes started_at/branch populated by DB updates.
		fresh, _ := models.GetDeploy(deploy.ID)
		if fresh != nil {
			deploy = fresh
		}

		subj, text, html := BuildDeployResultEmail(w.Config, svc, deploy, ok)
		dedupe := "deploy-result:" + strings.TrimSpace(deploy.ID)
		if _, err := models.EnqueueEmail(dedupe, "deploy_result", u.Email, subj, text, html); err != nil {
			// Avoid logging recipient PII.
			log.Printf("email enqueue failed: type=deploy_result deploy=%s err=%v", deploy.ID, err)
		}
	}()
}

// postGitHubPRComment posts a comment on the GitHub PR with the preview URL.
func (w *Worker) postGitHubPRComment(svc *models.Service, deploy *models.Deploy) {
	if w == nil || w.Config == nil || svc == nil || deploy == nil {
		return
	}

	pe, err := models.GetPreviewEnvironmentByServiceID(svc.ID)
	if err != nil || pe == nil || pe.PRNumber == 0 {
		return
	}

	// Extract owner/repo from the repository clone URL.
	// Supports: https://github.com/owner/repo.git or https://github.com/owner/repo
	ownerRepo := extractGitHubOwnerRepo(pe.Repository)
	if ownerRepo == "" {
		return
	}

	// Get the workspace owner's GitHub token.
	ghToken := w.workspaceGitHubToken(svc.WorkspaceID)
	if ghToken == "" {
		return
	}

	previewURL := utils.ServicePublicURL(svc.Type, svc.Name, svc.Subdomain, w.Config.Deploy.Domain, 0)
	if previewURL == "" {
		return
	}

	body := fmt.Sprintf(
		"### Preview Deploy Ready\n\n"+
			"| | |\n|---|---|\n"+
			"| **Preview URL** | %s |\n"+
			"| **Commit** | `%s` |\n"+
			"| **Branch** | `%s` |\n\n"+
			"Deployed by [RailPush](https://%s)",
		previewURL,
		deploy.CommitSHA,
		deploy.Branch,
		w.Config.Deploy.Domain,
	)

	commentURL := fmt.Sprintf("https://api.github.com/repos/%s/issues/%d/comments", ownerRepo, pe.PRNumber)
	payload, _ := json.Marshal(map[string]string{"body": body})
	req, err := http.NewRequest("POST", commentURL, bytes.NewReader(payload))
	if err != nil {
		log.Printf("preview PR comment: failed to create request: %v", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+ghToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("preview PR comment: request failed: %v", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("preview PR comment: GitHub API returned %d for %s PR#%d", resp.StatusCode, ownerRepo, pe.PRNumber)
		return
	}
	log.Printf("preview PR comment: posted to %s PR#%d", ownerRepo, pe.PRNumber)

	// Update preview environment status.
	_ = models.CreateOrUpdatePreviewEnvironment(&models.PreviewEnvironment{
		WorkspaceID: svc.WorkspaceID,
		ServiceID:   &svc.ID,
		Repository:  pe.Repository,
		PRNumber:    pe.PRNumber,
		PRTitle:     pe.PRTitle,
		PRBranch:    pe.PRBranch,
		BaseBranch:  pe.BaseBranch,
		CommitSHA:   deploy.CommitSHA,
		Status:      "live",
	})
}

// extractGitHubOwnerRepo extracts "owner/repo" from a GitHub clone URL.
func extractGitHubOwnerRepo(repoURL string) string {
	repoURL = strings.TrimSpace(repoURL)
	repoURL = strings.TrimSuffix(repoURL, ".git")
	// https://github.com/owner/repo
	if idx := strings.Index(repoURL, "github.com/"); idx >= 0 {
		parts := strings.SplitN(repoURL[idx+len("github.com/"):], "/", 3)
		if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
			return parts[0] + "/" + parts[1]
		}
	}
	return ""
}

func (w *Worker) emailOutboxLoop() {
	defer w.wg.Done()
	if w == nil || w.Config == nil || !w.Config.Email.Enabled() {
		return
	}

	pollEvery := time.Duration(w.Config.Email.Outbox.PollIntervalMS) * time.Millisecond
	if pollEvery <= 0 {
		pollEvery = 2 * time.Second
	}
	if pollEvery < 250*time.Millisecond {
		pollEvery = 250 * time.Millisecond
	}
	batchSize := w.Config.Email.Outbox.BatchSize
	if batchSize <= 0 {
		batchSize = 10
	}
	leaseSeconds := w.Config.Email.Outbox.LeaseSeconds
	if leaseSeconds <= 0 {
		leaseSeconds = 120
	}
	maxAttempts := w.Config.Email.Outbox.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 10
	}

	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.emailOutboxOnce(batchSize, leaseSeconds, maxAttempts)
		case <-w.stopCh:
			return
		}
	}
}

func (w *Worker) emailOutboxOnce(batchSize int, leaseSeconds int, maxAttempts int) {
	if w == nil || w.Config == nil || !w.Config.Email.Enabled() {
		return
	}
	emailer, err := w.GetEmailer()
	if err != nil || emailer == nil {
		return
	}

	msgs, err := models.ClaimEmailOutboxBatch(w.Owner, batchSize, leaseSeconds, maxAttempts)
	if err != nil || len(msgs) == 0 {
		return
	}

	for _, m := range msgs {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		sendErr := emailer.Send(ctx, EmailMessage{
			To:       m.ToEmail,
			Subject:  m.Subject,
			TextBody: m.BodyText,
			HTMLBody: m.BodyHTML,
		})
		cancel()

		if sendErr == nil {
			_ = models.MarkEmailOutboxSent(m.ID, w.Owner)
			log.Printf("email sent: type=%s id=%s", strings.TrimSpace(m.MessageType), m.ID[:8])
			continue
		}

		// Backoff with jitter; cap to 60 minutes.
		attempt := m.Attempts
		if attempt < 1 {
			attempt = 1
		}
		shift := attempt - 1
		if shift > 6 {
			shift = 6
		}
		delay := 15 * time.Second * time.Duration(1<<shift)
		if delay > 60*time.Minute {
			delay = 60 * time.Minute
		}
		// 0-10s jitter.
		jitter := time.Duration(time.Now().UnixNano()%int64(10*time.Second)) * time.Nanosecond
		delay += jitter

		errMsg := sendErr.Error()
		if m.Attempts >= maxAttempts {
			_ = models.MarkEmailOutboxDead(m.ID, w.Owner, errMsg)
			log.Printf("email dead: type=%s id=%s err=%v", strings.TrimSpace(m.MessageType), m.ID[:8], sendErr)
			continue
		}
		_ = models.MarkEmailOutboxRetry(m.ID, w.Owner, errMsg, delay)
		log.Printf("email retry: type=%s id=%s err=%v", strings.TrimSpace(m.MessageType), m.ID[:8], sendErr)
	}
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func (w *Worker) ensurePostgresTLSCert(idPrefix string) (string, error) {
	if w == nil || w.Config == nil {
		return "", fmt.Errorf("missing config")
	}
	idPrefix = strings.TrimSpace(idPrefix)
	if idPrefix == "" {
		return "", fmt.Errorf("missing idPrefix")
	}

	baseDir := strings.TrimSpace(w.Config.Deploy.DataDir)
	if baseDir == "" {
		baseDir = "/var/lib/railpush"
	}
	certDir := filepath.Join(baseDir, "db-certs", idPrefix)
	if err := os.MkdirAll(certDir, 0755); err != nil {
		return "", err
	}

	keyPath := filepath.Join(certDir, "server.key")
	crtPath := filepath.Join(certDir, "server.crt")
	if fileExists(keyPath) && fileExists(crtPath) {
		return certDir, nil
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", err
	}

	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return "", err
	}
	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: fmt.Sprintf("sr-db-%s", idPrefix),
		},
		NotBefore:             now.Add(-10 * time.Minute),
		NotAfter:              now.Add(3650 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{fmt.Sprintf("sr-db-%s", idPrefix)},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return "", err
	}

	keyFile, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return "", err
	}
	if err := pem.Encode(keyFile, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}); err != nil {
		keyFile.Close()
		return "", err
	}
	keyFile.Close()

	crtFile, err := os.OpenFile(crtPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return "", err
	}
	if err := pem.Encode(crtFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		crtFile.Close()
		return "", err
	}
	crtFile.Close()

	// Postgres alpine images run as uid/gid 70.
	_ = os.Chown(keyPath, 70, 70)
	_ = os.Chown(crtPath, 70, 70)
	_ = os.Chmod(keyPath, 0600)
	_ = os.Chmod(crtPath, 0644)

	return certDir, nil
}

// ProvisionDatabase spins up a real PostgreSQL container
func (w *Worker) ProvisionDatabase(db *models.ManagedDatabase, password string) {
	go func() {
		log.Printf("Provisioning database %s...", db.Name)
		models.UpdateManagedDatabaseStatus(db.ID, "creating", "")

		// Kubernetes mode: provision a StatefulSet + Service so in-cluster apps can connect via
		// `sr-db-<idPrefix>:5432` (Render-compatible).
		if w != nil && w.Config != nil && w.Config.Kubernetes.Enabled {
			kd, err := w.GetKubeDeployer()
			if err != nil {
				log.Printf("Failed to initialize kube deployer for database %s: %v", db.Name, err)
				_ = models.UpdateManagedDatabaseStatus(db.ID, "failed", "")
				return
			}
			name, err := kd.EnsureManagedDatabase(db, password)
			if err != nil {
				log.Printf("Failed to provision database %s in k8s: %v", db.Name, err)
				_ = models.UpdateManagedDatabaseStatus(db.ID, "failed", "")
				return
			}
			if err := kd.WaitForStatefulSetReady(name, 1); err != nil {
				log.Printf("Database %s did not become ready in time (k8s): %v", db.Name, err)
				_ = models.UpdateManagedDatabaseStatus(db.ID, "failed", "k8s:"+name)
				return
			}
			_ = models.UpdateManagedDatabaseStatus(db.ID, "available", "k8s:"+name)
			_ = models.UpdateManagedDatabaseConnection(db.ID, 5432, name)
			db.Host = name
			db.Port = 5432
			if w.ExternalDatabaseAccessEnabled() {
				if _, err := w.EnsureDatabaseExternalEndpoint(context.Background(), db); err != nil {
					log.Printf("Database %s external endpoint setup failed: %v", db.Name, err)
				}
			}
			w.syncDatabaseServiceLinks(db.ID)
			log.Printf("Database %s provisioned successfully in k8s (%s)", db.Name, name)

			// Run init script if specified (one-time, on first provision only).
			if initScript := strings.TrimSpace(db.InitScript); initScript != "" {
				log.Printf("Database %s: running init script (%d bytes)", db.Name, len(initScript))
				if err := kd.RunDatabaseInitScript(db, password, initScript); err != nil {
					log.Printf("Database %s: init script failed: %v (database is still available)", db.Name, err)
				} else {
					log.Printf("Database %s: init script completed successfully", db.Name)
				}
			}
			return
		}

		containerName := fmt.Sprintf("sr-db-%s", db.ID[:8])
		port := w.Deployer.findFreePort()
		certDir, certErr := w.ensurePostgresTLSCert(db.ID[:8])
		if certErr != nil {
			log.Printf("WARNING: Failed to create DB TLS certs for %s: %v (continuing without SSL)", db.Name, certErr)
			certDir = ""
		}

		args := []string{
			"run", "-d",
			"--name", containerName,
			"--network", w.Config.Docker.Network,
			"-e", fmt.Sprintf("POSTGRES_DB=%s", db.DBName),
			"-e", fmt.Sprintf("POSTGRES_USER=%s", db.Username),
			"-e", fmt.Sprintf("POSTGRES_PASSWORD=%s", password),
			"-p", fmt.Sprintf("127.0.0.1:%d:5432", port),
			"-v", fmt.Sprintf("sr-db-%s:/var/lib/postgresql/data", db.ID[:8]),
		}
		if certDir != "" {
			args = append(args, "-v", fmt.Sprintf("%s:/etc/postgres-ssl:ro", certDir))
		}
		args = append(args,
			fmt.Sprintf("postgres:%d-alpine", db.PGVersion),
			"postgres",
			"-c", "wal_level=replica",
			"-c", "archive_mode=on",
			"-c", "archive_timeout=60",
			"-c", "archive_command=mkdir -p /var/lib/postgresql/data/wal-archive && test ! -f /var/lib/postgresql/data/wal-archive/%f && cp %p /var/lib/postgresql/data/wal-archive/%f",
		)
		if certDir != "" {
			args = append(args,
				"-c", "ssl=on",
				"-c", "ssl_cert_file=/etc/postgres-ssl/server.crt",
				"-c", "ssl_key_file=/etc/postgres-ssl/server.key",
			)
		}

		out, err := w.Deployer.ExecCommand("docker", args...)
		if err != nil {
			log.Printf("Failed to provision database %s: %v - %s", db.Name, err, out)
			models.UpdateManagedDatabaseStatus(db.ID, "failed", "")
			return
		}

		cid := strings.TrimSpace(out)
		if len(cid) > 12 {
			cid = cid[:12]
		}

		// Wait for PostgreSQL to be ready
		ready := false
		for i := 0; i < 30; i++ {
			time.Sleep(time.Second)
			if err := w.Deployer.ExecCommandNoOutput("docker", "exec", containerName, "pg_isready", "-U", db.Username); err == nil {
				ready = true
				break
			}
		}

		if !ready {
			log.Printf("Database %s did not become ready in time", db.Name)
			models.UpdateManagedDatabaseStatus(db.ID, "failed", cid)
			return
		}

		models.UpdateManagedDatabaseStatus(db.ID, "available", cid)
		models.UpdateManagedDatabaseConnection(db.ID, port, "localhost")
		w.syncDatabaseServiceLinks(db.ID)
		log.Printf("Database %s provisioned successfully on port %d", db.Name, port)

		// Run init script if specified (one-time, on first provision only).
		if initScript := strings.TrimSpace(db.InitScript); initScript != "" {
			log.Printf("Database %s: running init script (%d bytes, docker mode)", db.Name, len(initScript))
			out, err := w.Deployer.ExecCommand("docker", "exec",
				"-e", fmt.Sprintf("PGPASSWORD=%s", password),
				containerName,
				"psql", "-U", db.Username, "-d", db.DBName, "-c", initScript,
			)
			if err != nil {
				log.Printf("Database %s: init script failed: %v (output: %s)", db.Name, err, out)
			} else {
				log.Printf("Database %s: init script completed successfully", db.Name)
			}
		}
	}()
}

// ProvisionDatabaseReplica spins up a read-only PostgreSQL replica container.
// This provides low-latency read endpoints and HA standby primitives.
func (w *Worker) ProvisionDatabaseReplica(primary *models.ManagedDatabase, replica *models.DatabaseReplica, password string) {
	go func() {
		log.Printf("Provisioning database replica %s for primary %s...", replica.Name, primary.Name)
		_ = models.UpdateDatabaseReplicaStatus(replica.ID, "creating", "", "", 0)

		containerName := fmt.Sprintf("sr-db-rep-%s", replica.ID[:8])
		port := w.Deployer.findFreePort()
		certDir, certErr := w.ensurePostgresTLSCert(replica.ID[:8])
		if certErr != nil {
			log.Printf("WARNING: Failed to create DB replica TLS certs for %s: %v (continuing without SSL)", replica.Name, certErr)
			certDir = ""
		}

		args := []string{
			"run", "-d",
			"--name", containerName,
			"--network", w.Config.Docker.Network,
			"-e", fmt.Sprintf("POSTGRES_DB=%s", primary.DBName),
			"-e", fmt.Sprintf("POSTGRES_USER=%s", primary.Username),
			"-e", fmt.Sprintf("POSTGRES_PASSWORD=%s", password),
			"-p", fmt.Sprintf("127.0.0.1:%d:5432", port),
			"-v", fmt.Sprintf("sr-db-rep-%s:/var/lib/postgresql/data", replica.ID[:8]),
		}
		if certDir != "" {
			args = append(args, "-v", fmt.Sprintf("%s:/etc/postgres-ssl:ro", certDir))
		}
		args = append(args, fmt.Sprintf("postgres:%d-alpine", primary.PGVersion))
		args = append(args, "postgres")
		if certDir != "" {
			args = append(args,
				"-c", "ssl=on",
				"-c", "ssl_cert_file=/etc/postgres-ssl/server.crt",
				"-c", "ssl_key_file=/etc/postgres-ssl/server.key",
			)
		}
		args = append(args, "-c", "default_transaction_read_only=on")

		out, err := w.Deployer.ExecCommand("docker", args...)
		if err != nil {
			log.Printf("Failed to provision database replica %s: %v - %s", replica.Name, err, out)
			_ = models.UpdateDatabaseReplicaStatus(replica.ID, "failed", "", "", 0)
			return
		}
		cid := strings.TrimSpace(out)
		if len(cid) > 12 {
			cid = cid[:12]
		}

		ready := false
		for i := 0; i < 40; i++ {
			time.Sleep(time.Second)
			if err := w.Deployer.ExecCommandNoOutput("docker", "exec", containerName, "pg_isready", "-U", primary.Username); err == nil {
				ready = true
				break
			}
		}

		if !ready {
			log.Printf("Database replica %s did not become ready in time", replica.Name)
			_ = models.UpdateDatabaseReplicaStatus(replica.ID, "failed", cid, "localhost", port)
			return
		}

		// Best-effort snapshot seed from primary to replica.
		primaryContainerName := fmt.Sprintf("sr-db-%s", primary.ID[:8])
		dumpOut, dumpErr := w.Deployer.ExecCommand(
			"docker", "exec",
			"-e", fmt.Sprintf("PGPASSWORD=%s", password),
			primaryContainerName,
			"pg_dump", "-U", primary.Username, "-d", primary.DBName, "--clean", "--if-exists",
		)
		if dumpErr == nil && strings.TrimSpace(dumpOut) != "" {
			cmd := exec.Command("docker", "exec", "-i",
				"-e", fmt.Sprintf("PGPASSWORD=%s", password),
				containerName,
				"psql", "-U", primary.Username, "-d", primary.DBName,
			)
			cmd.Stdin = strings.NewReader(dumpOut)
			if seedOut, err := cmd.CombinedOutput(); err != nil {
				log.Printf("Replica seed warning for %s: %v - %s", replica.Name, err, string(seedOut))
			}
		}

		_ = models.UpdateDatabaseReplicaStatus(replica.ID, "available", cid, "localhost", port)
		log.Printf("Database replica %s available on port %d", replica.Name, port)
	}()
}

// ProvisionKeyValue spins up a real Redis container
func (w *Worker) ProvisionKeyValue(kv *models.ManagedKeyValue, password string) {
	go func() {
		log.Printf("Provisioning key-value store %s...", kv.Name)
		models.UpdateManagedKeyValueStatus(kv.ID, "creating", "")

		// Kubernetes mode: provision a StatefulSet + Service so in-cluster apps can connect via
		// `sr-kv-<idPrefix>:6379`.
		if w != nil && w.Config != nil && w.Config.Kubernetes.Enabled {
			kd, err := w.GetKubeDeployer()
			if err != nil {
				log.Printf("Failed to initialize kube deployer for keyvalue %s: %v", kv.Name, err)
				_ = models.UpdateManagedKeyValueStatus(kv.ID, "failed", "")
				return
			}
			name, err := kd.EnsureManagedKeyValue(kv, password)
			if err != nil {
				log.Printf("Failed to provision keyvalue %s in k8s: %v", kv.Name, err)
				_ = models.UpdateManagedKeyValueStatus(kv.ID, "failed", "")
				return
			}
			if err := kd.WaitForStatefulSetReady(name, 1); err != nil {
				log.Printf("Keyvalue %s did not become ready in time (k8s): %v", kv.Name, err)
				_ = models.UpdateManagedKeyValueStatus(kv.ID, "failed", "k8s:"+name)
				return
			}
			_ = models.UpdateManagedKeyValueStatus(kv.ID, "available", "k8s:"+name)
			_ = models.UpdateManagedKeyValueConnection(kv.ID, 6379, name)
			log.Printf("Key-value store %s provisioned successfully in k8s (%s)", kv.Name, name)
			return
		}

		containerName := fmt.Sprintf("sr-kv-%s", kv.ID[:8])
		port := w.Deployer.findFreePort()

		args := []string{
			"run", "-d",
			"--name", containerName,
			"--network", w.Config.Docker.Network,
			"-p", fmt.Sprintf("127.0.0.1:%d:6379", port),
			"-v", fmt.Sprintf("sr-kv-%s:/data", kv.ID[:8]),
			"redis:7-alpine",
			"redis-server",
			"--requirepass", password,
			"--maxmemory-policy", kv.MaxmemoryPolicy,
		}

		out, err := w.Deployer.ExecCommand("docker", args...)
		if err != nil {
			log.Printf("Failed to provision key-value %s: %v - %s", kv.Name, err, out)
			models.UpdateManagedKeyValueStatus(kv.ID, "failed", "")
			return
		}

		cid := strings.TrimSpace(out)
		if len(cid) > 12 {
			cid = cid[:12]
		}

		// Wait for Redis to be ready
		ready := false
		for i := 0; i < 15; i++ {
			time.Sleep(time.Second)
			checkOut, _ := w.Deployer.ExecCommand("docker", "exec", containerName, "redis-cli", "-a", password, "ping")
			if strings.Contains(checkOut, "PONG") {
				ready = true
				break
			}
		}

		if !ready {
			log.Printf("Key-value store %s did not become ready in time", kv.Name)
			models.UpdateManagedKeyValueStatus(kv.ID, "failed", cid)
			return
		}

		models.UpdateManagedKeyValueStatus(kv.ID, "available", cid)
		models.UpdateManagedKeyValueConnection(kv.ID, port, "localhost")
		log.Printf("Key-value store %s provisioned successfully on port %d", kv.Name, port)
	}()
}

// revokeDatabaseExternalPorts removes legacy external database TCP proxy entries
// and clears stored external_port assignments. Databases remain reachable only via
// internal service networking.
func (w *Worker) revokeDatabaseExternalPorts() {
	if w == nil || w.Config == nil || !w.Config.Kubernetes.Enabled {
		return
	}
	kd, err := w.GetKubeDeployer()
	if err != nil || kd == nil {
		log.Printf("revokeDatabaseExternalPorts: kube deployer not available: %v", err)
		return
	}
	dbs, err := models.ListManagedDatabases()
	if err != nil {
		log.Printf("revokeDatabaseExternalPorts: list databases failed: %v", err)
		return
	}
	var revoked int
	for _, db := range dbs {
		if db.ExternalPort <= 0 {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := kd.RemoveTCPServiceEntry(ctx, db.ExternalPort); err != nil {
			log.Printf("revokeDatabaseExternalPorts: remove tcp entry for %s port %d: %v", db.Name, db.ExternalPort, err)
		}
		cancel()
		if err := models.ClearExternalPort(db.ID); err != nil {
			log.Printf("revokeDatabaseExternalPorts: clear external port for %s: %v", db.Name, err)
			continue
		}
		revoked++
		log.Printf("revokeDatabaseExternalPorts: disabled external access for database %s (port %d)", db.Name, db.ExternalPort)
	}
	if revoked > 0 {
		log.Printf("revokeDatabaseExternalPorts: removed external access from %d databases", revoked)
	}
	log.Printf("revokeDatabaseExternalPorts: complete (%d databases checked)", len(dbs))
}
