package tools

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/workflow"
)

const (
	workflowBootstrap = workflow.WorkflowBootstrap
	workflowDevelop   = workflow.WorkflowDevelop
	workflowRecipe    = workflow.WorkflowRecipe
)

// WorkflowInput is the input type for zerops_workflow.
type WorkflowInput struct {
	// Legacy: workflow name for static guidance (backward compat).
	Workflow string `json:"workflow,omitempty" jsonschema:"Workflow name: bootstrap, develop, recipe, cicd, or export."`

	// Multi-action fields.
	Action      string                     `json:"action,omitempty"      jsonschema:"Orchestration action: start, complete, skip, status, reset, iterate, resume, list, route, dispatch-brief-atom (retrieve one atom of an envelope-split dispatch brief), or generate-finalize (recipe only — generates all 13 recipe files from plan)."`
	Intent      string                     `json:"intent,omitempty"      jsonschema:"User intent description for start action (what you want to accomplish)."`
	Attestation string                     `json:"attestation,omitempty" jsonschema:"Description of what was verified or accomplished (required for complete actions)."`
	Step        string                     `json:"step,omitempty"        jsonschema:"Bootstrap step name for complete/skip actions (discover, provision, close)."`
	SubStep     string                     `json:"substep,omitempty"     jsonschema:"Optional sub-step name for recipe complete action (e.g. scaffold, zerops-yaml, app-code, readme, smoke-test). Completes a sub-step within the current step instead of the full step."`
	Plan        []workflow.BootstrapTarget `json:"plan,omitempty"        jsonschema:"Structured service plan: array of {runtime: {devHostname, type, bootstrapMode?, stageHostname?, isExisting?}, dependencies: [{hostname, type, mode?, resolution}]}. resolution: CREATE (new service), EXISTS (already in project), SHARED (created by another target in this plan). stageHostname: explicit stage hostname for standard mode when devHostname doesn't end in 'dev' (e.g. adopting existing services)."`
	Reason      string                     `json:"reason,omitempty"      jsonschema:"Reason for skipping a step (skip action). Defaults to 'skipped by user'."`
	SessionID   string                     `json:"sessionId,omitempty"   jsonschema:"Session ID for resume action."`
	Strategies  map[string]string          `json:"strategies,omitempty"  jsonschema:"Per-service strategy map for strategy action (e.g. {\"appdev\":\"push-git\"})."`
	Tier        string                     `json:"tier,omitempty"        jsonschema:"Recipe tier: minimal or showcase (recipe workflow only)."`
	RecipePlan  *workflow.RecipePlan       `json:"recipePlan,omitempty"  jsonschema:"Structured recipe plan for research step completion. Pass as a JSON object, NOT a stringified JSON blob — e.g. recipePlan={\"slug\":\"...\",\"recipeType\":\"...\",\"features\":[...],\"targets\":[...]}, not recipePlan=\"{\\\"slug\\\":...}\". The schema validator rejects strings for this field; stringifying costs a retry round-trip."`

	// Bootstrap route selection. The first call to action=start workflow=bootstrap
	// omits these — the engine returns a ranked list of route options. The LLM
	// picks one and calls start again with route set.
	Route      string `json:"route,omitempty"      jsonschema:"Bootstrap route: adopt, recipe, classic, or resume. Omit on first start call to receive ranked route options; set on second call to commit the chosen route."`
	RecipeSlug string `json:"recipeSlug,omitempty" jsonschema:"Recipe slug when route=recipe (pick one from the discovery response's routeOptions[].recipeSlug)."`

	// Develop workflow scope — the runtime service hostnames this task works
	// on. Required for action="start" workflow="develop". Fixed at start and
	// stays stable through the task; auto-close fires when every hostname in
	// scope has a succeeded deploy and a passed verify. Managed-service
	// hostnames are rejected (not deployable). Services newly bootstrapped
	// mid-task do NOT auto-join — close and start a new develop with the
	// expanded scope, or treat them as out-of-band.
	Scope []string `json:"scope,omitempty" jsonschema:"Runtime service hostnames this task works on (required for action='start' workflow='develop'). Fixed at start; auto-close requires every hostname in scope to have a successful deploy and passed verify. Example: [\"appdev\",\"appstage\"]. Reject managed services — only deployable runtime hostnames."`

	// Recipe workflow only — the agent's self-reported model identifier from its
	// own system prompt. Required at start for the recipe workflow because v13
	// shipped on Sonnet/200k by accident and doubled wall time while regressing
	// close-step severity. The agent must report its EXACT model ID (e.g.
	// "claude-opus-4-7[1m]" or "claude-opus-4-6[1m]"), not an alias like "opus".
	ClientModel string `json:"clientModel,omitempty" jsonschema:"Recipe workflow start only: the agent's exact model identifier from its own system prompt (e.g. 'claude-opus-4-7[1m]' or 'claude-opus-4-6[1m]'). Required — recipe workflow rejects non-Opus models and Opus variants without 1M context."`

	// Recipe comment inputs — passed to generate-finalize to bake agent-authored
	// per-env comments into the 6 import.yaml files, replacing per-file Edit.
	EnvComments map[string]workflow.EnvComments `json:"envComments,omitempty" jsonschema:"Recipe generate-finalize only: per-env comments for all 6 import.yaml files. Keyed by env index as string ('0'..'5'). Each env has {service: {hostname: comment}, project: comment}. Service keys match the hostnames that appear in that env's file — envs 0-1 (dev/stage pair) take 'appdev' and 'appstage'; envs 2-5 take the base hostname 'app'. Each env's commentary should reflect what makes THAT env distinct (AI agent workspace / remote CDE / local validator / stage / small prod with minContainers / HA prod with DEDICATED CPU + corePackage)."`

	// Recipe project-level env var inputs — passed to generate-finalize to bake
	// agent-authored per-env project.envVariables declarations into all 6
	// import.yaml files. Replaces the v5 anti-pattern of hand-editing generated
	// files (which were re-wiped on every generate-finalize re-run).
	ProjectEnvVariables map[string]map[string]string `json:"projectEnvVariables,omitempty" jsonschema:"Recipe generate-finalize only: per-env project-level envVariables for all 6 import.yaml files. Keyed by env index as string ('0'..'5'). Each env value is a flat {name: value} map baked into that env's project.envVariables block. Values may contain ${zeropsSubdomainHost} — the platform preprocessor resolves it at project import time. Different envs typically carry different shapes: envs 0-1 (dev/stage pair) carry DEV_* and STAGE_* URL constants derived from apidev/appdev/apistage/appstage hostnames; envs 2-5 (single-slot) carry STAGE_* only with hostnames api/app. Merge semantics: a non-empty map for an env REPLACES that env's prior map (atomic); an empty map CLEARS; omitting an env leaves it untouched. Refine one env at a time by passing only that env's key."`

	// AtomID is the atom identifier the main agent passes to
	// action="dispatch-brief-atom" when retrieving one component atom of
	// a dispatch brief whose inlined form would exceed the MCP tool-
	// response token cap. See Cx-BRIEF-OVERFLOW / defect-class-registry
	// §16.1. Fully-qualified dot-path, e.g. "briefs.writer.manifest-contract".
	AtomID string `json:"atomId,omitempty" jsonschema:"Dispatch-brief atom identifier for action=\"dispatch-brief-atom\". Fully-qualified dot-path (e.g. 'briefs.writer.manifest-contract'). Retrieved from the envelope listed in a substep guide when the composed dispatch brief exceeds the MCP response cap."`
}

