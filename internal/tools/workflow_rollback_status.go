package tools

import (
	"context"
	"sync"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// attachRollbackInfoConcurrency bounds how many per-service
// ops.AppVersionCandidates platform reads attachRollbackInfo runs at
// once, so a project with many runtime services doesn't serialize several
// seconds of API latency onto every status call.
const attachRollbackInfoConcurrency = 4

// attachRollbackInfo populates services[i].Rollback for every non-managed
// service (docs/spec-workflows.md §8 R2 generalised / §12.6 GF-8) — the
// tools-layer half of workflow.ApplyRollbackInfo, which stays ops-free so
// workflow/ never imports ops/. Lists the project's services ONCE to map
// hostname -> serviceID, then reads ops.AppVersionCandidates per runtime
// service — best-effort: a read failure (or a hostname with no live
// service) just leaves that hostname without a rollback block, mirroring
// attachRepoStatus. No-op without a platform client or project.
//
// The per-service ops.AppVersionCandidates reads run concurrently,
// bounded by attachRollbackInfoConcurrency, once the single hostname ->
// serviceID lookup is done. The map write is the only critical section,
// guarded by mu; this function returns only after every goroutine has
// finished (wg.Wait), so ctx cancellation never leaves one running past
// return.
func attachRollbackInfo(ctx context.Context, services []workflow.ServiceSnapshot, client platform.Client, projectID string) {
	if client == nil || projectID == "" {
		return
	}
	live, err := ops.ListProjectServices(ctx, client, projectID)
	if err != nil {
		return
	}
	idByHostname := make(map[string]string, len(live))
	for _, s := range live {
		idByHostname[s.Name] = s.ID
	}

	infos := make(map[string]workflow.RollbackInfo, len(services))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, attachRollbackInfoConcurrency)
	for _, svc := range services {
		if svc.RuntimeClass == topology.RuntimeManaged || svc.RuntimeClass == topology.RuntimeUnknown {
			continue
		}
		id, ok := idByHostname[svc.Hostname]
		if !ok {
			continue
		}
		wg.Add(1)
		go func(svc workflow.ServiceSnapshot, id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			candidates, candErr := ops.AppVersionCandidates(ctx, client, id)
			if candErr != nil {
				return
			}
			var info workflow.RollbackInfo
			for _, c := range candidates {
				switch {
				case c.Active:
					info.Active = c.ID
				case c.Backup:
					info.Backup = append(info.Backup, c.ID)
				}
			}
			if info.Active == "" && len(info.Backup) == 0 {
				return
			}

			mu.Lock()
			infos[svc.Hostname] = info
			mu.Unlock()
		}(svc, id)
	}
	wg.Wait()
	workflow.ApplyRollbackInfo(services, infos)
}
