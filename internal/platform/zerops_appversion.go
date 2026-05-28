package platform

import (
	"context"

	"github.com/zeropsio/zerops-go/dto/input/path"
	"github.com/zeropsio/zerops-go/types/enum"
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
// platform's UserDataList is a SUPERSET that also carries ~119 intrinsic vars
// (READ_ONLY/INTERNAL/EDITABLE) and the ZEROPS_YAML blob.
type userDataKind int

const (
	kindRunEnvVariable userDataKind = iota // zerops.yaml run.envVariables (Type ENV|SECRET)
	kindIntrinsic                          // platform-injected (READ_ONLY|INTERNAL|EDITABLE)
	kindZeropsYaml                         // the ZEROPS_YAML deployed-yaml blob
)

// zeropsYamlUserDataKey is the well-known key of the full deployed-yaml blob
// carried in UserDataList — never a run.envVariables var.
const zeropsYamlUserDataKey = "ZEROPS_YAML"

// classifyAppVersionUserData decides whether a UserDataList record is a genuine
// yaml-baked run.envVariables var, and whether it is sensitive. It is the single
// classifier used by BOTH the real client (GetAppVersionUserData) and the mock,
// so a test cannot model a shape the real API can't produce.
//
// Type-allowlist (keep ENV|SECRET): live-verified 2026-05-28 against a real
// app-version userDataList — intrinsics are READ_ONLY/INTERNAL/EDITABLE (never
// ENV); run.envVariables are ENV, baked envSecrets/dotEnvSecrets are SECRET. An
// allowlist is conservative-by-construction: a new (unobserved) intrinsic would
// be READ_ONLY/INTERNAL and excluded, whereas a denylist would leak it. The
// ZEROPS_YAML blob is dropped by literal key (it may itself arrive Type==ENV).
// Sensitive := Type==SECRET; unknown/empty Type → intrinsic (fail-safe: not
// admitted as a yaml-baked ref target, and under-redacts rather than leaks).
// SDK note: AppVersionUserData.Type is Deprecated; if a future SDK empties it,
// this degrades to "no yaml-baked layer" (fail-safe), not "everything is a ref".
func classifyAppVersionUserData(key, typeStr string) (kind userDataKind, sensitive bool) {
	if key == zeropsYamlUserDataKey {
		return kindZeropsYaml, false
	}
	switch enum.UserDataTypeEnum(typeStr) {
	case enum.UserDataTypeEnumEnv:
		return kindRunEnvVariable, false
	case enum.UserDataTypeEnumSecret:
		return kindRunEnvVariable, true
	case enum.UserDataTypeEnumReadOnly, enum.UserDataTypeEnumEditable, enum.UserDataTypeEnumInternal:
		return kindIntrinsic, false
	default: // empty / unknown (future SDK) Type → intrinsic (fail-safe)
		return kindIntrinsic, false
	}
}

// GetAppVersionUserData returns the app version's yaml-baked run.envVariables
// (templates like ${db_hostname}) with Sensitive derived from Type==SECRET.
// Intrinsic vars and the ZEROPS_YAML blob are filtered out at this boundary —
// the SDK UserDataList is a superset, but only Type ENV|SECRET records are
// genuine run.envVariables (classifyAppVersionUserData). Live-verified 2026-05-28
// as the ONLY API surface exposing yaml-baked vars (the GUI "Environment
// variables from master"). Callers must invoke this only for runtime services
// with an active app version — managed deps and never-deployed services have none.
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
		kind, sensitive := classifyAppVersionUserData(ud.Key.String(), ud.Type.String())
		if kind != kindRunEnvVariable {
			continue
		}
		vars = append(vars, ServiceEnvVar{
			Key:       ud.Key.String(),
			Content:   string(ud.Content),
			Type:      ServiceEnvType(ud.Type.String()),
			Sensitive: sensitive,
		})
	}
	return vars, nil
}
