package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gorilla/mux"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

const (
	serviceExecMaxOutputBytes        = 64 * 1024

	serviceShellDefaultIdleMinutes = 30
	serviceShellMaxIdleMinutes     = 120
	serviceShellMaxSessions        = 3
	serviceShellDefaultTimeout     = 60
	serviceShellMaxTimeout         = 300

	serviceShellMaxEnvVars      = 512
	serviceShellMaxEnvValueSize = 8192

	serviceFSDefaultPath      = "/app"
	serviceFSDefaultListLimit = 200
	serviceFSMaxListLimit     = 1000
	serviceFSDefaultReadBytes = 10 * 1024
	serviceFSMaxReadBytes     = 256 * 1024
	serviceFSDefaultSearchMax = 200
	serviceFSMaxSearchMax     = 1000
)

var shellEnvKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var serviceFSAllowedRoots = []string{"/app", "/tmp", "/workspace", "/srv"}

type createShellSessionRequest struct {
	IdleTimeoutMinutes int    `json:"idle_timeout_minutes"`
	WorkingDirectory   string `json:"working_directory"`
}

type shellExecRequest struct {
	Command                string `json:"command"`
	TimeoutSeconds         int    `json:"timeout_seconds"`
	AcknowledgeRiskyCommand bool   `json:"acknowledge_risky_command"`
	Reason                 string `json:"reason"`
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func truncateOutput(raw string, maxBytes int) (string, bool) {
	if maxBytes <= 0 {
		maxBytes = serviceExecMaxOutputBytes
	}
	if len(raw) <= maxBytes {
		return raw, false
	}
	return raw[:maxBytes], true
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func normalizeFSPath(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		p = serviceFSDefaultPath
	}
	if !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("path must be absolute")
	}
	p = path.Clean(p)
	allowed := false
	for _, root := range serviceFSAllowedRoots {
		if p == root || strings.HasPrefix(p, root+"/") {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("path must be under /app, /tmp, /workspace, or /srv")
	}
	return p, nil
}

func normalizeShellEnv(raw map[string]string) map[string]string {
	if len(raw) == 0 {
		return map[string]string{}
	}
	out := map[string]string{}
	for k, v := range raw {
		key := strings.TrimSpace(k)
		if key == "" || !shellEnvKeyRe.MatchString(key) {
			continue
		}
		if len(v) > serviceShellMaxEnvValueSize {
			continue
		}
		out[key] = v
		if len(out) >= serviceShellMaxEnvVars {
			break
		}
	}
	return out
}

func envFromDump(raw string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		key := line[:idx]
		value := line[idx+1:]
		if !shellEnvKeyRe.MatchString(key) {
			continue
		}
		if len(value) > serviceShellMaxEnvValueSize {
			continue
		}
		out[key] = value
		if len(out) >= serviceShellMaxEnvVars {
			break
		}
	}
	return out
}

func buildShellExecScript(command, cwd string, env map[string]string, token string) string {
	begin := "__RAILPUSH_SHELL_META_BEGIN_" + token + "__"
	end := "__RAILPUSH_SHELL_META_END_" + token + "__"
	cmdB64 := base64.StdEncoding.EncodeToString([]byte(command))

	parts := []string{"set +e"}
	if strings.TrimSpace(cwd) != "" {
		parts = append(parts, "cd "+shellQuote(cwd)+" 2>/dev/null || exit 91")
	}
	cleanEnv := normalizeShellEnv(env)
	keys := make([]string, 0, len(cleanEnv))
	for k := range cleanEnv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, "export "+key+"="+shellQuote(cleanEnv[key]))
	}
	parts = append(parts,
		"__rp_cmd=$(printf %s "+shellQuote(cmdB64)+" | base64 -d 2>/dev/null)",
		"if [ -z \"$__rp_cmd\" ]; then __rp_cmd=$(echo "+shellQuote(cmdB64)+" | base64 -d); fi",
		"eval \"$__rp_cmd\"",
		"__rp_exit=$?",
		"__rp_cwd_b64=$(pwd | tr -d '\\n' | base64 | tr -d '\\n')",
		"__rp_env_b64=$(env | base64 | tr -d '\\n')",
		"printf '\\n"+begin+"\\n%s\\n%s\\n%s\\n"+end+"\\n' \"$__rp_exit\" \"$__rp_cwd_b64\" \"$__rp_env_b64\"",
		"exit $__rp_exit",
	)
	return strings.Join(parts, "; ")
}

