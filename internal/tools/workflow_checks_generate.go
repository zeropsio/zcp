package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// checkEnvSelfShadow detects `key: ${key}` shape in `run.envVariables`
// (the canonical schema location). A same-key declaration reaches the
// process as the literal string `${key}` — for a Shared or the service's own
// vault value too — and every other entry of the service that references
// key gets the literal as well (live 2026-10-07). Until Zerops resolves
// same-name references, the entry is named what the app reads and the value
// it references carries a different name.
//
// Returns exactly one StepCheck — pass or fail. Nil entry is a pass
// (defensive; upstream `_zerops_yml_exists` reports a missing entry).
// ctx is threaded through for signature parity with the other checks;
// the predicate is a pure computation and ignores it.
func checkEnvSelfShadow(_ context.Context, hostname string, entry *ops.ZeropsYmlEntry) workflow.StepCheck {
	if entry == nil {
		return workflow.StepCheck{
			Name:   hostname + "_env_self_shadow",
			Status: statusPass,
		}
	}
	shadows := ops.DetectSelfShadows(entry.Run.EnvVariables)
	if len(shadows) == 0 {
		return workflow.StepCheck{
			Name:   hostname + "_env_self_shadow",
			Status: statusPass,
		}
	}
	return workflow.StepCheck{
		Name:   hostname + "_env_self_shadow",
		Status: statusFail,
		Detail: fmt.Sprintf(
			"same-key envVariables: %s — each entry has the shape `key: ${key}`, which reaches the app as the literal string `${key}`, and every other entry referencing key gets the literal too. Until Zerops resolves same-name references, name the entry what the app reads and give the value it references a different name: `APP_KEY: ${APP_KEY_SECRET}` for a vault value (store it under that name with zerops_env), `DB_HOST: ${db_hostname}` for another service's. Full rule set: zerops_knowledge uri=\"zerops://atoms/develop-env-var-model\".",
			strings.Join(shadows, ", "),
		),
	}
}
