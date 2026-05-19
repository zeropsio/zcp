package tools

import (
	"github.com/zeropsio/zcp/internal/workflow"
)

// resolveBuildTargetForHost returns the post-build runtime hostname for a
// service whose ACTUAL meta says it pushes under git-push and lands the
// build elsewhere. For direct-delivery shapes (closeMode=auto / manual /
// unset, OR git-push without GitPushConfigured) the returned hostname
// equals "" so the caller keeps the input host — dev half of a standard
// pair under closeMode=auto deploys to itself, and Phase 4 redirect must
// not steal that lookup.
//
// Returns ("", "") when no meta exists OR when the meta's current
// delivery resolves to direct — caller falls back to the input hostname
// unchanged in both cases.
//
// Used by verify + record-deploy in Phase 4 to redirect a record-deploy
// on the dev half (where the agent's bytes were pushed under git-push)
// onto the stage half (where Zerops actually rebuilt the runtime).
// Without this redirect an agent under git-push acks the wrong hostname
// and the develop session's gate never closes. WITH this redirect firing
// on closeMode=auto, the agent's verify on the dev half is silently
// answered against stage, which broke the
// greenfield-node-postgres-dev-stage flow-eval run on 2026-05-19.
//
// The resolver consumes the meta's ACTUAL CloseDeployMode +
// GitPushState (Deployed=true to skip the FirstDeployBypass branch —
// this helper only fires after a deploy has landed).
func resolveBuildTargetForHost(stateDir, host string) (string, string) {
	meta, _ := workflow.FindServiceMeta(stateDir, host)
	if meta == nil || !meta.IsComplete() {
		return "", ""
	}
	snap := workflow.ServiceSnapshot{
		Hostname:        host,
		Mode:            meta.ModeFor(host),
		CloseDeployMode: meta.CloseDeployMode,
		GitPushState:    meta.GitPushState,
		StageHostname:   meta.StageHostname,
		Deployed:        true,
	}
	snaps := workflow.SnapshotsFromMetas([]*workflow.ServiceMeta{meta})
	intent := workflow.Resolve(snap, snaps)
	if intent.Delivery != workflow.DeployDeliveryGitPush {
		// Direct / manual delivery: no redirect. The input host IS the
		// build target (self-deploy semantics); returning empty signals
		// the caller to keep the input.
		return "", ""
	}
	return intent.BuildTarget, intent.BuildSetup
}
