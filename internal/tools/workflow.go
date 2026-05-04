package tools

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
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
	workflowExport    = "export"
)

// WorkflowInput is the input type for zerops_workflow.
type WorkflowInput struct {
	// Legacy: workflow name for static guidance (backward compat).
	Workflow string `json:"workflow,omitempty" jsonschema:"Workflow name: bootstrap, develop, or export. For recipe authoring use the dedicated zerops_recipe tool (v3 engine, docs/zcprecipator3/plan.md)."`

	// Multi-action fields.
	Action      string                     `json:"action,omitempty"      jsonschema:"Orchestration action: start (workflow=bootstrap is two-phase: first call without route returns kind=\"route-menu\" with ranked options, second call with route=<chosen> commits the session and returns kind=\"session-active\"; agents key off the kind field instead of guessing from field presence), complete, skip, status, close, reset, iterate, resume, list, route, close-mode (set per-pair CloseDeployMode auto/git-push/manual), git-push-setup (provision GIT_TOKEN/.netrc/remote URL — pass service + remoteUrl), build-integration (wire ZCP-managed CI — pass service + integration), classify, adopt-local, dispatch-brief-atom (retrieve one atom of an envelope-split dispatch brief), record-deploy (stamp FirstDeployedAt for an externally-deployed service — zcli/CI/CD bridge; pass targetService), generate-finalize (recipe-flow generate-step finalization), build-subagent-brief (recipe-flow sub-agent dispatch brief), verify-subagent-dispatch (recipe-flow sub-agent dispatch brief)."`
	Intent      string                     `json:"intent,omitempty"      jsonschema:"User intent description for start action (what you want to accomplish)."`
	Attestation string                     `json:"attestation,omitempty" jsonschema:"Description of what was verified or accomplished (required for complete actions)."`
	Step        string                     `json:"step,omitempty"        jsonschema:"Bootstrap step name for complete/skip actions (discover, provision, close)."`
	SubStep     string                     `json:"substep,omitempty"     jsonschema:"Optional sub-step name for recipe complete action (e.g. scaffold, zerops-yaml, app-code, readme, smoke-test). Completes a sub-step within the current step instead of the full step."`
	Plan        []workflow.BootstrapTarget `json:"plan,omitempty"        jsonschema:"Structured service plan. Submit via action=\"complete\" step=\"discover\" — NOT accepted on action=\"start\" (start commits the route only; the plan is produced during the discover step from route-specific materials and submitted on the next call). Shape: array of {runtime: {devHostname, type, bootstrapMode, stageHostname?, isExisting?}, dependencies: [{hostname, type, mode?, resolution}]}. bootstrapMode is REQUIRED (dev|simple|standard). bootstrapMode and stageHostname MUST nest inside the runtime object — flattened top-level placement is hard-rejected with an actionable diagnostic. Examples: single dev container = [{\"runtime\":{\"devHostname\":\"appdev\",\"type\":\"go@1\",\"bootstrapMode\":\"dev\"}}]; dev/stage pair = [{\"runtime\":{\"devHostname\":\"appdev\",\"stageHostname\":\"appstage\",\"type\":\"go@1\",\"bootstrapMode\":\"standard\"}}]. resolution: CREATE (new service), EXISTS (already in project), SHARED (created by another target in this plan). stageHostname: required for bootstrapMode=standard (no hostname-suffix derivation); explicit per-runtime stage hostname (e.g. devHostname=appdev, stageHostname=appstage)."`
	Reason      string                     `json:"reason,omitempty"      jsonschema:"Reason for skipping a step (skip action). Defaults to 'skipped by user'."`
	SessionID   string                     `json:"sessionId,omitempty"   jsonschema:"Session ID for resume action."`
	CloseModes  map[string]string          `json:"closeMode,omitempty"   jsonschema:"Per-service close-deploy-mode map for action=close-mode (e.g. {\"appdev\":\"git-push\"}). Valid values per service: auto (zcli push direct on develop close), git-push (commit + push to remote on close — requires action=git-push-setup), manual (ZCP yields close orchestration)."`
	Integration string                     `json:"integration,omitempty" jsonschema:"ZCP-managed CI integration value for action=build-integration: 'webhook' (Zerops dashboard OAuth — Zerops pulls + builds on git push), 'actions' (GitHub Actions workflow runs zcli push from CI), or 'none' (no ZCP-managed integration; user may have independent CI/CD that ZCP doesn't track)."`
	RemoteURL   string                     `json:"remoteUrl,omitempty"   jsonschema:"Remote git repository URL for action=git-push-setup confirm step. Passed after the walkthrough atom completes; writes meta.GitPushState=configured + meta.RemoteURL. Omit on the first call to receive the env-aware setup atom."`
	Service     string                     `json:"service,omitempty"     jsonschema:"Single-target runtime service hostname for action=git-push-setup and action=build-integration. Pair-keyed lookup honors stage hostnames per spec-workflows.md §8 E8."`
	Force       FlexBool                   `json:"force,omitempty"       jsonschema:"Discard-and-replace flag for action=start workflow=develop. Required when the active session's services include a CloseDeployMode ∈ {manual, unset} and the new intent differs — auto-close cannot fire on those services, so the prior session needs an explicit close (or a force-discard via this flag) before a fresh session takes over (deploy-decomp P6 §3.4 Scenario D)."`
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

	// TargetService is used by action="adopt-local" to specify which
	// Zerops runtime service should be linked as this local project's
	// stage, by action="record-deploy" to stamp FirstDeployedAt on a
	// specific service's meta, and by workflow="export" (action="start")
	// to identify the runtime service to package into a self-referential
	// single-repo bundle. Resolves the ambiguity that surfaces when
	// multiple runtimes exist in the project.
	TargetService string `json:"targetService,omitempty" jsonschema:"Runtime service hostname. Used by: action=\"adopt-local\" (local env stage link target — must be a live runtime service, not managed); action=\"record-deploy\" (external-deploy ack target — stamps FirstDeployedAt on its ServiceMeta, no-op when meta is missing); workflow=\"export\" (the runtime service to package into a self-referential single-repo bundle — buildFromGit + zerops.yaml + code)."`

	// Variant is used by workflow="export" to select which half of a
	// pair (dev or stage) gets packaged into the export bundle. Only
	// meaningful for ModeStandard / ModeStage / ModeLocalStage — other
	// modes have a single half so the variant is forced. The agent
	// passes "" on the first export call and receives a variant-prompt;
	// the second call carries the chosen variant. Both halves emit
	// `services[].mode: NON_HA` in the rendered import.yaml — the
	// Zerops platform scaling enum is HA/NON_HA only, and ZCP topology
	// (dev/simple/local-only) is established by the destination
	// project's bootstrap on import, not embedded in the bundle (per
	// docs/spec-workflows.md §9 invariant E4 + plan §3.3 revision in
	// Phase 5). The variant tag is preserved on the bundle for
	// downstream consumers.
	Variant string `json:"variant,omitempty" jsonschema:"Export workflow only: which half of a pair to package — 'dev' packages the dev hostname's working tree, 'stage' packages the stage hostname's. Both bundle entries emit Zerops scaling mode=NON_HA; the destination project's topology Mode is established by ZCP's bootstrap on import. Omit on the first export call for ModeStandard / ModeStage / ModeLocalStage to receive the variant-prompt; single-half source modes (dev / simple / local-only) ignore this field."`

	// EnvClassifications carries the per-env user-resolved classification
	// bucket map for workflow="export" Phase B. Empty on the first two
	// export calls (variant prompt + classify prompt); populated on the
	// third call after the user accepts or corrects the per-env review
	// table per plan §3.4. Keys are env var names; values are one of
	// "infrastructure" / "auto-secret" / "external-secret" / "plain-config"
	// (see topology.SecretClassification). Stateless per-request input —
	// no server-side persistence, agent threads it across calls.
	EnvClassifications map[string]string `json:"envClassifications,omitempty" jsonschema:"Export workflow only: per-env classification map for Phase B publish. Keys are env var names; values are one of 'infrastructure' (drops from project.envVariables; keeps ${...} reference in zerops.yaml), 'auto-secret' (emits <@generateRandomString>), 'external-secret' (emits comment + <@pickRandom([\"REPLACE_ME\"])>), 'plain-config' (verbatim literal). Empty on first two export calls; populated on the third (publish) call after the agent surfaces the per-env review table and the user accepts or corrects per plan §3.4."`

	// Cx-SUBAGENT-BRIEF-BUILDER (v38 F-17 close).
	Role        string `json:"role,omitempty"        jsonschema:"Sub-agent role for action=\"build-subagent-brief\" / \"verify-subagent-dispatch\": one of writer, editorial-review, code-review."`
	Description string `json:"description,omitempty" jsonschema:"Task tool description string the main agent is about to submit. Required for action=\"verify-subagent-dispatch\"."`
	Prompt      string `json:"prompt,omitempty"      jsonschema:"Task tool prompt string the main agent is about to submit. Required for action=\"verify-subagent-dispatch\"."`

	// v39 Commit 5a — runtime classify lookup for recipe writer sub-agent.
	// The classification-pointer atom in the writer brief directs the
	// writer here for per-item override cases instead of inlining the full
	// 11KB classification-taxonomy + routing-matrix atoms.
	FactType      string `json:"factType,omitempty"      jsonschema:"Recipe classify action only: the fact's type (one of gotcha_candidate, ig_item_candidate, verified_behavior, platform_observation, fix_applied, cross_codebase_contract) — returned by zerops_record_fact when the writer reads back recorded facts."`
	TitleKeywords string `json:"titleKeywords,omitempty" jsonschema:"Recipe classify action only: space-separated keywords lifted from the fact's title (e.g. 'setGlobalPrefix Controller decorators collision' or 'env-var shadow cross-service'). The classify handler inspects these for framework-quirk / self-inflicted / platform-invariant indicators. Not required; without keywords the handler returns the default route for the type alone."`
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
// sshDeployer enables post-mount git init on each runtime target
// (ops.InitServiceGit). Nil in local env — the post-mount hook skips naturally
// because mounter is also nil there (see autoMountTargets).
func RegisterWorkflow(srv *mcp.Server, client platform.Client, httpClient ops.HTTPDoer, projectID string, cache *ops.StackTypeCache, schemaCache *schema.Cache, engine *workflow.Engine, logFetcher platform.LogFetcher, stateDir, selfHostname string, mounter ops.Mounter, sshDeployer ops.SSHDeployer, rt runtime.Info) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_workflow",
		Description: "Orchestrate Zerops operations. Call with action=\"start\" workflow=\"name\" to begin a tracked session with guidance. Workflows: bootstrap (create/adopt infrastructure only — not the user's application), develop (all development, deployment, fixing, investigating), recipe (create recipe repo files), export (turn a deployed service into a re-importable git repo with import.yaml + buildFromGit). Deploy configuration is split into three orthogonal actions: action=\"close-mode\" closeMode={hostname:value} sets the per-pair CloseDeployMode (auto/git-push/manual); action=\"git-push-setup\" service=hostname remoteUrl=URL provisions GIT_TOKEN/.netrc/remote URL; action=\"build-integration\" service=hostname integration=webhook|actions|none wires the ZCP-managed CI integration. After start: action=\"complete|skip|status\" (step progression), action=\"reset|iterate|resume|list|route|close-mode|git-push-setup|build-integration\".",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Workflow orchestration",
			ReadOnlyHint:   false,
			IdempotentHint: false,
			OpenWorldHint:  boolPtr(false),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input WorkflowInput) (*mcp.CallToolResult, any, error) {
		// New multi-action handler.
		if input.Action != "" {
			return handleWorkflowAction(ctx, projectID, engine, client, httpClient, cache, schemaCache, logFetcher, input, stateDir, selfHostname, mounter, sshDeployer, rt)
		}

		// Immediate workflows (export) may be fetched without action.
		// Orchestrated workflows (bootstrap, develop, recipe) always require
		// a session and must route through action="start".
		if input.Workflow == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"No workflow or action specified",
				`Use action="start" workflow="bootstrap|develop|recipe" for orchestrated workflows, or workflow="export" (with optional targetService=<hostname>) for the export-buildFromGit multi-call flow. Configure deploy via action="close-mode" / action="git-push-setup" / action="build-integration".`), WithRecoveryStatus()), nil, nil
		}
		if !workflow.IsImmediateWorkflow(input.Workflow) {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Workflow %q requires action=\"start\"", input.Workflow),
				fmt.Sprintf(`Use action="start" workflow=%q intent="..."`, input.Workflow)), WithRecoveryStatus()), nil, nil
		}
		// Export is the only immediate workflow today and has handler-side
		// orchestration (probe → generate → publish multi-call narrowing
		// per plan §3.5). Route to handleExport instead of the legacy atom-
		// guidance path. Other immediate workflows fall through to the
		// stateless atom-guidance synthesizer.
		if input.Workflow == workflowExport {
			return handleExport(ctx, projectID, engine, client, input, sshDeployer, stateDir, rt)
		}
		guidance, err := synthesizeImmediateGuidance(input.Workflow, engine, rt)
		if err != nil {
			return convertError(err, WithRecoveryStatus()), nil, nil
		}
		return textResult(guidance), nil, nil
	})
}

