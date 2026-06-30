package platform

import (
	"context"

	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/dto/input/query"
	"github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/uuid"
)

// directListLimit is a generous page size for the direct (non-ES) list reads.
// The direct endpoints paginate via limit/offset; without an explicit limit
// the server default page size can truncate. A project rarely holds more than
// a few dozen services / live processes, so one page of 1000 covers it.
const directListLimit = 1000

// ListServicesDirect reads the project's service stacks via the DIRECT
// GET /project/{id}/service-stack endpoint (SDK GetProjectServiceStack),
// NOT the Elasticsearch-backed /service-stack/search. The direct read
// reflects authoritative REST state immediately after a mutation, where the
// ES search lags by several seconds. Each item maps via mapFullServiceStack
// (the same mapper GetService by-id uses), so the shape matches GetService.
func (z *ZeropsClient) ListServicesDirect(ctx context.Context, projectID string) ([]ServiceStack, error) {
	pathParam := path.ProjectId{Id: uuid.ProjectId(projectID)}
	queryParam := query.ListProjectServiceStacks{
		Limit: types.NewIntNull(directListLimit),
	}

	resp, err := z.handler.GetProjectServiceStack(ctx, pathParam, queryParam)
	if err != nil {
		return nil, mapSDKError(err, "service")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "service")
	}

	services := make([]ServiceStack, 0, len(out.List))
	for _, s := range out.List {
		svc := mapFullServiceStack(s)
		if svc.ProjectID == projectID {
			services = append(services, svc)
		}
	}
	return services, nil
}

// GetProjectProcessesDirect reads all of a project's processes via the DIRECT
// GET /project/{id}/process endpoint (SDK GetProjectProcess), NOT the
// Elasticsearch-backed /process/search. Returns ALL processes (live + terminal)
// mapped via mapProcess (each carries the embedded appVersion phase); the caller
// (ops.ProjectActivity) filters to live + sorts newest-first. Used for live
// in-flight detection where the ES search lags writes.
func (z *ZeropsClient) GetProjectProcessesDirect(ctx context.Context, projectID string) ([]Process, error) {
	pathParam := path.ProjectId{Id: uuid.ProjectId(projectID)}
	queryParam := query.ListProjectProcesses{
		Limit: types.NewIntNull(directListLimit),
	}

	resp, err := z.handler.GetProjectProcess(ctx, pathParam, queryParam)
	if err != nil {
		return nil, mapSDKError(err, "process")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "process")
	}

	processes := make([]Process, 0, len(out.List))
	for _, p := range out.List {
		processes = append(processes, mapProcess(p))
	}
	return processes, nil
}
