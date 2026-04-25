package topology

// Topology vocabulary axes — one typed Mode enum carries every lifecycle
// reading of the "what kind of service is this" axis:
//
//   - PlanMode* — what the user picked at bootstrap / adopt time.
//   - Mode*     — per-service projection in the envelope, where a
//     standard-pair service splits into a dev-half (ModeStandard) and a
//     stage-half (ModeStage).
//   - DeployRole* — the subset that describes a per-service deploy role
//     (no local-* variants, since local topologies are project-keyed and
//     have no per-service role).
//
// PlanMode* and DeployRole* are aliases for the Mode values they describe;
// keeping the three vocabularies collapsed to one source means ops/ and
// workflow/ can't drift (the DeployRole copy in ops/deploy_validate.go
// was missing "simple" before consolidation — classic drift bug).

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