func handleWorkflowAction(ctx context.Context, projectID string, engine *workflow.Engine, client platform.Client, httpClient ops.HTTPDoer, cache *ops.StackTypeCache, schemaCache *schema.Cache, logFetcher platform.LogFetcher, input WorkflowInput, stateDir, selfHostname string, mounter ops.Mounter, sshDeployer ops.SSHDeployer, rt runtime.Info) (*mcp.CallToolResult, any, error) {
	// dispatch-brief-atom is a stateless content-retrieval action — it
	// reads an atom from the embedded recipe tree and does not touch
	// session state. Handle it before the engine-required guard so the
	// action works even when a session has not been started (the main
	// agent may retrieve atoms without an active session during debug).
	if input.Action == "dispatch-brief-atom" {
		return handleDispatchBriefAtom(engine, input)
	}
	if input.Action == "build-subagent-brief" {
		return handleBuildSubagentBrief(engine, input)
	}
	if input.Action == "verify-subagent-dispatch" {
		return handleVerifySubagentDispatch(engine, input)
	}
	// record-deploy bridges manual deployers (zcli, CI/CD outside MCP) to
	// MCP-tracked state by stamping FirstDeployedAt on the meta. Workflow-
	// less — runs without an active session because external deployers may
	// have happened before any develop session existed.
	if input.Action == "record-deploy" {
		return handleRecordDeploy(ctx, client, httpClient, projectID, stateDir, input)
	}
	if engine == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			"Workflow engine not initialized",
			"Ensure ZCP is configured with a state directory"), WithRecoveryStatus()), nil, nil
	}

	switch input.Action {
	case "start":
		// Phase 3 — export workflow has handler-based orchestration that
		// MUST run for both invocation shapes (`workflow="export"` no-action
		// AND `action="start" workflow="export"`). Without this fork, the
		// `action="start"` path falls through to `handleStart` and ends up
		// at `synthesizeImmediateGuidance` returning the legacy static atom
		// — split-brain UX flagged by Codex Phase 3 POST-WORK Blocker 2.
		// Phase 4 deletes the static atom; converging both paths here
		// keeps responses coherent in the meantime.
		if input.Workflow == workflowExport {
			return handleExport(ctx, projectID, engine, client, input, sshDeployer, stateDir, rt)
		}
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
				"Use zerops_deploy to deploy, zerops_verify to verify"), WithRecoveryStatus()), nil, nil
		}
		active := detectActiveWorkflow(engine)
		if active == workflowRecipe {
			return handleRecipeComplete(ctx, engine, client, cache, schemaCache, projectID, stateDir, input)
		}
		var liveTypes []platform.ServiceStackType
		if cache != nil && client != nil {
			liveTypes = cache.Get(ctx, client)
		}
		return handleBootstrapComplete(ctx, engine, client, cache, input, liveTypes, logFetcher, projectID, stateDir, mounter, sshDeployer)
	case "generate-finalize":
		if detectActiveWorkflow(engine) == workflowRecipe {
			return handleRecipeGenerateFinalize(engine, input.EnvComments, input.ProjectEnvVariables)
		}
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"generate-finalize is only available during recipe workflow",
			""), WithRecoveryStatus()), nil, nil
	case "skip":
		// Develop is stateless — step-based skipping is never valid.
		if isDevelopStep(input.Step) {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"Deploy steps are handled automatically by zerops_deploy pre-flight validation",
				"Use zerops_deploy to deploy, zerops_verify to verify"), WithRecoveryStatus()), nil, nil
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
		return handleRoute(ctx, engine, client, projectID, stateDir, selfHostname, rt)
	case "close-mode":
		return handleCloseMode(input, stateDir)
	case "git-push-setup":
		return handleGitPushSetup(input, stateDir, rt)
	case "build-integration":
		return handleBuildIntegration(ctx, client, projectID, input, stateDir, rt)
	case "classify":
		// v39 Commit 5a — per-item classify lookup for the recipe writer
		// sub-agent. Replaces the inlined classification-taxonomy +
		// routing-matrix atoms with a runtime response keyed on fact
		// type + title keywords.
		return handleRecipeClassify(input)
	case "adopt-local":
		return handleAdoptLocal(ctx, client, projectID, stateDir, input, rt)
	default:
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Unknown action %q", input.Action),
			"Valid actions: start, complete, close, skip, status, reset, iterate, resume, list, route, close-mode, git-push-setup, build-integration, classify, adopt-local, dispatch-brief-atom, record-deploy, generate-finalize, build-subagent-brief, verify-subagent-dispatch"), WithRecoveryStatus()), nil, nil
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
	// Immediate workflows (export) are stateless — they don't create a
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
			), WithRecoveryStatus()), nil, nil
		}
	}

	// Immediate workflows: stateless, atom-synthesized guidance.
	if workflow.IsImmediateWorkflow(input.Workflow) {
		guidance, err := synthesizeImmediateGuidance(input.Workflow, engine, rt)
		if err != nil {
			return convertError(err, WithRecoveryStatus()), nil, nil
		}
		return jsonResult(immediateResponse{
			Workflow: input.Workflow,
			Guidance: guidance,
		}), nil, nil
	}

	// Bootstrap conductor — discovery + commit split.
	if input.Workflow == workflowBootstrap {
		return handleBootstrapStart(ctx, projectID, engine, client, cache, input, rt)
	}

	// Develop workflow — stateless briefing, no session created.
	if input.Workflow == workflowDevelop {
		return handleDevelopBriefing(ctx, engine, client, projectID, input, rt)
	}

	// Recipe workflow moved to zerops_recipe (v3). v2's recipe sub-mode
	// is no longer reachable through zerops_workflow; an explicit error
	// steers callers to the dedicated tool. See docs/zcprecipator3/plan.md.
	if input.Workflow == workflowRecipe {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"recipe workflow is not available on zerops_workflow",
			"Call zerops_recipe action=start slug=<slug> outputRoot=<dir> instead. See the tool's description for the full action set."), WithRecoveryStatus()), nil, nil
	}

	// Unknown workflow — return error.
	return convertError(platform.NewPlatformError(
		platform.ErrInvalidParameter,
		fmt.Sprintf("Unknown orchestrated workflow %q", input.Workflow),
		"Valid workflows: bootstrap, develop, export. For recipe authoring use zerops_recipe."), WithRecoveryStatus()), nil, nil
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
//
// No `next` hint: per P4/KD-01 the canonical "what next" surface is
// `zerops_workflow action="status"`. Mutation responses stay terse; the
// agent calls status to get the Plan. Pre-fix every mutation handler
// rolled its own `next` string (G11) — drift class.
type resetReport struct {
	Cleared   resetSnapshot `json:"cleared"`
	Preserved resetSnapshot `json:"preserved"`
}

