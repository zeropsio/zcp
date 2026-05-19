package tools

import (
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// resolveBuildTargetForHost returns the post-build runtime hostname for a
// service that may be a push source under git-push delivery (i.e. dev half
// of a standard pair). For self-deploy / direct-delivery shapes the
// returned hostname equals the input. Returns ("", "") when no meta exists
// — caller falls back to the input hostname.
//
// Used by verify + record-deploy in Phase 4 to redirect a record-deploy on
// the dev half (where the agent's bytes were pushed) onto the stage half
// (where Zerops actually rebuilt the runtime). Without this redirect an
// agent under git-push acks the wrong hostname and the develop session's
// gate never closes.
//
// Treats the meta as anticipating git-push delivery (Deployed=true,
// CloseDeployMode=git-push, GitPushConfigured) regardless of the meta's
// current closeMode — record-deploy / verify run AFTER the first deploy
// landed, and the build target is decided by topology, not by whichever
// closeMode value happens to be set right now (the user may have wired
// integration without flipping closeMode yet).
func resolveBuildTargetForHost(stateDir, host string) (string, string) {
	meta, _ := workflow.FindServiceMeta(stateDir, host)
	if meta == nil || !meta.IsComplete() {
		return "", ""
	}
	snap := workflow.ServiceSnapshot{
		Hostname:        host,
		Mode:            meta.ModeFor(host),
		CloseDeployMode: topology.CloseModeGitPush,
		GitPushState:    topology.GitPushConfigured,
		StageHostname:   meta.StageHostname,
		Deployed:        true,
	}
	snaps := workflow.SnapshotsFromMetas([]*workflow.ServiceMeta{meta})
	intent := workflow.Resolve(snap, snaps)
	return intent.BuildTarget, intent.BuildSetup
}
