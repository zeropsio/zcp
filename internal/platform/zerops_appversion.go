package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/dto/input/query"
	"github.com/zeropsio/zerops-go/dto/output"
	"github.com/zeropsio/zerops-go/sdkBase"
	"github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/uuid"
)

// GetAppVersionAppCode returns the download URL for an app version's
// source archive (the bytes uploaded at deploy time) — used by the
// ResolveCanonicalSetup cascade (P1) step 4 to recover the deployed
// zerops.yaml when prior platform-read steps miss.
//
// Returns the bare URL string. Callers fetch the archive over HTTP,
// extract zerops.yaml, and parse setup blocks. No retry or cache here
// — single GET to obtain a fresh signed URL.
func (z *ZeropsClient) GetAppVersionAppCode(ctx context.Context, appVersionID string) (string, error) {
	pathParam := path.AppVersionId{Id: uuid.AppVersionId(appVersionID)}
	resp, err := z.handler.GetAppVersionAppCode(ctx, pathParam)
	if err != nil {
		return "", mapSDKError(err, "appVersion")
	}
	out, err := resp.Output()
	if err != nil {
		return "", mapSDKError(err, "appVersion")
	}
	return out.Url.String(), nil
}

// userDataKind classifies an app-version UserDataList record. Only
// kindRunEnvVariable records are genuine yaml-baked run.envVariables; the
// platform's UserDataList is a SUPERSET that also carries SYSTEM intrinsic
// vars (hostname, ZEROPS_*, …) and the ZEROPS_YAML blob.
type userDataKind int

const (
	kindRunEnvVariable userDataKind = iota // zerops.yaml run.envVariables (Type USER, editable:false)
	kindIntrinsic                          // platform-injected (Type SYSTEM)
	kindZeropsYaml                         // the ZEROPS_YAML deployed-yaml blob
)

// zeropsYamlUserDataKey is the well-known key of the full deployed-yaml blob
// carried in UserDataList — never a run.envVariables var.
const zeropsYamlUserDataKey = "ZEROPS_YAML"

// classifyAppVersionUserData decides whether a UserDataList record is a
// genuine yaml-baked run.envVariables var. It is the single classifier used
// by BOTH the real client (GetAppVersionUserData) and the mock, so a test
// cannot model a shape the real API can't produce.
//
// 2026-08 model (spec docs/spec-zerops-env-lifecycle.md §1, `[LIVE 08-21]`):
// the app-version userDataList enum collapsed to USER|SYSTEM — yaml-baked
// run.envVariables are Type USER with editable:false; intrinsics are Type
// SYSTEM. The legacy READ_ONLY|EDITABLE|SECRET|INTERNAL|ENV enum is retired
// on the wire. The ZEROPS_YAML blob is dropped by literal key regardless of
// Type. Unknown/empty Type → intrinsic (fail-safe: not admitted as a
// yaml-baked ref target). No Sensitive derivation here (unlike the old
// Type==SECRET model) — the SDK's AppVersionUserData DTO carries no
// Sensitive field at all (unlike the slim /env's ServiceStackEnv), so
// GetAppVersionUserData always emits Sensitive:false for every genuine
// run.envVariables record; yaml-baked values are templates the caller
// supplied in source, not platform-managed secrets.
// SDK note: AppVersionUserData.Type is Deprecated; if a future SDK empties
// it, this degrades to "no yaml-baked layer" (fail-safe), not "everything
// is a ref".
func classifyAppVersionUserData(key, typeStr string) userDataKind {
	if key == zeropsYamlUserDataKey {
		return kindZeropsYaml
	}
	switch ServiceEnvType(typeStr) {
	case ServiceEnvUser:
		return kindRunEnvVariable
	case ServiceEnvSystem:
		return kindIntrinsic
	default: // empty / unknown (future SDK) Type → intrinsic (fail-safe)
		return kindIntrinsic
	}
}