type resetSnapshot struct {
	BootstrapSessionID string   `json:"bootstrapSessionId,omitempty"`
	RecipeSessionID    string   `json:"recipeSessionId,omitempty"`
	CompletedSteps     int      `json:"completedSteps,omitempty"`
	IncompleteMetas    []string `json:"incompleteMetas,omitempty"`
	CompleteMetas      []string `json:"completeMetas,omitempty"`
	OrphanMetas        []string `json:"orphanMetas,omitempty"`
	LiveServices       int      `json:"liveServices,omitempty"`
	WorkSessions       int      `json:"workSessions,omitempty"`
}

func handleReset(ctx context.Context, engine *workflow.Engine, client platform.Client, projectID string) (*mcp.CallToolResult, any, error) {
	// Snapshot state before reset — Reset() clears engine memory + removes
	// the session file + deletes incomplete metas for the session.
	// Complete metas tied to live services are preserved; complete metas
	// whose live counterpart is gone (orphan-meta diff) are cleaned here
	// in the handler since Engine.Reset is intentionally session-scoped
	// and would leave them behind otherwise.
	preState, _ := engine.GetState()
	metasBefore, _ := workflow.ListServiceMetas(engine.StateDir())

	cleared := buildClearedSnapshot(preState, metasBefore)
	preserved := resetSnapshot{}

	// Live services for the orphan diff + preserved counter. Liveness is
	// unknown when client is nil or the API call errors — in that case
	// we explicitly skip orphan pruning rather than treating "no live"
	// as "delete everything".
	var liveServices []platform.ServiceStack
	livenessKnown := false
	if client != nil {
		if live, listErr := ops.ListProjectServices(ctx, client, projectID); listErr == nil {
			liveServices = live
			preserved.LiveServices = len(live)
			livenessKnown = true
		}
	}

	if err := engine.Reset(); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			fmt.Sprintf("Reset failed: %v", err),
			""), WithRecoveryStatus()), nil, nil
	}

	// Orphan-meta cleanup: any meta whose live counterpart is gone is
	// stale. Delete after engine.Reset so we don't race the
	// session-scoped incomplete-meta cleanup. Skipped entirely when
	// liveness is unknown (offline / client-less callers must not
	// trigger destructive cleanup based on missing data).
	if livenessKnown {
		cleared.OrphanMetas = workflow.PruneServiceMetas(engine.StateDir(), liveHostnamesMap(liveServices))
	}

	// Recompute preserved metas after reset + orphan cleanup to catch
	// every removal. Complete metas with live counterparts stay; that's
	// the set the agent can adopt or develop against next.
	metasAfter, _ := workflow.ListServiceMetas(engine.StateDir())
	preserved.CompleteMetas = completeMetaNames(metasAfter)

	report := resetReport{
		Cleared:   cleared,
		Preserved: preserved,
	}
	return jsonResult(report), nil, nil
}