// immediateResponse is returned from immediate (stateless) workflows.
type immediateResponse struct {
	Workflow string `json:"workflow"`
	Guidance string `json:"guidance"`
}

// RegisterWorkflow registers the zerops_workflow tool.
// rt carries the runtime detection (container vs local, self hostname, project
// ID from container env). selfHostname duplicates rt.ServiceName for handlers
// that haven't migrated yet — Phase 7 consolidates on rt.
// mounter enables auto-mounting runtime services after provision (nil in local env).
func RegisterWorkflow(srv *mcp.Server, client platform.Client, projectID string, cache *ops.StackTypeCache, schemaCache *schema.Cache, engine *workflow.Engine, logFetcher platform.LogFetcher, stateDir, selfHostname string, mounter ops.Mounter, rt runtime.Info) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_workflow",
		Description: "Orchestrate Zerops operations. Call with action=\"start\" workflow=\"name\" to begin a tracked session with guidance. Workflows: bootstrap (create/adopt infrastructure only — not the user's application), develop (all development, deployment, fixing, investigating), recipe (create recipe repo files), cicd (CI/CD setup), export (turn a deployed service into a re-importable git repo with import.yaml + buildFromGit). After start: action=\"complete|skip|status\" (step progression), action=\"reset|iterate|resume|list|route\".",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Workflow orchestration",
			ReadOnlyHint:   false,
			IdempotentHint: false,
			OpenWorldHint:  boolPtr(false),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input WorkflowInput) (*mcp.CallToolResult, any, error) {
		// New multi-action handler.
		if input.Action != "" {
			return handleWorkflowAction(ctx, projectID, engine, client, cache, schemaCache, logFetcher, input, stateDir, selfHostname, mounter, rt)
		}

		// Immediate workflows (cicd, export) may be fetched without action.
		// Orchestrated workflows (bootstrap, develop, recipe) always require
		// a session and must route through action="start".
		if input.Workflow == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"No workflow or action specified",
				`Use action="start" workflow="bootstrap|develop|recipe" for orchestrated workflows, or workflow="cicd|export" for immediate guidance`)), nil, nil
		}
		if !workflow.IsImmediateWorkflow(input.Workflow) {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Workflow %q requires action=\"start\"", input.Workflow),
				fmt.Sprintf(`Use action="start" workflow=%q intent="..."`, input.Workflow))), nil, nil
		}
		guidance, err := synthesizeImmediateGuidance(input.Workflow, engine, rt)
		if err != nil {
			return convertError(err), nil, nil
		}
		return textResult(guidance), nil, nil
	})
}

