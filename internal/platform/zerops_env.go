package platform

import (
	"context"

	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/uuid"
)

// ---------------------------------------------------------------------------
// Service environment
// ---------------------------------------------------------------------------

func (z *ZeropsClient) GetServiceEnv(ctx context.Context, serviceID string) ([]EnvVar, error) {
	pathParam := path.ServiceStackId{Id: uuid.ServiceStackId(serviceID)}
	resp, err := z.handler.GetServiceStackEnv(ctx, pathParam)
	if err != nil {
		return nil, mapSDKError(err, "service")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "service")
	}

	envs := make([]EnvVar, 0, len(out.Items))
	for _, e := range out.Items {
		envs = append(envs, EnvVar{
			ID:      e.Id.TypedString().String(),
			Key:     e.Key.String(),
			Content: string(e.Content),
		})
	}
	return envs, nil
}

func (z *ZeropsClient) SetServiceEnvFile(ctx context.Context, serviceID string, content string) (*Process, error) {
	pathParam := path.ServiceStackId{Id: uuid.ServiceStackId(serviceID)}
	envBody := body.UserDataPutEnvFile{
		EnvFile: types.NewText(content),
	}
	resp, err := z.handler.PutServiceStackUserDataEnvFile(ctx, pathParam, envBody)
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

func (z *ZeropsClient) DeleteUserData(ctx context.Context, userDataID string) (*Process, error) {
	pathParam := path.UserDataId{Id: uuid.UserDataId(userDataID)}
	resp, err := z.handler.DeleteUserData(ctx, pathParam)
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
// Project environment
// ---------------------------------------------------------------------------

func (z *ZeropsClient) GetProjectEnv(ctx context.Context, projectID string) ([]EnvVar, error) {
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
			body.EsSearchItem{
				Name:     types.NewString("id"),
				Operator: types.NewString("eq"),
				Value:    types.NewString(projectID),
			},
		},
		Sort: body.EsFilterSort{},
	}
	resp, err := z.handler.PostProjectSearch(ctx, filter)
	if err != nil {
		return nil, mapSDKError(err, "project")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "project")
	}
	if len(out.Items) == 0 {
		return nil, NewPlatformError(ErrServiceNotFound, "project not found", "Check projectId")
	}
	project := out.Items[0]

	envs := make([]EnvVar, 0, len(project.EnvList))
	for _, e := range project.EnvList {
		envs = append(envs, EnvVar{
			ID:      e.Id.TypedString().String(),
			Key:     e.Key.String(),
			Content: string(e.Content),
		})
	}
	return envs, nil
}

func (z *ZeropsClient) CreateProjectEnv(ctx context.Context, projectID, key, content string, sensitive bool) (*Process, error) {
	pathParam := path.ProjectId{Id: uuid.ProjectId(projectID)}
	envBody := body.ProjectEnvPost{
		Key:       types.NewString(key),
		Content:   types.NewText(content),
		Sensitive: types.NewBool(sensitive),
	}
	resp, err := z.handler.PostProjectEnv(ctx, pathParam, envBody)
	if err != nil {
		return nil, mapSDKError(err, "project")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "project")
	}
	proc := mapProcess(out)
	return &proc, nil
}

func (z *ZeropsClient) DeleteProjectEnv(ctx context.Context, envID string) (*Process, error) {
	pathParam := path.ProjectEnvId{Id: uuid.EnvId(envID)}
	resp, err := z.handler.DeleteProjectEnv(ctx, pathParam)
	if err != nil {
		return nil, mapSDKError(err, "project")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "project")
	}
	proc := mapProcess(out)
	return &proc, nil
}
