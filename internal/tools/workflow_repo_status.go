package tools

import (
	"context"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// attachRepoStatus populates services[i].Repo for every non-managed
// service by reading its repo state fresh over SSH (docs/spec-workflows.md
// §8 GLC-7, G1/G2) — the tools-layer half of workflow.ApplyRepoStatus, which
// stays SSH-free so workflow/ never imports ops/. No-op outside a
// container (local-mode envelope status doesn't carry a per-service repo
// block in this slice — the bootstrap-time local check in
// checkRepoInitAt covers local mode) or without an SSH deployer.
func attachRepoStatus(ctx context.Context, services []workflow.ServiceSnapshot, ssh ops.SSHDeployer, rt runtime.Info) {
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
		statuses[svc.Hostname] = workflow.RepoStatus{Present: st.Present, Head: st.Head, Baseline: st.Baseline}
	}
	workflow.ApplyRepoStatus(services, statuses)
}