// GetAppVersionUserData returns the app version's yaml-baked run.envVariables
// (templates like ${db_hostname}), Sensitive always false (the DTO carries
// no Sensitive field — see classifyAppVersionUserData). Intrinsic vars and
// the ZEROPS_YAML blob are filtered out at this boundary — the SDK
// UserDataList is a superset, but only Type USER records are genuine
// run.envVariables (classifyAppVersionUserData). This is the GUI
// "Environment variables from master" source and, since 2026-08, these
// yaml-baked vars are ALSO mirrored read-only on the slim GetServiceEnv
// (spec §1) — this endpoint remains the canonical source because the slim
// mirror can't be told apart from a user-set var by Type alone (both USER).
// Callers must invoke this only for runtime services with an active app
// version — managed deps and never-deployed services have none.
func (z *ZeropsClient) GetAppVersionUserData(ctx context.Context, appVersionID string) ([]ServiceEnvVar, error) {
	pathParam := path.AppVersionId{Id: uuid.AppVersionId(appVersionID)}
	resp, err := z.handler.GetAppVersion(ctx, pathParam)
	if err != nil {
		return nil, mapSDKError(err, "appVersion")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "appVersion")
	}
	vars := make([]ServiceEnvVar, 0, len(out.UserDataList))
	for _, ud := range out.UserDataList {
		if classifyAppVersionUserData(ud.Key.String(), ud.Type.String()) != kindRunEnvVariable {
			continue
		}
		vars = append(vars, ServiceEnvVar{
			Key:     ud.Key.String(),
			Content: string(ud.Content),
			Type:    ServiceEnvType(ud.Type.String()),
			// Sensitive: always false — the app-version DTO carries no
			// Sensitive field to derive it from (see classifier doc).
		})
	}
	return vars, nil
}

// GetAppVersionZeropsYaml returns the app version's ZEROPS_YAML user-data
// blob — the full deployed zerops.yaml TEXT, verbatim. Unlike
// GetAppVersionUserData (which filters ZEROPS_YAML out by key — it only
// ever returns genuine run.envVariables), this is the one accessor that
// surfaces the blob itself. Used by the R2 artifact-redeploy recovery
// (docs/spec-workflows.md §8 R2 / ops.RedeployLastAppVersion) as the FIRST
// yaml source — the platform does not reuse the stored yaml on its own, so
// ZCP must resend it verbatim on PUT /app-version/{id}/deploy.
//
// Returns ("", nil) when no ZEROPS_YAML record exists (e.g. a
// startWithoutCode appVersion) — callers fall back to the app-code archive.
func (z *ZeropsClient) GetAppVersionZeropsYaml(ctx context.Context, appVersionID string) (string, error) {
	pathParam := path.AppVersionId{Id: uuid.AppVersionId(appVersionID)}
	resp, err := z.handler.GetAppVersion(ctx, pathParam)
	if err != nil {
		return "", mapSDKError(err, "appVersion")
	}
	out, err := resp.Output()
	if err != nil {
		return "", mapSDKError(err, "appVersion")
	}
	for _, ud := range out.UserDataList {
		if ud.Key.String() == zeropsYamlUserDataKey {
			return string(ud.Content), nil
		}
	}
	return "", nil
}

// ListServiceAppVersions reads a service's app-version history via the
// DIRECT (non-Elasticsearch) GET /service-stack/{id}/app-version — lag-
// free, unlike the ES-backed SearchAppVersions. Used by
// ops.ComputeRecoveryState to read facts about the newest appVersion
// (ArtifactBuilt/GitProvisioned/HasContainer) right after a mutation,
// where ES lag could otherwise misclassify a just-settled service.
//
// Returns newest-first by Sequence descending regardless of what order the
// platform emits `list` in — callers key off index 0 as "the newest".
func (z *ZeropsClient) ListServiceAppVersions(ctx context.Context, serviceID string) ([]AppVersionEvent, error) {
	pathParam := path.ServiceStackId{Id: uuid.ServiceStackId(serviceID)}
	resp, err := z.handler.GetServiceStackAppVersion(ctx, pathParam, query.ListServiceStackAppVersions{})
	if err != nil {
		return nil, mapSDKError(err, "appVersion")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "appVersion")
	}
	events := make([]AppVersionEvent, 0, len(out.List))
	for _, av := range out.List {
		events = append(events, mapDirectAppVersion(av))
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Sequence > events[j].Sequence })
	return events, nil
}

