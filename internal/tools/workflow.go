// Package tools — workflow dispatcher entry point.
//
// inline string literals across this file because they are pinned at
// AST level by TestAtomLintAcceptedActionsMatchDispatcher,
// TestAtomLintAcceptedStrategiesMatchGate, and
// TestAtomLintAcceptedWorkflowsMatchDispatcher. Promoting to constants
// would break the pin since those tests inspect *ast.BasicLit nodes. goconst
// is excluded for this file in .golangci.yaml for the same reason.
package tools

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
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
	// workflowRecipe survives only as a redirect token: the retired v2
	// recipe sub-mode is rejected with routing to bootstrap (consumption)
	// and the ZCP_AUTHORING gate (authoring). Installed agents may still
	// carry workflow="recipe" references in their project CLAUDE.md.
	workflowRecipe = "recipe"
	workflowExport = "export"
	// workflowLaunchProduction routes through handleLaunchProduction (Phase D)
	// for the dev/stage → prod transition. Stateless read-side narrowing
	// (scope-prompt / classify-prompt / ready-to-launch) plus a mutation
	// pipeline (launching / failed / launched) gated by a user-supplied
	// one-shot LaunchKey.
	workflowLaunchProduction = "launch-production"
	// actionGitPushSetup names the chained recovery action shared by the
	// launch gate blockers + credentialsRequired asks.
	actionGitPushSetup = "git-push-setup"
)

