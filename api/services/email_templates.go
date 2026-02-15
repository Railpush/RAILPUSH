package services

import (
	"fmt"
	"strings"
	"time"

	"github.com/railpush/api/config"
	"github.com/railpush/api/models"
)

func controlPlaneBaseURL(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	raw := strings.TrimSpace(cfg.ControlPlane.Domain)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		return strings.TrimSuffix(raw, "/")
	}
	scheme := "https"
	if raw == "localhost" || strings.HasPrefix(raw, "localhost:") || strings.HasPrefix(raw, "127.0.0.1") {
		scheme = "http"
	}
	return scheme + "://" + raw
}

func BuildWelcomeEmail(cfg *config.Config, user *models.User, workspaceName string) (subject string, text string, html string) {
	cp := controlPlaneBaseURL(cfg)
	loginURL := cp + "/login"
	if cp == "" {
		loginURL = "/login"
	}

	name := "there"
	if user != nil {
		if u := strings.TrimSpace(user.Username); u != "" {
			name = u
		} else if e := strings.TrimSpace(user.Email); e != "" {
			name = e
		}
	}

	ws := strings.TrimSpace(workspaceName)
	if ws == "" {
		ws = "your workspace"
	}

	subject = "Welcome to RailPush"
	text = fmt.Sprintf(
		"Hi %s,\n\nYour RailPush account is ready.\n\nNext steps:\n- Log in: %s\n- Create a service, connect GitHub, and ship.\n\nWorkspace: %s\n\nIf you did not create this account, you can ignore this message.\n",
		name,
		loginURL,
		ws,
	)

	html = fmt.Sprintf(`<!doctype html>
<html>
  <body style="margin:0;padding:0;background:#0b1020;color:#e9edf7;font-family:ui-sans-serif,system-ui,-apple-system,Segoe UI,Roboto;">
    <div style="max-width:640px;margin:0 auto;padding:28px 18px;">
      <div style="background:#12151f;border:1px solid rgba(255,255,255,0.08);border-radius:14px;padding:22px;">
        <div style="font-size:12px;letter-spacing:0.14em;text-transform:uppercase;color:#a5b1cc;margin-bottom:10px;">RailPush</div>
        <h1 style="margin:0 0 8px 0;font-size:22px;line-height:1.25;">Welcome, %s</h1>
        <p style="margin:0 0 16px 0;color:#a5b1cc;line-height:1.6;">
          Your RailPush account is ready. Create a service, connect GitHub, and ship.
        </p>
        <a href="%s" style="display:inline-block;background:#4f8bff;color:#0e1118;text-decoration:none;font-weight:700;padding:10px 14px;border-radius:10px;">
          Log in
        </a>
        <div style="height:14px;"></div>
        <div style="color:#7d8aaa;font-size:12px;line-height:1.6;">
          Workspace: <span style="color:#e9edf7;">%s</span><br/>
          If you did not create this account, you can ignore this message.
        </div>
      </div>
    </div>
  </body>
</html>`, htmlEscape(name), loginURL, htmlEscape(ws))

	return subject, text, html
}

func shortSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) >= 7 {
		return sha[:7]
	}
	return sha
}

func BuildDeployResultEmail(cfg *config.Config, svc *models.Service, deploy *models.Deploy, ok bool) (subject string, text string, html string) {
	cp := controlPlaneBaseURL(cfg)
	logsURL := ""
	if cp != "" && svc != nil && strings.TrimSpace(svc.ID) != "" {
		logsURL = cp + "/services/" + strings.TrimSpace(svc.ID) + "/logs?type=deploy"
	}

	serviceName := ""
	if svc != nil {
		serviceName = strings.TrimSpace(svc.Name)
	}
	if serviceName == "" {
		serviceName = "your service"
	}

	statusWord := "succeeded"
	if !ok {
		statusWord = "failed"
	}

	branch := ""
	sha := ""
	msg := ""
	started := ""
	if deploy != nil {
		branch = strings.TrimSpace(deploy.Branch)
		sha = strings.TrimSpace(deploy.CommitSHA)
		msg = strings.TrimSpace(deploy.CommitMessage)
		if deploy.StartedAt != nil {
			started = deploy.StartedAt.UTC().Format(time.RFC3339)
		}
	}

	subject = fmt.Sprintf("Deploy %s: %s", statusWord, serviceName)
	text = fmt.Sprintf(
		"Deploy %s for %s.\n\nBranch: %s\nCommit: %s\nMessage: %s\nStarted: %s\n\nView logs: %s\n",
		statusWord,
		serviceName,
		branch,
		shortSHA(sha),
		msg,
		started,
		logsURL,
	)
	if strings.TrimSpace(logsURL) == "" {
		text = strings.ReplaceAll(text, "\n\nView logs: \n", "\n")
	}

	html = fmt.Sprintf(`<!doctype html>
<html>
  <body style="margin:0;padding:0;background:#0b1020;color:#e9edf7;font-family:ui-sans-serif,system-ui,-apple-system,Segoe UI,Roboto;">
    <div style="max-width:640px;margin:0 auto;padding:28px 18px;">
      <div style="background:#12151f;border:1px solid rgba(255,255,255,0.08);border-radius:14px;padding:22px;">
        <div style="font-size:12px;letter-spacing:0.14em;text-transform:uppercase;color:#a5b1cc;margin-bottom:10px;">Deploy %s</div>
        <h1 style="margin:0 0 8px 0;font-size:20px;line-height:1.25;">%s</h1>
        <div style="color:#a5b1cc;font-size:13px;line-height:1.7;margin-bottom:16px;">
          Branch: <span style="color:#e9edf7;">%s</span><br/>
          Commit: <span style="color:#e9edf7;font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace;">%s</span><br/>
          Started: <span style="color:#e9edf7;">%s</span><br/>
          Message: <span style="color:#e9edf7;">%s</span>
        </div>
        %s
      </div>
    </div>
  </body>
</html>`,
		htmlEscape(strings.ToUpper(statusWord)),
		htmlEscape(serviceName),
		htmlEscape(branch),
		htmlEscape(shortSHA(sha)),
		htmlEscape(started),
		htmlEscape(msg),
		func() string {
			if logsURL == "" {
				return `<div style="color:#7d8aaa;font-size:12px;">Open the RailPush dashboard to view logs.</div>`
			}
			return `<a href="` + logsURL + `" style="display:inline-block;background:#4f8bff;color:#0e1118;text-decoration:none;font-weight:700;padding:10px 14px;border-radius:10px;">View logs</a>`
		}(),
	)

	return subject, text, html
}