func handleWorkflowAction(ctx context.Context, projectID string, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, schemaCache *schema.Cache, logFetcher platform.LogFetcher, input WorkflowInput, stateDir, selfHostname string, mounter ops.Mounter, rt runtime.Info) (*mcp.CallToolResult, any, error) {
	// dispatch-brief-atom is a stateless content-retrieval action — it
	// reads an atom from the embedded recipe tree and does not touch
	// session state. Handle it before the engine-required guard so the
	// action works even when a session has not been started (the main
	// agent may retrieve atoms without an active session during debug).
	if input.Action == "dispatch-brief-atom" {
		return handleDispatchBriefAtom(input)
	}
	if engine == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			"Workflow engine not initialized",
			"Ensure ZCP is configured with a state directory")), nil, nil
	}

	switch input.Action {
	case "start":
		return handleStart(ctx, projectID, engine, client, cache, input, rt)
	case "reset":
		return handleReset(ctx, engine, client, projectID)
	case "iterate":
		return handleIterate(ctx, engine, client, cache)
	case "complete":
		// Develop is stateless — step-based completion is never valid.
		if isDevelopStep(input.Step) {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"Deploy steps are handled automatically by zerops_deploy pre-flight validation",
				"Use zerops_deploy to deploy, zerops_verify to verify")), nil, nil
		}
		active := detectActiveWorkflow(engine)
		if active == workflowRecipe {
			return handleRecipeComplete(ctx, engine, client, cache, schemaCache, projectID, stateDir, input)
		}
		var liveTypes []platform.ServiceStackType
		if cache != nil && client != nil {
			liveTypes = cache.Get(ctx, client)
		}
		return handleBootstrapComplete(ctx, engine, client, cache, input, liveTypes, logFetcher, projectID, stateDir, mounter)
	case "generate-finalize":
		if detectActiveWorkflow(engine) == workflowRecipe {
			return handleRecipeGenerateFinalize(engine, input.EnvComments, input.ProjectEnvVariables)
		}
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"generate-finalize is only available during recipe workflow",
			"")), nil, nil
	case "skip":
		// Develop is stateless — step-based skipping is never valid.
		if isDevelopStep(input.Step) {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"Deploy steps are handled automatically by zerops_deploy pre-flight validation",
				"Use zerops_deploy to deploy, zerops_verify to verify")), nil, nil
		}
		active := detectActiveWorkflow(engine)
		if active == workflowRecipe {
			return handleRecipeSkip(ctx, engine, input)
		}
		return handleBootstrapSkip(ctx, engine, client, cache, input)
	case "status":
		active := detectActiveWorkflow(engine)
		if active == workflowRecipe {
			return handleRecipeStatus(ctx, engine)
		}
		if active == workflowBootstrap {
			return handleBootstrapStatus(ctx, engine, client, cache)
		}
		return handleLifecycleStatus(ctx, engine, client, projectID, rt)
	case "close":
		return handleWorkSessionClose(engine, input)
	case "resume":
		return handleResume(ctx, engine, client, cache, input)
	case "list":
		return handleListSessions(engine)
	case "route":
		return handleRoute(ctx, engine, client, projectID, stateDir, selfHostname)
	case "strategy":
		return handleStrategy(engine, input, stateDir)
	default:
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Unknown action %q", input.Action),
			"Valid actions: start, complete, close, skip, status, reset, iterate, resume, list, route, strategy, dispatch-brief-atom")), nil, nil
	}
}

