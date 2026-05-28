package platform

import (
	"context"

	"github.com/zeropsio/zerops-go/dto/input/path"
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

// GetAppVersionUserData returns the app version's userData records —
// yaml-baked run.envVariables (as templates, e.g. ${db_hostname}),
// intrinsic vars, and ZEROPS_YAML. Live-verified 2026-05-28: this is
// the ONLY API surface exposing yaml-baked vars (the slim
// GetServiceEnv returns only ~9 intrinsic + user userData). The GUI's
// "Environment variables from master" reads the same source. Callers
// must only invoke this for runtime services with an active app
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
		vars = append(vars, ServiceEnvVar{
			Key:     ud.Key.String(),
			Content: string(ud.Content),
			Type:    ServiceEnvType(ud.Type.String()),
		})
	}
	return vars, nil
}