func parseShellExecMetadata(stdout, token string) (userStdout string, exitCode int, cwd string, env map[string]string, err error) {
	begin := "__RAILPUSH_SHELL_META_BEGIN_" + token + "__"
	end := "__RAILPUSH_SHELL_META_END_" + token + "__"
	beginIdx := strings.LastIndex(stdout, begin)
	if beginIdx == -1 {
		return "", 0, "", nil, fmt.Errorf("missing shell metadata")
	}
	endIdx := strings.LastIndex(stdout, end)
	if endIdx == -1 || endIdx < beginIdx {
		return "", 0, "", nil, fmt.Errorf("missing shell metadata terminator")
	}

	userStdout = stdout[:beginIdx]
	meta := strings.TrimSpace(stdout[beginIdx+len(begin) : endIdx])
	parts := strings.Split(meta, "\n")
	if len(parts) < 3 {
		return "", 0, "", nil, fmt.Errorf("incomplete shell metadata")
	}

	exitCode, err = strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return "", 0, "", nil, fmt.Errorf("invalid exit code metadata")
	}
	cwdRaw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(parts[1]))
	if err != nil {
		return "", 0, "", nil, fmt.Errorf("invalid cwd metadata")
	}
	envRaw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(parts[2]))
	if err != nil {
		return "", 0, "", nil, fmt.Errorf("invalid env metadata")
	}

	cwd = strings.TrimSpace(string(cwdRaw))
	if cwd == "" {
		cwd = "/"
	}
	env = envFromDump(string(envRaw))
	return userStdout, exitCode, cwd, env, nil
}

func parseFSReadMetadata(stdout, token string) (int, []byte, error) {
	begin := "__RAILPUSH_FS_READ_BEGIN_" + token + "__"
	end := "__RAILPUSH_FS_READ_END_" + token + "__"
	b := strings.Index(stdout, begin)
	e := strings.LastIndex(stdout, end)
	if b == -1 || e == -1 || e < b {
		return 0, nil, fmt.Errorf("missing file read metadata")
	}
	meta := strings.TrimSpace(stdout[b+len(begin) : e])
	parts := strings.Split(meta, "\n")
	if len(parts) < 2 {
		return 0, nil, fmt.Errorf("incomplete file read metadata")
	}
	size, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, nil, fmt.Errorf("invalid file size metadata")
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, nil, fmt.Errorf("invalid file content metadata")
	}
	return size, decoded, nil
}

func mapExecInfraStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if strings.Contains(strings.ToLower(err.Error()), "timed out") {
		return http.StatusGatewayTimeout
	}
	return http.StatusBadGateway
}

func (h *ServiceHandler) runServiceCommand(svc *models.Service, command string, timeout time.Duration, user string) (string, string, int, string, error) {
	if h == nil || h.Worker == nil {
		return "", "", -1, "", fmt.Errorf("worker not initialized")
	}
	if svc == nil {
		return "", "", -1, "", fmt.Errorf("missing service")
	}
	if h.Worker.Config != nil && h.Worker.Config.Kubernetes.Enabled {
		if strings.TrimSpace(user) != "" {
			return "", "", -1, "k8s_exec", fmt.Errorf("user override is not supported in kubernetes exec mode")
		}
		kd, err := h.Worker.GetKubeDeployer()
		if err != nil || kd == nil {
			return "", "", -1, "k8s_exec", fmt.Errorf("kubernetes executor unavailable")
		}
		stdout, stderr, exitCode, _, _, execErr := kd.RunServiceExecCommand(svc, command, "", timeout, serviceExecMaxOutputBytes)
		return stdout, stderr, exitCode, "k8s_exec", execErr
	}

	containerID := strings.TrimSpace(svc.ContainerID)
	if containerID == "" {
		return "", "", -1, "docker_exec", fmt.Errorf("service container is not running")
	}
	args := []string{"exec"}
	if strings.TrimSpace(user) != "" {
		args = append(args, "-u", strings.TrimSpace(user))
	}
	args = append(args, containerID, "sh", "-lc", command)
	out, exitCode, execErr := h.Worker.Deployer.ExecCommandWithExitCode("docker", args...)
	return out, "", exitCode, "docker_exec", execErr
}

