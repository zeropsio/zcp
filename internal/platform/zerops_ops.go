package platform

import (
	"context"

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