func handleStart(ctx context.Context, projectID string, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, input WorkflowInput, rt runtime.Info) (*mcp.CallToolResult, any, error) {
	// v8.90 Fix A: reject action=start when a DIFFERENT workflow is already
	// active. This closes the sub-agent-misuse path: a sub-agent spawned by
	// the main agent inside a running recipe calling action=start
	// workflow=develop should not be told "Run bootstrap first" (the develop
	// handler's prereq-missing message). The main agent owns workflow state;
	// the sub-agent's job is whatever the dispatch brief scoped it to.
	//
	// Immediate workflows (cicd, export) are stateless — they don't create a
	// session, so the active-session check doesn't apply. Same-workflow
	// re-starts fall through to the workflow-specific handler, which owns
	// idempotency (e.g. handleRecipeStart returning the current state).
	if !workflow.IsImmediateWorkflow(input.Workflow) {
		if active := detectActiveWorkflow(engine); active != "" && active != input.Workflow {
			return convertError(platform.NewPlatformError(
				platform.ErrSubagentMisuse,
				fmt.Sprintf(
					"A %q workflow session is already active — cannot start a %q workflow inside it.",
					active, input.Workflow,
				),
				"If you are a sub-agent spawned by the main agent inside a recipe session, "+
					"do NOT call zerops_workflow. The main agent holds workflow state. "+
					"Perform your scoped task using the tools listed in your dispatch brief and return.",
			)), nil, nil
		}
	}

	// Immediate workflows: stateless, atom-synthesized guidance.
	if workflow.IsImmediateWorkflow(input.Workflow) {
		guidance, err := synthesizeImmediateGuidance(input.Workflow, engine, rt)
		if err != nil {
			return convertError(err), nil, nil
		}
		return jsonResult(immediateResponse{
			Workflow: input.Workflow,
			Guidance: guidance,
		}), nil, nil
	}

	// Bootstrap conductor — discovery + commit split.
	if input.Workflow == workflowBootstrap {
		return handleBootstrapStart(ctx, projectID, engine, client, cache, input)
	}

	// Develop workflow — stateless briefing, no session created.
	if input.Workflow == workflowDevelop {
		return handleDevelopBriefing(ctx, engine, client, projectID, input, rt)
	}

	// Recipe workflow.
	if input.Workflow == workflowRecipe {
		return handleRecipeStart(ctx, projectID, engine, client, cache, input)
	}

	// Unknown workflow — return error.
	return convertError(platform.NewPlatformError(
		platform.ErrInvalidParameter,
		fmt.Sprintf("Unknown orchestrated workflow %q", input.Workflow),
		"Valid workflows: bootstrap, develop, recipe, cicd, export")), nil, nil
}

// isDevelopStep returns true if the step name is a develop workflow step.
func isDevelopStep(step string) bool {
	return step == workflow.DeployStepPrepare || step == workflow.DeployStepExecute || step == workflow.DeployStepVerify
}

// detectActiveWorkflow returns the active workflow type from engine state.
func detectActiveWorkflow(engine *workflow.Engine) string {
	if !engine.HasActiveSession() {
		return ""
	}
	state, err := engine.GetState()
	if err != nil {
		return ""
	}
	if state.Recipe != nil && state.Recipe.Active {
		return workflowRecipe
	}
	if state.Bootstrap != nil && state.Bootstrap.Active {
		return workflowBootstrap
	}
	return ""
}

// resetReport is the structured audit returned by handleReset so the agent
// sees exactly what the mutation cleared and what survived — observability
// for a state transition that was previously a one-line "success" message.
type resetReport struct {
	Cleared   resetSnapshot `json:"cleared"`
	Preserved resetSnapshot `json:"preserved"`
	Next      string        `json:"next"`
}