// WorkflowInput is the input type for zerops_workflow.
type WorkflowInput struct {
	// Legacy: workflow name for static guidance (backward compat).
	Workflow string `json:"workflow,omitempty" jsonschema:"Workflow name: bootstrap, develop, export, or launch-production."`

	// Multi-action fields.
	Action      string                     `json:"action,omitempty"      jsonschema:"Orchestration action: start (workflow=bootstrap is two-phase: first call without route returns kind=\"route-menu\" with ranked options, second call with route=<chosen> commits the session and returns kind=\"session-active\"; agents key off the kind field instead of guessing from field presence), complete, skip, status, close, reset, iterate, resume, list, route, close-mode (set per-pair CloseDeployMode auto/git-push/manual), git-push-setup (verify + configure git-push capability — pass service + remoteUrl + gitToken in container mode; handler probes auth BEFORE writing project state), build-integration (wire ZCP-managed CI — pass service + integration), adopt-local, set-default-setup (write the target service's PrimarySetupName/StageSetupName — resolves requiresSetupInput blockers; pass targetService + setup), record-deploy (stamp FirstDeployedAt for an externally-deployed service — zcli/CI/CD bridge; pass targetService), release (source-side release act: verifies clean tree + pushed HEAD, suggests the next vX.Y.Z from the remote tags, then tags + pushes — the tag fires the production pipeline; pass service, then re-call with releaseVersion after the user confirms)."`
	Intent      string                     `json:"intent,omitempty"      jsonschema:"User intent description for start action (what you want to accomplish)."`
	Attestation string                     `json:"attestation,omitempty" jsonschema:"Description of what was verified or accomplished (required for complete actions)."`
	Step        string                     `json:"step,omitempty"        jsonschema:"Bootstrap step name for complete/skip actions (discover, provision, close)."`
	Plan        []workflow.BootstrapTarget `json:"plan,omitempty"        jsonschema:"Structured service plan. Submit via action=\"complete\" step=\"discover\" — NOT accepted on action=\"start\" (start commits the route only; the plan is produced during the discover step from route-specific materials and submitted on the next call). route=adopt: OMIT plan and pass scope=[\"hostname\",...] to derive from exactly those live adoptable services; submit a plan only to override the derived shape (e.g. adopt a dev/stage pair as bootstrapMode=standard). route=recipe: OMIT plan to accept the recipe's derived shape; submit a plan ONLY to rename a colliding runtime hostname or flip a managed dependency to resolution=EXISTS — type, mode and pairing are the recipe's and are derived, not authored. Shape: array of {runtime: {devHostname, type, bootstrapMode, stageHostname?, isExisting?}, dependencies: [{hostname, type, mode?, resolution}]}. bootstrapMode is REQUIRED (dev|simple|standard). bootstrapMode and stageHostname MUST nest inside the runtime object — flattened top-level placement is hard-rejected with an actionable diagnostic. resolution: CREATE (new service), EXISTS (already in project), SHARED (created by another target in this plan). stageHostname: required for bootstrapMode=standard (no hostname-suffix derivation); explicit per-runtime stage hostname (e.g. devHostname=appdev, stageHostname=appstage)."`
	Reason      string                     `json:"reason,omitempty"      jsonschema:"Reason for skipping a step (skip action). Defaults to 'skipped by user'."`
	SessionID   string                     `json:"sessionId,omitempty"   jsonschema:"Session ID for resume action."`
	CloseModes  map[string]string          `json:"closeMode,omitempty"   jsonschema:"Per-service close-deploy-mode map for action=close-mode (e.g. {\"appdev\":\"git-push\"}). Valid values per service: auto (zcli push direct on develop close), git-push (commit + push to remote on close — requires action=git-push-setup), manual (ZCP yields close orchestration)."`
	Integration string                     `json:"integration,omitempty" jsonschema:"ZCP-managed CI integration value for action=build-integration: 'webhook' (Zerops dashboard OAuth — Zerops pulls + builds on git push), 'actions' (GitHub Actions workflow runs zcli push from CI), or 'none' (no ZCP-managed integration; user may have independent CI/CD that ZCP doesn't track)."`
	RemoteURL   string                     `json:"remoteUrl,omitempty"   jsonschema:"Remote git repository URL for action=git-push-setup confirm step. Passed after the walkthrough atom completes; HTTPS only in container env (https://github.com/owner/repo). Omit on first call to receive env-aware setup atom."`
	Service     string                     `json:"service,omitempty"     jsonschema:"Single-target runtime service hostname for action=git-push-setup and action=build-integration. Pair-keyed lookup honors stage hostnames (one ServiceMeta per dev/stage pair, indexed by either hostname)."`
	GitToken    string                     `json:"gitToken,omitempty"    jsonschema:"Fine-grained PAT for action=git-push-setup confirm step (container env only). Required when remoteUrl is set in container mode. Handler probes the token against the remote BEFORE writing the service-scope secret or restarting — failed probe leaves project state untouched. Never echoed back in any response or state file."`
	Force       FlexBool                   `json:"force,omitempty"       jsonschema:"Discard-and-replace flag for action=start workflow=develop. Required when the active session's services include a CloseDeployMode ∈ {manual, unset} and the new intent differs — auto-close cannot fire on those services, so the prior session needs an explicit close (or a force-discard via this flag) before a fresh session takes over (deploy-decomp P6 §3.4 Scenario D)."`

	// Bootstrap route selection. The first call to action=start workflow=bootstrap
	// omits these — the engine returns a ranked list of route options. The LLM
	// picks one and calls start again with route set.
	Route      string `json:"route,omitempty"      jsonschema:"Bootstrap route: adopt, recipe, classic, or resume. Omit on first start call to receive ranked route options; set on second call to commit the chosen route."`
	RecipeSlug string `json:"recipeSlug,omitempty" jsonschema:"Recipe slug when route=recipe (pick one from the discovery response's routeOptions[].recipeSlug)."`

	// RecipeNarrow opts a standard recipe into a dev-only provision at
	// action="complete" step="discover" (route=recipe). Set ONLY when the user
	// explicitly asked for dev only — never as a default. See RecipeNarrowDevOnly.
	RecipeNarrow string `json:"recipeNarrow,omitempty" jsonschema:"Recipe route only (action='complete' step='discover'): set to 'dev-only' to narrow a STANDARD recipe to a dev-only provision — the dev container + managed deps are created and the paid stage is skipped (PlanModeDev, no promote target). Use ONLY when the user explicitly asked for dev only; omit to provision the recipe's full shape. Rejected (with the reason) for recipes that are already simple/dev or have no single standard pair."`

	// Develop/adopt workflow scope. For develop start, the runtime service
	// hostnames this task works on. For bootstrap route=adopt complete
	// step=discover with an omitted plan, the runtime service hostnames to
	// adopt; empty scope lists candidates and commits nothing.
	Scope []string `json:"scope,omitempty" jsonschema:"Runtime service hostnames in scope. Required for action='start' workflow='develop' and for action='complete' step='discover' route=adopt when plan is omitted. Develop: fixed at start; auto-close requires every REQUIRED hostname in scope to have a successful deploy and passed verify. Adopt: derive from exactly these live adoptable hostnames; empty scope lists candidates and commits nothing. Example: [\"appdev\",\"appstage\"]. Reject managed services — only deployable runtime hostnames."`

	// OutOfScope marks declared services that must NOT block this session's
	// auto-close (RC-B). Use when the user scoped the task to part of a
	// standard pair — e.g. "redesign the dev homepage, leave staging as it is"
	// → outOfScope=["appstage"]. The standard-pair stage half is auto-included
	// in scope by default (so the common promote-to-stage flow still completes
	// only when stage is deployed); listing it here flips it to a visible,
	// non-blocking reminder so the session can auto-close on the dev half alone.
	// At least one declared service must remain required.
	OutOfScope []string `json:"outOfScope,omitempty" jsonschema:"Hostnames to exclude from this session's auto-close requirement (action='start' workflow='develop'). For dev-only work on a standard pair where the user said leave staging untouched: list just the dev half in scope (scope=[\"appdev\"]) and the stage half here (outOfScope=[\"appstage\"]) — the stage half is auto-included into scope, so you do NOT need to repeat it in scope. Excluded services stay visible as non-blocking reminders. At least one service must remain required."`

	// TargetService is used by action="adopt-local" to specify which
	// Zerops runtime service should be linked as this local project's
	// stage, by action="record-deploy" to stamp FirstDeployedAt on a
	// specific service's meta, and by workflow="export" (action="start")
	// to identify the runtime service to package into a self-referential
	// single-repo bundle. Resolves the ambiguity that surfaces when
	// multiple runtimes exist in the project.
	TargetService string `json:"targetService,omitempty" jsonschema:"Runtime service hostname. Used by: action=\"adopt-local\" (local env stage link target — must be a live runtime service, not managed); action=\"record-deploy\" (external-deploy ack target — stamps FirstDeployedAt on its ServiceMeta, no-op when meta is missing); workflow=\"export\" (the runtime service to package into a self-referential single-repo bundle — buildFromGit + zerops.yaml + code); workflow=\"launch-production\" (the runtime service to promote into the new production project — pair-keyed dev-half hostname; passing a stage-half surfaces a scope-prompt blocker)."`

	// Variant is DEPRECATED + IGNORED (accepted only for backward
	// compatibility — the MCP input schema is strict, so removing the
	// field would reject installed agents/recipes that still pass it).
	// The export dev/stage variant dimension was removed: the chosen
	// targetService hostname alone determines which half of a pair is
	// packaged (`appdev` → dev, `appstage` → stage). The handler never
	// reads this field.
	Variant string `json:"variant,omitempty" jsonschema:"Deprecated; ignored, no effect."`

	// EnvClassifications carries the per-env user-resolved classification
	// bucket map for workflow="export" Phase B. Empty on the first
	// export calls (classify prompt); populated on the publish call
	// after the user accepts or corrects the per-env review table per
	// plan §3.4. Keys are env var names; values are one of
	// "infrastructure" / "auto-secret" / "external-secret" /
	// "plain-config" / "exclude" (see topology.SecretClassification).
	// Stateless per-request input — no server-side persistence, agent
	// threads it across calls.
	EnvClassifications map[string]string `json:"envClassifications,omitempty" jsonschema:"Export and launch-production workflows: per-env classification map. Keys are env var names; values are one of 'infrastructure' (drops from project.envVariables; keeps ${...} reference in zerops.yaml), 'auto-secret' (emits <@generateRandomString>), 'external-secret' (emits comment + <@pickRandom([\"REPLACE_ME\"])>), 'plain-config' (verbatim literal), 'exclude' (stale env no longer used by the app — drops entirely, no value and no ${...} reference; verify nothing references it before excluding). Empty on first calls (read-side narrowing); populated on the publish call after the agent surfaces the per-env review table and the user accepts or corrects."`

	// ConfirmDestructive is the diagnose-before-destruct ack used by
	// `action="reset"` for `workflow="launch-production"` (FIX 1 PR 2).
	// The first call without this field returns a `wouldDestroy` payload
	// listing the launch state-file path + target project name; the agent
	// reads the payload, then re-calls with this field populated to match
	// `operation="launch-production-reset"` + `acknowledgedTargets=[<targetProjectName>]`.
	// Same shape as the import-override gate; structure pinned in
	// internal/tools/destructive_ack.go.
	ConfirmDestructive *DestructiveAck `json:"confirmDestructive,omitempty" jsonschema:"Diagnose-before-destruct acknowledgment. Required on the second call after action=\"reset\" workflow=\"launch-production\" (also used by zerops_import override). Set operation to the wouldDestroy.operation echoed in the first-call refusal, and acknowledgedTargets to the same hostname / target-project-name set."`

	// Launch-production workflow (Phase D.1 read-side, D.2 mutation).
	// Per-request inputs threaded across calls — same stateless pattern
	// as export's WorkflowInput.{TargetService, Variant, EnvClassifications}.
	ProductionProjectName string   `json:"productionProjectName,omitempty" jsonschema:"Launch-production only: target project name in Zerops. Must not collide with existing projects in the org. Required for ready-to-launch."`
	Region                string   `json:"region,omitempty"                jsonschema:"Launch-production only: target region code (default 'eu-central'). The scope-prompt response lists every region the platform currently offers (availableRegions, derived from the live import schema)."`
	ProdOperation         string   `json:"prodOperation,omitempty"         jsonschema:"Bring-up management operation for action=prod-ops: which operation to run on the launched production project. One of: status, logs, env-keys, restart, stop, start, scale (container range via runtimeScaling={host:{minContainers,maxContainers}}), delete-service. Every call also needs productionProjectName; the launch-window token is read from the staged ZCP_LAUNCH_TOKEN secret on the source push service (pass launchKey only as fallback when the staged secret is gone)."`
	CorePackage           string   `json:"corePackage,omitempty"           jsonschema:"Launch-production only: production project core tier. SERIOUS (dedicated core) is the default and recommendation for production; LIGHT (shared core) is an allowed cheaper choice — the readiness check surfaces a recommendation, never a block."`
	KeepNonHA             []string `json:"keepNonHa,omitempty"             jsonschema:"Launch-production only: managed-service hostnames to keep at NON_HA in production (default behavior promotes all managed deps to HA). Use for cost optimization or per-service constraints."`
	// LaunchKey is the one-shot Zerops API token with project-creation
	// permission used during the mutation window. Generated by the user
	// in Zerops dashboard (Settings → Access Tokens Management → Custom
	// access per project + Allow creating projects toggle ON), passed on
	// the publish call only, and discarded after launched/failed status.
	// P-LP-1 is enforced at the OUTPUT boundary — no field on
	// launchProductionResponse, launchState, or launchAuditEntry carries
	// this value, and the auth-failure error wrapper never echoes it.
	// The input tag accepts the field so the mcp-go schema exposes it
	// (without this, the agent gets "unexpected additional properties
	// [launchKey]" on every call).
	LaunchKey string `json:"launchKey,omitempty" jsonschema:"Launch-production publish only: the Zerops integration token with project-creation permission, passed ONCE on the mutation call (ready-to-launch → launching). Generated by the user in the Zerops dashboard (Settings → Access Tokens Management → select 'Custom access per project' + turn ON the 'Allow creating projects' toggle). The mutation stages it as the ZCP_LAUNCH_TOKEN service secret on the source push service; every later launch-window call (prod-ops, pipeline resume, reset, confirm-production) reads the staged secret — do NOT re-send the value. Re-pass it only as fallback when the staged secret is gone. ZCP holds it in memory per invocation — never persisted to state, audit log, or response payloads."`
	// ExistingProjectID + ExistingProdToken together drive the launch-
	// production existing-project mutation path (plans/workflow-family-
	// architecture-2026-05-14.md §6.2 + §6.6). When both set, the
	// workflow imports services into an existing target project instead
	// of creating a fresh one — Phase 6b will add the Q1 method prompt
	// that emits these fields explicitly; Phase 2c plumbing accepts
	// them already.
	ExistingProjectID string `json:"existingProjectId,omitempty" jsonschema:"Launch-production existing-project path: ID of the pre-existing target project. Pairs with existingProdToken (the project-scoped Zerops token for that project). Mutually exclusive with launchKey (new-project path)."`
	// ExistingProdToken — same credential-handling rules as LaunchKey:
	// P-LP-1 invariant (never persisted to state/response/audit log).
	// Pinned by sentinel scans + TestExistingProdToken_NeverInResponse.
	ExistingProdToken string `json:"existingProdToken,omitempty" jsonschema:"Launch-production existing-project path: project-scoped Zerops API token for the existing target project. Generated by the user in that project's dashboard (Settings → Access Tokens Management). MUST be scoped to exactly one project matching existingProjectId — ZCP validates scope before mutation. Held in memory for the workflow invocation only; never persisted to state, audit log, or response payloads. Mutually exclusive with launchKey."`
	// SkipPipelineSetup is the launch-production v1 escape hatch: when
	// true, the handler skips the configuring-pipeline status check and
	// proceeds directly to launched. Use when the user explicitly does
	// not want ongoing CD configured for the new prod project (manual
	// `zcli push` workflow, or pre-existing integration the user wants
	// preserved). See plans/production-lifecycle-part2-2026-05-12.md §5.5.
	SkipPipelineSetup FlexBool `json:"skipPipelineSetup,omitempty" jsonschema:"Launch-production only: skip configuring-pipeline status and proceed straight to launched. Use when ongoing CD setup is not wanted (manual zcli push only)."`
	// ConfirmFunctional is the explicit user ack for
	// action="confirm-production": the production project has been
	// verified fully functional (first release live + smoke check done),
	// so the launch window may close — the staged ZCP_LAUNCH_TOKEN
	// secret is deleted and further launch-window operations stop
	// resolving a token.
	ConfirmFunctional FlexBool `json:"confirmFunctional,omitempty" jsonschema:"confirm-production only: explicit user acknowledgment that the production project is verified fully functional (first release live on the prod runtimes + smoke check passed). Required to close the launch window — the close DELETES the staged ZCP_LAUNCH_TOKEN secret, after which CI/CD fixes and reset-with-delete need a re-supplied token. Ask the user before setting true."`
	// SkipStageRecommendation acks the no-stage consent question on the
	// launch scope-prompt (proceed with direct promotion).
	SkipStageRecommendation FlexBool `json:"skipStageRecommendation,omitempty" jsonschema:"Launch-production only: acknowledge the stage-first recommendation and proceed with direct promotion of a no-stage runtime. Set after the user explicitly declines creating a stage half."`
	// ManagedDeps records the per-dependency include/exclude decisions
	// for the production bundle (gap plan P2.0 — the 'jen weather' case:
	// unreferenced managed services must be excludable).
	ManagedDeps map[string]string `json:"managedDeps,omitempty" jsonschema:"Launch-production only: per-managed-dependency decision map {hostname: include|exclude}. The scope response lists the source's managed deps; the ready-to-launch preview marks each as referenced=true/false and recommends excluding unreferenced ones — confirm with the user. Omitted deps default to include."`
	// RuntimeScaling records the consented production container counts
	// per promoted runtime (gap plan P2.1 — HA consent: 2 recommended,
	// 1 allowed with explicit consent, more for load).
	RuntimeScaling map[string]launchRuntimeScaling `json:"runtimeScaling,omitempty" jsonschema:"Launch-production only: per-runtime consented container counts {hostname:{minContainers,maxContainers}}. Default (no entry) applies the production HA floor of 2. minContainers=1 is accepted as an explicit consent (cheaper, no failover — confirm with the user); the readiness rubric reports it as a warn, never a block."`
	// ReleaseVersion confirms the version for action="release" (the §7
	// source-side release act): vMAJOR.MINOR.PATCH, tagged at the
	// verified pushed HEAD. Empty → the handler returns release-prompt
	// with the suggested next version derived from the remote's tags.
	ReleaseVersion string `json:"releaseVersion,omitempty" jsonschema:"Release action only: the version tag to create (vMAJOR.MINOR.PATCH). Omit to get a release-prompt with the suggested next version; confirm with the user before re-calling with the value."`
	// PipelineTagRegex overrides the default tag-trigger regex
	// (^v\\d+\\.\\d+\\.\\d+$, the Zerops-documented production
	// recommendation). Surface only — the value is embedded in the
	// recommendation payload of the not-configured blocker so the agent
	// can echo it to the user when guiding dashboard setup. ZCP itself
	// never PUTs to the platform's integration endpoint in v1 (Path B).
	PipelineTagRegex string `json:"pipelineTagRegex,omitempty" jsonschema:"Launch-production only: tag-trigger regex to recommend when guiding dashboard setup (default '^v\\d+\\.\\d+\\.\\d+$' per Zerops production-checklist)."`
	// ProdSetupNameOverride lets the agent point the launch composer at a
	// setup block named something other than "prod". Surfaces when the
	// source zerops.yaml uses generic-named blocks (e.g. `setup: app`,
	// `setup: appprod`, `setup: web`) instead of the canonical "prod".
	// Without this knob the source-control gate refuses every launch
	// attempt against a non-canonically-named source. Plumbs into
	// ops.LaunchBundleInputs.SetupName as well.
	ProdSetupNameOverride string `json:"prodSetupNameOverride,omitempty" jsonschema:"Launch-production only: override the source zerops.yaml setup name used as the production reference. Default 'prod' — set when the source uses a different name (e.g. 'app', 'appprod', 'web'). Source-control gate uses this name and the launch composer references it as the runtime's setup block."`

	// SkipBuildIntegration is the per-hostname acknowledgement for
	// build-integration-recommended warn-blockers raised by the
	// source-control gate. List the hostnames whose missing
	// `meta.BuildIntegration` the user has explicitly chosen NOT to
	// configure before promotion. Each subsequent gate evaluation
	// suppresses the warn for those hostnames so the workflow can
	// advance to classify-prompt. Hostnames not listed continue to
	// surface the warn until either configured or acknowledged.
	SkipBuildIntegration []string `json:"skipBuildIntegration,omitempty" jsonschema:"Launch-production only: list of source hostnames whose missing build-integration the user explicitly opted out of configuring before launch. Ack mechanism for the source-control gate's build-integration-recommended warn-blocker. Each listed hostname suppresses that warn on subsequent re-calls; the launch advances to classify-prompt once every other source-control check is green."`

	// Promotables names the multi-runtime list of services the launch
	// composer promotes into the production project. Each entry's
	// Hostname accepts either the dev OR stage half of a standard
	// pair (the handler normalizes to the canonical dev-half via the
	// pair-keyed ServiceMeta). When empty, the handler falls back to
	// single-runtime promotion derived from TargetService (legacy /
	// single-promotable path). Composer emits one runtime services[]
	// entry per Promotables entry and deduplicates managed deps so
	// shared infra lands once.
	//
	// Production runtime hostnames are derived from each promotable's
	// dev-half hostname (`appdev` / `appstage` → `app`, `workerstage`
	// → `worker`) unless ProdHostname is set on the entry.
	Promotables []LaunchPromotableInput `json:"promotables,omitempty" jsonschema:"Launch-production only: list of runtimes to promote into the production project. Each entry's hostname accepts either the dev or stage half of a standard pair (handler normalizes). Composer emits one runtime entry per promotable + dedupes shared managed deps. Empty list falls back to single-runtime promotion via targetService."`

	// MergeStrategy is the per-prod-hostname acknowledgement of how
	// to resolve conflicts with services already present in an
	// existing target project (existing-project launch path). Keys
	// are prod-side hostnames (after derivation); values are
	// "skip" (additive launch — do not promote this entry) or
	// "replace" (overwrite existing target service — REQUIRES the
	// ConfirmDestructive ack per diagnose-before-destruct invariant).
	// When the existing-project gate detects conflicts, it surfaces
	// `existing-project-conflict-prompt`; the agent asks the user
	// per conflict + re-calls with this map populated.
	MergeStrategy map[string]string `json:"mergeStrategy,omitempty" jsonschema:"Launch-production existing-project only: per-prod-hostname conflict resolution. Keys are prod-side hostnames; values are 'skip' (additive — drop from bundle) or 'replace' (overwrite existing service in target — requires confirmDestructive ack). Populated on re-call after the existing-project-conflict-prompt response."`

	// Setup is the canonical zerops.yaml setup-block name for the
	// targetService runtime. Used by action="set-default-setup" to
	// resolve the requiresSetupInput / staleMetaSetup blocker by
	// writing ServiceMeta.PrimarySetupName explicitly. Validated
	// against any available zerops.yaml (workingDir for local mode,
	// per-service SSHFS mount for container mode) before the write.
	Setup string `json:"setup,omitempty" jsonschema:"Used by action=set-default-setup: canonical zerops.yaml setup-block name to write into the target service's local ServiceMeta as PrimarySetupName. Validated against any readable yaml (workingDir or per-service mount) before write — failure surfaces availableSetups so the agent can re-call with a corrected value."`

	// StageSetup is the canonical zerops.yaml setup-block name for the
	// targetService runtime's stage half (pair shapes only). When
	// supplied, writes ServiceMeta.StageSetupName. Singleton runtimes
	// (PlanModeDev / PlanModeSimple) ignore this field.
	StageSetup string `json:"stageSetup,omitempty" jsonschema:"Used by action=set-default-setup: optional stage-half setup-block name (pair shapes only). Writes ServiceMeta.StageSetupName when supplied. Singletons (dev/simple) ignore this field."`
}

