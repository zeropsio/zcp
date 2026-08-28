package tools

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
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

// needsStacks returns true only at the discover step, where the agent is
// CHOOSING service types. A completed/inactive response (Current == nil) must
// NOT re-attach the catalog: the choice is already made, so re-dumping the
// ~1KB version list there is pure WRONG-TIME noise (it re-shipped on every
// status/transition/completed response — the catalog informing a decision
// already past). Presentation is now one-shot at the decision point.
func needsStacks(resp *workflow.BootstrapResponse) bool {
	if resp == nil || resp.Current == nil {
		return false
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

	// Structured plan routing for the "discover" step. route=adopt and route=recipe
	// both DERIVE the plan (the agent authors nothing): adopt from live discovery,
	// recipe from the matched recipe's import YAML. An empty/omitted plan derives;
	// a submitted plan is reconciled (adopt: override the derived shape; recipe:
	// rename a colliding hostname / flip a managed dep to EXISTS). route=classic
	// validates an explicit plan here; an empty classic plan falls through to the
	// attestation path (managed-only).
	if input.Step == "discover" {
		if bootstrapSessionRoute(engine) == workflow.BootstrapRouteAdopt {
			existing, listErr := ops.ListProjectServices(ctx, client, projectID)
			if listErr != nil {
				return convertError(platform.NewPlatformError(
					platform.ErrAPIError,
					fmt.Sprintf("Failed to list services for adoption: %v", listErr),
					"Retry; if it persists check VPN / API connectivity")), nil, nil
			}
			// Activity gate: refuse adopting a target with a LIVE build/deploy/
			// lifecycle process (the recipe-first-deploy race). Sits ahead of BOTH
			// dispatch branches and resolves targets from scope ∪ plan, so the
			// reported same-stack pair path (explicit plan=[...]) is covered. No
			// meta is written on refusal — we return before the dispatch.
			if gate := adoptActivityGate(ctx, client, projectID, input, existing); gate != nil {
				return convertError(gate, WithRecoveryStatus()), nil, nil
			}
			var resp *workflow.BootstrapResponse
			var err error
			if len(input.Plan) == 0 {
				resp, err = engine.BootstrapCompleteAdoptPlan(existing, input.Scope, rt, schemas)
			} else {
				resp, err = engine.BootstrapCompletePlan(input.Plan, schemas, existing)
			}
			if err != nil {
				pe := platform.NewPlatformError(
					platform.ErrInvalidParameter,
					fmt.Sprintf("Adopt plan failed: %v", err),
					"Omit plan and pass scope=[\"hostname\",...] to adopt exactly those services, or submit an explicit plan.")
				pe.Subcode = bootstrapPlanSubcode(err)
				return convertError(pe, WithRecoveryStatus()), nil, nil
			}
			// Reflect live git-push state into the just-adopted metas: a service
			// already git-push-configured outside ZCP keeps that state instead of
			// being reset to unconfigured (which would force a needless — and
			// token-destroying — git-push-setup re-run before launch).
			if reconciled := reconcileAdoptedGitPush(ctx, client, sshDeployer, rt, stateDir, existing); len(reconciled) > 0 {
				resp.Message += fmt.Sprintf(" Git-push state reconciled from live for %s — these services already have a working remote + token, so launch-production will NOT require re-running git-push-setup on them.", strings.Join(reconciled, ", "))
			}
			if needsStacks(resp) {
				populateStacks(ctx, resp, schemaCache)
			}
			return bootstrapResult(ctx, resp, engine, client, projectID, rt), nil, nil
		}
		// Recipe route: the plan is DERIVED from the recipe (the owner). An
		// empty/omitted plan derives the recipe's full shape; a submitted plan
		// is reconciled into overrides (rename / managed EXISTS) — the agent
		// never re-authors the shape. This handles both, so recipe never falls
		// through to the classic explicit-plan or attestation paths below.
		if bootstrapSessionRoute(engine) == workflow.BootstrapRouteRecipe {
			devOnly := input.RecipeNarrow == workflow.RecipeNarrowDevOnly
			resp, err := engine.BootstrapCompleteRecipePlan(input.Plan, devOnly, schemas, nil)
			if err != nil {
				pe := platform.NewPlatformError(
					platform.ErrInvalidParameter,
					fmt.Sprintf("Recipe plan failed: %v", err),
					"Omit the plan to accept the recipe's derived shape, or submit a plan only to rename a colliding hostname / flip a managed dep to EXISTS. For a dev-only provision of a standard recipe set recipeNarrow=\"dev-only\".")
				pe.Subcode = bootstrapPlanSubcode(err)
				return convertError(pe, WithRecoveryStatus()), nil, nil
			}
			if needsStacks(resp) {
				populateStacks(ctx, resp, schemaCache)
			}
			return bootstrapResult(ctx, resp, engine, client, projectID, rt), nil, nil
		}
		if input.Plan != nil {
			resp, err := engine.BootstrapCompletePlan(input.Plan, schemas, nil)
			if err != nil {
				pe := platform.NewPlatformError(
					platform.ErrInvalidParameter,
					fmt.Sprintf("Plan validation failed: %v", err),
					"Provide valid plan: [{runtime: {devHostname, type}, dependencies: [{hostname, type, resolution}]}]. Hostnames: lowercase a-z0-9, max 25 chars.")
				pe.Subcode = bootstrapPlanSubcode(err)
				return convertError(pe, WithRecoveryStatus()), nil, nil
			}
			if needsStacks(resp) {
				populateStacks(ctx, resp, schemaCache)
			}
			return bootstrapResult(ctx, resp, engine, client, projectID, rt), nil, nil
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
	populateRuntimeURLs(ctx, client, projectID, engine, resp)
	if needsStacks(resp) {
		populateStacks(ctx, resp, schemaCache)
	}
	return bootstrapResult(ctx, resp, engine, client, projectID, rt), nil, nil
}

// bootstrapPlanSubcode narrows a discover-step plan-completion failure into
// the telemetry error_subcode catalog (docs/spec-telemetry.md §4.2,
// platform.Subcode* — telemetry-production-readiness plan S4, the worst
// INVALID_PARAMETER conflations per plans/telemetry-analysis-2026-07-02.md
// §3): the adopt route's pairing-ambiguity refusal, the recipe route's
// derived-shape mismatch, and the classic/explicit-plan shape validator are
// three DISTINCT root causes that otherwise collapse into one
// undiagnosable top-level code. Dispatches via errors.Is against the
// workflow-layer sentinels — never string-sniffing the message — so a
// wording change can't silently break the classification. Returns "" (no
// subcode) for every other plan-completion failure (session/route/step
// guards, recipe-corpus problems), which is the correct un-narrowed
// outcome, not a missed case.
func bootstrapPlanSubcode(err error) string {
	switch {
	case errors.Is(err, workflow.ErrAdoptPairingChoice):
		return platform.SubcodeAmbiguousScope
	case errors.Is(err, workflow.ErrRecipePlanMismatch):
		return platform.SubcodePlanTypeMismatch
	case errors.Is(err, workflow.ErrPlanShapeInvalid):
		return platform.SubcodeWorkerPlanShape
	default:
		return ""
	}
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

// populateRuntimeURLs attaches the structured runtime-URL collection (RCO-7)
// to resp once the bootstrap session is at or past the administrative close
// step: the response that follows a successful provision (Current advances
// to "close"), any action="status" taken while close remains active, and
// the terminal close response (Current nil) all qualify — anything earlier
// (discover/provision still in progress) is left untouched.
//
// Resolution happens here at L4 via ops.ResolveSubdomainURL because
// internal/workflow must not import internal/ops (hard layering rule,
// depguard-enforced) — workflow only defines the RuntimeURL shape and
// renders guidance from it (workflow.FormatRuntimeURLsForGuide), never
// resolves URLs itself. Best-effort: a service whose URL can't be resolved
// (ListProjectServices error, or ops.ResolveSubdomainURL returning "") is
// simply left out of the collection — never a fabricated URL, never an
// error that blocks close. The rendered guidance is appended to the close
// step's DetailedGuide (session still open) or to resp.Message (terminal),
// so the "guidance derives from the collection" contract holds regardless
// of which shape the response takes.
func populateRuntimeURLs(ctx context.Context, client platform.Client, projectID string, engine *workflow.Engine, resp *workflow.BootstrapResponse) {
	if resp == nil || (resp.Current != nil && resp.Current.Name != workflow.StepClose) {
		return
	}
	state, err := engine.GetState()
	if err != nil || state == nil || state.Bootstrap == nil || state.Bootstrap.Plan == nil || len(state.Bootstrap.Plan.Targets) == 0 {
		return
	}
	services, err := ops.ListProjectServices(ctx, client, projectID)
	if err != nil {
		return
	}

	resp.RuntimeURLs = buildRuntimeURLs(ctx, client, projectID, services, state.Bootstrap.Plan)
	guidance := workflow.FormatRuntimeURLsForGuide(resp.RuntimeURLs)
	if resp.Current != nil {
		resp.Current.DetailedGuide += "\n\n" + guidance
	} else {
		resp.Message += "\n\n" + guidance
	}
}

// buildRuntimeURLs resolves the composed subdomain URL for every
// subdomain-enabled service in services, classifying each hostname's role
// against plan. A service whose URL can't be resolved (ResolveSubdomainURL
// returning "") is omitted — best-effort, never a fabricated URL.
func buildRuntimeURLs(ctx context.Context, client platform.Client, projectID string, services []platform.ServiceStack, plan *workflow.ServicePlan) []workflow.RuntimeURL {
	var urls []workflow.RuntimeURL
	for i := range services {
		svc := &services[i]
		if !svc.SubdomainAccess {
			continue
		}
		url := ops.ResolveSubdomainURL(ctx, client, projectID, svc)
		if url == "" {
			continue
		}
		role := runtimeRoleForHostname(plan, svc.Name)
		urls = append(urls, workflow.RuntimeURL{
			Hostname: svc.Name,
			Role:     role,
			URL:      url,
			Handoff:  role == workflow.RuntimeURLRoleStage,
		})
	}
	return urls
}

// runtimeRoleForHostname classifies hostname against the bootstrap plan:
// RuntimeURLRoleDev for a target's DevHostname, RuntimeURLRoleStage for a
// target's StageHostname(), RuntimeURLRoleOther for anything else
// (a managed dependency exposing HTTP, or a service outside the plan).
func runtimeRoleForHostname(plan *workflow.ServicePlan, hostname string) string {
	for _, t := range plan.Targets {
		if t.Runtime.DevHostname == hostname {
			return workflow.RuntimeURLRoleDev
		}
		if t.Runtime.StageHostname() == hostname {
			return workflow.RuntimeURLRoleStage
		}
	}
	return workflow.RuntimeURLRoleOther
}

func handleBootstrapSkip(ctx context.Context, engine *workflow.Engine, client platform.Client, projectID string, rt runtime.Info, schemaCache *schema.Cache, input WorkflowInput) (*mcp.CallToolResult, any, error) {
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
	return bootstrapResult(ctx, resp, engine, client, projectID, rt), nil, nil
}

// handleBootstrapStatus is the direct action="status" path (FocusBootstrap
// dispatch in handleWorkflowAction) — the ONE of the three bootstrap-status
// callers (alongside handleResume/handleIterate, which share
// bootstrapStatusResult) that RCO-7 names explicitly ("action=status while
// close remains active" must carry the structured runtime-URL collection).
// It therefore needs client+projectID to resolve URLs via
// populateRuntimeURLs — its own small body instead of delegating to
// bootstrapStatusResult, which stays a narrower shared helper for the two
// callers that don't have those two params threaded to them.
func handleBootstrapStatus(ctx context.Context, engine *workflow.Engine, client platform.Client, projectID string, rt runtime.Info, schemaCache *schema.Cache) (*mcp.CallToolResult, any, error) {
	resp, err := engine.BootstrapStatus()
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrBootstrapNotActive,
			fmt.Sprintf("Bootstrap status failed: %v", err),
			""), WithRecoveryStatus()), nil, nil
	}
	populateRuntimeURLs(ctx, client, projectID, engine, resp)
	if needsStacks(resp) {
		populateStacks(ctx, resp, schemaCache)
	}
	return bootstrapResult(ctx, resp, engine, client, projectID, rt), nil, nil
}

// bootstrapStatusResult returns the current bootstrap status as a
// BootstrapResponse. Shared by handleResume and handleIterate — neither
// has client/projectID threaded to it (their sole callers in
// handleWorkflowAction predate RCO-7), so neither carries the
// structured runtime-URL collection; only the direct action="status" path
// (handleBootstrapStatus) does, per the RCO-7 contract.
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
			if initErr := ops.InitServiceGit(ctx, sshDeployer, hostname, target.Runtime.Type); initErr != nil {
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
