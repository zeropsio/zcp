package tools

import (
	"fmt"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
)

// actionStart is the canonical "start" verb shared by zerops_workflow,
// zerops_manage, and zerops_dev_server action params. Kept as a constant
// so goconst is satisfied across the three emitters that share the string.
const actionStart = "start"

// NextActions constants provide actionable follow-up instructions for LLMs.
const (
	nextActionDeploySuccess = "Run zerops_verify for runtime state."
	nextActionImportSuccess = "Verify services: zerops_discover. Continue workflow: mount dev, discover env vars, write code, then deploy."
	nextActionImportPartial = "Check failed processes: zerops_events. Fix and re-import via zerops_workflow."
	// nextActionEnvSetSuccess + nextActionEnvDeleteSuccess removed — zerops_env
	// now auto-restarts affected services and crafts its own per-call message
	// listing what was restarted (see envChangeResult.NextActions).
	nextActionManageStart      = "Verify service is running: zerops_discover."
	nextActionManageStop       = "Service stopped. Start with: zerops_manage action=start."
	nextActionManageRestart    = "Verify health: zerops_logs severity=ERROR since=1m."
	nextActionManageReload     = "Verify health: zerops_logs severity=ERROR since=1m."
	nextActionManageConnect    = "Verify storage mount: zerops_discover."
	nextActionManageDisconnect = "Storage disconnected. Verify: zerops_discover."
	nextActionScaleSuccess     = "Verify scaling: zerops_discover."
	nextActionSubdomainEnable  = "Subdomain active. Verify: zerops_verify."
)

// deploySuccessNextActions returns the post-deploy next-tool pointer.
// The pointer is route-aware (which next call), NOT state-claiming
// (does NOT assert process liveness, does NOT embed SSH commands).
//
// Invariant DS-01 (plans/archive/dev-server-canonical-primitive.md):
// post-deploy messaging never branches on the deprecated runtime-class
// liveness heuristics that produced dishonest "server NOT running" /
// "auto-start" claims. The canonical post-DS-01 predicate is
// `topology.IsDeferredStart` (dev/standard mode + dynamic runtime) —
// same predicate used by `deploy_subdomain.go::maybeAutoEnableSubdomain`
// to skip the L7 HTTP-readiness probe. It names the next tool to call,
// not what the runtime is doing right now.
//
// Falls back to the generic `zerops_verify` pointer when meta is
// unavailable (mode=="" / class=="") — recipe-authoring scaffolds and
// freshly-imported services without a bootstrap session reach this
// path. Verify is correct in every shape (web/worker/managed) so the
// fallback is never wrong, only sometimes less specific.
func deploySuccessNextActions(_ *ops.DeployResult, mode topology.Mode, class topology.RuntimeClass) string {
	if mode != "" && class != "" && topology.IsDeferredStart(mode, class) {
		return "Dev-mode dynamic runtime is idle (no start command). Start the dev server: zerops_dev_server action=start. Then run zerops_verify."
	}
	return nextActionDeploySuccess
}

// deploySuggestionForStatus is the FALLBACK suggestion for a non-ACTIVE deploy
// status the classifier did NOT classify — i.e. one with no mapped failure
// phase (CANCELED, or an unknown status). Build / prepare / init guidance is
// owned by the classifier baselines in `ops/deploy_failure.go` and sourced
// into result.Suggestion by deploy_poll (P7 single-owner); this only handles
// the no-classification tail so the response always carries something.
func deploySuggestionForStatus(status string, _ bool) string {
	if status == statusCanceled {
		return "Deploy was canceled. Re-run zerops_deploy."
	}
	return fmt.Sprintf("Deploy ended with status %s — inspect failedPhase + the logs in the response, then redeploy.", status)
}

// deployNextActionForStatus is the FALLBACK next-action for an unclassified
// non-ACTIVE status (see deploySuggestionForStatus — classified failures are
// sourced from the classifier owner by deploy_poll).
func deployNextActionForStatus(status string, _ bool) string {
	if status == statusCanceled {
		return "Re-run zerops_deploy."
	}
	return "Address the failure shown in the response, then re-run zerops_deploy."
}