// mapDirectAppVersion projects the SDK's GetAppVersion (as returned by the
// DIRECT service-stack app-version list) onto AppVersionEvent — the same
// wire shape ES-backed SearchAppVersions returns (mapEsAppVersionEvent in
// zerops_event_mappers.go), so callers can treat both sources
// interchangeably. Kept self-contained here rather than sharing that
// mapper: the two source DTOs (output.EsAppVersion / output.GetAppVersion)
// are structurally similar but distinct generated types.
func mapDirectAppVersion(av output.GetAppVersion) AppVersionEvent {
	event := AppVersionEvent{
		ID:             av.Id.TypedString().String(),
		ProjectID:      av.ProjectId.TypedString().String(),
		ServiceStackID: av.ServiceStackId.TypedString().String(),
		Source:         av.Source.String(),
		Status:         av.Status.String(),
		Sequence:       av.Sequence.Native(),
		Created:        av.Created.Format(time.RFC3339Nano),
		LastUpdate:     av.LastUpdate.Format(time.RFC3339Nano),
	}
	if av.PublicGitSource != nil {
		event.PublicGitSource = &AppVersionGitSource{
			GitURL:     av.PublicGitSource.GitUrl.String(),
			BranchName: av.PublicGitSource.BranchName.String(),
		}
	}
	return event
}

// RedeployAppVersion re-deploys an existing appVersion via PUT
// /app-version/{id}/deploy: no rebuild, no re-import, the already-built
// artifact goes ACTIVE. Two callers, two body shapes (docs/spec-workflows.md
// §8 R2 / §12.6 GF-8):
//   - R2 recovery of a never-activated DEPLOY_FAILED version: BOTH
//     zeropsYaml and zeropsYamlSetup MUST be sent — the platform does NOT
//     reuse the stored yaml (live-verified: omitting either 400s with
//     zeropsYamlSetupNotFound).
//   - GF-8 rollback of a BACKUP version: the platform re-activates with the
//     config recorded at the version's original activation and ignores a
//     null body — but REJECTS empty strings (`zeropsYamlSetup … value
//     should not be empty`, 400 invalidUserInput; live 2026-09-14, eval G5).
//     An empty argument is therefore sent as JSON null, never as "".
func (z *ZeropsClient) RedeployAppVersion(ctx context.Context, appVersionID, zeropsYaml, setup string) (*Process, error) {
	pathParam := path.AppVersionId{Id: uuid.AppVersionId(appVersionID)}
	var bodyParam body.PutAppVersionDeploy // zero values marshal as null
	if zeropsYaml != "" {
		bodyParam.ZeropsYaml = types.NewMediumTextNull(zeropsYaml)
	}
	if setup != "" {
		bodyParam.ZeropsYamlSetup = types.NewStringNull(setup)
	}
	resp, err := z.handler.PutAppVersionDeploy(ctx, pathParam, bodyParam)
	if err != nil {
		return nil, mapSDKError(err, "appVersion")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "appVersion")
	}
	proc := mapProcess(out)
	return &proc, nil
}

// App version statuses a commit deploy reads besides the build ones
// (BuildStatus*): a created version is UPLOADING until build-and-deploy is
// accepted, and ACTIVE once it runs.
const (
	AppVersionStatusUploading = "UPLOADING"
	AppVersionStatusActive    = "ACTIVE"
)

// UploadStall bounds an upload by progress, not by its length: it fails
// once no archive byte moved, or no answer came after the last, for this
// long. An upload runs as long as its archive needs, so the API client's
// whole-request timeout (DefaultAPITimeout) does not apply to it.
const UploadStall = 60 * time.Second

// errUploadStalled is the upload's context cause when UploadStall ran out.
var errUploadStalled = errors.New("upload stalled")