// liveHostnamesMap turns a platform service list into the lookup map
// `workflow.PruneServiceMetas` consumes — a thin adapter that keeps the
// pair-keyed pruning logic in one place (service_meta.go) while the
// tool layer owns the platform-client I/O.
func liveHostnamesMap(live []platform.ServiceStack) map[string]bool {
	out := make(map[string]bool, len(live))
	for _, s := range live {
		out[s.Name] = true
	}
	return out
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
func handleDispatchBriefAtom(engine *workflow.Engine, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if input.AtomID == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"atomId is required for dispatch-brief-atom action",
			"Pass the atomId from the envelope listed in the substep guide's dispatch-brief section"), WithRecoveryStatus()), nil, nil
	}
	// Cx-ENVFOLDERS-WIRED: when an active recipe plan is loadable,
	// render template expressions against plan context before
	// returning. Without this, atoms containing `{{.EnvFolders}}` /
	// `{{.ProjectRoot}}` shipped raw to the writer sub-agent — v36
	// F-9 root cause. Pre-session debug fetches (no plan) fall back
	// to the raw body via RenderContextFromPlan(nil, "").
	var plan *workflow.RecipePlan
	if engine != nil {
		if state := engine.RecipeSession(); state != nil {
			plan = state.Plan
		}
	}
	body, err := workflow.LoadAtomBodyRendered(input.AtomID, workflow.RenderContextFromPlan(plan, ""))
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("dispatch-brief atom %q unknown or unreadable: %v", input.AtomID, err),
			"Check the atomId against the envelope in the current substep guide"), WithRecoveryStatus()), nil, nil
	}
	return jsonResult(map[string]any{
		"atomId": input.AtomID,
		"body":   body,
	}), nil, nil
}

