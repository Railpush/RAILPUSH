package services

// planMonthlyCostCents returns the current monthly cost for a plan in cents.
//
// Note: This is duplicated in the billing overview handler (planCost). Long term,
// these should be sourced from Stripe or a shared pricing module.
func planMonthlyCostCents(plan string) int64 {
	p, ok := NormalizePlan(plan)
	if !ok {
		return 0
	}
	switch p {
	case PlanStarter:
		return 700
	case PlanStandard:
		return 2500
	case PlanPro:
		return 8500
	default:
		return 0
	}
}

