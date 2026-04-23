package platform

import (
	"context"
	"errors"
	"fmt"

	"github.com/zeropsio/zerops-go/dto/input/body"
	"github.com/zeropsio/zerops-go/types"
	"github.com/zeropsio/zerops-go/types/enum"
	"github.com/zeropsio/zerops-go/types/stringId"
)

// ZeropsYamlValidation operations. DEPLOY validates run.* only (fast path
// for push-dev which skips build). BUILD_AND_DEPLOY validates build.* and
// run.* (used for push-git and any path that triggers a server-side build).
// Empirically (see plans/api-validation-plumbing.md §1.3), BUILD_AND_DEPLOY
// tolerates missing build blocks and is the safe default for all deploy
// flows — one operation covers every case without per-strategy branching.
const (
	ValidationOperationDeploy         = "DEPLOY"
	ValidationOperationBuildAndDeploy = "BUILD_AND_DEPLOY"
)

// ValidateZeropsYamlInput carries everything the server validator needs.
// Matches RequestZeropsYamlValidation in the public OpenAPI spec
// (see internal/platform/zerops_validate.go-wise DTOs under zerops-go SDK).
type ValidateZeropsYamlInput struct {
	// ServiceStackTypeID is the Zerops stack-type ID for the target service.
	// Live-looked-up via ListServiceStackTypes or via an already-discovered
	// ServiceStack. Required by the endpoint.
	ServiceStackTypeID string
	// ServiceStackTypeVersion e.g. "nodejs@22". Required by the endpoint.
	ServiceStackTypeVersion string
	// ServiceStackName matches the `setup:` key in zerops.yaml. Empirically
	// this is the identifier the server uses to locate the relevant setup
	// block in the submitted YAML; mismatches yield zeropsYamlSetupNotFound.
	ServiceStackName string
	// ZeropsYaml is the full zerops.yaml contents (not just the matched
	// setup block — the validator walks the full document).
	ZeropsYaml string
	// Operation controls which sections are validated. Use
	// ValidationOperationBuildAndDeploy for the superset check; callers
	// should have a strong reason to ever pass DEPLOY (none today).
	Operation string
	// ZeropsYamlSetup is optional — rarely needed. Left empty in current
	// deploy flows; server infers the setup block from ServiceStackName.
	ZeropsYamlSetup string
}

// ValidateZeropsYaml calls POST /service-stack/zerops-yaml-validation.
// Shape documented at plans/api-validation-plumbing.md §1.3.
//
// Returns:
//   - nil on 200 (server says valid).
//   - *PlatformError{Code: ErrInvalidZeropsYml, APIMeta: ...} on 400 with
//     any of the validation-specific error codes (zeropsYamlInvalidParameter,
//     yamlValidationInvalidYaml, errorList, zeropsYamlSetupNotFound).
//     The APIMeta array carries field-level detail (e.g. build.base: [...]).
//   - The mapped SDK error for transport / auth / server-side failures
//     (NETWORK_ERROR, AUTH_TOKEN_EXPIRED, API_ERROR 5xx, etc.) — same
//     mapping every other platform method uses.
func (z *ZeropsClient) ValidateZeropsYaml(ctx context.Context, in ValidateZeropsYamlInput) error {
	if in.ServiceStackTypeID == "" {
		return NewPlatformError(ErrInvalidParameter, "ServiceStackTypeID is required", "Resolve the service stack type ID before calling ValidateZeropsYaml")
	}
	if in.ServiceStackTypeVersion == "" {
		return NewPlatformError(ErrInvalidParameter, "ServiceStackTypeVersion is required (e.g. 'nodejs@22')", "Resolve the service stack type version before calling ValidateZeropsYaml")
	}
	if in.ServiceStackName == "" {
		return NewPlatformError(ErrInvalidParameter, "ServiceStackName is required (must match a setup: key in zerops.yaml)", "Pass the setup name that matches the target service in zerops.yaml")
	}
	if in.ZeropsYaml == "" {
		return NewPlatformError(ErrInvalidParameter, "ZeropsYaml is required", "Pass the full zerops.yaml content")
	}
	operation := in.Operation
	if operation == "" {
		operation = ValidationOperationBuildAndDeploy
	}

	dto := body.ZeropsYamlValidation{
		ServiceStackName:            types.NewString(in.ServiceStackName),
		ServiceStackTypeId:          stringId.ServiceStackTypeId(in.ServiceStackTypeID),
		ServiceStackTypeVersionName: types.NewString(in.ServiceStackTypeVersion),
		ZeropsYaml:                  types.NewMediumText(in.ZeropsYaml),
		Operation:                   enum.ZeropsYamlValidationOperationEnum(operation),
	}
	if in.ZeropsYamlSetup != "" {
		dto.ZeropsYamlSetup = types.NewStringNull(in.ZeropsYamlSetup)
	}

	resp, err := z.handler.PostServiceStackZeropsYamlValidation(ctx, dto)
	if err != nil {
		return mapSDKError(err, "zeropsYaml")
	}
	_, outErr := resp.Output()
	if outErr == nil {
		return nil
	}
	return reclassifyValidationError(mapSDKError(outErr, "zeropsYaml"))
}

// reclassifyValidationError promotes known zerops-yaml-validation 4xx codes
// from the generic ErrAPIError bucket into the domain-specific
// ErrInvalidZeropsYml so deploy handlers can distinguish "your YAML is
// wrong" from "Zerops is down" by Code alone — no string-sniffing of
// APICode. Non-matching errors (network, auth, server-side 5xx) pass
// through untouched so the original classification wins.
func reclassifyValidationError(err error) error {
	var pe *PlatformError
	if !errors.As(err, &pe) || !isValidationErrorCode(pe.APICode) {
		return err
	}
	pe.Code = ErrInvalidZeropsYml
	if len(pe.APIMeta) > 0 {
		pe.Suggestion = fmt.Sprintf(
			"Platform validator rejected zerops.yaml (code: %s) — read apiMeta for each field's failure reason, fix, and retry.",
			pe.APICode,
		)
	} else {
		pe.Suggestion = fmt.Sprintf(
			"Platform validator rejected zerops.yaml (code: %s). %s",
			pe.APICode, pe.Message,
		)
	}
	return pe
}

// isValidationErrorCode names the 4xx error codes the zerops-yaml-validation
// endpoint emits for user-fixable YAML problems. Transport/auth failures
// land here with an empty APICode (mapSDKError never populates it for
// non-apiError transport errors), so they naturally fall through.
func isValidationErrorCode(apiCode string) bool {
	switch apiCode {
	case "zeropsYamlInvalidParameter",
		"yamlValidationInvalidYaml",
		"errorList",
		"zeropsYamlSetupNotFound":
		return true
	}
	return false
}
