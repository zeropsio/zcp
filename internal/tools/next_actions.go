package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/devsup"
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
// same predicate used by `deploy_subdomain.go::ensurePublicAccess`
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

// deployNextActionsFor is deploySuccessNextActions, told the truth by the
// supervisor when one answers: a dev setup under zcp-devsup starts its declared
// servers itself, so the reply names every declared server with its status —
// read once each starting one has answered, within the start budget; a
// crashed one with its exit and the way back, or as coming back when the
// supervisor's one more start is pending; one still starting or coming back
// with when to look again; a stopped one, whose desired state is stopped, only
// as stopped; once, that systemd restarted every server because the service
// ran out of memory — instead of calling the container idle. A
// container without the supervisor or a declaration keeps today's pointer;
// one whose supervisor does not answer in time says so, never a start.
func deployNextActionsFor(ctx context.Context, ssh ops.SSHDeployer, root string, result *ops.DeployResult, mode topology.Mode, class topology.RuntimeClass) string {
	base := deploySuccessNextActions(result, mode, class)
	if ssh == nil || base == nextActionDeploySuccess {
		return base
	}
	ds, err := ops.ReadDevServersSettled(ctx, ssh, result.TargetService, root)
	var unresponsive *ops.SupervisorUnresponsiveError
	if errors.As(err, &unresponsive) {
		return unresponsive.Error() + " Then run zerops_verify."
	}
	if err != nil || len(ds.Servers()) == 0 {
		return base
	}
	var states, down, unsettled []string
	for _, s := range ds.Servers() {
		st, ok := ds.State.Servers[s.Name]
		switch {
		case !ok:
			states = append(states, s.Name+" not started")
			down = append(down, s.Name)
		case st.Status == devsup.StatusRunning:
			states = append(states, s.Name+" running")
		case st.Status == devsup.StatusStopped:
			states = append(states, s.Name+" stopped")
		case st.Status == devsup.StatusStarting:
			states = append(states, s.Name+" still starting")
			unsettled = append(unsettled, s.Name)
		case st.Status == devsup.StatusCrashed && st.TaskLimit != nil:
			states = append(states, s.Name+" crashed"+exitNote(st.Exit)+": "+ops.TaskLimitNote(*st.TaskLimit, st.RetryPending))
			if st.RetryPending {
				unsettled = append(unsettled, s.Name)
			} else {
				down = append(down, s.Name)
			}
		case st.Status == devsup.StatusCrashed && st.RetryPending:
			states = append(states, s.Name+" crashed while starting"+exitNote(st.Exit)+"; "+ops.RetryPendingNote)
			unsettled = append(unsettled, s.Name)
		default:
			states = append(states, s.Name+" "+string(st.Status)+exitNote(st.Exit))
			down = append(down, s.Name)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Dev servers on %s after the deploy: %s.", result.TargetService, strings.Join(states, ", "))
	for _, s := range ds.Servers() {
		// Systemd restarted every server at once: one sentence says why.
		if st := ds.State.Servers[s.Name]; st.OutOfMemory && st.LastRestartReason != "" {
			fmt.Fprintf(&b, " %s%s. %s", strings.ToUpper(st.LastRestartReason[:1]), st.LastRestartReason[1:], ops.OutOfMemoryHint)
			break
		}
	}
	if len(down) > 0 {
		if nc := result.NotCarried; nc != nil && nc.Count > 0 {
			fmt.Fprintf(&b, " The deploy did not carry %d git-ignored path(s) (%s): reinstall dependencies first.", nc.Count, strings.Join(nc.Sample, ", "))
		}
		fmt.Fprintf(&b, " For each server that is down: read zerops_dev_server action=logs name=%q, fix it, then zerops_dev_server action=start name=%q.", down[0], down[0])
	}
	if len(unsettled) > 0 {
		b.WriteString(" " + ops.RecheckSentence(result.TargetService, "", unsettled))
	}
	b.WriteString(" Then run zerops_verify.")
	return b.String()
}

// exitNote is a stopped or crashed server's last exit, "" when none is known.
func exitNote(exit *devsup.Exit) string {
	switch {
	case exit == nil:
		return ""
	case exit.Signal != "":
		return " (" + exit.Signal + ")"
	}
	return fmt.Sprintf(" (exit %d)", exit.Code)
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
