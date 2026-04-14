package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/content"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
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
	Workflow string `json:"workflow,omitempty" jsonschema:"Workflow name: bootstrap, develop, recipe, or cicd."`

	// Multi-action fields.
	Action      string                     `json:"action,omitempty"      jsonschema:"Orchestration action: start, complete, skip, status, reset, iterate, resume, list, route, or generate-finalize (recipe only — generates all 13 recipe files from plan)."`
	Intent      string                     `json:"intent,omitempty"      jsonschema:"User intent description for start action (what you want to accomplish)."`
	Attestation string                     `json:"attestation,omitempty" jsonschema:"Description of what was verified or accomplished (required for complete actions)."`
	Step        string                     `json:"step,omitempty"        jsonschema:"Bootstrap step name for complete/skip actions (e.g. discover, provision, generate, deploy, close)."`
	SubStep     string                     `json:"substep,omitempty"     jsonschema:"Optional sub-step name for recipe complete action (e.g. scaffold, zerops-yaml, app-code, readme, smoke-test). Completes a sub-step within the current step instead of the full step."`
	Plan        []workflow.BootstrapTarget `json:"plan,omitempty"        jsonschema:"Structured service plan: array of {runtime: {devHostname, type, bootstrapMode?, stageHostname?, isExisting?}, dependencies: [{hostname, type, mode?, resolution}]}. resolution: CREATE (new service), EXISTS (already in project), SHARED (created by another target in this plan). stageHostname: explicit stage hostname for standard mode when devHostname doesn't end in 'dev' (e.g. adopting existing services)."`
	Reason      string                     `json:"reason,omitempty"      jsonschema:"Reason for skipping a step (skip action). Defaults to 'skipped by user'."`
	SessionID   string                     `json:"sessionId,omitempty"   jsonschema:"Session ID for resume action."`
	Strategies  map[string]string          `json:"strategies,omitempty"  jsonschema:"Per-service strategy map for strategy action (e.g. {\"appdev\":\"push-git\"})."`
	Tier        string                     `json:"tier,omitempty"        jsonschema:"Recipe tier: minimal or showcase (recipe workflow only)."`
	RecipePlan  *workflow.RecipePlan       `json:"recipePlan,omitempty"  jsonschema:"Structured recipe plan for research step completion."`

	// Recipe workflow only — the agent's self-reported model identifier from its
	// own system prompt. Required at start for the recipe workflow because v13
	// shipped on Sonnet/200k by accident and doubled wall time while regressing
	// close-step severity. The agent must report its EXACT model ID (e.g.
	// "claude-opus-4-6[1m]"), not an alias like "opus".
	ClientModel string `json:"clientModel,omitempty" jsonschema:"Recipe workflow start only: the agent's exact model identifier from its own system prompt (e.g. 'claude-opus-4-6[1m]'). Required — recipe workflow rejects non-Opus models and Opus variants without 1M context."`

	// Recipe comment inputs — passed to generate-finalize to bake agent-authored
	// per-env comments into the 6 import.yaml files, replacing per-file Edit.
	EnvComments map[string]workflow.EnvComments `json:"envComments,omitempty" jsonschema:"Recipe generate-finalize only: per-env comments for all 6 import.yaml files. Keyed by env index as string ('0'..'5'). Each env has {service: {hostname: comment}, project: comment}. Service keys match the hostnames that appear in that env's file — envs 0-1 (dev/stage pair) take 'appdev' and 'appstage'; envs 2-5 take the base hostname 'app'. Each env's commentary should reflect what makes THAT env distinct (AI agent workspace / remote CDE / local validator / stage / small prod with minContainers / HA prod with DEDICATED CPU + corePackage)."`

	// Recipe project-level env var inputs — passed to generate-finalize to bake
	// agent-authored per-env project.envVariables declarations into all 6
	// import.yaml files. Replaces the v5 anti-pattern of hand-editing generated
	// files (which were re-wiped on every generate-finalize re-run).
	ProjectEnvVariables map[string]map[string]string `json:"projectEnvVariables,omitempty" jsonschema:"Recipe generate-finalize only: per-env project-level envVariables for all 6 import.yaml files. Keyed by env index as string ('0'..'5'). Each env value is a flat {name: value} map baked into that env's project.envVariables block. Values may contain ${zeropsSubdomainHost} — the platform preprocessor resolves it at project import time. Different envs typically carry different shapes: envs 0-1 (dev/stage pair) carry DEV_* and STAGE_* URL constants derived from apidev/appdev/apistage/appstage hostnames; envs 2-5 (single-slot) carry STAGE_* only with hostnames api/app. Merge semantics: a non-empty map for an env REPLACES that env's prior map (atomic); an empty map CLEARS; omitting an env leaves it untouched. Refine one env at a time by passing only that env's key."`
}

// immediateResponse is returned from immediate (stateless) workflows.
type immediateResponse struct {
	Workflow string `json:"workflow"`
	Guidance string `json:"guidance"`
}

