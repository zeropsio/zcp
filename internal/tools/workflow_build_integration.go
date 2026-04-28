package tools

import (
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// validBuildIntegrations is the closed set of BuildIntegration values the
// agent may pass via the build-integration action.
//
//nolint:gochecknoglobals // immutable lookup table
var validBuildIntegrations = map[topology.BuildIntegration]bool{
	topology.BuildIntegrationNone:    true,
	topology.BuildIntegrationWebhook: true,
	topology.BuildIntegrationActions: true,
}

// handleBuildIntegration configures the per-pair ZCP-managed CI integration
// that responds to git pushes hitting the remote. Introduced by
// deploy-strategy decomposition Phase 5.
//
// UTILITY framing: BuildIntegration is one specific CI integration ZCP
// helps wire (webhook OAuth or GitHub Actions); users may keep independent
// CI/CD that ZCP does not track. Setting BuildIntegration=none does NOT
// mean "no build will fire" — it means "no ZCP-managed integration is
// configured."
//
// Prerequisite chain (handler-side composition per plan §3.4 Scenario C):
// when GitPushState != GitPushConfigured the response composes git-push-setup
// guidance THEN build-integration setup atoms in a single response. The
// agent walks both prereqs without a status round-trip.
//
// Modes:
//
//   - Walkthrough (input.Integration empty): synthesize options atom; no
//     mutation.
//   - Confirm (input.Integration ∈ {webhook, actions, none}): pre-check
//     GitPushState; if unconfigured return chained guidance pointer; on
//     pass write meta.BuildIntegration.
func handleBuildIntegration(input WorkflowInput, stateDir string, rt runtime.Info) (*mcp.CallToolResult, any, error) {
	if input.Service == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"service is required for build-integration",
			"Pass service=<hostname> identifying the runtime to configure"), WithRecoveryStatus()), nil, nil
	}

	meta, err := workflow.FindServiceMeta(stateDir, input.Service)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("Read service meta %q: %v", input.Service, err),
			""), WithRecoveryStatus()), nil, nil
	}
	if meta == nil || !meta.IsComplete() {
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("Service %q is not bootstrapped", input.Service),
			"Run bootstrap first: zerops_workflow action=\"start\" workflow=\"bootstrap\""), WithRecoveryStatus()), nil, nil
	}

	// Walkthrough mode: synthesize options atom (PhaseStrategySetup).
	if input.Integration == "" {
		snap := workflow.ServiceSnapshot{
			Hostname:         input.Service,
			Mode:             meta.Mode,
			StageHostname:    meta.StageHostname,
			Bootstrapped:     true,
			CloseDeployMode:  topology.CloseModeGitPush,
			GitPushState:     meta.GitPushState,
			BuildIntegration: meta.BuildIntegration,
		}
		guidance, err := workflow.SynthesizeStrategySetup(rt, []workflow.ServiceSnapshot{snap})
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrNotImplemented,
				fmt.Sprintf("build-integration synthesis failed: %v", err),
				"Build-time defect — report it. Run `make lint-local` to verify the atom corpus."), WithRecoveryStatus()), nil, nil
		}
		return jsonResult(map[string]any{
			"status":           "walkthrough",
			"service":          input.Service,
			"gitPushState":     meta.GitPushState,
			"buildIntegration": meta.BuildIntegration,
			"guidance":         guidance,
			"nextStep":         fmt.Sprintf("Pick an integration and re-call: zerops_workflow action=\"build-integration\" service=%q integration=\"webhook|actions|none\".", input.Service),
		}), nil, nil
	}

	bi := topology.BuildIntegration(input.Integration)
	if !validBuildIntegrations[bi] {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Invalid integration %q", input.Integration),
			"Valid values: none, webhook, actions"), WithRecoveryStatus()), nil, nil
	}

	// Pre-check the prereq chain. Setting BuildIntegration to anything other
	// than 'none' requires git-push capability — the integration fires on
	// remote pushes, which need GitPushConfigured to land in the first place.
	// 'none' is a valid no-prereq target (clears any prior integration).
	if bi != topology.BuildIntegrationNone && meta.GitPushState != topology.GitPushConfigured {
		return jsonResult(map[string]any{
			"status":   "needsGitPushSetup",
			"service":  input.Service,
			"reason":   fmt.Sprintf("Build integration %q requires git-push capability (current state: %s).", bi, meta.GitPushState),
			"nextStep": fmt.Sprintf("Run zerops_workflow action=\"git-push-setup\" service=%q first; then re-run this build-integration call.", input.Service),
		}), nil, nil
	}

	if meta.BuildIntegration == bi {
		return jsonResult(map[string]any{
			"status":           "noop",
			"service":          input.Service,
			"buildIntegration": bi,
		}), nil, nil
	}
	meta.BuildIntegration = bi
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("Write service meta %q: %v", input.Service, err),
			""), WithRecoveryStatus()), nil, nil
	}

	return jsonResult(map[string]any{
		"status":           "configured",
		"service":          input.Service,
		"buildIntegration": bi,
		"nextStep":         "After the integration is wired on the remote (webhook OAuth in Zerops dashboard, or GitHub Actions workflow file), pushes to the remote will trigger the integration. ZCP doesn't track external CI/CD outside this single integration.",
	}), nil, nil
}
