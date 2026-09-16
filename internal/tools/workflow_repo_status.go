package tools

import (
	"context"
	"sync"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// attachRepoStatusConcurrency bounds how many per-service SSH round trips
// (plus the accompanying ServiceMeta disk read) attachRepoStatus runs at
// once, so a project with many runtime services doesn't serialize several
// seconds of SSH latency onto every status call.
const attachRepoStatusConcurrency = 4

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
//
// Per-service reads (SSH round trip + ServiceMeta disk read) run
// concurrently, bounded by attachRepoStatusConcurrency, since each is
// independent I/O with no shared state until the result is stored. The
// map write is the only critical section, guarded by mu; ctx cancellation
// propagates into ops.ReadRepoStatus and this function still returns only
// after every goroutine has finished (wg.Wait), so no goroutine outlives
// the call.
func attachRepoStatus(ctx context.Context, services []workflow.ServiceSnapshot, ssh ops.SSHDeployer, rt runtime.Info, stateDir string) {
	if !rt.InContainer || ssh == nil {
		return
	}
	statuses := make(map[string]workflow.RepoStatus, len(services))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, attachRepoStatusConcurrency)
	for _, svc := range services {
		if svc.RuntimeClass == topology.RuntimeManaged || svc.RuntimeClass == topology.RuntimeUnknown {
			continue
		}
		wg.Add(1)
		go func(svc workflow.ServiceSnapshot) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			st, err := ops.ReadRepoStatus(ctx, ssh, svc.Hostname)
			if err != nil {
				return // best-effort — a read failure just leaves this hostname without a repo block
			}
			status := workflow.RepoStatus{Present: st.Present, Head: st.Head, RepoState: st.RepoState}
			if meta, metaErr := workflow.FindServiceMeta(stateDir, svc.Hostname); metaErr == nil && meta != nil && meta.Hostname == svc.Hostname && meta.Repo != nil {
				status.Baseline = meta.Repo.BaselineAppVersion
				status.Provenance = meta.Repo.Provenance
			}

			mu.Lock()
			statuses[svc.Hostname] = status
			mu.Unlock()
		}(svc)
	}
	wg.Wait()
	workflow.ApplyRepoStatus(services, statuses)
}
