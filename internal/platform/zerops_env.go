package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"

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
// instead of going through the SDK's generated per-endpoint handler. No
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
		return nil, fmt.Errorf("create service env var %q: %w", key, mapSDKError(sdkResp.Err, "service"))
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
	if sensitive {
		scrubAPIError(&apiErrResp.Error, content)
	}
	return nil, fmt.Errorf("create service env var %q: %w", key, mapSDKError(apiErrResp.Error, "service"))
}

// scrubAPIError removes secret from what a well-formed refusal echoed back —
// its code, its message and every string of its meta, map keys included — as
// the Swarm and Box clients scrub theirs (SwarmClient.scrub, scrubBoxError).
// A value sent as a secret is one no caller may see again: zcp's own writes
// (the Mate's HQ credential, a dev's secrets copied to a pool) send values no
// agent ever saw, and the refusal's words reach errors, logs and tool results.
// A server echoing the JSON body it was sent carries the value as JSON
// encoded it, so every form secretForms names is removed (review R4-11); a
// truncated or partial echo is not recognized.
func scrubAPIError(e *apiError.Error, secret string) {
	forms := secretForms(secret)
	if len(forms) == 0 {
		return
	}
	e.ErrorCode = redactForms(e.ErrorCode, forms)
	e.Message = redactForms(e.Message, forms)
	e.Meta = scrubDecodedJSON(e.Meta, forms)
}

// secretForms are the forms secret takes in an echo: as JSON encodes it —
// Go's HTML-safe escaping (`<` as \u003c, the body zcp sends) and plain
// escaping — then as it is, longest first, each once; none for "".
func secretForms(secret string) []string {
	if secret == "" {
		return nil
	}
	forms := []string{secret}
	if raw, err := json.Marshal(secret); err == nil {
		forms = append(forms, string(raw[1:len(raw)-1]))
	}
	var plain bytes.Buffer
	enc := json.NewEncoder(&plain)
	enc.SetEscapeHTML(false)
	if enc.Encode(secret) == nil {
		if raw := bytes.TrimSpace(plain.Bytes()); len(raw) >= 2 {
			forms = append(forms, string(raw[1:len(raw)-1]))
		}
	}
	slices.SortStableFunc(forms, func(a, b string) int { return len(b) - len(a) })
	return slices.Compact(forms)
}

// redactForms is text with every one of forms removed (redactToken).
func redactForms(text string, forms []string) string {
	for _, form := range forms {
		text = redactToken(text, form)
	}
	return text
}

// scrubDecodedJSON is v — a decoded JSON value — with every one of forms
// removed from every string in it.
func scrubDecodedJSON(v any, forms []string) any {
	switch t := v.(type) {
	case string:
		return redactForms(t, forms)
	case []any:
		for i := range t {
			t[i] = scrubDecodedJSON(t[i], forms)
		}
		return t
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[redactForms(k, forms)] = scrubDecodedJSON(x, forms)
		}
		return out
	}
	return v
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
	clientID, err := z.OrgID(ctx)
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