func (h *ServiceHandler) CreateShellSession(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	svc, err := models.GetService(id)
	if err != nil || svc == nil {
		respondServiceNotFound(w, id)
		return
	}
	if strings.EqualFold(strings.TrimSpace(svc.Status), "soft_deleted") {
		respondServiceNotFound(w, id)
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleDeveloper) {
		return
	}

	var req createShellSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.WorkingDirectory = strings.TrimSpace(req.WorkingDirectory)
	idleMinutes := req.IdleTimeoutMinutes
	if idleMinutes <= 0 {
		idleMinutes = serviceShellDefaultIdleMinutes
	}
	idleMinutes = clampInt(idleMinutes, 1, serviceShellMaxIdleMinutes)

	_ = models.DeleteExpiredServiceShellSessions()
	activeCount, err := models.CountActiveServiceShellSessions(svc.ID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load active shell sessions")
		return
	}
	if activeCount >= serviceShellMaxSessions {
		utils.RespondErrorWithSuggestion(w, http.StatusTooManyRequests, "SHELL_SESSION_LIMIT_REACHED", "too many active shell sessions for this service", "Close an existing session or wait for expiration, then try again.")
		return
	}

	token := strconv.FormatInt(time.Now().UnixNano(), 36)
	script := buildShellExecScript(":", req.WorkingDirectory, nil, token)
	stdout, stderr, exitCode, mode, execErr := h.runServiceCommand(svc, script, 20*time.Second, "")
	if execErr != nil && exitCode < 0 {
		utils.RespondError(w, mapExecInfraStatus(execErr), execErr.Error())
		return
	}
	_, _, cwd, env, parseErr := parseShellExecMetadata(stdout, token)
	if parseErr != nil {
		utils.RespondError(w, http.StatusBadGateway, "failed to initialize shell session")
		return
	}
	if exitCode == 91 {
		utils.RespondValidationErrors(w, http.StatusBadRequest, []utils.ValidationIssue{{Field: "working_directory", Message: "does not exist in container"}})
		return
	}
	if exitCode != 0 {
		utils.RespondJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":     "failed to initialize shell session",
			"exit_code": exitCode,
			"stderr":    strings.TrimSpace(stderr),
		})
		return
	}

	createdBy := strings.TrimSpace(userID)
	sess := &models.ServiceShellSession{
		ServiceID:          svc.ID,
		WorkspaceID:        svc.WorkspaceID,
		CWD:                cwd,
		BaseEnv:            env,
		CurrentEnv:         env,
		IdleTimeoutMinutes: idleMinutes,
		ExpiresAt:          time.Now().UTC().Add(time.Duration(idleMinutes) * time.Minute),
	}
	if createdBy != "" {
		sess.CreatedBy = &createdBy
	}
	if err := models.CreateServiceShellSession(sess); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create shell session")
		return
	}

	services.Audit(svc.WorkspaceID, userID, "shell.session_created", "service", svc.ID, map[string]interface{}{
		"session_id":            sess.ID,
		"idle_timeout_minutes":  sess.IdleTimeoutMinutes,
		"initial_cwd":           sess.CWD,
		"working_directory":     req.WorkingDirectory,
		"initialization_mode":   mode,
		"initialization_stderr": strings.TrimSpace(stderr),
	})

	utils.RespondJSON(w, http.StatusCreated, map[string]interface{}{
		"session_id":            sess.ID,
		"service_id":            sess.ServiceID,
		"workspace_id":          sess.WorkspaceID,
		"cwd":                   sess.CWD,
		"idle_timeout_minutes":  sess.IdleTimeoutMinutes,
		"expires_at":            sess.ExpiresAt,
	})
}

