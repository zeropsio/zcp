package ops

import (
	"context"

	"github.com/zeropsio/zcp/internal/platform"
)

// RunPreDeployValidation invokes the Zerops `zerops-yaml-validation` endpoint
// for the target service's zerops.yaml, reading the YAML from workingDir.
// It's the filesystem-based entry point used by push-dev flows (both
// container SSHFS mount and local working dir).
//
// Contract:
//   - Nil error   → validation passed; caller proceeds with deploy.
//   - Non-nil err → deploy MUST abort. Structured validation failures
//     arrive as *PlatformError{Code: ErrInvalidZeropsYml, APIMeta: ...};
//     transport/auth failures as NETWORK_ERROR/AUTH_TOKEN_EXPIRED/etc.
//
// No fallback. If the validator is unreachable the deploy cannot confirm
// the YAML is acceptable, and downstream push/build steps would hit the
// same API anyway — failing here is strictly earlier, not different.
//
// When zerops.yaml cannot be read from disk the function returns nil —
// missing YAML is a separate upstream error surface (ops.ValidateZeropsYml
// reports it in Warnings), and blocking here would double-surface.
func RunPreDeployValidation(
	ctx context.Context,
	client platform.Client,
	target *platform.ServiceStack,
	setupName string,
	workingDir string,
) error {
	if target == nil || client == nil || workingDir == "" {
		return nil
	}
	yamlBytes, err := ReadZeropsYmlRaw(workingDir)
	if err != nil || len(yamlBytes) == 0 {
		return nil
	}
	return ValidatePreDeployContent(ctx, client, target, setupName, string(yamlBytes))
}

// ValidatePreDeployContent is the content-based entry point for the Zerops
// `zerops-yaml-validation` endpoint. Used by callers that cannot read
// zerops.yaml from a local path — e.g. the SSH-based git-push handler
// reads the file over SSH (cat) and passes the content here.
//
// Same contract as RunPreDeployValidation (nil = valid; any error = abort).
// An empty yamlContent returns nil (nothing to validate) to keep the
// "no yaml = no validation" rule consistent across entry points.
func ValidatePreDeployContent(
	ctx context.Context,
	client platform.Client,
	target *platform.ServiceStack,
	setupName string,
	yamlContent string,
) error {
	if target == nil || client == nil || yamlContent == "" {
		return nil
	}
	if setupName == "" {
		setupName = target.Name
	}
	return client.ValidateZeropsYaml(ctx, platform.ValidateZeropsYamlInput{
		ServiceStackTypeID:      target.ServiceStackTypeInfo.ServiceStackTypeID,
		ServiceStackTypeVersion: target.ServiceStackTypeInfo.ServiceStackTypeVersionName,
		ServiceStackName:        setupName,
		ZeropsYaml:              yamlContent,
		Operation:               platform.ValidationOperationBuildAndDeploy,
	})
}