type resetSnapshot struct {
	BootstrapSessionID string   `json:"bootstrapSessionId,omitempty"`
	RecipeSessionID    string   `json:"recipeSessionId,omitempty"`
	CompletedSteps     int      `json:"completedSteps,omitempty"`
	IncompleteMetas    []string `json:"incompleteMetas,omitempty"`
	CompleteMetas      []string `json:"completeMetas,omitempty"`
	LiveServices       int      `json:"liveServices,omitempty"`
	WorkSessions       int      `json:"workSessions,omitempty"`
}

func handleReset(ctx context.Context, engine *workflow.Engine, client platform.Client, projectID string) (*mcp.CallToolResult, any, error) {
	// Snapshot state before reset — Reset() clears engine memory + removes
	// the session file + deletes incomplete metas for the session.
	// Complete metas, work sessions, and live platform services are never
	// touched; surface that explicitly so the agent isn't guessing.
	preState, _ := engine.GetState()
	metasBefore, _ := workflow.ListServiceMetas(engine.StateDir())

	cleared := buildClearedSnapshot(preState, metasBefore)
	preserved := resetSnapshot{}
	if client != nil {
		if live, listErr := client.ListServices(ctx, projectID); listErr == nil {
			preserved.LiveServices = len(live)
		}
	}

	if err := engine.Reset(); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			fmt.Sprintf("Reset failed: %v", err),
			"")), nil, nil
	}

	// Recompute preserved metas after reset to catch cleanIncompleteMetas
	// removals. Complete metas (Bootstrapped=true) stay; that's the set
	// the agent can adopt or develop against next.
	metasAfter, _ := workflow.ListServiceMetas(engine.StateDir())
	preserved.CompleteMetas = completeMetaNames(metasAfter)

	report := resetReport{
		Cleared:   cleared,
		Preserved: preserved,
		Next:      buildResetNextHint(preserved),
	}
	return jsonResult(report), nil, nil
}

// buildClearedSnapshot captures everything reset will destroy: the active
// bootstrap/recipe session plus any incomplete ServiceMetas (those with
// no BootstrappedAt). Preserved state (complete metas, live services) is
// computed after reset by the caller since cleanIncompleteMetasForSession
// can only be observed post-mutation.
func buildClearedSnapshot(preState *workflow.WorkflowState, metasBefore []*workflow.ServiceMeta) resetSnapshot {
	cleared := resetSnapshot{}
	if preState != nil {
		if preState.Bootstrap != nil && preState.Bootstrap.Active {
			cleared.BootstrapSessionID = preState.SessionID
			cleared.CompletedSteps = countCompletedBootstrapSteps(preState.Bootstrap)
		}
		if preState.Recipe != nil && preState.Recipe.Active {
			cleared.RecipeSessionID = preState.SessionID
		}
	}
	for _, m := range metasBefore {
		if m == nil {
			continue
		}
		if m.IsComplete() {
			// Complete metas survive reset.
			continue
		}
		cleared.IncompleteMetas = append(cleared.IncompleteMetas, m.Hostname)
	}
	sort.Strings(cleared.IncompleteMetas)
	return cleared
}

func completeMetaNames(metas []*workflow.ServiceMeta) []string {
	var names []string
	for _, m := range metas {
		if m == nil || !m.IsComplete() {
			continue
		}
		names = append(names, m.Hostname)
	}
	sort.Strings(names)
	return names
}

func countCompletedBootstrapSteps(bs *workflow.BootstrapState) int {
	if bs == nil {
		return 0
	}
	n := 0
	for _, s := range bs.Steps {
		if s.Status == workflow.StepStatusComplete || s.Status == workflow.StepStatusSkipped {
			n++
		}
	}
	return n
}

// buildResetNextHint picks the most useful follow-up call based on what
// survived reset — live services suggest adopt; complete metas with no
// live services (rare, e.g. after UI deletion) suggest develop; empty
// state suggests a fresh classic start.
func buildResetNextHint(preserved resetSnapshot) string {
	switch {
	case preserved.LiveServices > 0 && len(preserved.CompleteMetas) == 0:
		return "Live services exist without metas — action=\"start\" workflow=\"bootstrap\" route=\"adopt\""
	case preserved.LiveServices > 0 && len(preserved.CompleteMetas) > 0:
		return "Services still adopted — action=\"start\" workflow=\"develop\" intent=\"...\" scope=[...]"
	case len(preserved.CompleteMetas) > 0:
		return "Metas remain but no live services — verify state via zerops_discover, then start develop or re-bootstrap"
	default:
		return "Empty project — action=\"start\" workflow=\"bootstrap\" (no route to see options)"
	}
}

