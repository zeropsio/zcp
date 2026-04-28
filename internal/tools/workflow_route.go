package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// handleRoute gathers router input from live API + local state and returns flow offerings.
//
// Relocated to its own file by deploy-decomp P5 (tool API split) when
// workflow_strategy.go was deleted along with the legacy action=strategy
// handler. handleRoute is on the route action, not strategy — colocating it
// with the strategy code was incidental.
func handleRoute(ctx context.Context, _ *workflow.Engine, client platform.Client, projectID, stateDir, selfHostname string, rt runtime.Info) (*mcp.CallToolResult, any, error) {
	var liveHostnames []string
	var unmanagedRuntimes []string
	liveStatus := make(map[string]string)

	metas, _ := workflow.ListServiceMetas(stateDir)
	// Pair-keyed index (spec-workflows.md §8 E8): both halves of a
	// standard-mode pair resolve to the same *ServiceMeta; an incomplete meta
	// still appears under its hostname so the unmanaged-vs-known distinction
	// below remains correct for orphan cases.
	metaIdx := workflow.ManagedRuntimeIndex(metas)

	if client != nil && projectID != "" {
		if svcs, err := ops.ListProjectServices(ctx, client, projectID); err == nil {
			for _, s := range svcs {
				if s.IsSystem() || (selfHostname != "" && s.Name == selfHostname) {
					continue
				}
				liveHostnames = append(liveHostnames, s.Name)
				liveStatus[s.Name] = s.Status
				typeName := s.ServiceStackTypeInfo.ServiceStackTypeVersionName
				if !topology.IsManagedService(typeName) {
					if m, ok := metaIdx[s.Name]; !ok || !m.IsComplete() {
						unmanagedRuntimes = append(unmanagedRuntimes, s.Name)
					}
				}
			}
		}
	}

	sessions, _ := workflow.ListSessions(stateDir)
	ws, _ := workflow.CurrentWorkSession(stateDir)
	return jsonResult(workflow.Route(workflow.RouterInput{
		ServiceMetas:      metas,
		ActiveSessions:    sessions,
		LiveServices:      liveHostnames,
		LiveServiceStatus: liveStatus,
		UnmanagedRuntimes: unmanagedRuntimes,
		WorkSession:       ws,
		Environment:       workflow.DetectEnvironment(rt),
	})), nil, nil
}
