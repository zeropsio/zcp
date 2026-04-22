package workflow

// Topology primitives consolidated under plan phase B.1. One typed Mode
// enum replaces three parallel vocabularies that had grown apart:
//
//   - PlanMode* string constants (bootstrap.go) — what the user picked
//     at bootstrap / adopt time.
//   - Mode* typed (envelope.go) — per-service projection in the
//     envelope, where a standard-pair service splits into a dev-half
//     (ModeStandard) and a stage-half (ModeStage).
//   - DeployRole* string constants (this file + duplicated in
//     ops/deploy_validate.go, where "simple" was missing — a drift
//     bug waiting to happen).
//
// They all speak the same set of values, differing only in when in the
// lifecycle the value is read. A single typed Mode eliminates the
// duplication. PlanMode* and DeployRole* are kept as aliases below so
// existing callers keep compiling during the cross-package rename; the
// next cleanup cycle can delete the aliases after in-tree uses switch
// to the typed Mode form.

// Mode is the canonical topology/role vocabulary. See envelope.go for
// the type declaration and the doc explaining per-service projection.
//
// Plan-time values (what bootstrap or adopt picked):
//   - ModeDev:        single dev-flavored container
//   - ModeStandard:   dev + stage pair (this value is persisted on the
//     dev-keyed meta; the stage half projects as ModeStage
//     in the envelope)
//   - ModeSimple:     single prod-flavored container with healthCheck
//   - ModeLocalStage: local dir + one linked Zerops stage runtime
//   - ModeLocalOnly:  local dir only; no Zerops runtime linked
//
// Envelope-only projection:
//   - ModeStage: surfaced for the stage half of a standard pair, so
//     stage-scoped atoms fire only on that hostname.
//
// Stage is intentionally not a plan-time input; callers building plans
// use ModeStandard and the engine derives the stage hostname separately.
const (
	ModeLocalStage Mode = "local-stage"
	ModeLocalOnly  Mode = "local-only"
)

// Plan-mode string aliases. Typed as Mode so new callers can drop the
// Plan prefix; left in place so existing callers compile unchanged.
// Compared directly against meta.Mode (still string-typed under phase
// B.2 — typing that field is a separate commit).
const (
	PlanModeStandard   = ModeStandard
	PlanModeDev        = ModeDev
	PlanModeSimple     = ModeSimple
	PlanModeLocalStage = ModeLocalStage
	PlanModeLocalOnly  = ModeLocalOnly
)

// DeployRole* aliases. These mirror the subset of Mode values that
// describe per-service deploy roles (no local-* — a local-only or
// local-stage project has no per-service role in the Zerops-runtime
// sense). Kept as aliases so ops/ and existing workflow/ callers keep
// compiling without the mechanical import sweep; the duplicated
// definitions in ops/deploy_validate.go are removed in favor of these.
const (
	DeployRoleDev    = ModeDev
	DeployRoleStage  = ModeStage
	DeployRoleSimple = ModeSimple
)

// WorkflowDevelop is the workflow name for develop sessions.
const WorkflowDevelop = "develop"

// Deploy step constants.
const (
	DeployStepPrepare = "prepare"
	DeployStepExecute = "execute"
	DeployStepVerify  = "verify"
)
