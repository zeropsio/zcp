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