// RegisterWorkflow registers the zerops_workflow tool.
// selfHostname is the hostname of the service running ZCP (empty when local).
// mounter enables auto-mounting runtime services after provision (nil in local env).
func RegisterWorkflow(srv *mcp.Server, client platform.Client, projectID string, cache *ops.StackTypeCache, schemaCache *schema.Cache, engine *workflow.Engine, logFetcher platform.LogFetcher, stateDir, selfHostname string, mounter ops.Mounter) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_workflow",
		Description: "Orchestrate Zerops operations. Call with action=\"start\" workflow=\"name\" to begin a tracked session with guidance. Workflows: bootstrap (create/adopt infrastructure only — not the user's application), develop (all development, deployment, fixing, investigating), recipe (create recipe repo files), cicd (CI/CD setup). After start: action=\"complete|skip|status\" (step progression), action=\"reset|iterate|resume|list|route\".",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Workflow orchestration",
			ReadOnlyHint:   false,
			IdempotentHint: false,
			OpenWorldHint:  boolPtr(false),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input WorkflowInput) (*mcp.CallToolResult, any, error) {
		// New multi-action handler.
		if input.Action != "" {
			return handleWorkflowAction(ctx, projectID, engine, client, cache, schemaCache, logFetcher, input, stateDir, selfHostname, mounter)
		}

		// Legacy: static workflow guidance.
		if input.Workflow == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"No workflow specified",
				"Use workflow=\"bootstrap\", workflow=\"develop\", or workflow=\"cicd\"")), nil, nil
		}
		wfContent, err := content.GetWorkflow(input.Workflow)
		if err != nil {
			return convertError(err), nil, nil
		}

		// Inject live stack types into bootstrap/develop workflows.
		if (input.Workflow == workflowBootstrap || input.Workflow == workflowDevelop) && client != nil && cache != nil {
			if types := cache.Get(ctx, client); len(types) > 0 {
				stackList := knowledge.FormatStackList(types)
				wfContent = injectStacks(wfContent, stackList)
			}
		}

		return textResult(wfContent), nil, nil
	})
}

func handleWorkflowAction(ctx context.Context, projectID string, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, schemaCache *schema.Cache, logFetcher platform.LogFetcher, input WorkflowInput, stateDir, selfHostname string, mounter ops.Mounter) (*mcp.CallToolResult, any, error) {
	if engine == nil {
		return convertError(platform.NewPlatformError(
			platform.ErrNotImplemented,
			"Workflow engine not initialized",
			"Ensure ZCP is configured with a state directory")), nil, nil
	}

	switch input.Action {
	case "start":
		return handleStart(ctx, projectID, engine, client, cache, input, mounter, selfHostname)
	case "reset":
		return handleReset(engine)
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
		return handleBootstrapStatus(ctx, engine, client, cache)
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
			"Valid actions: start, complete, skip, status, reset, iterate, resume, list, route, strategy")), nil, nil
	}
}

func handleStart(ctx context.Context, projectID string, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, input WorkflowInput, mounter ops.Mounter, selfHostname string) (*mcp.CallToolResult, any, error) {
	// Immediate workflows: stateless, return guidance directly.
	if workflow.IsImmediateWorkflow(input.Workflow) {
		wfContent, err := content.GetWorkflow(input.Workflow)
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Workflow %q not found: %v", input.Workflow, err),
				"Valid workflows: bootstrap, develop, recipe, cicd, export")), nil, nil
		}

		// CI/CD: prepend service context from ServiceMeta (if available).
		if input.Workflow == "cicd" && engine != nil {
			if ctx := buildCICDContext(engine.StateDir()); ctx != "" {
				wfContent = ctx + "\n\n---\n\n" + wfContent
			}
		}

		return jsonResult(immediateResponse{
			Workflow: input.Workflow,
			Guidance: wfContent,
		}), nil, nil
	}

	// Bootstrap conductor.
	if input.Workflow == workflowBootstrap {
		resp, err := engine.BootstrapStart(projectID, input.Intent)
		if err != nil {
			return convertError(platform.NewPlatformError(
				platform.ErrWorkflowActive,
				fmt.Sprintf("Bootstrap start failed: %v", err),
				"Reset existing session first with action=reset")), nil, nil
		}
		populateStacks(ctx, resp, client, cache)
		return jsonResult(resp), nil, nil
	}

	// Develop workflow — stateless briefing, no session created.
	if input.Workflow == workflowDevelop {
		return handleDevelopBriefing(ctx, engine, client, projectID, input, cache, mounter, selfHostname)
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

func handleReset(engine *workflow.Engine) (*mcp.CallToolResult, any, error) {
	if err := engine.Reset(); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			fmt.Sprintf("Reset failed: %v", err),
			"")), nil, nil
	}
	return textResult("Session reset successfully."), nil, nil
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
