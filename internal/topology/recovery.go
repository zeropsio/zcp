package topology

// Recovery is the structured next-tool pointer attached to failing checks
// and rejected operations so the agent has an explicit, machine-readable
// recovery surface.
//
// Lives in topology because both ops and workflow produce Recovery values
// (verify checks, workflow_checks status rejections, deploy preflight
// gates, dev_server pre-spawn gates) and tools.RecoveryHint is just the
// wire-form alias of this type. Promoting from per-layer mirrors to one
// foundational vocabulary type removes the boilerplate of "ops has its
// own Recovery, tools has its own RecoveryHint, both with identical
// JSON tags" — they are now the same type.
type Recovery struct {
	Tool   string            `json:"tool"`
	Action string            `json:"action"`
	Args   map[string]string `json:"args,omitempty"`
}

// RecoveryShape is the one classification a service's non-healthy state
// falls into (docs/spec-workflows.md §8 "Recovery classification" R1).
// ops.ComputeRecoveryState is the single producer; every recovery reader
// (verify/provision Recovery pointers, the zerops_import override gate,
// atom selection) consumes it rather than re-deriving a branch from
// service.status alone.
type RecoveryShape string

const (
	// RecoveryHealthy covers RUNNING/ACTIVE, an intentional pre-deploy/
	// stopped state, or a service with a LIVE process (busy is not
	// failure — CLAUDE.md trap) — no recovery candidate.
	RecoveryHealthy RecoveryShape = "healthy"
	// RecoveryFreshMisconfigured is READY_TO_DEPLOY with no deploy
	// attempt ever (imported without startWithoutCode and without
	// buildFromGit) — the only shape whose gate carries a ready-made
	// override retry (R2).
	RecoveryFreshMisconfigured RecoveryShape = "fresh-misconfigured"
	// RecoveryFailedBuild is a classified build-pipeline failure
	// (BUILD_FAILED).
	RecoveryFailedBuild RecoveryShape = "failed-build"
	// RecoveryFailedInit is a classified prepare-runtime / init-command /
	// deploy-phase failure (PREPARING_RUNTIME_FAILED, DEPLOY_FAILED).
	RecoveryFailedInit RecoveryShape = "failed-init"
	// RecoveryStuckBuilding is a WAITING_TO_BUILD appVersion whose build
	// process FAILED (or whose state can't otherwise be positively
	// classified as fresh/healthy) — a real prior attempt worth reading
	// before any reset.
	RecoveryStuckBuilding RecoveryShape = "stuck-building"
)
