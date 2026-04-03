package platform

import (
	"context"

	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/types/uuid"
)

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

func (z *ZeropsClient) StartService(ctx context.Context, serviceID string) (*Process, error) {
	pathParam := path.ServiceStackId{Id: uuid.ServiceStackId(serviceID)}
	resp, err := z.handler.PutServiceStackStart(ctx, pathParam)
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

func (z *ZeropsClient) StopService(ctx context.Context, serviceID string) (*Process, error) {
	pathParam := path.ServiceStackId{Id: uuid.ServiceStackId(serviceID)}
	resp, err := z.handler.PutServiceStackStop(ctx, pathParam)
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

func (z *ZeropsClient) RestartService(ctx context.Context, serviceID string) (*Process, error) {
	pathParam := path.ServiceStackId{Id: uuid.ServiceStackId(serviceID)}
	resp, err := z.handler.PutServiceStackRestart(ctx, pathParam)
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

func (z *ZeropsClient) ReloadService(ctx context.Context, serviceID string) (*Process, error) {
	pathParam := path.ServiceStackId{Id: uuid.ServiceStackId(serviceID)}
	resp, err := z.handler.PutServiceStackReload(ctx, pathParam)
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

func (z *ZeropsClient) ConnectSharedStorage(ctx context.Context, serviceID, storageID string) (*Process, error) {
	pathParam := path.ServiceStackId{Id: uuid.ServiceStackId(serviceID)}
	bodyParam := body.PutSharedStorageAction{SharedStorageId: uuid.ServiceStackId(storageID)}
	resp, err := z.handler.PutServiceStackConnectSharedStorage(ctx, pathParam, bodyParam)
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

func (z *ZeropsClient) DisconnectSharedStorage(ctx context.Context, serviceID, storageID string) (*Process, error) {
	pathParam := path.ServiceStackId{Id: uuid.ServiceStackId(serviceID)}
	bodyParam := body.PutSharedStorageAction{SharedStorageId: uuid.ServiceStackId(storageID)}
	resp, err := z.handler.PutServiceStackDisconnectSharedStorage(ctx, pathParam, bodyParam)
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
// Export
// ---------------------------------------------------------------------------

func (z *ZeropsClient) GetProjectExport(ctx context.Context, projectID string) (string, error) {
	pathParam := path.ProjectId{Id: uuid.ProjectId(projectID)}
	resp, err := z.handler.GetProjectExport(ctx, pathParam)
	if err != nil {
		return "", mapSDKError(err, "project")
	}
	out, err := resp.Output()
	if err != nil {
		return "", mapSDKError(err, "project")
	}
	return out.Yaml.Native(), nil
}

func (z *ZeropsClient) GetServiceStackExport(ctx context.Context, serviceID string) (string, error) {
	pathParam := path.ServiceStackId{Id: uuid.ServiceStackId(serviceID)}
	resp, err := z.handler.GetServiceStackExport(ctx, pathParam)
	if err != nil {
		return "", mapSDKError(err, "service")
	}
	out, err := resp.Output()
	if err != nil {
		return "", mapSDKError(err, "service")
	}
	return out.Yaml.Native(), nil
}

// ---------------------------------------------------------------------------
// Scaling
// ---------------------------------------------------------------------------

func (z *ZeropsClient) SetAutoscaling(ctx context.Context, serviceID string, params AutoscalingParams) (*Process, error) {
	pathParam := path.ServiceStackId{Id: uuid.ServiceStackId(serviceID)}

	autoscalingBody := buildAutoscalingBody(params)
	resp, err := z.handler.PutServiceStackAutoscaling(ctx, pathParam, autoscalingBody)
	if err != nil {
		return nil, mapSDKError(err, "service")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "service")
	}
	if out.Process == nil {
		return nil, nil //nolint:nilnil // intentional: nil process means sync (no async process)
	}
	proc := mapProcess(*out.Process)
	return &proc, nil
}
