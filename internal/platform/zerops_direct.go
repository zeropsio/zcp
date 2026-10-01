package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/zeropsio/zerops-go/apiError"
	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/dto/input/query"
	"github.com/zeropsio/zerops-go/dto/output"
	"github.com/zeropsio/zerops-go/sdkBase"
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

// processErrorCode is the platform process DTO's `error` subfield — the
// FAILURE reason for processes where it lives ONLY there, not in
// PublicMeta (docs/spec-workflows.md §8 O3; live-verified 2026-09-13: a
// redundant enable-subdomain-access request ends FAILED with
// publicMeta: null and error: {"code":"noSubdomainPorts", "message":"no
// http ports found for subdomain support"}). The pinned SDK's typed
// output.Process has no `error` field, so its normal decode silently
// drops it.
type processErrorCode struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// processRawErrors mirrors just enough of the process list JSON shape to
// recover each entry's `error` subfield by index (same order as
// output.ProcessList.List, decoded from the same response body).
type processRawErrors struct {
	List []struct {
		Error *processErrorCode `json:"error"`
	} `json:"list"`
}

// directProcessErrorResponse decodes the platform's {"error": {...}}
// envelope on a non-2xx direct process-list read.
type directProcessErrorResponse struct {
	Error apiError.Error `json:"error"`
}

// GetProjectProcessesDirect reads all of a project's processes via the DIRECT
// GET /project/{id}/process endpoint, NOT the Elasticsearch-backed
// /process/search. Returns ALL processes (live + terminal) mapped via
// mapProcess (each carries the embedded appVersion phase); the caller
// (ops.ProjectActivity) filters to live + sorts newest-first. Used for live
// in-flight detection where the ES search lags writes.
//
// Hand-rolled on the SDK's authorized transport (sdkBase.Get), not the
// generated GetProjectProcess handler: the handler's typed decode discards
// the response body after mapping into output.ProcessList, so there is no
// way to recover the `error` subfield afterward. This reads the raw body
// once and decodes it twice — once into output.ProcessList (the fields
// mapProcess already knows), once into processRawErrors (the extra field
// the SDK type lacks) — and folds error.code (+": "+message) into
// Process.FailReason wherever the PublicMeta path (mapProcess) yielded
// none.
func (z *ZeropsClient) GetProjectProcessesDirect(ctx context.Context, projectID string) ([]Process, error) {
	raw, err := z.directGet(ctx, fmt.Sprintf("/api/rest/public/project/%s/process?limit=%d", projectID, directListLimit), "process")
	if err != nil {
		return nil, err
	}

	var out output.ProcessList
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, withCause(NewPlatformError(ErrAPIError,
			"project processes: malformed response",
			"Retry; if it persists the platform API changed — report it"), err)
	}

	var rawErrs processRawErrors
	_ = json.Unmarshal(raw, &rawErrs) // best-effort: an absent/malformed error subfield leaves FailReason as PublicMeta gives it

	processes := make([]Process, 0, len(out.List))
	for i, p := range out.List {
		proc := mapProcess(p)
		if i < len(rawErrs.List) {
			foldProcessError(&proc, rawErrs.List[i].Error)
		}
		processes = append(processes, proc)
	}
	return processes, nil
}

// directGet reads one public-API path on the SDK's authorized transport and
// returns the raw body of a 2xx answer; a non-2xx answer maps to the
// platform's error for entity.
func (z *ZeropsClient) directGet(ctx context.Context, u, entity string) ([]byte, error) {
	sdkResp := sdkBase.Get(ctx, z.env, u)
	if sdkResp.Err != nil {
		return nil, mapSDKError(sdkResp.Err, entity)
	}
	status := sdkResp.HttpResponse.StatusCode
	raw := sdkResp.ResponseData.Bytes()
	if status >= http.StatusMultipleChoices {
		var apiErrResp directProcessErrorResponse
		if err := json.Unmarshal(raw, &apiErrResp); err != nil {
			pe := withCause(NewPlatformError(ErrAPIError,
				fmt.Sprintf("%s: malformed %d response", entity, status),
				"Retry; if it persists the platform API changed — report it"), err)
			// A gateway's 5xx page in front of the API: the platform's side.
			pe.serverSide = status >= http.StatusInternalServerError
			return nil, pe
		}
		apiErrResp.Error.HttpStatusCode = status
		return nil, mapSDKError(apiErrResp.Error, entity)
	}
	return raw, nil
}

// foldProcessError puts a process DTO's `error` (code, then ": " and its
// message) into FailReason wherever the PublicMeta path left none.
func foldProcessError(proc *Process, e *processErrorCode) {
	if proc.FailReason != nil || e == nil || e.Code == "" {
		return
	}
	fr := e.Code
	if e.Message != "" {
		fr += ": " + e.Message
	}
	proc.FailReason = &fr
}
