package tools

import (
	"errors"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// classifyTransportError runs the deploy-failure classifier against an error
// returned by a deploy entrypoint BEFORE the build was triggered (SSH push,
// zcli push, git push, preflight). The classifier reads the error tail +
// any wrapped *platform.PlatformError to emit a structured next-step.
//
// strategy is the active deploy strategy (push-dev / push-git / manual) so
// the classifier scopes credential-class signals correctly.
//
// Returns nil when the error doesn't fit any known transport-phase signal
// AND no baseline applies (currently rare — the transport baseline is
// always emitted for transport-phase failures).
func classifyTransportError(err error, strategy string) *topology.DeployFailureClassification {
	if err == nil {
		return nil
	}
	in := ops.FailureInput{
		Strategy: strategy,
		Phase:    ops.PhaseTransport,
	}
	var pe *platform.PlatformError
	if errors.As(err, &pe) {
		in.APIErr = pe
		// Preflight-style codes (INVALID_ZEROPS_YML, PREREQUISITE_MISSING)
		// land here — switch to PhasePreflight so the preflight signals
		// fire instead of the transport ones.
		switch pe.Code {
		case platform.ErrInvalidZeropsYml,
			platform.ErrPrerequisiteMissing,
			platform.ErrInvalidImportYml,
			platform.ErrInvalidParameter,
			platform.ErrInvalidEnvFormat,
			platform.ErrPreflightFailed:
			in.Phase = ops.PhasePreflight
		}
	}
	in.TransportErr = err
	return ops.ClassifyDeployFailure(in)
}