// LaunchPromotableInput names one runtime to include in the launch
// bundle. The shape lets the agent override per-runtime production
// hostname / setup-block name on a per-promotable basis without
// forcing every multi-runtime launch through the same defaults.
type LaunchPromotableInput struct {
	// Hostname is the source-side hostname (dev OR stage half of a
	// pair; handler normalizes to dev-half via ServiceMeta).
	Hostname string `json:"hostname" jsonschema:"Source runtime hostname (dev or stage half of a pair). The handler normalizes to the canonical dev-half via the pair-keyed ServiceMeta lookup."`
	// ProdHostname overrides the derived production runtime name. When
	// empty, the handler derives via deriveProdHostnameFromSource:
	// strip the canonical `-dev` / `-stage` / `-worker` suffix and
	// fall back to the hostname itself.
	ProdHostname string `json:"prodHostname,omitempty" jsonschema:"Optional override for the production-side runtime hostname. Default derived from the source hostname (appdev/appstage → app, workerstage → worker); supply only when the convention does not produce the wanted name."`
	// ProdSetupNameOverride targets a specific zerops.yaml setup block
	// for this runtime in the production bundle. Default falls back to
	// WorkflowInput.ProdSetupNameOverride, then to canonical "prod".
	ProdSetupNameOverride string `json:"prodSetupNameOverride,omitempty" jsonschema:"Optional per-runtime override for the zerops.yaml setup block the production runtime references. Default per-input ProdSetupNameOverride (workflow-level), then canonical 'prod'."`
}

