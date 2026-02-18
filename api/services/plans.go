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

// PlanRank returns a numeric rank for plan comparison. Higher = more expensive.
func PlanRank(plan string) int {
	switch strings.ToLower(strings.TrimSpace(plan)) {
	case PlanFree:
		return 0
	case PlanStarter:
		return 1
	case PlanStandard:
		return 2
	case PlanPro:
		return 3
	default:
		return 0
	}
}

// IsDowngrade returns true if changing from oldPlan to newPlan is a downgrade.
func IsDowngrade(oldPlan, newPlan string) bool {
	return PlanRank(newPlan) < PlanRank(oldPlan)
}