// handleBuildSubagentBrief is the Cx-SUBAGENT-BRIEF-BUILDER entry point
// (v38 F-17 close). Returns the fully-stitched dispatch brief for the
// named role plus its SHA-256 hash; stores the hash in RecipeState so
// a follow-up verify-subagent-dispatch call can compare against it.
// The main agent's contract is to forward the returned prompt to Task
// verbatim — any paraphrase fails the guard with SUBAGENT_MISUSE.
func handleBuildSubagentBrief(engine *workflow.Engine, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if engine == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			"Workflow engine not initialized",
			"Ensure ZCP is configured with a state directory"), WithRecoveryStatus()), nil, nil
	}
	state := engine.RecipeSession()
	if state == nil || state.Plan == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"build-subagent-brief requires an active recipe session with a plan",
			"Call action=start workflow=recipe first, then progress through research+provision+generate+deploy before dispatching sub-agents"), WithRecoveryStatus()), nil, nil
	}
	if strings.TrimSpace(input.Role) == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"role is required for build-subagent-brief action",
			"Pass role=writer|editorial-review|code-review"), WithRecoveryStatus()), nil, nil
	}

	factsLogPath := ""
	if sessionID := engine.SessionID(); sessionID != "" {
		factsLogPath = fmt.Sprintf("/tmp/zcp-facts-%s.jsonl", sessionID)
	}
	manifestPath := ""
	if state.OutputDir != "" {
		manifestPath = state.OutputDir + "/ZCP_CONTENT_MANIFEST.json"
	}

	built, err := workflow.BuildSubagentBrief(state.Plan, input.Role, factsLogPath, manifestPath)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("build-subagent-brief: %v", err),
			"Check role against writer|editorial-review|code-review and confirm an active plan"), WithRecoveryStatus()), nil, nil
	}

	if err := engine.RecordSubagentBrief(built); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("persist subagent brief: %v", err),
			""), WithRecoveryStatus()), nil, nil
	}

	return jsonResult(map[string]any{
		"role":        built.Role,
		"description": built.Description,
		"prompt":      built.Prompt,
		"promptSha":   built.PromptSHA,
		"promptSize":  len(built.Prompt),
		"nextTool":    built.NextTool,
	}), nil, nil
}