// handleDispatchBriefAtom returns a single atom body by its fully-
// qualified dot-path ID. Cx-BRIEF-OVERFLOW delivery mechanism: when a
// composed dispatch brief exceeds the MCP tool-response token cap, the
// substep guide embeds an envelope listing atom IDs the main agent
// retrieves via this action, then stitches locally before dispatching
// the sub-agent. See docs/zcprecipator2/HANDOFF-to-I6.md
// §Cx-BRIEF-OVERFLOW and defect-class-registry §16.1.
//
// Returns JSON `{"atomId":"X","body":"..."}`. Atom IDs are drawn from
// envelopes the server itself emits — the agent should not invent IDs.
// Unknown IDs return an INVALID_PARAMETER error.
func handleDispatchBriefAtom(input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.AtomID == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"atomId is required for dispatch-brief-atom action",
			"Pass the atomId from the envelope listed in the substep guide's dispatch-brief section")), nil, nil
	}
	body, err := workflow.LoadAtomBody(input.AtomID)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("dispatch-brief atom %q unknown or unreadable: %v", input.AtomID, err),
			"Check the atomId against the envelope in the current substep guide")), nil, nil
	}
	return jsonResult(map[string]any{
		"atomId": input.AtomID,
		"body":   body,
	}), nil, nil
}

func handleIterate(ctx context.Context, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache) (*mcp.CallToolResult, any, error) {
	if _, err := engine.Iterate(); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			fmt.Sprintf("Iterate failed: %v", err),
			"Start a session first")), nil, nil
	}
	active := detectActiveWorkflow(engine)
	if active == workflowRecipe {
		return handleRecipeStatus(ctx, engine)
	}
	return bootstrapStatusResult(ctx, engine, client, cache)
}

func handleResume(ctx context.Context, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.SessionID == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"sessionId is required for resume action",
			"Specify the session ID to resume")), nil, nil
	}
	if _, err := engine.Resume(input.SessionID); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			fmt.Sprintf("Resume failed: %v", err),
			"Session may not exist or may still be active")), nil, nil
	}
	active := detectActiveWorkflow(engine)
	if active == workflowRecipe {
		return handleRecipeStatus(ctx, engine)
	}
	return bootstrapStatusResult(ctx, engine, client, cache)
}

// handleBootstrapStart dispatches the bootstrap "start" action into one of
// three sub-paths based on input.Route:
//
//  1. Empty route → discovery mode. Fetches existing services, calls
//     engine.BootstrapDiscover, returns ranked route options without
//     committing a session. The LLM reads the options and calls start
//     again with route set.
//  2. route=resume → delegates to handleResume (existing session resume
//     flow). The LLM passes sessionId from the discovery response's
//     resumeSession field.
//  3. route=adopt|recipe|classic → commits session via
//     BootstrapStartWithRoute with the LLM's explicit choice.
func handleBootstrapStart(ctx context.Context, projectID string, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	route := input.Route

	// Plan is not accepted in start. The two-phase bootstrap (route
	// selection → plan production) intentionally keeps them separate:
	// start commits the route (discovery→decision reasoning space); the
	// plan emerges during the discover step from route-specific materials
	// (recipe YAML, zerops_discover for adopt, reasoning for classic) and
	// is submitted via action="complete" step="discover" plan=[...].
	// Silently accepting plan here hid real bugs — the agent passed it,
	// thought it stuck, and didn't notice until three calls later.
	if len(input.Plan) > 0 {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"plan is not accepted in action=start; submit it via action=\"complete\" step=\"discover\" plan=[...]",
			"Start commits the route only. The discover step is the reasoning space where the plan is produced from route-specific materials; commit it there.")), nil, nil
	}

	// Discovery pass — no route specified, no session committed.
	if route == "" {
		existing, listErr := listExistingServices(ctx, client, projectID)
		if listErr != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrAPIError,
				fmt.Sprintf("Bootstrap discovery failed: %v", listErr),
				"Check project access and try again")), nil, nil
		}
		resp, err := engine.BootstrapDiscover(projectID, input.Intent, existing) //nolint:contextcheck // BootstrapDiscover is synchronous, no I/O to cancel
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrAPIError,
				fmt.Sprintf("Bootstrap discovery failed: %v", err),
				"")), nil, nil
		}
		return jsonResult(resp), nil, nil
	}

	// Resume route — dispatch into the existing resume flow.
	if route == string(workflow.BootstrapRouteResume) {
		if input.SessionID == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"route=resume requires sessionId (pick it from the discovery response's resumeSession field)",
				"Call action=start workflow=bootstrap without route first to see resumable sessions")), nil, nil
		}
		return handleResume(ctx, engine, client, cache, input)
	}

	// Commit pass — start a session with the chosen route.
	resp, err := engine.BootstrapStartWithRoute(projectID, input.Intent, workflow.BootstrapRoute(route), input.RecipeSlug)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrWorkflowActive,
			fmt.Sprintf("Bootstrap start failed: %v", err),
			"Call action=start workflow=bootstrap without route to discover valid options, or action=reset to clear the existing session")), nil, nil
	}
	populateStacks(ctx, resp, client, cache)
	return jsonResult(resp), nil, nil
}