// workflowInputSchema derives the published InputSchema for zerops_workflow
// from WorkflowInput, then replaces the two FlexBool fields with the
// oneOf[boolean,string] shape. zerops_workflow is the only FlexBool-carrying
// tool that previously relied on schema inference (no explicit InputSchema):
// inference reflects FlexBool's underlying bool kind and publishes
// type:boolean, which the SDK validates BEFORE UnmarshalJSON — so force="true"
// (a stringified boolean from some agents) was rejected with a non-actionable
// schema error, the exact class FlexBool exists to absorb. Deriving (not
// hand-authoring) keeps the ~30 other fields' descriptions in sync with their
// struct tags. On the practically-impossible inference error we fall back to
// nil (inference); TestWorkflowInputSchema_FlexBoolPublished pins success.
func workflowInputSchema() *jsonschema.Schema {
	s, err := jsonschema.For[WorkflowInput](nil)
	if err != nil || s == nil {
		return nil
	}
	patchFlexBoolProperty(s, "force")
	patchFlexBoolProperty(s, "skipPipelineSetup")
	return s
}

// patchFlexBoolProperty replaces an inferred (type:boolean) property with the
// FlexBool oneOf[boolean,string] schema, preserving the inferred description.
func patchFlexBoolProperty(s *jsonschema.Schema, key string) {
	if s.Properties == nil {
		return
	}
	desc := ""
	if prop, ok := s.Properties[key]; ok && prop != nil {
		desc = prop.Description
	}
	s.Properties[key] = flexBoolSchema(desc)
}