// handleVerifySubagentDispatch validates a proposed Task dispatch
// against the last-built brief SHA for the detected role. Used by a
// PreToolUse hook or by the main agent itself as a trust-but-verify
// step before calling Task. Returns `{"allowed": bool, "role": ...,
// "reason": ...}`. A mismatch is surfaced as SUBAGENT_MISUSE so the
// hook can block the dispatch.
func handleVerifySubagentDispatch(engine *workflow.Engine, input WorkflowInput) (*mcp.CallToolResult, any, error) {
	if engine == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			"Workflow engine not initialized",
			""), WithRecoveryStatus()), nil, nil
	}
	state := engine.RecipeSession()
	if strings.TrimSpace(input.Description) == "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"description is required for verify-subagent-dispatch",
			"Pass the Task dispatch description string"), WithRecoveryStatus()), nil, nil
	}
	role, ok, reason := workflow.VerifySubagentDispatch(state, input.Description, input.Prompt)
	if !ok {
		return convertError(platform.NewPlatformError(
			platform.ErrSubagentMisuse,
			reason,
			fmt.Sprintf("Call zerops_workflow action=build-subagent-brief role=%s first, then dispatch via Task with the prompt byte-identical to the returned .prompt field.", role)), WithRecoveryStatus()), nil, nil
	}
	return jsonResult(map[string]any{
		"allowed":     true,
		"role":        role,
		"description": input.Description,
	}), nil, nil
}

