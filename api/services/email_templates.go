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

func emailHTMLShell(title string, preheader string, bodyHTML string) string {
	title = strings.TrimSpace(title)
	preheader = strings.TrimSpace(preheader)

	hiddenPreheader := ""
	if preheader != "" {
		hiddenPreheader = `<div style="display:none;max-height:0;overflow:hidden;opacity:0;color:transparent;">` + htmlEscape(preheader) + `</div>`
	}

	return fmt.Sprintf(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <meta name="x-apple-disable-message-reformatting" />
    <title>%s</title>
  </head>
  <body style="margin:0;padding:0;background:#f6f7fb;">
    %s
    <table role="presentation" width="100%%" cellspacing="0" cellpadding="0" style="background:#f6f7fb;padding:24px 0;">
      <tr>
        <td align="center" style="padding:0 12px;">
          <table role="presentation" width="100%%" cellspacing="0" cellpadding="0" style="max-width:600px;background:#ffffff;border:1px solid #e7e8ee;border-radius:14px;overflow:hidden;">
            <tr>
              <td style="padding:22px 24px 10px 24px;font-family:Arial,Helvetica,sans-serif;">
                <div style="font-size:12px;letter-spacing:0.14em;text-transform:uppercase;color:#6b7280;">RailPush</div>
              </td>
            </tr>
            <tr>
              <td style="padding:0 24px 22px 24px;font-family:Arial,Helvetica,sans-serif;font-size:16px;line-height:1.65;color:#111827;">
                %s
              </td>
            </tr>
          </table>
          <div style="max-width:600px;font-family:Arial,Helvetica,sans-serif;font-size:12px;line-height:1.5;color:#6b7280;padding:12px 8px;">
            If you did not request this email, you can ignore it.
          </div>
        </td>
      </tr>
    </table>
  </body>
</html>`, htmlEscape(title), hiddenPreheader, bodyHTML)
}

func emailButton(label string, href string) string {
	label = strings.TrimSpace(label)
	href = strings.TrimSpace(href)
	if label == "" || href == "" {
		return ""
	}
	return `<a href="` + href + `" style="display:inline-block;background:#111827;color:#ffffff;text-decoration:none;font-weight:700;padding:12px 16px;border-radius:10px;">` + htmlEscape(label) + `</a>`
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

	html = emailHTMLShell(
		"Welcome to RailPush",
		"Your RailPush account is ready.",
		fmt.Sprintf(
			`<h1 style="margin:0 0 10px 0;font-size:24px;line-height:1.25;">Welcome, %s</h1>
<p style="margin:0 0 16px 0;color:#374151;">Your RailPush account is ready. Create a service, connect GitHub, and ship.</p>
<div style="margin:0 0 18px 0;">%s</div>
<div style="color:#6b7280;font-size:13px;line-height:1.55;">
  Workspace: <span style="color:#111827;font-weight:600;">%s</span>
</div>`,
			htmlEscape(name),
			emailButton("Log in", loginURL),
			htmlEscape(ws),
		),
	)

	return subject, text, html
}

func BuildVerifyEmail(cfg *config.Config, user *models.User, verifyURL string) (subject string, text string, html string) {
	cp := controlPlaneBaseURL(cfg)
	if strings.TrimSpace(verifyURL) == "" && cp != "" {
		verifyURL = cp + "/verify"
	}

	name := "there"
	if user != nil {
		if u := strings.TrimSpace(user.Username); u != "" {
			name = u
		} else if e := strings.TrimSpace(user.Email); e != "" {
			name = e
		}
	}

	subject = "Verify your RailPush email"
	text = fmt.Sprintf(
		"Hi %s,\n\nVerify your email to activate your RailPush account:\n%s\n\nIf you did not request this, you can ignore this message.\n",
		name,
		strings.TrimSpace(verifyURL),
	)

	html = emailHTMLShell(
		"Verify your email",
		"Verify your email to activate your RailPush account.",
		fmt.Sprintf(
			`<h1 style="margin:0 0 10px 0;font-size:24px;line-height:1.25;">Verify your email</h1>
<p style="margin:0 0 16px 0;color:#374151;">Hi %s. Click the button below to verify your email and activate your RailPush account.</p>
<div style="margin:0 0 18px 0;">%s</div>
<p style="margin:0;color:#6b7280;font-size:13px;line-height:1.55;">If the button doesn’t work, open this link: <a href="%s" style="color:#111827;">%s</a></p>`,
			htmlEscape(name),
			emailButton("Verify email", strings.TrimSpace(verifyURL)),
			strings.TrimSpace(verifyURL),
			htmlEscape(strings.TrimSpace(verifyURL)),
		),
	)

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

	html = emailHTMLShell(
		"Deploy "+statusWord+": "+serviceName,
		"Deploy "+statusWord+" for "+serviceName+".",
		fmt.Sprintf(
			`<h1 style="margin:0 0 10px 0;font-size:22px;line-height:1.25;">Deploy %s</h1>
<p style="margin:0 0 14px 0;color:#374151;">%s</p>
<div style="margin:0 0 16px 0;color:#374151;font-size:14px;line-height:1.65;">
  Branch: <span style="color:#111827;font-weight:600;">%s</span><br/>
  Commit: <span style="color:#111827;font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace;">%s</span><br/>
  Started: <span style="color:#111827;">%s</span><br/>
  Message: <span style="color:#111827;">%s</span>
</div>
%s`,
			htmlEscape(statusWord),
			htmlEscape(serviceName),
			htmlEscape(branch),
			htmlEscape(shortSHA(sha)),
			htmlEscape(started),
			htmlEscape(msg),
			func() string {
				if logsURL == "" {
					return `<div style="color:#6b7280;font-size:13px;">Open the RailPush dashboard to view logs.</div>`
				}
				return `<div style="margin-top:6px;">` + emailButton("View logs", logsURL) + `</div>`
			}(),
		),
	)

	return subject, text, html
}
