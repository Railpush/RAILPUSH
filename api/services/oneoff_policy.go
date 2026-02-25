package services

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

const oneOffMaxCommandLength = 4096

type OneOffCommandPolicy struct {
	Blocked      bool
	Risky        bool
	BlockReasons []string
	RiskReasons  []string
}

var oneOffBlockedPatterns = []struct {
	reason string
	re     *regexp.Regexp
}{
	{reason: "destructive root filesystem delete", re: regexp.MustCompile(`(?i)\brm\s+-rf\s+--no-preserve-root\s+/`)},
	{reason: "destructive root filesystem delete", re: regexp.MustCompile(`(?i)\brm\s+-rf\s+/($|\s|;)`)},
	{reason: "disk formatting command", re: regexp.MustCompile(`(?i)\b(mkfs|mkfs\.\S+|fdisk|parted)\b`)},
	{reason: "host shutdown command", re: regexp.MustCompile(`(?i)\b(shutdown|poweroff|reboot|halt)\b`)},
	{reason: "shell fork bomb detected", re: regexp.MustCompile(`:\s*\(\)\s*\{\s*:\|:\s*&\s*\};\s*:`)},
	{reason: "reverse shell pattern", re: regexp.MustCompile(`(?i)(nc|ncat)\s+.*\s-e\s+|/dev/tcp/`)},
}

var oneOffRiskyPatterns = []struct {
	reason string
	re     *regexp.Regexp
}{
	{reason: "database schema mutation", re: regexp.MustCompile(`(?i)\b(drop\s+database|drop\s+schema|truncate\s+table|alter\s+table)\b`)},
	{reason: "mass data mutation", re: regexp.MustCompile(`(?i)\b(delete\s+from|update\s+\S+\s+set)\b`)},
	{reason: "broad filesystem mutation", re: regexp.MustCompile(`(?i)\b(chmod|chown)\s+-R\b`)},
	{reason: "destructive delete", re: regexp.MustCompile(`(?i)\brm\s+-rf\b`)},
	{reason: "remote script execution", re: regexp.MustCompile(`(?i)(curl|wget)\s+[^\n]+\|\s*(sh|bash)\b`)},
}

func EvaluateOneOffCommand(command string) OneOffCommandPolicy {
	policy := OneOffCommandPolicy{}
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		policy.Blocked = true
		policy.BlockReasons = append(policy.BlockReasons, "command is required")
		return policy
	}
	if len(trimmed) > oneOffMaxCommandLength {
		policy.Blocked = true
		policy.BlockReasons = append(policy.BlockReasons, "command exceeds maximum length")
	}

	for _, p := range oneOffBlockedPatterns {
		if p.re.MatchString(trimmed) {
			policy.Blocked = true
			policy.BlockReasons = append(policy.BlockReasons, p.reason)
		}
	}
	for _, p := range oneOffRiskyPatterns {
		if p.re.MatchString(trimmed) {
			policy.Risky = true
			policy.RiskReasons = append(policy.RiskReasons, p.reason)
		}
	}
	policy.BlockReasons = dedupeStrings(policy.BlockReasons)
	policy.RiskReasons = dedupeStrings(policy.RiskReasons)
	return policy
}

func OneOffCommandSHA256(command string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(command)))
	return hex.EncodeToString(sum[:])
}

func OneOffCommandPreview(command string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 160
	}
	normalized := strings.Join(strings.Fields(strings.TrimSpace(command)), " ")
	if normalized == "" {
		return ""
	}
	if len(normalized) <= maxLen {
		return normalized
	}
	return normalized[:maxLen] + "..."
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
