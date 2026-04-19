package workflow

import "context"

// StepCheckResult holds the outcome of a hard check on a bootstrap step.
type StepCheckResult struct {
	Passed  bool        `json:"passed"`
	Checks  []StepCheck `json:"checks"`
	Summary string      `json:"summary"`

	// NextRoundPrediction is a one-line estimate of how many rounds the
	// agent should expect to take to converge on a fix, derived from the
	// structured fields of the failing checks. Populated by
	// AnnotateNextRoundPrediction when Passed is false. Empty when Passed
	// is true (no fix needed). Possible values:
	//   - "single-round-fix-expected" — every failing check carries a
	//     concrete HowToFix and no CoupledWith dependencies; one revision
	//     round should converge.
	//   - "coupled-surfaces-require-sequencing" — at least one failing
	//     check has CoupledWith entries; the author must edit the
	//     coupled files in sequence to keep them in sync.
	//   - "multi-round-likely" — at least one failing check has no
	//     HowToFix; the author will have to infer the remedy and may
	//     bounce.
	NextRoundPrediction string `json:"nextRoundPrediction,omitempty"`
}

// StepCheck is a single check within a step verification.
type StepCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // pass, fail, skip
	Detail string `json:"detail,omitempty"`

	// v8.96 — structured failure diagnostics. All optional; legacy checks
	// emit only Name/Status/Detail. Checks that surface in convergence
	// loops SHOULD populate these so the author can converge on a fix in
	// one revision round instead of inferring the remedy from Detail prose.

	// ReadSurface describes what the check actually read. Names a file
	// path and, when relevant, a fragment marker or line range. Lets the
	// author distinguish between e.g. the embedded YAML in a README's
	// integration-guide fragment vs the on-disk zerops.yaml.
	ReadSurface string `json:"readSurface,omitempty"`

	// Required is the threshold or shape required to pass. Loosely typed
	// (string) so each check emits whatever makes sense — a ratio floor,
	// an enum value, an expected pattern.
	Required string `json:"required,omitempty"`

	// Actual is the observed value the check computed. Same loose typing
	// as Required.
	Actual string `json:"actual,omitempty"`

	// CoupledWith lists file paths whose state is implicitly bound to
	// the ReadSurface. An author edit to any CoupledWith[i] may
	// invalidate the check's pass state unless ReadSurface is resynced.
	CoupledWith []string `json:"coupledWith,omitempty"`

	// HowToFix is a concrete 1-3-sentence remedy in imperative mood.
	// Forbidden: hedging words ("consider", "you might", "try"). When
	// CoupledWith is non-empty, HowToFix must mention at least one of
	// those paths so the coupling isn't silently dropped.
	HowToFix string `json:"howToFix,omitempty"`

	// Probe is a shell command the author can run as-is to re-evaluate
	// this check on their current workspace state. Optional — not every
	// check has a trivially-probeable form. Reserved for v8.97; v8.96
	// migrations leave this empty.
	Probe string `json:"probe,omitempty"`
}

// StepChecker validates that a bootstrap step's requirements are met.
// Checkers verify observable infrastructure state (service status, file existence,
// health checks) — they are the real enforcement gates. Attestations are a separate
// audit trail and do NOT affect checker outcomes.
// Returns nil result to skip checking (equivalent to always-pass).
type StepChecker func(ctx context.Context, plan *ServicePlan, state *BootstrapState) (*StepCheckResult, error)

// RecipeStepChecker validates recipe workflow step postconditions.
// Returns nil result to skip checking (equivalent to always-pass).
type RecipeStepChecker func(ctx context.Context, plan *RecipePlan, state *RecipeState) (*StepCheckResult, error)

// AnnotateNextRoundPrediction sets NextRoundPrediction on a fail-state
// StepCheckResult based on the structured fields of its failing checks.
// No-op when Passed is true — a passing result needs no prediction.
//
// Heuristic (kept small and inspectable):
//
//   - If any failing check has HowToFix empty, the author has no concrete
//     remedy and will likely take multiple rounds → "multi-round-likely".
//   - Otherwise, if any failing check has non-empty CoupledWith, the
//     author must keep coupled files in sync within a single round, which
//     is a sequencing concern → "coupled-surfaces-require-sequencing".
//   - Otherwise → "single-round-fix-expected".
//
// The prediction is telemetry-grade: post-hoc analysis of logs can
// correlate it against the actual round count to validate the heuristic.
func AnnotateNextRoundPrediction(r *StepCheckResult) {
	if r == nil || r.Passed {
		return
	}
	hasMissingHowToFix := false
	hasCoupling := false
	for _, c := range r.Checks {
		if c.Status != "fail" {
			continue
		}
		if c.HowToFix == "" {
			hasMissingHowToFix = true
		}
		if len(c.CoupledWith) > 0 {
			hasCoupling = true
		}
	}
	switch {
	case hasMissingHowToFix:
		r.NextRoundPrediction = "multi-round-likely"
	case hasCoupling:
		r.NextRoundPrediction = "coupled-surfaces-require-sequencing"
	default:
		r.NextRoundPrediction = "single-round-fix-expected"
	}
}
