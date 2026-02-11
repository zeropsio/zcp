package platform

import (
	"context"
	"strings"

	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/dto/input/query"
	"github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/uuid"
)

// ---------------------------------------------------------------------------
// Import / Delete
// ---------------------------------------------------------------------------

func (z *ZeropsClient) ImportServices(ctx context.Context, projectID, yamlContent string) (*ImportResult, error) {
	pathParam := path.ProjectId{Id: uuid.ProjectId(projectID)}
	importBody := body.ServiceStackImport{
		Yaml: types.Text(yamlContent),
	}
	resp, err := z.handler.PostProjectServiceStackImport(ctx, pathParam, importBody)
	if err != nil {
		return nil, mapSDKError(err, "service")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "service")
	}

	result := &ImportResult{
		ProjectID:   out.ProjectId.TypedString().String(),
		ProjectName: out.ProjectName.String(),
	}
	for _, stack := range out.ServiceStacks {
		imported := ImportedServiceStack{
			ID:   stack.Id.TypedString().String(),
			Name: stack.Name.String(),
		}
		if stack.Error != nil {
			imported.Error = &APIError{
				Code:    stack.Error.Code.String(),
				Message: stack.Error.Message.String(),
			}
		}
		for _, proc := range stack.Processes {
			imported.Processes = append(imported.Processes, mapProcess(proc))
		}
		result.ServiceStacks = append(result.ServiceStacks, imported)
	}
	return result, nil
}

func (z *ZeropsClient) DeleteService(ctx context.Context, serviceID string) (*Process, error) {
	pathParam := path.ServiceStackId{Id: uuid.ServiceStackId(serviceID)}
	resp, err := z.handler.DeleteServiceStack(ctx, pathParam)
	if err != nil {
		return nil, mapSDKError(err, "service")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "service")
	}
	proc := mapProcess(out)
	return &proc, nil
}

// ---------------------------------------------------------------------------
// Process
// ---------------------------------------------------------------------------

func (z *ZeropsClient) GetProcess(ctx context.Context, processID string) (*Process, error) {
	pathParam := path.ProcessId{Id: uuid.ProcessId(processID)}
	resp, err := z.handler.GetProcess(ctx, pathParam)
	if err != nil {
		return nil, mapSDKError(err, "process")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "process")
	}
	proc := mapProcess(out)
	return &proc, nil
}

func (z *ZeropsClient) CancelProcess(ctx context.Context, processID string) (*Process, error) {
	pathParam := path.ProcessId{Id: uuid.ProcessId(processID)}
	resp, err := z.handler.PutProcessCancel(ctx, pathParam)
	if err != nil {
		return nil, mapSDKError(err, "process")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "process")
	}
	proc := mapProcess(out)
	return &proc, nil
}

// ---------------------------------------------------------------------------
// Subdomain
// ---------------------------------------------------------------------------

func (z *ZeropsClient) EnableSubdomainAccess(ctx context.Context, serviceID string) (*Process, error) {
	pathParam := path.ServiceStackId{Id: uuid.ServiceStackId(serviceID)}
	resp, err := z.handler.PutServiceStackEnableSubdomainAccess(ctx, pathParam)
	if err != nil {
		return nil, mapSDKError(err, "service")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "service")
	}
	proc := mapProcess(out)
	return &proc, nil
}

func (z *ZeropsClient) DisableSubdomainAccess(ctx context.Context, serviceID string) (*Process, error) {
	pathParam := path.ServiceStackId{Id: uuid.ServiceStackId(serviceID)}
	resp, err := z.handler.PutServiceStackDisableSubdomainAccess(ctx, pathParam)
	if err != nil {
		return nil, mapSDKError(err, "service")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "service")
	}
	proc := mapProcess(out)
	return &proc, nil
}

// ---------------------------------------------------------------------------
// Logs
// ---------------------------------------------------------------------------

func (z *ZeropsClient) GetProjectLog(ctx context.Context, projectID string) (*LogAccess, error) {
	pathParam := path.ProjectId{Id: uuid.ProjectId(projectID)}
	queryParam := query.GetProjectLog{}

	resp, err := z.handler.GetProjectLog(ctx, pathParam, queryParam)
	if err != nil {
		return nil, mapSDKError(err, "project")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "project")
	}

	urlStr := out.Url.String()
	urlStr = strings.TrimPrefix(urlStr, "GET ")
	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		urlStr = "https://" + urlStr
	}

	return &LogAccess{
		URL:         urlStr,
		AccessToken: string(out.AccessToken),
	}, nil
}

// ---------------------------------------------------------------------------
// Activity search
// ---------------------------------------------------------------------------

func (z *ZeropsClient) SearchProcesses(ctx context.Context, projectID string, limit int) ([]ProcessEvent, error) {
	clientID, err := z.getClientID(ctx)
	if err != nil {
		return nil, err
	}

	filter := body.EsFilter{
		Search: body.EsFilterSearch{
			body.EsSearchItem{
				Name:     types.NewString("clientId"),
				Operator: types.NewString("eq"),
				Value:    types.NewString(clientID),
			},
		},
		Sort: body.EsFilterSort{
			body.EsSortItem{
				Name:      types.NewString("created"),
				Ascending: types.NewBoolNull(false),
			},
		},
		Limit: types.NewIntNull(limit),
	}

	resp, err := z.handler.PostProcessSearch(ctx, filter)
	if err != nil {
		return nil, mapSDKError(err, "process")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "process")
	}

	events := make([]ProcessEvent, 0, len(out.Items))
	for _, p := range out.Items {
		pid := p.ProjectId.TypedString().String()
		if pid != projectID {
			continue
		}
		events = append(events, mapEsProcessEvent(p))
	}
	return events, nil
}

func (z *ZeropsClient) SearchAppVersions(ctx context.Context, projectID string, limit int) ([]AppVersionEvent, error) {
	clientID, err := z.getClientID(ctx)
	if err != nil {
		return nil, err
	}

	filter := body.EsFilter{
		Search: body.EsFilterSearch{
			body.EsSearchItem{
				Name:     types.NewString("clientId"),
				Operator: types.NewString("eq"),
				Value:    types.NewString(clientID),
			},
		},
		Sort: body.EsFilterSort{
			body.EsSortItem{
				Name:      types.NewString("created"),
				Ascending: types.NewBoolNull(false),
			},
		},
		Limit: types.NewIntNull(limit),
	}

	resp, err := z.handler.PostAppVersionSearch(ctx, filter)
	if err != nil {
		return nil, mapSDKError(err, "service")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "service")
	}

	events := make([]AppVersionEvent, 0, len(out.Items))
	for _, av := range out.Items {
		pid := av.ProjectId.TypedString().String()
		if pid != projectID {
			continue
		}
		events = append(events, mapEsAppVersionEvent(av))
	}
	return events, nil
}