// CreateAppVersion is POST /service-stack/{id}/app-version {name}: the new
// version's id.
func (z *ZeropsClient) CreateAppVersion(ctx context.Context, serviceID, name string) (string, error) {
	pathParam := path.ServiceStackId{Id: uuid.ServiceStackId(serviceID)}
	resp, err := z.handler.PostServiceStackAppVersion(ctx, pathParam, body.PostAppVersion{Name: types.NewStringNull(name)})
	if err != nil {
		return "", mapSDKError(err, "appVersion")
	}
	out, err := resp.Output()
	if err != nil {
		return "", mapSDKError(err, "appVersion")
	}
	return out.Id.TypedString().String(), nil
}

// UploadAppVersion is PUT /app-version/{id}/upload, the archive streamed
// chunked as application/octet-stream (Client). The SDK's own upload call
// sends application/json and runs under the whole-request timeout, so it is
// hand-rolled on the SDK's authorized request with a client of its own: the
// same base transport and key-refusal watch as the API client
// (newZeropsClientOn), no whole-request timeout, UploadStall instead.
func (z *ZeropsClient) UploadAppVersion(ctx context.Context, appVersionID string, archive io.Reader) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	body := &stallReader{archive: archive, timer: time.AfterFunc(UploadStall, func() { cancel(errUploadStalled) })}
	defer body.timer.Stop()
	req, err := z.env.Request(ctx, http.MethodPut, "/api/rest/public/app-version/"+url.PathEscape(appVersionID)+"/upload", body)
	if err != nil {
		return mapSDKError(err, "appVersion")
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = -1 // chunked (ZEROPS-004)
	client := &http.Client{Transport: keyRefusalTransport{base: http.DefaultTransport, hook: &z.keyRefused}}
	resp, err := client.Do(req)
	if readErr := body.failure(); readErr != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return readErr
	}
	if err != nil {
		if errors.Is(context.Cause(ctx), errUploadStalled) {
			// The stall, not the cancellation it caused, is the cause:
			// ClassOf reads a cancellation as the caller's own stop.
			return withCause(NewPlatformError(ErrAPITimeout,
				fmt.Sprintf("upload: nothing moved for %s", UploadStall), "Retry the deploy"), errUploadStalled)
		}
		return mapSDKError(err, "appVersion")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return mapSDKError(err, "appVersion")
	}
	if resp.StatusCode >= http.StatusMultipleChoices {
		return answerError(resp.StatusCode, raw, "appVersion")
	}
	return nil
}

// stallReader is an upload's body: every read that moves bytes, and the
// archive's end, restart the stall timer; a read error of the archive is
// kept, so the upload reports it rather than the transport's wrapping.
type stallReader struct {
	archive io.Reader
	timer   *time.Timer
	mu      sync.Mutex
	err     error
}

func (r *stallReader) Read(p []byte) (int, error) {
	n, err := r.archive.Read(p)
	if n > 0 || err == io.EOF {
		r.timer.Reset(UploadStall)
	}
	if err != nil && err != io.EOF {
		r.mu.Lock()
		r.err = err
		r.mu.Unlock()
	}
	return n, err
}

func (r *stallReader) failure() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// answerError is a non-2xx answer of a hand-rolled call as the platform's
// error for entity, as directGet maps one.
func answerError(status int, raw []byte, entity string) error {
	var apiErrResp directProcessErrorResponse
	if err := json.Unmarshal(raw, &apiErrResp); err != nil {
		pe := withCause(NewPlatformError(ErrAPIError,
			fmt.Sprintf("%s: malformed %d response", entity, status),
			"Retry; if it persists the platform API changed — report it"), err)
		pe.serverSide = status >= http.StatusInternalServerError
		return pe
	}
	apiErrResp.Error.HttpStatusCode = status
	return mapSDKError(apiErrResp.Error, entity)
}

