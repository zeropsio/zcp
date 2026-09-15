package tools

import (
	"context"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// attachRollbackInfo populates services[i].Rollback for every non-managed
// service (docs/spec-workflows.md §8 R2 generalised / §12.6 GF-8) — the
// tools-layer half of workflow.ApplyRollbackInfo, which stays ops-free so
// workflow/ never imports ops/. Lists the project's services ONCE to map
// hostname -> serviceID, then reads ops.AppVersionCandidates per runtime
// service — best-effort: a read failure (or a hostname with no live
// service) just leaves that hostname without a rollback block, mirroring
// attachRepoStatus. No-op without a platform client or project.
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
	for _, svc := range services {
		if svc.RuntimeClass == topology.RuntimeManaged || svc.RuntimeClass == topology.RuntimeUnknown {
			continue
		}
		id, ok := idByHostname[svc.Hostname]
		if !ok {
			continue
		}
		candidates, candErr := ops.AppVersionCandidates(ctx, client, id)
		if candErr != nil {
			continue
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
			continue
		}
		infos[svc.Hostname] = info
	}
	workflow.ApplyRollbackInfo(services, infos)
}