func (h *ServiceHandler) ExecShellSession(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	sessionID := strings.TrimSpace(mux.Vars(r)["sessionId"])
	sess, err := models.GetServiceShellSession(sessionID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load shell session")
		return
	}
	if sess == nil {
		utils.RespondError(w, http.StatusNotFound, "shell session not found")
		return
	}
	if time.Now().UTC().After(sess.ExpiresAt) {
		_ = models.DeleteServiceShellSession(sess.ID)
		utils.RespondError(w, http.StatusNotFound, "shell session expired")
		return
	}

	svc, err := models.GetService(sess.ServiceID)
	if err != nil || svc == nil {
		_ = models.DeleteServiceShellSession(sess.ID)
		respondServiceNotFound(w, sess.ServiceID)
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleDeveloper) {
		return
	}

	var req shellExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Command = strings.TrimSpace(req.Command)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Command == "" {
		utils.RespondValidationErrors(w, http.StatusBadRequest, []utils.ValidationIssue{{Field: "command", Message: "is required"}})
		return
	}

	policy := services.EvaluateOneOffCommand(req.Command)
	apiKeyID := middleware.GetAPIKeyID(r)
	apiKeyScopes := middleware.GetAPIKeyScopes(r)
	commandPreview := services.OneOffCommandPreview(req.Command, 180)
	commandSHA := services.OneOffCommandSHA256(req.Command)
	auditDetails := map[string]interface{}{
		"service_id":         svc.ID,
		"session_id":         sess.ID,
		"command_preview":    commandPreview,
		"command_sha256":     commandSHA,
		"command_length":     len(req.Command),
		"risky":              policy.Risky,
		"risk_reasons":       policy.RiskReasons,
		"acknowledged_risk":  req.AcknowledgeRiskyCommand,
		"reason":             req.Reason,
		"api_key_id":         apiKeyID,
		"api_key_scopes":     apiKeyScopes,
		"client_ip":          middleware.ClientIPString(r),
		"user_agent":         strings.TrimSpace(r.UserAgent()),
		"cwd_before":         sess.CWD,
	}

	if policy.Blocked {
		auditDetails["blocked_reasons"] = policy.BlockReasons
		services.Audit(svc.WorkspaceID, userID, "shell.exec_denied", "service", svc.ID, auditDetails)
		utils.RespondJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":           "command blocked by shell policy",
			"blocked_reasons": policy.BlockReasons,
		})
		return
	}

	if policy.Risky && apiKeyID != "" && !models.HasAnyAPIKeyScope(apiKeyScopes, models.APIKeyScopeAdmin) {
		auditDetails["blocked_reasons"] = []string{"risky shell commands via API key require admin scope"}
		services.Audit(svc.WorkspaceID, userID, "shell.exec_denied", "service", svc.ID, auditDetails)
		utils.RespondJSON(w, http.StatusForbidden, map[string]interface{}{
			"error":        "risky shell command denied for API key",
			"risk_reasons": policy.RiskReasons,
		})
		return
	}
	if policy.Risky && !req.AcknowledgeRiskyCommand {
		services.Audit(svc.WorkspaceID, userID, "shell.exec_denied", "service", svc.ID, auditDetails)
		utils.RespondJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":        "risky shell command requires acknowledge_risky_command=true",
			"risk_reasons": policy.RiskReasons,
		})
		return
	}

	timeoutSeconds := clampInt(req.TimeoutSeconds, 1, serviceShellMaxTimeout)
	if req.TimeoutSeconds <= 0 {
		timeoutSeconds = serviceShellDefaultTimeout
	}
	token := strconv.FormatInt(time.Now().UnixNano(), 36)
	script := buildShellExecScript(req.Command, sess.CWD, sess.CurrentEnv, token)
	started := time.Now()
	stdoutRaw, stderrRaw, exitCodeRaw, mode, execErr := h.runServiceCommand(svc, script, time.Duration(timeoutSeconds)*time.Second, "")
	if execErr != nil && exitCodeRaw < 0 {
		auditDetails["execution_mode"] = mode
		auditDetails["infra_error"] = execErr.Error()
		services.Audit(svc.WorkspaceID, userID, "shell.exec_failed", "service", svc.ID, auditDetails)
		utils.RespondError(w, mapExecInfraStatus(execErr), execErr.Error())
		return
	}

	stdout, parsedExitCode, cwdAfter, envAfter, parseErr := parseShellExecMetadata(stdoutRaw, token)
	if parseErr != nil {
		services.Audit(svc.WorkspaceID, userID, "shell.exec_failed", "service", svc.ID, map[string]interface{}{
			"session_id":      sess.ID,
			"service_id":      svc.ID,
			"metadata_error":  parseErr.Error(),
			"execution_mode":  mode,
		})
		utils.RespondError(w, http.StatusBadGateway, "failed to parse shell command metadata")
		return
	}
	if err := models.UpdateServiceShellSessionState(sess.ID, cwdAfter, envAfter); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update shell session state")
		return
	}

	stdout, stdoutTruncated := truncateOutput(stdout, serviceExecMaxOutputBytes)
	stderr, stderrTruncated := truncateOutput(stderrRaw, serviceExecMaxOutputBytes)
	durationMs := time.Since(started).Milliseconds()
	auditDetails["execution_mode"] = mode
	auditDetails["exit_code"] = parsedExitCode
	auditDetails["duration_ms"] = durationMs
	auditDetails["stdout_truncated"] = stdoutTruncated
	auditDetails["stderr_truncated"] = stderrTruncated
	auditDetails["cwd_after"] = cwdAfter
	auditDetails["had_error"] = execErr != nil
	services.Audit(svc.WorkspaceID, userID, "shell.exec", "service", svc.ID, auditDetails)

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"session_id":      sess.ID,
		"service_id":      svc.ID,
		"execution_mode":  mode,
		"exit_code":       parsedExitCode,
		"stdout":          stdout,
		"stderr":          stderr,
		"cwd":             cwdAfter,
		"duration_ms":     durationMs,
		"truncated":       stdoutTruncated || stderrTruncated,
		"timeout_seconds": timeoutSeconds,
	})
}