// RegisterWorkflow registers the zerops_workflow tool.
// rt carries the runtime detection (container vs local, self hostname, project
// ID from container env). selfHostname duplicates rt.ServiceName for handlers
// that haven't migrated yet — Phase 7 consolidates on rt.
// mounter enables auto-mounting runtime services after provision (nil in local env).
// sshDeployer enables post-mount git init on each runtime target
// (ops.InitServiceGit). Nil in local env — the post-mount hook skips naturally
// because mounter is also nil there (see autoMountTargets).
func RegisterWorkflow(srv *mcp.Server, client platform.Client, httpClient ops.HTTPDoer, projectID string, schemaCache *schema.Cache, engine *workflow.Engine, logFetcher platform.LogFetcher, stateDir, selfHostname string, mounter ops.Mounter, sshDeployer ops.SSHDeployer, rt runtime.Info, apiHost string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_workflow",
		Description: "Orchestrate Zerops operations. Call with action=\"start\" workflow=\"name\" to begin a tracked session with guidance. Workflows: bootstrap (entry point for ANY new or adopted project, INCLUDING projects starting from a Zerops recipe — pass intent describing your stack and the recipe match surfaces as a route option), develop (all development, deployment, fixing, investigating), export (turn a deployed service into a re-importable git repo with import.yaml + buildFromGit), launch-production (PROMOTE an existing working dev/stage Zerops project to a SEPARATE production Zerops project — bundle composition with HA managed deps + production runtime scaling + tag-trigger CD pipeline guidance + one-shot launchKey trust model; trigger phrases the agent should route here: \"launch production\", \"deploy to prod\", \"promote to production\", \"make a production project\", \"create production environment\", \"transfer to prod\", \"go live\", \"udělej produkční projekt\", \"přesuň to na produkci\", \"nasaď to na prod\" — requires existing source dev/stage, NOT for greenfield-from-scratch which goes through workflow=\"bootstrap\"). Deploy configuration is split into three orthogonal actions: action=\"close-mode\" closeMode={hostname:value} sets the per-pair CloseDeployMode (auto/git-push/manual); action=\"git-push-setup\" service=hostname remoteUrl=URL gitToken=PAT (container) probes auth + writes GIT_TOKEN as a service-scope secret on the push source + restarts it + syncs origin + stamps GitPushState=configured (probe-first: failed probe = NO state mutation); action=\"build-integration\" service=hostname integration=webhook|actions|none wires the ZCP-managed CI integration. After start: action=\"complete|skip|status\" (step progression), action=\"reset|iterate|resume|list|route|close-mode|git-push-setup|build-integration\".",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Workflow orchestration",
			ReadOnlyHint:   false,
			IdempotentHint: false,
			OpenWorldHint:  boolPtr(false),
		},
		InputSchema: workflowInputSchema(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input WorkflowInput) (*mcp.CallToolResult, any, error) {
		// New multi-action handler.
		if input.Action != "" {
			return handleWorkflowAction(ctx, projectID, engine, client, httpClient, schemaCache, logFetcher, input, stateDir, selfHostname, mounter, sshDeployer, rt, apiHost)
		}

		// Immediate workflows (export) may be fetched without action.
		// Orchestrated workflows (bootstrap, develop, recipe) always require
		// a session and must route through action="start".
		if input.Workflow == "" {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"No workflow or action specified",
				`Use action="start" workflow="bootstrap|develop" for orchestrated workflows, or workflow="export" / workflow="launch-production" for the multi-call flows. Configure deploy via action="close-mode" / action="git-push-setup" / action="build-integration".`), WithRecoveryStatus()), nil, nil
		}
		// Export is the only stateless (no-session) workflow and has
		// handler-side orchestration (probe → generate → publish multi-call
		// narrowing). Every other workflow requires action="start".
		if input.Workflow == workflowExport {
			return handleExport(ctx, projectID, engine, client, input, sshDeployer, stateDir, rt)
		}
		// workflow="recipe" without action must not be steered INTO the
		// hard-blocked start combination — reuse the block's routing.
		if input.Workflow == workflowRecipe {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"recipe workflow is not available on zerops_workflow",
				"To deploy FROM an existing recipe, use action=\"start\" workflow=\"bootstrap\" — the route menu surfaces recipe matches. "+
					"Recipe AUTHORING (publishing to the corpus) is maintainer tooling enabled by ZCP_AUTHORING=1."), WithRecoveryStatus()), nil, nil
		}
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Workflow %q requires action=\"start\"", input.Workflow),
			fmt.Sprintf(`Use action="start" workflow=%q intent="..."`, input.Workflow)), WithRecoveryStatus()), nil, nil
	})
}

