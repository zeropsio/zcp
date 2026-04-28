package tools

import (
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// handleGitPushSetup walks the agent through configuring git-push capability
// for a service: GIT_TOKEN on the container (or origin URL on local) plus
// the remote repository URL. Introduced by deploy-strategy decomposition
// Phase 5.
//
// Two modes:
//
//   - Walkthrough (input.RemoteURL empty): synthesize the env-aware setup
//     atom from the corpus — agent reads, executes the steps, then re-calls
//     with input.RemoteURL set. No meta mutation.
//   - Confirm (input.RemoteURL set): writes meta.GitPushState=configured
//     plus meta.RemoteURL on a successful pre-flight, returns confirmation.
//     The pre-flight is light at this layer — full GIT_TOKEN / .netrc
//     verification still runs at zerops_deploy time (deploy-decomp P4).
//
// service param is required and resolves via FindServiceMeta (pair-keyed).
// Stage-hostname targets are rejected with the same source-of-push remediation
// as the deploy handlers.
func handleGitPushSetup(input WorkflowInput, stateDir string, rt runtime.Info) (*mcp.CallToolResult, any, error) {
	if input.Service == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"service is required for git-push-setup",
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
	if !meta.IsPushSourceFor(input.Service) {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("git-push-setup target %q is not a source-of-push (build target half of the pair); set up from %q instead", input.Service, meta.Hostname),
			fmt.Sprintf("Retry with: zerops_workflow action=\"git-push-setup\" service=%q", meta.Hostname),
		), WithRecoveryStatus()), nil, nil
	}

	// Walkthrough mode: synthesize the env-aware setup atom and return.
	// Atoms still use the legacy strategies/triggers axes pre-Phase-8;
	// the snapshot we hand to the synthesizer maps the new state onto
	// the legacy vocabulary so existing strategy-push-git-push-{container,
	// local} atoms render. Phase 8 swaps both halves to the new axes.
	if input.RemoteURL == "" {
		snap := workflow.ServiceSnapshot{
			Hostname:        input.Service,
			Mode:            meta.Mode,
			StageHostname:   meta.StageHostname,
			Bootstrapped:    true,
			Strategy:        topology.StrategyPushGit,
			Trigger:         topology.TriggerUnset,
			CloseDeployMode: topology.CloseModeGitPush,
			GitPushState:    topology.GitPushUnconfigured,
		}
		guidance, err := workflow.SynthesizeStrategySetup(rt, []workflow.ServiceSnapshot{snap})
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrNotImplemented,
				fmt.Sprintf("git-push-setup synthesis failed: %v", err),
				"Build-time defect — report it. Run `make lint-local` to verify the atom corpus."), WithRecoveryStatus()), nil, nil
		}
		return jsonResult(map[string]any{
			"status":   "walkthrough",
			"service":  input.Service,
			"guidance": guidance,
			"nextStep": fmt.Sprintf("After completing the setup steps, re-call: zerops_workflow action=\"git-push-setup\" service=%q remoteUrl=<configured-remote-url>", input.Service),
		}), nil, nil
	}

	// Confirm mode: write meta state. Full GIT_TOKEN / .netrc verification
	// happens later at deploy time (deploy_git_push.go pre-flight); this
	// layer takes the agent's word that the walkthrough completed and the
	// remote URL resolves.
	meta.GitPushState = topology.GitPushConfigured
	meta.RemoteURL = input.RemoteURL
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("Write service meta %q: %v", input.Service, err),
			""), WithRecoveryStatus()), nil, nil
	}

	return jsonResult(map[string]any{
		"status":       "configured",
		"service":      input.Service,
		"gitPushState": meta.GitPushState,
		"remoteUrl":    meta.RemoteURL,
		"nextStep":     fmt.Sprintf("git-push capability is now ready. Push via: zerops_deploy targetService=%q strategy=\"git-push\". Configure a build integration (webhook|actions) via: zerops_workflow action=\"build-integration\" service=%q integration=\"webhook|actions\".", input.Service, input.Service),
	}), nil, nil
}
