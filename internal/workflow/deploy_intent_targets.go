package workflow

import (
	"slices"

	"github.com/zeropsio/zcp/internal/topology"
)

// SnapshotsFromMetas builds minimal ServiceSnapshots from ServiceMeta for
// resolver consumption in non-envelope contexts — the auto-close gate in
// work_session.go evaluates without live API state, so it can't reuse the
// envelope's `buildServiceSnapshots`. For standard pairs, this helper emits
// both the dev-half snapshot (with its stages populated) AND a synthetic
// snapshot per stage (Mode=ModeStage) so DeployIntent.Resolve can answer
// findDevHalfForStage and produce a paired BuildTarget.
//
// Status / Deployed / Resumable / TypeVersion / RuntimeClass stay zero —
// those are live-API state irrelevant to deploy-target resolution, which
// keys only off Mode / CloseDeployMode / GitPushState / StageHostname /
// Hostname.
//
// Skips nil and !IsComplete() metas so callers don't have to pre-filter.
// A pair keyed by its dev pool yields the pool half marked DevIsPool (with
// ModeFor's empty mode) and its stages, as the envelope does.
func SnapshotsFromMetas(metas []*ServiceMeta) []ServiceSnapshot {
	out := make([]ServiceSnapshot, 0, len(metas)*2)
	for _, m := range metas {
		if m == nil || !m.IsComplete() {
			continue
		}
		deployed := m.FirstDeployedAt != ""
		out = append(out, ServiceSnapshot{
			Hostname:         m.Hostname,
			Mode:             m.ModeFor(m.Hostname),
			CloseDeployMode:  m.CloseDeployMode,
			GitPushState:     m.GitPushState,
			BuildIntegration: m.BuildIntegration,
			RemoteURL:        m.RemoteURL,
			TrackedRef:       m.TrackedRef,
			StageHostname:    m.FirstStage().Hostname,
			Stages:           slices.Clone(m.Stages),
			DevIsPool:        m.DevIsPool(),
			Bootstrapped:     true,
			Deployed:         deployed,
		})
		if m.Mode != topology.ModeStandard {
			continue
		}
		for _, stage := range m.Stages {
			out = append(out, ServiceSnapshot{
				Hostname:         stage.Hostname,
				Mode:             topology.ModeStage,
				CloseDeployMode:  m.CloseDeployMode,
				GitPushState:     m.GitPushState,
				BuildIntegration: m.BuildIntegration,
				RemoteURL:        m.RemoteURL,
				TrackedRef:       m.TrackedRef,
				Bootstrapped:     true,
				Deployed:         deployed,
			})
		}
	}
	return out
}

// ResolvedDeployTargets returns the deduplicated set of hostnames whose
// deploy/verify state actually gates auto-close, after DeployIntent.Resolve
// collapses self/cross-deploy and git-push pair-build-target cases.
//
// For closeMode=auto self-deploy modes this returns ws.Services (deduped).
// For git-push standard pairs it collapses to the stage build target so the
// gate doesn't wait for a dev deploy that no longer happens under git-push
// delivery.
//
// Reads service metas via ListServiceMetas. Falls back to ws.Services (with
// dedup) when stateDir is empty, ListServiceMetas errors, or no metas exist
// — degraded mode preserves pre-resolver gate behavior so legacy
// adopted-without-meta paths keep working.
//
// With metas, a service no pair names any more drops out: develop's scope
// admits only hostnames a pair names, so one no pair names is a service whose
// pair went — deleted, pruned, or a dev a retire re-keyed onto its pool — and
// a session of any process stops waiting on it (work sessions are per
// process; the retire cannot reach theirs). A pool keying its pair drops out
// too: nothing builds from it (Resolve), its stage gates in its own right.
func ResolvedDeployTargets(stateDir string, ws *WorkSession) []string {
	if ws == nil {
		return nil
	}
	// RC-B: only RoleRequired services gate completion. Deferred / out-of-scope
	// services stay in ws.Services (visible) but are not deploy targets.
	required := RequiredServices(ws)
	if stateDir == "" {
		return dedupStrings(required)
	}
	metas, err := ListServiceMetas(stateDir)
	if err != nil || len(metas) == 0 {
		return dedupStrings(required)
	}
	snaps := SnapshotsFromMetas(metas)
	named := ManagedRuntimeIndex(metas)
	seen := map[string]bool{}
	var out []string
	for _, h := range required {
		if named[h] == nil {
			continue
		}
		target := snapshotByHostname(snaps, h)
		if target.Hostname == "" {
			target.Hostname = h
		}
		if target.DevIsPool {
			continue
		}
		built := []string{h}
		if bts := Resolve(target, snaps).Builds(); len(bts) > 0 {
			built = built[:0]
			for _, bt := range bts {
				built = append(built, bt.Hostname)
			}
		}
		for _, bt := range built {
			if !seen[bt] {
				seen[bt] = true
				out = append(out, bt)
			}
		}
	}
	return out
}

func dedupStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