func (h *ServiceHandler) CloseShellSession(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	sessionID := strings.TrimSpace(mux.Vars(r)["sessionId"])
	sess, err := models.GetServiceShellSession(sessionID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load shell session")
		return
	}
	if sess == nil {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{"status": "closed", "session_id": sessionID})
		return
	}
	svc, err := models.GetService(sess.ServiceID)
	if err != nil || svc == nil {
		_ = models.DeleteServiceShellSession(sess.ID)
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{"status": "closed", "session_id": sessionID})
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleDeveloper) {
		return
	}
	if err := models.DeleteServiceShellSession(sess.ID); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to close shell session")
		return
	}
	services.Audit(svc.WorkspaceID, userID, "shell.session_closed", "service", svc.ID, map[string]interface{}{
		"session_id": sess.ID,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{"status": "closed", "session_id": sessionID})
}

func (h *ServiceHandler) ListServiceFilesystem(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	svc, err := models.GetService(id)
	if err != nil || svc == nil {
		respondServiceNotFound(w, id)
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleDeveloper) {
		return
	}

	fsPath, err := normalizeFSPath(r.URL.Query().Get("path"))
	if err != nil {
		utils.RespondValidationErrors(w, http.StatusBadRequest, []utils.ValidationIssue{{Field: "path", Message: err.Error()}})
		return
	}
	limit := serviceFSDefaultListLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, perr := strconv.Atoi(raw); perr == nil {
			limit = clampInt(parsed, 1, serviceFSMaxListLimit)
		}
	}

	script := "set -e; if [ ! -d " + shellQuote(fsPath) + " ]; then echo 'directory not found' >&2; exit 2; fi; ls -1ApA " + shellQuote(fsPath)
	stdout, stderr, exitCode, mode, execErr := h.runServiceCommand(svc, script, 20*time.Second, "")
	if execErr != nil && exitCode < 0 {
		utils.RespondError(w, mapExecInfraStatus(execErr), execErr.Error())
		return
	}
	if exitCode != 0 {
		status := http.StatusBadRequest
		if strings.Contains(strings.ToLower(stderr), "not found") {
			status = http.StatusNotFound
		}
		utils.RespondError(w, status, strings.TrimSpace(stderr))
		return
	}

	entries := make([]map[string]interface{}, 0)
	lines := strings.Split(stdout, "\n")
	truncated := false
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		entryType := "file"
		if strings.HasSuffix(name, "/") {
			entryType = "directory"
			name = strings.TrimSuffix(name, "/")
		}
		entries = append(entries, map[string]interface{}{"name": name, "type": entryType})
		if len(entries) >= limit {
			truncated = true
			break
		}
	}

	services.Audit(svc.WorkspaceID, userID, "service.fs_list", "service", svc.ID, map[string]interface{}{
		"path":           fsPath,
		"limit":          limit,
		"entries":        len(entries),
		"truncated":      truncated,
		"execution_mode": mode,
	})

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"service_id":     svc.ID,
		"path":           fsPath,
		"entries":        entries,
		"truncated":      truncated,
		"execution_mode": mode,
	})
}