func handleIterate(ctx context.Context, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache) (*mcp.CallToolResult, any, error) {
	if _, err := engine.Iterate(); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			fmt.Sprintf("Iterate failed: %v", err),
			"Start a session first"), WithRecoveryStatus()), nil, nil
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
			"Specify the session ID to resume"), WithRecoveryStatus()), nil, nil
	}
	if _, err := engine.Resume(input.SessionID); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			fmt.Sprintf("Resume failed: %v", err),
			"Session may not exist or may still be active"), WithRecoveryStatus()), nil, nil
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
func handleBootstrapStart(ctx context.Context, projectID string, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, input WorkflowInput, rt runtime.Info) (*mcp.CallToolResult, any, error) {
	// Parse the route at the boundary so all downstream comparisons use the
	// typed BootstrapRoute and the engine API takes its native vocabulary.
	route := workflow.BootstrapRoute(input.Route)

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
			"Start commits the route only. The discover step is the reasoning space where the plan is produced from route-specific materials; commit it there."), WithRecoveryStatus()), nil, nil
	}

	// Discovery pass — no route specified, no session committed.
	if route == "" {
		existing, listErr := listExistingServices(ctx, client, projectID)
		if listErr != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrAPIError,
				fmt.Sprintf("Bootstrap discovery failed: %v", listErr),
				"Check project access and try again"), WithRecoveryStatus()), nil, nil
		}
		resp, err := engine.BootstrapDiscover(projectID, input.Intent, existing, rt) //nolint:contextcheck // BootstrapDiscover is synchronous, no I/O to cancel
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrAPIError,
				fmt.Sprintf("Bootstrap discovery failed: %v", err),
				""), WithRecoveryStatus()), nil, nil
		}
		return jsonResult(resp), nil, nil
	}

	// Resume route — dispatch into the existing resume flow.
	if route == workflow.BootstrapRouteResume {
		if input.SessionID == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"route=resume requires sessionId (pick it from the discovery response's resumeSession field)",
				"Call action=start workflow=bootstrap without route first to see resumable sessions"), WithRecoveryStatus()), nil, nil
		}
		return handleResume(ctx, engine, client, cache, input)
	}

	// Commit pass — start a session with the chosen route.
	//
	// Auto-prune orphan ServiceMetas (E3) BEFORE starting the new session.
	// Stale records would otherwise collide with the new bootstrap's hostnames.
	// Skipped when liveness is unknown so an offline/client-less invocation
	// doesn't trigger destructive cleanup based on missing data.
	cleanedOrphans := pruneOrphanMetasBeforeBootstrap(ctx, client, projectID, engine.StateDir())

	resp, err := engine.BootstrapStartWithRoute(projectID, input.Intent, route, input.RecipeSlug)
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrWorkflowActive,
			fmt.Sprintf("Bootstrap start failed: %v", err),
			"Call action=start workflow=bootstrap without route to discover valid options, or action=reset to clear the existing session"), WithRecoveryStatus()), nil, nil
	}
	resp.CleanedUpOrphanMetas = cleanedOrphans
	populateStacks(ctx, resp, client, cache)
	return jsonResult(resp), nil, nil
}

