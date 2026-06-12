package workflow

import (
	"context"

	"github.com/zeropsio/zcp/internal/topology"
)

// StepCheckResult holds the outcome of a hard check on a bootstrap step.
//
// C-10 (2026-04-20 shape flip): the v8.96 Theme A `NextRoundPrediction`
// field + its structured-field derivation (`ReadSurface`/`Required`/
// `Actual`/`CoupledWith`/`HowToFix`/`PerturbsChecks` on `StepCheck`) is
// removed. P1 `PreAttestCmd` + `ExpectedExit` replace the verbose-
// diagnostic shape with a runnable-command contract: if you want to
// know whether a check would pass, run the command and compare exit
// codes. The author-side `zcp check <name>` shim tree (C-7e) is the
// runnable form for every §16/§18 check; editorial-review (§16a) uses
// the marker string directly because no shell form exists.
type StepCheckResult struct {
	Passed  bool        `json:"passed"`
	Checks  []StepCheck `json:"checks"`
	Summary string      `json:"summary"`
}

// StepCheck is a single check within a step verification.
//
// C-10 (2026-04-20 shape flip): verbose v8.96 Theme A fields
// (`ReadSurface`/`Required`/`Actual`/`CoupledWith`/`HowToFix`/`Probe`/
// `PerturbsChecks`) are removed from the agent-facing payload in
// favor of P1 `PreAttestCmd` + `ExpectedExit`. The principle: the
// agent should not need to read five advisory fields to decide
// whether a check would pass — it should run one command and compare
// exit codes. Rich diagnostics live in `Detail` (one-line summary);
// server-side debug logs can retain the pre-shape-flip verbose fields
// at emission time (not emitted over the wire).
type StepCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // pass, fail, skip
	Detail string `json:"detail,omitempty"`

	// PreAttestCmd is a shell command the author can run against their
	// current workspace to re-evaluate this check. For §16 / §18 checks
	// this is the `zcp check <name> ...` shim invocation (see C-7e);
	// for §16a editorial-review checks it is the marker string
	// `dispatched via close.editorial-review (no author-side equivalent;
	// reviewer IS the runner)`. Optional — checks without a clean shell
	// form leave this empty.
	PreAttestCmd string `json:"preAttestCmd,omitempty"`

	// ExpectedExit is the exit code PreAttestCmd should return on pass.
	// Canonical value is 0 (command succeeds). Non-zero values apply
	// only to checks whose shim intentionally exits non-zero on a pass
	// condition — none in the current catalog. Omitted from JSON when
	// zero.
	ExpectedExit int `json:"expectedExit,omitempty"`

	// Recovery is the structured next-tool pointer attached when this
	// check failed in a way the agent can recover from (non-running
	// terminal status, missing precondition that another tool can fix).
	// Omitted from JSON when nil. Diagnose-before-destruct gates surface
	// Recovery here so the engine's check-rejection path can pass it
	// through to tools.CheckWire without a separate plumbing channel.
	// Plan v4 §1.4.
	Recovery *topology.Recovery `json:"recovery,omitempty"`
}

// StepChecker validates that a bootstrap step's requirements are met.
// Checkers verify observable infrastructure state (service status, file existence,
// health checks) — they are the real enforcement gates. Attestations are a separate
// audit trail and do NOT affect checker outcomes.
// Returns nil result to skip checking (equivalent to always-pass).
type StepChecker func(ctx context.Context, plan *ServicePlan, state *BootstrapState) (*StepCheckResult, error)
