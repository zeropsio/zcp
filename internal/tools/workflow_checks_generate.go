package tools

import (
	"context"

	"github.com/zeropsio/zcp/internal/ops"
	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
	"github.com/zeropsio/zcp/internal/workflow"
)

// checkEnvSelfShadow flags entries where a `run.envVariables` key has the
// shape `KEY: ${KEY}` (same key on both sides). For project-level vars
// this is a true self-shadow (the project value auto-inherits, and the
// service-level same-key declaration produces the literal string in the
// process env). For cross-service vars under default isolation the
// right-hand template has nothing to resolve to. Both shapes are
// invalid and resolve to the literal `${...}` placeholder at runtime.
//
// The predicate lives in `internal/ops/checks` and emits one row per
// invocation (never a slice); the contract guarantees exactly one row, so we
// unwrap here to keep the caller shape stable. ctx is threaded through so
// contextcheck stays quiet — the predicate is a pure computation and ignores
// ctx.
//
// Used by the recipe-authoring generate-step checker
// (`workflow_checks_recipe.go::checkRecipeGenerateCodebase`); the
// bootstrap-side `checkGenerate` that previously called this lived in this
// file as well, but moved out under Option A (commit 7abc7280 — bootstrap is
// infrastructure-only, develop owns code + first deploy) and the dead
// scaffolding was removed in the source-mount-yaml fix sweep.
func checkEnvSelfShadow(ctx context.Context, hostname string, entry *ops.ZeropsYmlEntry) workflow.StepCheck {
	rows := opschecks.CheckEnvSelfShadow(ctx, hostname, entry)
	if len(rows) == 0 {
		// Defensive: the predicate always emits one row, but if a future
		// contract change emits zero, surface a pass so callers don't
		// crash on an empty slice index.
		return workflow.StepCheck{
			Name: hostname + "_env_self_shadow", Status: statusPass,
		}
	}
	return rows[0]
}