func (h *ServiceHandler) ReadServiceFilesystemFile(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	svc, err := models.GetService(id)
	if err != nil || svc == nil {
		respondServiceNotFound(w, id)
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleDeveloper) {
		return
	}

	fsPath, err := normalizeFSPath(r.URL.Query().Get("path"))
	if err != nil {
		utils.RespondValidationErrors(w, http.StatusBadRequest, []utils.ValidationIssue{{Field: "path", Message: err.Error()}})
		return
	}
	maxBytes := serviceFSDefaultReadBytes
	if raw := strings.TrimSpace(r.URL.Query().Get("max_bytes")); raw != "" {
		if parsed, perr := strconv.Atoi(raw); perr == nil {
			maxBytes = clampInt(parsed, 1, serviceFSMaxReadBytes)
		}
	}

	token := strconv.FormatInt(time.Now().UnixNano(), 36)
	begin := "__RAILPUSH_FS_READ_BEGIN_" + token + "__"
	end := "__RAILPUSH_FS_READ_END_" + token + "__"
	script := strings.Join([]string{
		"set -e",
		"if [ ! -f " + shellQuote(fsPath) + " ]; then echo 'file not found' >&2; exit 2; fi",
		"__rp_size=$(wc -c < " + shellQuote(fsPath) + " | tr -d ' ')",
		"__rp_data=$(head -c " + strconv.Itoa(maxBytes) + " " + shellQuote(fsPath) + " | base64 | tr -d '\\n')",
		"printf '\\n" + begin + "\\n%s\\n%s\\n" + end + "\\n' \"$__rp_size\" \"$__rp_data\"",
	}, "; ")

	stdout, stderr, exitCode, mode, execErr := h.runServiceCommand(svc, script, 20*time.Second, "")
	if execErr != nil && exitCode < 0 {
		utils.RespondError(w, mapExecInfraStatus(execErr), execErr.Error())
		return
	}
	if exitCode != 0 {
		status := http.StatusBadRequest
		if strings.Contains(strings.ToLower(stderr), "not found") {
			status = http.StatusNotFound
		}
		utils.RespondError(w, status, strings.TrimSpace(stderr))
		return
	}

	sizeBytes, content, parseErr := parseFSReadMetadata(stdout, token)
	if parseErr != nil {
		utils.RespondError(w, http.StatusBadGateway, "failed to parse filesystem read response")
		return
	}
	if sizeBytes < 0 {
		sizeBytes = 0
	}
	truncated := sizeBytes > maxBytes

	resp := map[string]interface{}{
		"service_id":     svc.ID,
		"path":           fsPath,
		"size_bytes":     sizeBytes,
		"truncated":      truncated,
		"execution_mode": mode,
	}
	if utf8.Valid(content) {
		resp["encoding"] = "utf-8"
		resp["content"] = string(content)
	} else {
		resp["encoding"] = "base64"
		resp["content_base64"] = base64.StdEncoding.EncodeToString(content)
	}

	services.Audit(svc.WorkspaceID, userID, "service.fs_read", "service", svc.ID, map[string]interface{}{
		"path":           fsPath,
		"max_bytes":      maxBytes,
		"size_bytes":     sizeBytes,
		"truncated":      truncated,
		"encoding":       resp["encoding"],
		"execution_mode": mode,
	})

	utils.RespondJSON(w, http.StatusOK, resp)
}

