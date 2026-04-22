package workflow

// Topology primitives — one typed Mode enum carries every lifecycle
// reading of the "what kind of service is this" axis:
//
//   - PlanMode* (bootstrap.go) — what the user picked at bootstrap /
//     adopt time.
//   - Mode* (envelope.go) — per-service projection in the envelope,
//     where a standard-pair service splits into a dev-half
//     (ModeStandard) and a stage-half (ModeStage).
//   - DeployRole* — the subset that describes a per-service deploy
//     role (no local-* variants, since local topologies are
//     project-keyed and have no per-service role).
//
// PlanMode* and DeployRole* are aliases for the Mode values they
// describe; keeping the three vocabularies collapsed to one source
// means ops/ and workflow/ can't drift (the DeployRole copy in
// ops/deploy_validate.go was missing "simple" before consolidation
// — classic drift bug).

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

// PlanMode* — the plan-time subset. Names reflect the caller perspective
// (bootstrap plan input, adopt-local handler, etc.) rather than the role
// the service plays at deploy time.
const (
	PlanModeStandard   = ModeStandard
	PlanModeDev        = ModeDev
	PlanModeSimple     = ModeSimple
	PlanModeLocalStage = ModeLocalStage
	PlanModeLocalOnly  = ModeLocalOnly
)

// DeployRole* — the deploy-time subset. Mirrors the Mode values that
// describe a per-service role; no local-* variants, since local
// topologies are project-keyed and have no per-service role in the
// Zerops-runtime sense.
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
