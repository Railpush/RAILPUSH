package services

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	githubCredentialURLPattern = regexp.MustCompile(`https?://[^\s/@]+@github\.com`)
	githubPATPattern          = regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`)
	githubTokenPattern        = regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`)
)

func RepoURLHasEmbeddedCredentials(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err == nil && u != nil && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) {
		if u.User != nil {
			if strings.TrimSpace(u.User.Username()) != "" {
				return true
			}
			if pw, ok := u.User.Password(); ok && strings.TrimSpace(pw) != "" {
				return true
			}
		}
	}
	lower := strings.ToLower(raw)
	return strings.Contains(lower, "@github.com/") && strings.Contains(lower, "://")
}

func RedactRepoURLCredentials(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err == nil && u != nil && u.User != nil && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) {
		u.User = nil
		return u.String()
	}
	return raw
}

func RedactSecretsInText(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	masked := githubCredentialURLPattern.ReplaceAllStringFunc(raw, func(match string) string {
		u, err := url.Parse(match)
		if err != nil || u == nil {
			return "https://github.com"
		}
		u.User = nil
		return u.String()
	})
	masked = githubPATPattern.ReplaceAllString(masked, "<redacted>")
	masked = githubTokenPattern.ReplaceAllString(masked, "<redacted>")
	return masked
}
