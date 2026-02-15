package services

import "strings"

const (
	PlanFree     = "free"
	PlanStarter  = "starter"
	PlanStandard = "standard"
	PlanPro      = "pro"
)

// NormalizePlan trims and lowercases a plan string and validates it against the supported tiers.
// It returns the normalized plan and whether it is valid.
func NormalizePlan(raw string) (string, bool) {
	p := strings.ToLower(strings.TrimSpace(raw))
	switch p {
	case PlanFree, PlanStarter, PlanStandard, PlanPro:
		return p, true
	default:
		return "", false
	}
}

func IsPaidPlan(plan string) bool {
	p, ok := NormalizePlan(plan)
	return ok && p != PlanFree
}

