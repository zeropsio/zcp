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
	nextActionDeploySuccess         = "Run zerops_verify for runtime state."
	nextActionDeployBuildFail       = "Build failed — check buildLogs in response for build output. Fix and redeploy."
	nextActionDeployBuildFailNoLogs = "Build failed and no logs were captured (sub-10s container exit, common for early-failing buildCommands). Re-check zerops.yaml buildCommands syntax + dependency manifests; if recurring, simplify to bisect the failing step."
	nextActionImportSuccess         = "Verify services: zerops_discover. Continue workflow: mount dev, discover env vars, write code, then deploy."
	nextActionImportPartial         = "Check failed processes: zerops_events. Fix and re-import via zerops_workflow."
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

// deploySuggestionForStatus returns a phase-aware suggestion for a non-ACTIVE
// deploy status. The agent needs to know WHICH phase failed AND where to find
// the actual stderr:
//   - BUILD_FAILED: buildCommands in build container → logs in deploy response buildLogs
//   - DEPLOY_FAILED: run.initCommands in runtime container → stderr in runtime logs, NOT buildLogs
//   - PREPARING_RUNTIME_FAILED: run.prepareCommands in runtime prep phase → check both
func deploySuggestionForStatus(status string, hasLogs bool) string {
	switch status {
	case statusBuildFailed:
		if hasLogs {
			return "BUILD phase failed — buildCommands exited non-zero. See buildLogs for build container output. Fix buildCommands in zerops.yaml and redeploy."
		}
		return "BUILD phase failed — build logs unavailable. Check zerops.yaml buildCommands syntax, package manifests, and dependencies."
	case statusDeployFailed:
		return "DEPLOY phase failed — build succeeded, but a run.initCommand crashed the new container on startup. The deploy response's 'error' field identifies the exact failing command. For the actual stderr output, fetch runtime logs: zerops_logs serviceHostname={service} severity=ERROR since=5m. The buildLogs field does NOT contain this error (it's build container output). Common causes: initCommand references /build/source paths baked into build-time caches (move cache commands like artisan config:cache from buildCommands to run.initCommands), DB connection issues during migration, missing env vars at container start."
	case statusPreparingRuntimeFailed:
		if hasLogs {
			return "RUNTIME PREPARE failed — run.prepareCommands exited non-zero before deploy files arrived. " +
				"READ buildLogs below for the exact error. " +
				"Common causes: (1) missing sudo — ALL package install commands need sudo (e.g. sudo apk add --no-cache pkg), containers run as zerops user; " +
				"(2) wrong package name — Alpine PHP extensions use version prefix: php84-ctype, NOT php-ctype; php84-pdo_pgsql, NOT php-pgsql. " +
				"Some extensions are built-in since PHP 8.0 (json, tokenizer) — do NOT try to install them; " +
				"(3) referencing /var/www/ paths (empty during prepare — use addToRunPrepare + /home/zerops/ instead)."
		}
		return "RUNTIME PREPARE failed — run.prepareCommands exited non-zero. Logs unavailable; fetch via zerops_logs serviceHostname={service} severity=ERROR since=5m. " +
			"Most common cause: missing sudo in prepareCommands (containers run as zerops user)."
	case statusCanceled:
		return "Deploy was canceled. Re-run zerops_deploy."
	}
	return fmt.Sprintf("Deploy ended with status %s — see buildLogs for output.", status)
}

// deployNextActionForStatus returns the next-action for a non-ACTIVE status.
//
// `hasLogs` is the same signal threaded into deploySuggestionForStatus —
// the two response fields populate from the same DeployResult and reach
// the agent together; one telling them to "check buildLogs in response"
// while the other says "build logs unavailable" is a self-contradiction
// that surfaced in greenfield-fullstack-multi-runtime (eval suite
// 20260505-151844).
//
// statusDeployFailed deliberately does NOT branch on hasLogs — runtime
// stderr requires a separate `zerops_logs` call (runtime container,
// post-init), the deploy response never carries it.
func deployNextActionForStatus(status string, hasLogs bool) string {
	switch status {
	case statusDeployFailed:
		return "DEPLOY failed — fix run.initCommands in zerops.yaml (NOT buildCommands). Fetch runtime stderr: zerops_logs serviceHostname={target} severity=ERROR since=5m. If the error mentions /build/source paths, a build-time cache (e.g. config:cache) baked build-container paths into the runtime — move that cache command to run.initCommands."
	case statusPreparingRuntimeFailed:
		return "RUNTIME PREPARE failed — fix run.prepareCommands in zerops.yaml (NOT buildCommands, NOT initCommands). " +
			"Checklist: (1) every apk/apt command prefixed with sudo; " +
			"(2) Alpine PHP extensions match version: php84-<ext> for php@8.4; " +
			"(3) prepareCommands run BEFORE deploy files arrive at /var/www — use addToRunPrepare to ship needed files to /home/zerops/."
	case statusBuildFailed:
		if hasLogs {
			return nextActionDeployBuildFail
		}
		return nextActionDeployBuildFailNoLogs
	}
	if hasLogs {
		return nextActionDeployBuildFail
	}
	return nextActionDeployBuildFailNoLogs
}
