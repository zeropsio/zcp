package tools

import (
	"context"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// attachRepoStatus populates services[i].Repo for every non-managed
// service (docs/spec-workflows.md §8 GLC-7, G1/G2) — the tools-layer half
// of workflow.ApplyRepoStatus, which stays SSH-free so workflow/ never
// imports ops/. Present/Head are read fresh over SSH on every call. Baseline and
// Provenance are recorded adoption facts read from
// ServiceMeta.Repo under stateDir, never live state (AdoptBaseline's case
// isn't something a live git read can reconstruct). No-op outside a
// container (local-mode envelope status doesn't carry a per-service repo
// block in this slice — the bootstrap-time local check in
// checkRepoInitAt covers local mode) or without an SSH deployer.
func attachRepoStatus(ctx context.Context, services []workflow.ServiceSnapshot, ssh ops.SSHDeployer, rt runtime.Info, stateDir string) {
	if !rt.InContainer || ssh == nil {
		return
	}
	statuses := make(map[string]workflow.RepoStatus, len(services))
	for _, svc := range services {
		if svc.RuntimeClass == topology.RuntimeManaged || svc.RuntimeClass == topology.RuntimeUnknown {
			continue
		}
		st, err := ops.ReadRepoStatus(ctx, ssh, svc.Hostname)
		if err != nil {
			continue // best-effort — a read failure just leaves this hostname without a repo block
		}
		status := workflow.RepoStatus{Present: st.Present, Head: st.Head, RepoState: st.RepoState}
		if meta, metaErr := workflow.FindServiceMeta(stateDir, svc.Hostname); metaErr == nil && meta != nil && meta.Hostname == svc.Hostname && meta.Repo != nil {
			status.Baseline = meta.Repo.BaselineAppVersion
			status.Provenance = meta.Repo.Provenance
		}
		statuses[svc.Hostname] = status
	}
	workflow.ApplyRepoStatus(services, statuses)
}
