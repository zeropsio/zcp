package tools

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/workflow"
)

// stackSteps are the steps where the stack catalog is useful.
var stackSteps = map[string]bool{
	workflow.StepDiscover: true,
}

// needsStacks returns true if stacks should be populated for the response.
func needsStacks(resp *workflow.BootstrapResponse) bool {
	if resp == nil || resp.Current == nil {
		return true // inactive bootstrap or completed — include for safety
	}
	return stackSteps[resp.Current.Name]
}

func handleBootstrapComplete(ctx context.Context, engine *workflow.Engine, client platform.Client, schemaCache *schema.Cache, input WorkflowInput, logFetcher platform.LogFetcher, projectID string, stateDir string, mounter ops.Mounter, sshDeployer ops.SSHDeployer, rt runtime.Info) (*mcp.CallToolResult, any, error) {
	// Schema-derived catalog for plan validation + stack listing (the single
	// client-side source; nil-safe when the cache is absent).
	var schemas *schema.Schemas
	if schemaCache != nil {
		schemas = schemaCache.Get(ctx)
	}
	if input.Step == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Step is required for complete action",
			"Specify step name (e.g., step=\"discover\")"), WithRecoveryStatus()), nil, nil
	}

	// Structured plan routing for the "discover" step. For route=adopt the plan is
	// derivable from live discovery, so an empty/omitted plan auto-derives (the agent
	// authors nothing) and an explicit plan is honored but live-validated. Other
	// routes keep the prior behavior: an explicit plan is validated here; an empty
	// plan falls through to the attestation path (managed-only / recipe).
	if input.Step == "discover" {
		if bootstrapSessionRoute(engine) == workflow.BootstrapRouteAdopt {
			existing, listErr := ops.ListProjectServices(ctx, client, projectID)
			if listErr != nil {
				return convertError(platform.NewPlatformError(
					platform.ErrAPIError,
					fmt.Sprintf("Failed to list services for adoption: %v", listErr),
					"Retry; if it persists check VPN / API connectivity")), nil, nil
			}
			var resp *workflow.BootstrapResponse
			var err error
			if len(input.Plan) == 0 {
				resp, err = engine.BootstrapCompleteAdoptPlan(existing, rt, schemas)
			} else {
				resp, err = engine.BootstrapCompletePlan(input.Plan, schemas, existing)
			}
			if err != nil {
				return convertError(platform.NewPlatformError(
					platform.ErrInvalidParameter,
					fmt.Sprintf("Adopt plan failed: %v", err),
					"Omit plan to auto-derive from adoptable services, or submit an explicit plan."), WithRecoveryStatus()), nil, nil
			}
			if needsStacks(resp) {
				populateStacks(ctx, resp, schemaCache)
			}
			return jsonResult(resp), nil, nil
		}
		if input.Plan != nil {
			resp, err := engine.BootstrapCompletePlan(input.Plan, schemas, nil)
			if err != nil {
				return convertError(platform.NewPlatformError(
					platform.ErrInvalidParameter,
					fmt.Sprintf("Plan validation failed: %v", err),
					"Provide valid plan: [{runtime: {devHostname, type}, dependencies: [{hostname, type, resolution}]}]. Hostnames: lowercase a-z0-9, max 25 chars."), WithRecoveryStatus()), nil, nil
			}
			if needsStacks(resp) {
				populateStacks(ctx, resp, schemaCache)
			}
			return jsonResult(resp), nil, nil
		}
	}

	// Default: free-text attestation.
	if input.Attestation == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Attestation is required for complete action",
			"Describe what was accomplished in this step"), WithRecoveryStatus()), nil, nil
	}

	httpClient := &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}},
	}
	checker := buildStepChecker(input.Step, client, logFetcher, projectID, httpClient, engine, stateDir)

	resp, err := engine.BootstrapComplete(ctx, input.Step, input.Attestation, checker)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrBootstrapNotActive,
			fmt.Sprintf("Complete step failed: %v", err),
			"Start bootstrap first with action=start workflow=bootstrap"), WithRecoveryStatus()), nil, nil
	}

	// Auto-mount runtime services after successful provision completion.
	// mounter is nil in local env — no-op naturally.
	if input.Step == workflow.StepProvision && (resp.CheckResult == nil || resp.CheckResult.Passed) {
		resp.AutoMounts = autoMountTargets(ctx, client, projectID, mounter, sshDeployer, engine)
		cleanupImportYAML(stateDir, resp.AutoMounts, engine.Environment() == workflow.EnvContainer)
	}

	appendTransitionMessage(resp, engine)
	if needsStacks(resp) {
		populateStacks(ctx, resp, schemaCache)
	}
	return jsonResult(resp), nil, nil
}

// bootstrapSessionRoute reads the active bootstrap session's route for dispatch
// selection. The authoritative route check lives in the engine methods; this is a
// best-effort pre-read (empty on no/expired session) so the handler picks the right
// auto-derive vs explicit-plan path.
func bootstrapSessionRoute(engine *workflow.Engine) workflow.BootstrapRoute {
	state, err := engine.GetState()
	if err != nil || state == nil || state.Bootstrap == nil {
		return ""
	}
	return state.Bootstrap.Route
}