// BuildAndDeployAppVersion is PUT /app-version/{id}/build-and-deploy
// {zeropsYaml, zeropsYamlSetup}: the build-and-deploy process.
func (z *ZeropsClient) BuildAndDeployAppVersion(ctx context.Context, appVersionID, zeropsYaml, setup string) (*Process, error) {
	pathParam := path.AppVersionId{Id: uuid.AppVersionId(appVersionID)}
	bodyParam := body.PutAppVersionBuildAndDeploy{
		ZeropsYaml:      types.MediumText(zeropsYaml),
		ZeropsYamlSetup: types.NewStringNull(setup),
	}
	resp, err := z.handler.PutAppVersionBuildAndDeploy(ctx, pathParam, bodyParam)
	if err != nil {
		return nil, mapSDKError(err, "appVersion")
	}
	out, err := resp.Output()
	if err != nil {
		return nil, mapSDKError(err, "appVersion")
	}
	proc := mapProcess(out)
	return &proc, nil
}

// DeleteAppVersion is DELETE /app-version/{id}.
func (z *ZeropsClient) DeleteAppVersion(ctx context.Context, appVersionID string) error {
	resp, err := z.handler.DeleteAppVersion(ctx, path.AppVersionId{Id: uuid.AppVersionId(appVersionID)})
	if err != nil {
		return mapSDKError(err, "appVersion")
	}
	if _, err := resp.Output(); err != nil {
		return mapSDKError(err, "appVersion")
	}
	return nil
}

// ListUploadingAppVersions reads the service's versions still UPLOADING,
// each with its name: POST /app-version/search (Elasticsearch), the read
// that carries a version's name — the direct per-service list has none
// (s0a F6). A version a cut upload left behind stays UPLOADING for 14 min
// and more; this is how a commit deploy finds its own by name. Being a
// search it may trail a write by seconds, so a caller never reads it for a
// version it just made.
func (z *ZeropsClient) ListUploadingAppVersions(ctx context.Context, projectID, serviceID string) ([]AppVersionEvent, error) {
	clientID, err := z.OrgID(ctx)
	if err != nil {
		return nil, err
	}
	filter := body.EsFilter{
		Search: body.EsFilterSearch{
			body.EsSearchItem{Name: types.NewString("clientId"), Operator: types.NewString("eq"), Value: types.NewString(clientID)},
			body.EsSearchItem{Name: types.NewString("projectId"), Operator: types.NewString("eq"), Value: types.NewString(projectID)},
			body.EsSearchItem{Name: types.NewString("serviceStackId"), Operator: types.NewString("eq"), Value: types.NewString(serviceID)},
			body.EsSearchItem{Name: types.NewString("status"), Operator: types.NewString("eq"), Value: types.NewString(AppVersionStatusUploading)},
		},
		Limit: types.NewIntNull(directListLimit),
	}
	// Hand-rolled on the SDK's transport: the SDK's EsAppVersion has no
	// `name`, so the raw answer is decoded twice.
	resp := sdkBase.Post(ctx, z.env, "/api/rest/public/app-version/search", filter)
	if resp.Err != nil {
		return nil, mapSDKError(resp.Err, "appVersion")
	}
	raw := resp.ResponseData.Bytes()
	if status := resp.HttpResponse.StatusCode; status >= http.StatusMultipleChoices {
		return nil, answerError(status, raw, "appVersion")
	}
	var out output.EsAppVersionResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, withCause(NewPlatformError(ErrAPIError, "app version search: malformed response",
			"Retry; if it persists the platform API changed — report it"), err)
	}
	var named struct {
		Items []struct {
			Name *string `json:"name"`
		} `json:"items"`
	}
	_ = json.Unmarshal(raw, &named) // the same body decoded above; a name it lacks stays ""
	events := make([]AppVersionEvent, 0, len(out.Items))
	for i, av := range out.Items {
		event := mapEsAppVersionEvent(av)
		if event.ServiceStackID != serviceID || event.Status != AppVersionStatusUploading {
			continue // the search's own filter, asserted
		}
		if i < len(named.Items) && named.Items[i].Name != nil {
			event.Name = *named.Items[i].Name
		}
		events = append(events, event)
	}
	return events, nil
}
