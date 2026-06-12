package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// checkEnvSelfShadow detects `key: ${key}` shape in `run.envVariables`
// (the canonical schema location). Same-key declarations resolve to
// the literal string `${key}` inside the container. For project-level
// vars (which auto-inherit) this is a true self-shadow; for
// cross-service vars (which do not auto-inject under default isolation)
// the right-hand template has nothing to resolve to. Both shapes are
// invalid; flag uniformly.
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
			"same-key envVariables: %s — each entry has the shape `key: ${key}`, which resolves to the literal string `${key}` inside the container. Project-level vars (`${API_URL}`, `${APP_SECRET}`, ...) auto-inherit into every container; re-declaring under the same key produces the literal shadow above. Cross-service vars (`${db_hostname}`, `${queue_user}`, ...) reach the app only via an alias under a DIFFERENT key (`DB_HOST: ${db_hostname}`). DELETE these lines or rename under your own key. Only valid run.envVariables shapes: renames with keys that DIFFER (`DB_HOST: ${db_hostname}`) or literal mode flags (`NODE_ENV: production`). Full rule set: zerops_knowledge uri=\"zerops://atoms/develop-env-var-model\".",
			strings.Join(shadows, ", "),
		),
	}
}