// appendTransitionMessage rewrites resp.Message to the rich transition guidance
// (service list + deploy-model primer + "start the develop workflow" hint) once
// every bootstrap step is done. Called from both complete and skip paths —
// the skip path previously returned only "Bootstrap complete. All steps
// finished." with no next-action, leaving the agent to decide whether to call
// status (often: it didn't), so any code change went outside a develop session.
func appendTransitionMessage(resp *workflow.BootstrapResponse, engine *workflow.Engine) {
	if resp == nil || resp.Current != nil {
		return
	}
	state, stateErr := engine.GetState()
	if stateErr != nil {
		return
	}
	resp.Message = workflow.BuildTransitionMessage(state)
}

func handleBootstrapSkip(ctx context.Context, engine *workflow.Engine, schemaCache *schema.Cache, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.Step == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"Step is required for skip action",
			"Specify step name (e.g., step=\"generate\")"), WithRecoveryStatus()), nil, nil
	}

	reason := input.Reason
	if reason == "" {
		reason = defaultSkipReason
	}

	resp, err := engine.BootstrapSkip(input.Step, reason)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrBootstrapNotActive,
			fmt.Sprintf("Skip step failed: %v", err),
			"Only skippable steps (generate, deploy, close) can be skipped"), WithRecoveryStatus()), nil, nil
	}
	appendTransitionMessage(resp, engine)
	if needsStacks(resp) {
		populateStacks(ctx, resp, schemaCache)
	}
	return jsonResult(resp), nil, nil
}

func handleBootstrapStatus(ctx context.Context, engine *workflow.Engine, schemaCache *schema.Cache) (*mcp.CallToolResult, any, error) {
	return bootstrapStatusResult(ctx, engine, schemaCache)
}

// bootstrapStatusResult returns the current bootstrap status as a BootstrapResponse.
// Shared by handleBootstrapStatus, handleResume, and handleIterate.
func bootstrapStatusResult(ctx context.Context, engine *workflow.Engine, schemaCache *schema.Cache) (*mcp.CallToolResult, any, error) {
	resp, err := engine.BootstrapStatus()
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrBootstrapNotActive,
			fmt.Sprintf("Bootstrap status failed: %v", err),
			""), WithRecoveryStatus()), nil, nil
	}
	if needsStacks(resp) {
		populateStacks(ctx, resp, schemaCache)
	}
	return jsonResult(resp), nil, nil
}

// autoMountTargets mounts runtime services from the bootstrap plan after provision
// and initializes /var/www/.git/ container-side on each successfully-mounted service.
// Best-effort: mount failures are reported but don't block step advancement; git init
// failures are logged to stderr but don't mark the mount failed (deploy-time safety
// net in buildSSHCommand re-inits on demand — GLC-1/GLC-2).
//
// Returns nil when mounter is nil (local env) or no plan targets exist. sshDeployer
// is also nil in local env, so the post-mount git init skips even if the function
// were entered — the mounter guard short-circuits first.
func autoMountTargets(ctx context.Context, client platform.Client, projectID string, mounter ops.Mounter, sshDeployer ops.SSHDeployer, engine *workflow.Engine) []workflow.AutoMountInfo {
	if mounter == nil {
		return nil
	}
	state, err := engine.GetState()
	if err != nil || state.Bootstrap == nil || state.Bootstrap.Plan == nil {
		return nil
	}

	var results []workflow.AutoMountInfo
	for _, target := range state.Bootstrap.Plan.Targets {
		hostname := target.Runtime.DevHostname
		if hostname == "" {
			continue
		}
		result, mountErr := ops.MountService(ctx, client, projectID, mounter, hostname)
		if mountErr != nil {
			results = append(results, workflow.AutoMountInfo{
				Hostname: hostname,
				Status:   "FAILED",
				Error:    mountErr.Error(),
			})
			continue
		}
		results = append(results, workflow.AutoMountInfo{
			Hostname:  hostname,
			MountPath: result.MountPath,
			Status:    result.Status,
		})
		// Post-mount git lifecycle: init /var/www/.git/ container-side with
		// deploy identity. GLC-1 is enforced here (every managed runtime
		// service gets .git/ initialized at bootstrap), so subsequent
		// deploys don't race to init or re-config.
		//
		// Best-effort: errors are logged to stderr rather than surfaced in
		// AutoMountInfo. The mount is semantically separate from the git
		// init; recording a git-init hiccup as a mount failure would mis-
		// attribute it. The deploy safety-net (buildSSHCommand) re-inits
		// on demand, so a transient SSH failure here doesn't block any
		// downstream deploy.
		if sshDeployer != nil {
			if initErr := ops.InitServiceGit(ctx, sshDeployer, hostname); initErr != nil {
				fmt.Fprintf(os.Stderr, "zcp: InitServiceGit %s: %v\n", hostname, initErr)
			}
		}
	}
	return results
}

// populateStacks injects the schema-derived stack catalog into a bootstrap response.
func populateStacks(ctx context.Context, resp *workflow.BootstrapResponse, schemaCache *schema.Cache) {
	if resp == nil || schemaCache == nil {
		return
	}
	if list := knowledge.FormatStackList(schemaCache.Get(ctx)); list != "" {
		resp.AvailableStacks = list
	}
}