func handleWorkflowAction(ctx context.Context, projectID string, engine *workflow.Engine, client platform.Client, httpClient ops.HTTPDoer, schemaCache *schema.Cache, logFetcher platform.LogFetcher, input WorkflowInput, stateDir, selfHostname string, mounter ops.Mounter, sshDeployer ops.SSHDeployer, rt runtime.Info, apiHost string) (*mcp.CallToolResult, any, error) {
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
		if input.Workflow == workflowLaunchProduction {
			return handleLaunchProduction(ctx, projectID, client, schemaCache, input, stateDir, rt, sshDeployer, apiHost)
		}
		return handleStart(ctx, projectID, engine, client, schemaCache, input, rt)
	case "reset":
		// Launch-production reset clears the per-launchID state file
		// (.zcp/state/launch-production/<launchID>.json). Separate from
		// the session-scoped handleReset which clears bootstrap /
		// develop / recipe engine state. Diagnose-before-destruct
		// pattern (DiagnosedDestruction + ConfirmDestructive) — same
		// shape as zerops_import override. FIX 1 PR 2.
		if input.Workflow == workflowLaunchProduction {
			return handleLaunchReset(ctx, stateDir, projectID, client, input, apiHost)
		}
		return handleReset(ctx, engine, client, projectID)
	case "iterate":
		return handleIterate(ctx, engine, schemaCache)
	case "complete":
		// Develop is stateless — step-based completion is never valid.
		if isDevelopStep(input.Step) {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"Deploy steps are handled automatically by zerops_deploy pre-flight validation",
				"Use zerops_deploy to deploy, zerops_verify to verify"), WithRecoveryStatus()), nil, nil
		}
		return handleBootstrapComplete(ctx, engine, client, schemaCache, input, logFetcher, projectID, stateDir, mounter, sshDeployer, rt)
	case "skip":
		// Develop is stateless — step-based skipping is never valid.
		if isDevelopStep(input.Step) {
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"Deploy steps are handled automatically by zerops_deploy pre-flight validation",
				"Use zerops_deploy to deploy, zerops_verify to verify"), WithRecoveryStatus()), nil, nil
		}
		return handleBootstrapSkip(ctx, engine, schemaCache, input)
	case "status":
		// SPINE-1 fix: resolve precedence from DISK via the single
		// ResolveLifecycle resolver (not the in-memory detectActiveWorkflow),
		// so the dispatcher and the envelope agree. Focus rule (§5.3/§6.2):
		// infra (bootstrap/recipe) foregrounds an open work session.
		// engine.StateDir() (NOT the stateDir param, which the envelope path also
		// ignores in favor of engine.StateDir()) so the dispatcher's precedence
		// reads the SAME disk the envelope (ComputeEnvelope) does.
		ws, _ := workflow.CurrentWorkSession(engine.StateDir())
		switch workflow.ResolveLifecycle(engine.StateDir(), ws) {
		case workflow.FocusBootstrap:
			// Bootstrap is PRIMARY; the work session (if any) is surfaced as a
			// backgrounded block inside BootstrapResponse so it is not hidden.
			return handleBootstrapStatus(ctx, engine, schemaCache)
		case workflow.FocusWork:
			// Develop is primary. An in-flight launch is a PROJECT OVERLAY,
			// appended inside handleLifecycleStatus (launchOverlayAddendum) — it
			// no longer preempts and hides develop (the old launch-recovery
			// short-circuit ran before handleLifecycleStatus).
			return handleLifecycleStatus(ctx, engine, client, projectID, rt)
		case workflow.FocusIdle: // no infra, no open work: launch recovery may take over.
			// Mid-flight launch-production recovery: a non-terminal state file
			// for this source project → resumable launch envelope. Read-only
			// (P-LP-2: no ProjectAdminClient construction).
			if launchActive, allLaunches, _ := findActiveLaunchState(stateDir, projectID); launchActive != nil {
				corpus, _ := workflow.LoadAtomCorpus()
				return renderLaunchActiveRecovery(corpus, launchActive, allLaunches), nil, nil
			}
			// Terminal launch recovery: most-recent launch ended failed/launched
			// → dedicated envelope so the agent learns it terminated rather than
			// reading idle and retrying blindly.
			if recent, _, _ := findRecentLaunchState(stateDir, projectID); recent != nil && isTerminalLaunchStatus(recent.Status) {
				corpus, _ := workflow.LoadAtomCorpus()
				return renderLaunchTerminalRecovery(corpus, recent), nil, nil
			}
			return handleLifecycleStatus(ctx, engine, client, projectID, rt)
		}
		// Unreachable: ResolveLifecycle returns one of the four Focus values
		// handled above; the compiler can't prove the switch exhaustive.
		return handleLifecycleStatus(ctx, engine, client, projectID, rt)
	case "close":
		return handleWorkSessionClose(engine, input)
	case "resume":
		return handleResume(ctx, engine, schemaCache, input)
	case "list":
		return handleListSessions(engine)
	case "route":
		return handleRoute(ctx, engine, client, projectID, stateDir, selfHostname, rt)
	case "close-mode":
		return handleCloseMode(input, stateDir)
	case "git-push-setup":
		return handleGitPushSetup(ctx, client, sshDeployer, projectID, input, stateDir, rt)
	case "release":
		return handleRelease(ctx, sshDeployer, input, stateDir, rt)
	case "prod-ops":
		return handleLaunchProdOps(ctx, projectID, client, logFetcher, input, stateDir, apiHost)
	case "confirm-production":
		return handleLaunchConfirmProduction(ctx, projectID, client, input, stateDir, apiHost)
	case "build-integration":
		return handleBuildIntegration(ctx, client, sshDeployer, projectID, input, stateDir, rt)
	case "adopt-local":
		return handleAdoptLocal(ctx, client, projectID, stateDir, input, rt)
	case "set-default-setup":
		return handleSetDefaultSetup(ctx, client, projectID, input, stateDir)
	default:
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Unknown action %q", input.Action),
			"Valid actions: start, complete, close, skip, status, reset, iterate, resume, list, route, close-mode, git-push-setup, build-integration, prod-ops, confirm-production, adopt-local, set-default-setup, record-deploy, release"), WithRecoveryStatus()), nil, nil
	}
}

