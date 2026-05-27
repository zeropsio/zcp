package platform

import (
	"context"

	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/types/uuid"
)

// GetAppVersionAppCode returns the download URL for an app version's
// source archive. The archive is the bytes uploaded at deploy time —
// for ZCP this is the only platform-side way to recover the deployed
// zerops.yaml content (the appVersion DTOs themselves do not carry
// yaml; only the archive does).
//
// Used by the ResolveCanonicalSetup cascade (P1) step 4: when prior
// platform-read steps (UserData, GH integration, ActiveAppVersion GH)
// miss but the service has at least one active app version, the
// archive's zerops.yaml is the canonical source for what shipped.
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