// listExistingServices is a best-effort wrapper around client.ListServices
// that tolerates a nil client (test fixtures without platform access).
func listExistingServices(ctx context.Context, client platform.Client, projectID string) ([]platform.ServiceStack, error) {
	if client == nil || projectID == "" {
		return nil, nil
	}
	return client.ListServices(ctx, projectID)
}

func handleListSessions(engine *workflow.Engine) (*mcp.CallToolResult, any, error) {
	sessions, err := engine.ListActiveSessions()
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			fmt.Sprintf("List sessions failed: %v", err),
			"")), nil, nil
	}
	return jsonResult(sessions), nil, nil
}

// handleLifecycleStatus returns the canonical orientation block. Used when
// no bootstrap/recipe session is active — covers both idle and develop phases.
//
// Pipeline: ComputeEnvelope (parallel I/O) → Synthesize (typed knowledge
// atoms) → BuildPlan (typed NextActions) → RenderStatus (markdown). A loader
// error on the atom corpus is fatal — the atoms ship embedded so a failure
// here means a malformed build, not a runtime condition.
func handleLifecycleStatus(ctx context.Context, engine *workflow.Engine, client platform.Client, projectID string, rt runtime.Info) (*mcp.CallToolResult, any, error) {
	envelope, err := workflow.ComputeEnvelope(ctx, client, engine.StateDir(), projectID, rt, time.Now())
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			fmt.Sprintf("Compute envelope: %v", err),
			"")), nil, nil
	}
	corpus, err := workflow.LoadAtomCorpus()
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			fmt.Sprintf("Load knowledge atoms: %v", err),
			"")), nil, nil
	}
	guidance, err := workflow.Synthesize(envelope, corpus)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			fmt.Sprintf("Synthesize guidance: %v", err),
			"")), nil, nil
	}
	plan := workflow.BuildPlan(envelope)
	return textResult(workflow.RenderStatus(workflow.Response{
		Envelope: envelope,
		Guidance: guidance,
		Plan:     &plan,
	})), nil, nil
}

// handleWorkSessionClose closes the current-PID work session. Always
// succeeds — close is session cleanup, not commitment. Any edits live on
// the SSHFS mount and any deploys live on the platform; close only removes
// the per-PID session file. Auto-close is the "task done, objectively
// verified" signal (scope-all-green); manual close is "I'm done here, for
// whatever reason".
//
// 1 task = 1 session invariant: callers restart with a new intent to open
// the next task. New-intent starts auto-close prior in handleDevelopBriefing
// already, so explicit close is rarely needed except for investigation
// tasks with no deploy activity.
func handleWorkSessionClose(engine *workflow.Engine, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.Workflow != "" && input.Workflow != workflowDevelop {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("close is only supported for workflow=\"develop\" (got %q)", input.Workflow),
			"")), nil, nil
	}
	pid := os.Getpid()
	stateDir := engine.StateDir()

	_ = workflow.DeleteWorkSession(stateDir, pid)
	_ = workflow.UnregisterSession(stateDir, workflow.WorkSessionID(pid))
	return textResult("Work session closed. Start the next task: zerops_workflow action=\"start\" workflow=\"develop\" intent=\"...\" scope=[\"hostname\",...]"), nil, nil
}