// pruneOrphanMetasBeforeBootstrap deletes ServiceMeta files whose live
// counterpart is gone, returning the sorted list of pruned hostnames so the
// bootstrap-start response can surface the cleanup transparently. Returns
// nil when the platform client is unavailable or the live-services lookup
// fails — destructive cleanup must never run on stale data.
func pruneOrphanMetasBeforeBootstrap(ctx context.Context, client platform.Client, projectID, stateDir string) []string {
	if client == nil || projectID == "" || stateDir == "" {
		return nil
	}
	live, err := ops.ListProjectServices(ctx, client, projectID)
	if err != nil {
		return nil
	}
	return workflow.PruneServiceMetas(stateDir, liveHostnamesMap(live))
}

// listExistingServices is a best-effort wrapper around ops.ListProjectServices
// that tolerates a nil client (test fixtures without platform access).
func listExistingServices(ctx context.Context, client platform.Client, projectID string) ([]platform.ServiceStack, error) {
	if client == nil || projectID == "" {
		return nil, nil
	}
	return ops.ListProjectServices(ctx, client, projectID)
}

func handleListSessions(engine *workflow.Engine) (*mcp.CallToolResult, any, error) {
	sessions, err := engine.ListActiveSessions()
	if err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			fmt.Sprintf("List sessions failed: %v", err),
			""), WithRecoveryStatus()), nil, nil
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
		return convertError(wrapStageErr("Compute envelope", err), WithRecoveryStatus()), nil, nil
	}
	corpus, err := workflow.LoadAtomCorpus()
	if err != nil {
		return convertError(wrapStageErr("Load knowledge atoms", err), WithRecoveryStatus()), nil, nil
	}
	matches, err := workflow.Synthesize(envelope, corpus)
	if err != nil {
		return convertError(wrapStageErr("Synthesize guidance", err), WithRecoveryStatus()), nil, nil
	}
	plan := workflow.BuildPlan(envelope)
	return textResult(workflow.RenderStatus(workflow.Response{
		Envelope: envelope,
		Guidance: workflow.BodiesOf(matches),
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
			""), WithRecoveryStatus()), nil, nil
	}
	pid := os.Getpid()
	stateDir := engine.StateDir()

	_ = workflow.DeleteWorkSession(stateDir, pid)
	_ = workflow.UnregisterSession(stateDir, workflow.WorkSessionID(pid))
	// Terse confirmation: per P4/KD-01 the canonical "what next" surface
	// is `zerops_workflow action="status"`. Pre-fix this returned a
	// hand-rolled hint with a hardcoded `scope=["hostname",...]` literal
	// (G6-class drift) — agent now calls status to get the real Plan
	// against the actual envelope.
	return textResult("Work session closed."), nil, nil
}
