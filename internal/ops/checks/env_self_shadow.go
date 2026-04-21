package checks

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// CheckEnvSelfShadow detects `key: ${key}` shape (self-shadow) in both
// top-level `envVariables` (deprecated schema location) and
// `run.envVariables` (canonical). Self-shadows resolve to the literal
// string `${key}` inside the container, breaking the cross-service auto-
// inject contract. v8.85 lineage; deliberately enumerates both locations
// because v28 evidence showed self-shadows leaking in through both
// surfaces when only one was audited.
//
// Returns exactly one StepCheck per invocation — pass or fail. Nil entry
// is a pass (defensive; upstream `_zerops_yml_exists` reports a missing
// entry).
func CheckEnvSelfShadow(_ context.Context, hostname string, entry *ops.ZeropsYmlEntry) []workflow.StepCheck {
	if entry == nil {
		return []workflow.StepCheck{{
			Name:   hostname + "_env_self_shadow",
			Status: StatusPass,
		}}
	}
	topLevel := ops.DetectSelfShadows(entry.EnvVariables)
	runLevel := ops.DetectSelfShadows(entry.Run.EnvVariables)
	shadows := make([]string, 0, len(topLevel)+len(runLevel))
	shadows = append(shadows, topLevel...)
	shadows = append(shadows, runLevel...)
	if len(shadows) == 0 {
		return []workflow.StepCheck{{
			Name:   hostname + "_env_self_shadow",
			Status: StatusPass,
		}}
	}
	return []workflow.StepCheck{{
		Name:   hostname + "_env_self_shadow",
		Status: StatusFail,
		Detail: fmt.Sprintf(
			"self-shadowed envVariables: %s — each entry has the shape `key: ${key}`, which resolves to the literal string `${key}` inside the container. Cross-service vars (`${db_hostname}`, `${queue_user}`, ...) and project-level vars (`${STAGE_API_URL}`, ...) already auto-inject as OS env vars project-wide; DELETE these lines — they are redundant and actively break the runtime env. Only declare a var in run.envVariables if you are renaming (`DB_HOST: ${db_hostname}` with keys that DIFFER) or setting a literal mode flag (`NODE_ENV: production`). See zerops_guidance topic=\"env-var-model\" for the full rule set.",
			strings.Join(shadows, ", "),
		),
	}}
}
