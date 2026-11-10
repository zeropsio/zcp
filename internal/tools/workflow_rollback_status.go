package tools

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// attachRollbackInfo populates services[i].Rollback for every app-runtime
// service (docs/spec-workflows.md §8 R2 generalised / §12.6 GF-8) — the
// tools-layer half of workflow.ApplyRollbackInfo, which stays ops-free so
// workflow/ never imports ops/. Lists the project's services ONCE to map
// hostname -> serviceID, then reads ops.AppVersionCandidates per runtime
// service — best-effort: a read failure (or a hostname with no live
// service) just leaves that hostname without a rollback block, mirroring
// attachRepoStatus. No-op without a platform client or project.
//
// The per-service ops.AppVersionCandidates reads run concurrently once the
// single hostname -> serviceID lookup is done. The map write is guarded by mu;
// this function returns only after every goroutine has finished (wg.Wait), so
// ctx cancellation never leaves one running past return.
func attachRollbackInfo(ctx context.Context, services []workflow.ServiceSnapshot, client platform.Client, projectID string) {
	if client == nil || projectID == "" {
		return
	}
	started := time.Now()
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
	count := 0
	for _, svc := range services {
		if !svc.RuntimeClass.IsAppRuntime() {
			continue
		}
		id, ok := idByHostname[svc.Hostname]
		if !ok {
			continue
		}
		count++
		wg.Add(1)
		go func(svc workflow.ServiceSnapshot, id string) {
			defer wg.Done()

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
	fmt.Fprintf(os.Stderr, "[zcp] rollback read services=%d elapsedMs=%d\n", count, time.Since(started).Milliseconds())
}