func (h *ServiceHandler) SearchServiceFilesystem(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	svc, err := models.GetService(id)
	if err != nil || svc == nil {
		respondServiceNotFound(w, id)
		return
	}
	if !h.ensureAccess(w, userID, svc.WorkspaceID, models.RoleDeveloper) {
		return
	}

	fsPath, err := normalizeFSPath(r.URL.Query().Get("path"))
	if err != nil {
		utils.RespondValidationErrors(w, http.StatusBadRequest, []utils.ValidationIssue{{Field: "path", Message: err.Error()}})
		return
	}
	pattern := strings.TrimSpace(r.URL.Query().Get("pattern"))
	if pattern == "" {
		pattern = "*"
	}
	if len(pattern) > 256 {
		utils.RespondValidationErrors(w, http.StatusBadRequest, []utils.ValidationIssue{{Field: "pattern", Message: "is too long"}})
		return
	}
	recursive := true
	if raw := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("recursive"))); raw != "" {
		recursive = raw == "1" || raw == "true" || raw == "yes"
	}
	limit := serviceFSDefaultSearchMax
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, perr := strconv.Atoi(raw); perr == nil {
			limit = clampInt(parsed, 1, serviceFSMaxSearchMax)
		}
	}

	findCmd := "find " + shellQuote(fsPath) + " -type f -name " + shellQuote(pattern) + " -print"
	if !recursive {
		findCmd = "find " + shellQuote(fsPath) + " -maxdepth 1 -type f -name " + shellQuote(pattern) + " -print"
	}
	script := "set -e; if [ ! -d " + shellQuote(fsPath) + " ]; then echo 'directory not found' >&2; exit 2; fi; " + findCmd

	stdout, stderr, exitCode, mode, execErr := h.runServiceCommand(svc, script, 20*time.Second, "")
	if execErr != nil && exitCode < 0 {
		utils.RespondError(w, mapExecInfraStatus(execErr), execErr.Error())
		return
	}
	if exitCode != 0 {
		status := http.StatusBadRequest
		if strings.Contains(strings.ToLower(stderr), "not found") {
			status = http.StatusNotFound
		}
		utils.RespondError(w, status, strings.TrimSpace(stderr))
		return
	}

	matches := make([]string, 0)
	truncated := false
	for _, line := range strings.Split(stdout, "\n") {
		entry := strings.TrimSpace(line)
		if entry == "" {
			continue
		}
		matches = append(matches, entry)
		if len(matches) >= limit {
			truncated = true
			break
		}
	}

	services.Audit(svc.WorkspaceID, userID, "service.fs_search", "service", svc.ID, map[string]interface{}{
		"path":           fsPath,
		"pattern":        pattern,
		"recursive":      recursive,
		"limit":          limit,
		"matches":        len(matches),
		"truncated":      truncated,
		"execution_mode": mode,
	})

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"service_id":     svc.ID,
		"path":           fsPath,
		"pattern":        pattern,
		"recursive":      recursive,
		"matches":        matches,
		"truncated":      truncated,
		"execution_mode": mode,
	})
}