func handleStart(ctx context.Context, projectID string, engine *workflow.Engine, client platform.Client, schemaCache *schema.Cache, input WorkflowInput, rt runtime.Info) (*mcp.CallToolResult, any, error) {
	// v8.90 Fix A: reject action=start when a DIFFERENT workflow is already
	// active. This closes the sub-agent-misuse path: a sub-agent spawned by
	// the main agent inside a running recipe calling action=start
	// workflow=develop should not be told "Run bootstrap first" (the develop
	// handler's prereq-missing message). The main agent owns workflow state;
	// the sub-agent's job is whatever the dispatch brief scoped it to.
	//
	// The stateless workflows (export, launch-production) are forked before
	// handleStart, so any workflow reaching here is session-backed: a
	// different active session blocks starting a new one. Same-workflow
	// re-starts fall through to the workflow-specific handler, which owns
	// idempotency (e.g. handleRecipeStart returning the current state).
	if active := detectActiveWorkflow(engine); active != "" && active != input.Workflow {
		return convertError(platform.NewPlatformError(
			platform.ErrSubagentMisuse,
			fmt.Sprintf(
				"A %q workflow session is already active — cannot start a %q workflow inside it.",
				active, input.Workflow,
			),
			"If you are a sub-agent spawned by the main agent inside an active session, "+
				"do NOT call zerops_workflow. The main agent holds workflow state. "+
				"Perform your scoped task using the tools you were given and return.",
		), WithRecoveryStatus()), nil, nil
	}

	// Bootstrap conductor — discovery + commit split.
	if input.Workflow == workflowBootstrap {
		return handleBootstrapStart(ctx, projectID, engine, client, schemaCache, input, rt)
	}

	// Develop workflow — stateless briefing, no session created.
	if input.Workflow == workflowDevelop {
		return handleDevelopBriefing(ctx, engine, client, projectID, input, rt)
	}

	// The retired v2 recipe sub-mode is not reachable through
	// zerops_workflow. Recipe AUTHORING is maintainer tooling behind the
	// ZCP_AUTHORING gate (docs/spec-authoring-boundary.md); deploying
	// FROM an existing recipe is bootstrap's recipe route.
	if input.Workflow == workflowRecipe {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"recipe workflow is not available on zerops_workflow",
			"To deploy FROM an existing recipe, use action=\"start\" workflow=\"bootstrap\" — the route menu surfaces recipe matches. "+
				"Recipe AUTHORING (publishing to the corpus) is maintainer tooling enabled by ZCP_AUTHORING=1."), WithRecoveryStatus()), nil, nil
	}

	// Unknown workflow — return error.
	return convertError(platform.NewPlatformError(
		platform.ErrInvalidParameter,
		fmt.Sprintf("Unknown orchestrated workflow %q", input.Workflow),
		"Valid workflows: bootstrap, develop, export, launch-production."), WithRecoveryStatus()), nil, nil
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
// bootstrap session plus any incomplete ServiceMetas (those with
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

