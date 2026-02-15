package services

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/railpush/api/config"
)

type EmailMessage struct {
	To       string
	Subject  string
	TextBody string
	HTMLBody string
}

type Emailer interface {
	Send(ctx context.Context, msg EmailMessage) error
}

type SMTPEmailer struct {
	host    string
	port    int
	user    string
	pass    string
	from    string
	replyTo string
}

func NewSMTPEmailer(cfg *config.EmailConfig) (*SMTPEmailer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("missing email config")
	}
	host := strings.TrimSpace(cfg.SMTPHost)
	if host == "" {
		return nil, fmt.Errorf("missing SMTP_HOST")
	}
	from := strings.TrimSpace(cfg.SMTPFrom)
	if from == "" {
		return nil, fmt.Errorf("missing SMTP_FROM")
	}
	port := cfg.SMTPPort
	if port <= 0 {
		port = 587
	}
	return &SMTPEmailer{
		host:    host,
		port:    port,
		user:    strings.TrimSpace(cfg.SMTPUser),
		pass:    cfg.SMTPPassword,
		from:    from,
		replyTo: strings.TrimSpace(cfg.SMTPReplyTo),
	}, nil
}

func sanitizeHeaderValue(raw string) string {
	// Prevent header injection by stripping CR/LF.
	raw = strings.ReplaceAll(raw, "\r", " ")
	raw = strings.ReplaceAll(raw, "\n", " ")
	return strings.TrimSpace(raw)
}

func randomHex(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

func (s *SMTPEmailer) buildMIME(msg EmailMessage) []byte {
	to := sanitizeHeaderValue(msg.To)
	subject := sanitizeHeaderValue(msg.Subject)
	from := sanitizeHeaderValue(s.from)
	replyTo := sanitizeHeaderValue(s.replyTo)

	// RFC 2046 multipart/alternative
	boundary := "rp_" + randomHex(12)
	if boundary == "" {
		boundary = "rp_fallback"
	}

	lines := []string{
		"From: " + from,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative; boundary=" + boundary,
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
	}
	if replyTo != "" {
		lines = append(lines, "Reply-To: "+replyTo)
	}

	// Include a stable-ish message id for debugging (do not embed PII).
	if domain := strings.TrimSpace(strings.TrimPrefix(s.host, "smtp.")); domain != "" {
		lines = append(lines, "Message-ID: <"+randomHex(10)+"@"+domain+">")
	}

	lines = append(lines, "", "--"+boundary)
	lines = append(lines, "Content-Type: text/plain; charset=UTF-8", "Content-Transfer-Encoding: 8bit", "")
	lines = append(lines, strings.TrimSpace(msg.TextBody))
	lines = append(lines, "", "--"+boundary)
	lines = append(lines, "Content-Type: text/html; charset=UTF-8", "Content-Transfer-Encoding: 8bit", "")
	lines = append(lines, strings.TrimSpace(msg.HTMLBody))
	lines = append(lines, "", "--"+boundary+"--", "")

	return []byte(strings.Join(lines, "\r\n"))
}

func (s *SMTPEmailer) dial(ctx context.Context) (*smtp.Client, error) {
	addr := net.JoinHostPort(s.host, fmt.Sprintf("%d", s.port))

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if deadline, ok := ctx.Deadline(); ok {
		dialer.Deadline = deadline
	}

	// Port 465 is usually implicit TLS.
	if s.port == 465 {
		conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: s.host})
		if err != nil {
			return nil, err
		}
		c, err := smtp.NewClient(conn, s.host)
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		return c, nil
	}

	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	c, err := smtp.NewClient(conn, s.host)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return c, nil
}

func (s *SMTPEmailer) Send(ctx context.Context, msg EmailMessage) error {
	if ctx == nil {
		ctx = context.Background()
	}
	to := strings.TrimSpace(msg.To)
	if to == "" {
		return fmt.Errorf("missing recipient")
	}
	if strings.TrimSpace(msg.Subject) == "" {
		return fmt.Errorf("missing subject")
	}
	if strings.TrimSpace(msg.TextBody) == "" && strings.TrimSpace(msg.HTMLBody) == "" {
		return fmt.Errorf("missing body")
	}
	if strings.TrimSpace(msg.TextBody) == "" {
		msg.TextBody = strings.TrimSpace(stripHTMLTags(msg.HTMLBody))
	}
	if strings.TrimSpace(msg.HTMLBody) == "" {
		msg.HTMLBody = "<pre>" + htmlEscape(msg.TextBody) + "</pre>"
	}

	c, err := s.dial(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	// Opportunistic STARTTLS when available.
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: s.host}); err != nil {
			return err
		}
	}

	// Authenticate if credentials are provided.
	if s.user != "" || s.pass != "" {
		auth := smtp.PlainAuth("", s.user, s.pass, s.host)
		if err := c.Auth(auth); err != nil {
			return err
		}
	}

	if err := c.Mail(s.from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	wc, err := c.Data()
	if err != nil {
		return err
	}

	raw := s.buildMIME(msg)
	if _, err := wc.Write(raw); err != nil {
		_ = wc.Close()
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func stripHTMLTags(in string) string {
	// Minimal fallback: not a full HTML parser, but good enough for "HTML only" inputs.
	out := make([]rune, 0, len(in))
	inTag := false
	for _, r := range in {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				out = append(out, r)
			}
		}
	}
	return string(out)
}

func htmlEscape(in string) string {
	in = strings.ReplaceAll(in, "&", "&amp;")
	in = strings.ReplaceAll(in, "<", "&lt;")
	in = strings.ReplaceAll(in, ">", "&gt;")
	in = strings.ReplaceAll(in, `"`, "&quot;")
	in = strings.ReplaceAll(in, "'", "&#39;")
	return in
}

