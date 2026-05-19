package workflow

import "github.com/zeropsio/zcp/internal/topology"

// SnapshotsFromMetas builds minimal ServiceSnapshots from ServiceMeta for
// resolver consumption in non-envelope contexts — the auto-close gate in
// work_session.go evaluates without live API state, so it can't reuse the
// envelope's `buildServiceSnapshots`. For standard pairs, this helper emits
// both the dev-half snapshot (with StageHostname populated) AND a synthetic
// stage-half snapshot (Mode=ModeStage) so DeployIntent.Resolve can answer
// findDevHalfForStage and produce a paired BuildTarget.
//
// Status / Deployed / Resumable / TypeVersion / RuntimeClass stay zero —
// those are live-API state irrelevant to deploy-target resolution, which
// keys only off Mode / CloseDeployMode / GitPushState / StageHostname /
// Hostname.
//
// Skips nil and !IsComplete() metas so callers don't have to pre-filter.
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
			StageHostname:    m.StageHostname,
			Bootstrapped:     true,
			Deployed:         deployed,
		})
		if m.Mode == topology.ModeStandard && m.StageHostname != "" {
			out = append(out, ServiceSnapshot{
				Hostname:         m.StageHostname,
				Mode:             topology.ModeStage,
				CloseDeployMode:  m.CloseDeployMode,
				GitPushState:     m.GitPushState,
				BuildIntegration: m.BuildIntegration,
				RemoteURL:        m.RemoteURL,
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
func ResolvedDeployTargets(stateDir string, ws *WorkSession) []string {
	if ws == nil {
		return nil
	}
	if stateDir == "" {
		return dedupStrings(ws.Services)
	}
	metas, err := ListServiceMetas(stateDir)
	if err != nil || len(metas) == 0 {
		return dedupStrings(ws.Services)
	}
	snaps := SnapshotsFromMetas(metas)
	seen := map[string]bool{}
	var out []string
	for _, h := range ws.Services {
		target := snapshotByHostname(snaps, h)
		if target.Hostname == "" {
			target.Hostname = h
		}
		bt := Resolve(target, snaps).BuildTarget
		if bt == "" {
			bt = h
		}
		if !seen[bt] {
			seen[bt] = true
			out = append(out, bt)
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