func handleIterate(ctx context.Context, engine *workflow.Engine, schemaCache *schema.Cache) (*mcp.CallToolResult, any, error) {
	if _, err := engine.Iterate(); err != nil {
		return convertError(platform.NewPlatformError(
			platform.ErrSessionNotFound,
			fmt.Sprintf("Iterate failed: %v", err),
			"Start a session first"), WithRecoveryStatus()), nil, nil
	}
	return bootstrapStatusResult(ctx, engine, schemaCache)
}

func handleResume(ctx context.Context, engine *workflow.Engine, schemaCache *schema.Cache, input WorkflowInput) (*mcp.CallToolResult, any, error) {
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
	return bootstrapStatusResult(ctx, engine, schemaCache)
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
func handleBootstrapStart(ctx context.Context, projectID string, engine *workflow.Engine, client platform.Client, schemaCache *schema.Cache, input WorkflowInput, rt runtime.Info) (*mcp.CallToolResult, any, error) {
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
		return handleResume(ctx, engine, schemaCache, input)
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
	// Attach the catalog only at the discover step (where the agent chooses
	// types). start-with-route lands on discover, so it attaches here; gating
	// via needsStacks keeps the "present once, at the decision point" contract
	// rather than re-dumping on later responses.
	if needsStacks(resp) {
		populateStacks(ctx, resp, schemaCache)
	}
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
//
// Local-mode addendum: when running on a developer machine (rt is not in
// a Zerops container), the lifecycle status also runs the dotenv-freshness
// check and surfaces non-fresh / non-skipped results as a guidance line
// so agents notice when `.env` has drifted from sources without having
// to invoke generate-dotenv preview manually.
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
	// Budget backstop (R1): keep the synthesized guidance under the payload cap
	// by demoting the lowest-relevance atoms to a one-line head rather than
	// letting an oversized live payload hit the MCP transport cap. No-op when the
	// matched set already fits.
	matches = workflow.ComposeUnderBudget(matches, corpus, workflow.ComposeBodyBudget)
	plan := workflow.BuildPlan(envelope)
	guidance := workflow.BodiesOf(matches)
	if !rt.InContainer && projectID != "" {
		if extra := localDotenvGuidanceAddendum(ctx, client, projectID); extra != "" {
			guidance = append(guidance, extra)
		}
	}
	// Launch-production is a PROJECT overlay (plan I10): recoverable by any PID,
	// independent of the work session. Surface it as a uniform element on develop
	// status so an open work session (FocusWork) no longer hides an in-flight or
	// terminal launch — the pre-rebuild dispatch surfaced launch unconditionally.
	if extra := launchOverlayAddendum(engine.StateDir(), projectID); extra != "" {
		guidance = append(guidance, extra)
	}
	return textResult(workflow.RenderStatus(workflow.Response{
		Envelope: envelope,
		Guidance: guidance,
		Plan:     &plan,
	})), nil, nil
}

// localDotenvGuidanceAddendum runs checkLocalDotenvFresh against the
// caller's CWD and returns a guidance-block string when the dotenv is
// non-fresh and non-skipped. Empty string when fresh (or not
// applicable). Errors are swallowed silently — the dotenv check is a
// best-effort UX surface, not a status correctness gate.
func localDotenvGuidanceAddendum(ctx context.Context, client platform.Client, projectID string) string {
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		return ""
	}
	res := checkLocalDotenvFresh(ctx, client, projectID, "", cwd)
	switch res.Status {
	case LocalDotenvFresh, LocalDotenvSkipped:
		return ""
	case LocalDotenvStale, LocalDotenvUnownedEdits, LocalDotenvMissing, LocalDotenvVPNDown:
		return formatLocalDotenvGuidance(res)
	default:
		return ""
	}
}

// formatLocalDotenvGuidance renders a LocalDotenvCheckResult into a
// status-friendly markdown block. Stable phrasing — agents key on the
// "Local .env" heading to attribute the line.
func formatLocalDotenvGuidance(res LocalDotenvCheckResult) string {
	var b strings.Builder
	b.WriteString("### Local .env state — ")
	b.WriteString(string(res.Status))
	b.WriteString("\n\n")
	b.WriteString(res.Detail)
	b.WriteString("\n")
	if res.RecoveryHint != nil {
		b.WriteString("\nRecovery: `")
		b.WriteString(res.RecoveryHint.Tool)
		b.WriteString(" action=\"")
		b.WriteString(res.RecoveryHint.Action)
		b.WriteByte('"')
		for k, v := range res.RecoveryHint.Args {
			b.WriteByte(' ')
			b.WriteString(k)
			b.WriteString("=\"")
			b.WriteString(v)
			b.WriteByte('"')
		}
		b.WriteString("` — ")
		b.WriteString(res.RecoveryHint.Comment)
		b.WriteByte('\n')
	}
	return b.String()
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
