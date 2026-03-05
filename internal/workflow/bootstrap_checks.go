package workflow

import "context"

// StepCheckResult holds the outcome of a hard check on a bootstrap step.
type StepCheckResult struct {
	Passed  bool        `json:"passed"`
	Checks  []StepCheck `json:"checks"`
	Summary string      `json:"summary"`
}

// StepCheck is a single check within a step verification.
type StepCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // pass, fail, skip
	Detail string `json:"detail,omitempty"`
}

// StepChecker validates that a bootstrap step's requirements are met.
// Returns nil result to skip checking (equivalent to always-pass).
type StepChecker func(ctx context.Context, plan *ServicePlan) (*StepCheckResult, error)
