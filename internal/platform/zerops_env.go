package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/zeropsio/zerops-go/apiError"
	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/dto/output"
	"github.com/zeropsio/zerops-go/sdkBase"
	"github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/uuid"
)

// ---------------------------------------------------------------------------
// Service environment
// ---------------------------------------------------------------------------

func (z *ZeropsClient) GetServiceEnv(ctx context.Context, serviceID string) ([]ServiceEnvVar, error) {
	pathParam := path.ServiceStackId{Id: uuid.ServiceStackId(serviceID)}
	resp, err := z.handler.GetServiceStackEnv(ctx, pathParam)
	if err != nil {
		return nil, mapSDKError(err, "service")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "service")
	}

	envs := make([]ServiceEnvVar, 0, len(out.Items))
	for _, e := range out.Items {
		envs = append(envs, ServiceEnvVar{
			ID:        e.Id.TypedString().String(),
			Key:       e.Key.String(),
			Content:   string(e.Content),
			Type:      ServiceEnvType(e.Type),
			Sensitive: bool(e.Sensitive),
		})
	}
	return envs, nil
}

// userDataPostBody is the hand-rolled POST /service-stack/{id}/user-data
// wire body. The pinned zerops-go SDK's generated body.UserDataPost has no
// Sensitive field (TestSDKUserDataBody_StillLacksSensitive) even though the
// platform's 2026-08 model requires it on every write, so CreateServiceEnvVar
// sends this directly on the SDK's own authorized transport (sdkBase.Post)
// instead of calling the generated PostServiceStackUserData handler. No
// omitempty on Sensitive: false must land on the wire, never be silently
// dropped.
type userDataPostBody struct {
	Key       string `json:"key"`
	Content   string `json:"content"`
	Sensitive bool   `json:"sensitive"`
}

// userDataErrorResponse decodes the platform's {"error": {...}} envelope on
// a non-2xx service userData write. Hand-decoded (mirrors
// zerops_delegation.go's ListOwnTokenDelegations) so a malformed/absent
// error body maps to a generic PlatformError WITHOUT ever surfacing the raw
// response bytes — a submitted credential could be echoed back verbatim by
// a broken/proxied error response.
type userDataErrorResponse struct {
	Error apiError.Error `json:"error"`
}

func (z *ZeropsClient) CreateServiceEnvVar(ctx context.Context, serviceID, key, content string, sensitive bool) (*Process, error) {
	u := "/api/rest/public/service-stack/" + serviceID + "/user-data"
	reqBody := userDataPostBody{Key: key, Content: content, Sensitive: sensitive}
	sdkResp := sdkBase.Post(ctx, z.env, u, reqBody)
	if sdkResp.Err != nil {
		return nil, mapSDKError(sdkResp.Err, "service")
	}

	status := sdkResp.HttpResponse.StatusCode
	decoder := json.NewDecoder(sdkResp.ResponseData)
	if status < http.StatusMultipleChoices {
		var out output.Process
		if err := decoder.Decode(&out); err != nil {
			return nil, withCause(NewPlatformError(ErrAPIError,
				fmt.Sprintf("service env write: malformed %d response", status),
				"Retry; if it persists the platform API changed — report it"), err)
		}
		proc := mapProcess(out)
		return &proc, nil
	}

	var apiErrResp userDataErrorResponse
	if err := decoder.Decode(&apiErrResp); err != nil {
		return nil, withCause(NewPlatformError(ErrAPIError,
			fmt.Sprintf("service env write: malformed %d response", status),
			"Retry; if it persists the platform API changed — report it"), err)
	}
	apiErrResp.Error.HttpStatusCode = status
	return nil, mapSDKError(apiErrResp.Error, "service")
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

func (z *ZeropsClient) GetProjectEnv(ctx context.Context, projectID string) ([]ProjectEnvVar, error) {
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

	envs := make([]ProjectEnvVar, 0, len(project.EnvList))
	for _, e := range project.EnvList {
		envs = append(envs, ProjectEnvVar{
			ID:        e.Id.TypedString().String(),
			Key:       e.Key.String(),
			Content:   string(e.Content),
			Type:      ProjectEnvType(e.Type),
			Sensitive: bool(e.Sensitive),
			Editable:  bool(e.Editable),
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
