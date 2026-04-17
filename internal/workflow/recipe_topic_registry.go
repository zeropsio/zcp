package workflow

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/content"
)

// GuidanceTopic maps a stable topic ID to one or more <block> tags in
// recipe.md. The zerops_guidance tool resolves a topic by extracting and
// concatenating the named blocks, filtered through the plan's predicates.
type GuidanceTopic struct {
	ID          string                 // stable topic ID, matches skeleton [topic: ...] markers
	Step        string                 // which recipe step this topic belongs to
	Description string                 // one-line summary for skeleton references
	Predicate   func(*RecipePlan) bool // nil = always available
	BlockNames  []string               // <block> tags to extract and concatenate

	// EagerAt promotes a topic from "fetch on demand" to "always inlined"
	// at a specific scope. Values:
	//
	//   ""              not eager — agent fetches via zerops_guidance when needed
	//   EagerStepEntry  inlined into the step's detailedGuide at step transition
	//   <SubStep const> inlined into that specific sub-step's focused guide
	//
	// Use sparingly. v13 demonstrated flat fetch-markers are not fetched in
	// priority order — dev-server-hostcheck was offered, ignored, then re-
	// discovered 45 minutes later as a 403 from appdev. Eager closes that
	// gap by landing the block in context whether the agent fetches or not.
	//
	// Scope matters. A topic whose teaching is needed the moment the step
	// begins (e.g. where-commands-run: SSH vs zcp-side execution, applies
	// from the first dev-server start) belongs at EagerStepEntry. A topic
	// whose teaching is only needed at a specific sub-step (e.g. readme-
	// fragments: relevant only when authoring READMEs) belongs at that
	// sub-step — inlining it at step entry forces the agent to hold ~18 KB
	// of template in context for 10+ tool calls before it becomes actionable,
	// and fattens the step-entry envelope past Claude Code's persist-to-disk
	// threshold. v8.84 moved 3 topics from step-entry to sub-step scope
	// after v8.82's deploy-step-entry response crossed 50 KB.
	EagerAt string
}

// EagerAt scope values. Sub-step scope uses the SubStep* constants directly
// from recipe_substeps.go — no new constant needed per sub-step.
const (
	EagerStepEntry = "step-entry"
)

// topicRegistry maps topic IDs to their definitions. Populated at init
// from the per-step topic slices below.
var topicRegistry map[string]*GuidanceTopic

func init() {
	all := make([]*GuidanceTopic, 0, 64)
	all = append(all, recipeGenerateTopics...)
	all = append(all, recipeDeployTopics...)
	all = append(all, recipeFinalizeTopics...)
	all = append(all, recipeCloseTopics...)
	topicRegistry = make(map[string]*GuidanceTopic, len(all))
	for _, t := range all {
		topicRegistry[t.ID] = t
	}
}

// ──────────────────────────────────────────────────────────────────────
// Generate step topics
// ──────────────────────────────────────────────────────────────────────

var recipeGenerateTopics = []*GuidanceTopic{
	{
		ID: "container-state", Step: RecipeStepGenerate,
		Description: "What's available vs unavailable during generate",
		BlockNames:  []string{"container-state"},
	},
	{
		ID: "where-to-write", Step: RecipeStepGenerate,
		Description: "File placement rules for your recipe shape",
		Predicate:   nil, // composeSkeleton filters the marker; ResolveTopic returns whichever block matches
		BlockNames:  []string{"where-to-write-files-single", "where-to-write-files-multi"},
	},
	{
		ID: "recipe-types", Step: RecipeStepGenerate,
		Description: "What to generate per recipe type (showcase)",
		Predicate:   isShowcase,
		BlockNames:  []string{"what-to-generate-showcase"},
	},
	{
		ID: "import-yaml-kinds", Step: RecipeStepGenerate,
		Description: "Workspace vs recipe import.yaml distinction",
		BlockNames:  []string{"two-kinds-of-import-yaml"},
	},
	{
		ID: "execution-order", Step: RecipeStepGenerate,
		Description: "Mandatory write sequence",
		BlockNames:  []string{"execution-order"},
	},
	{
		ID: "zerops-yaml-rules", Step: RecipeStepGenerate,
		Description: "Complete zerops.yaml writing rules",
		BlockNames: []string{
			"zerops-yaml-header", "setup-dev-rules", "setup-prod-rules",
			"shared-across-setups", "generate-schema-pointer",
		},
	},
	{
		ID: "dual-runtime-urls", Step: RecipeStepGenerate,
		Description: "Dual-runtime URL pattern and consumption",
		Predicate:   isDualRuntime,
		BlockNames: []string{
			"dual-runtime-url-shapes", "dual-runtime-consumption",
			"project-env-vars-pointer", "dual-runtime-what-not-to-do",
		},
	},
	{
		ID: "serve-only-dev", Step: RecipeStepGenerate,
		Description: "Dev-base override for serve-only prod targets",
		Predicate:   hasServeOnlyProd,
		BlockNames:  []string{"serve-only-dev-override"},
	},
	{
		ID: "multi-base-dev", Step: RecipeStepGenerate,
		Description: "Secondary runtime dependency preinstall",
		Predicate:   needsMultiBaseGuidance,
		BlockNames:  []string{"dev-dep-preinstall"},
	},
	{
		ID: "dev-server-hostcheck", Step: RecipeStepGenerate,
		Description: "Dev-server host-check allow-list",
		Predicate:   hasBundlerDevServer,
		BlockNames:  []string{"dev-server-host-check"},
		// EagerAt step-entry: v7 documented this gotcha, v8-v12 inlined it
		// via the chain-recipe predecessor for nestjs-minimal recipes, but
		// v13 (a fresh showcase from a runtime where the predecessor lacks
		// the gotcha) re-discovered it as a 403 from appdev 45 minutes into
		// deploy. Eager-injecting at generate step entry means the scaffold
		// sub-agent brief includes the allowedHosts setting regardless of
		// whether the agent fetches the topic explicitly.
		EagerAt: EagerStepEntry,
	},
	{
		ID: "worker-setup", Step: RecipeStepGenerate,
		Description: "Worker setup shape (shared vs separate)",
		Predicate:   hasWorker,
		BlockNames:  []string{"worker-setup-block"},
	},
	{
		ID: "dashboard-skeleton", Step: RecipeStepGenerate,
		Description: "What to write in the skeleton vs what the subagent writes",
		Predicate:   isShowcase,
		BlockNames:  []string{"dashboard-skeleton"},
	},
	{
		ID: "scaffold-subagent-brief", Step: RecipeStepGenerate,
		Description: "Scope contract for scaffold sub-agents (multi-codebase only)",
		Predicate:   func(p *RecipePlan) bool { return isShowcase(p) && hasMultipleCodebases(p) },
		BlockNames:  []string{"scaffold-subagent-brief"},
		// EagerAt step-entry: v13 fetched 10 generate topics but skipped
		// this one and composed scaffold briefs from its own understanding
		// instead. The brief contains the explicit DO-NOT-WRITE list that
		// keeps the scaffold narrow — without it the sub-agents drift into
		// writing item CRUD, cache demos, and search forms at scaffold
		// time, which is the exact v10/v11 contract-mismatch failure mode
		// the bare-baseline design exists to prevent.
		EagerAt: EagerStepEntry,
	},
	{
		ID: "env-conventions", Step: RecipeStepGenerate,
		Description: ".env.example and framework env var naming",
		BlockNames:  []string{"env-example-preservation", "framework-env-conventions"},
	},
	{
		ID: "asset-pipeline", Step: RecipeStepGenerate,
		Description: "Build pipeline / view consistency",
		BlockNames:  []string{"asset-pipeline-consistency"},
	},
	// v14: readme-fragments moved to RecipeStepDeploy — see recipeDeployTopics
	// below. README writing happens at the post-verify `readmes` sub-step so
	// the gotchas section narrates lived debug experience.
	{
		ID: "code-quality", Step: RecipeStepGenerate,
		Description: "Comment ratio, pre-deploy verification",
		BlockNames:  []string{"code-quality", "pre-deploy-checklist"},
	},
	{
		ID: "smoke-test", Step: RecipeStepGenerate,
		Description: "On-container validation before deploy",
		BlockNames:  []string{"on-container-smoke-test"},
	},
	{
		ID: "comment-anti-patterns", Step: RecipeStepGenerate,
		Description: "Comment formatting anti-patterns (separators, decorators)",
		BlockNames:  []string{"comment-anti-patterns"},
	},
	{
		// v8.85 — platform env-var model. Cross-service vars (`${db_hostname}`,
		// `${queue_user}`, etc.) and project-level vars both auto-inject as OS
		// env vars into every container in the project. `run.envVariables` is
		// for mode flags + framework-convention renames only. Re-declaring
		// `varname: ${varname}` self-shadows.
		//
		// Session-log 16 shipped workerdev/zerops.yaml with every cross-service
		// var self-shadowed (db_hostname: ${db_hostname} × 8). Root cause: the
		// agent's received content taught "put cross-service refs in
		// envVariables" without ever stating they auto-inject. This topic is
		// the corrective teaching. EagerAt SubStepZeropsYAML lands it at the
		// exact sub-step where the agent authors zerops.yaml.
		//
		// Full mechanics live in the `environment-variables` knowledge guide;
		// this topic distills the actionable rule set.
		ID: "env-var-model", Step: RecipeStepGenerate,
		Description: "Platform env-var model — auto-inject rules, envVariables legitimate uses, self-shadow trap",
		BlockNames:  []string{"env-var-model"},
		EagerAt:     SubStepZeropsYAML,
	},
}

// ──────────────────────────────────────────────────────────────────────
// Deploy step topics
// ──────────────────────────────────────────────────────────────────────

var recipeDeployTopics = []*GuidanceTopic{
	{
		ID: "deploy-flow", Step: RecipeStepDeploy,
		Description: "Core deploy execution flow",
		BlockNames:  []string{"deploy-framing", "deploy-core-universal"},
	},
	{
		ID: "deploy-api-first", Step: RecipeStepDeploy,
		Description: "API-first deploy ordering and verification",
		Predicate:   isDualRuntime,
		BlockNames:  []string{"deploy-api-first"},
	},
	{
		ID: "deploy-asset-dev-server", Step: RecipeStepDeploy,
		Description: "Asset dev server setup and port collision avoidance",
		Predicate:   hasBundlerDevServer,
		BlockNames:  []string{"deploy-asset-dev-server"},
	},
	{
		ID: "deploy-worker-process", Step: RecipeStepDeploy,
		Description: "Worker process startup (shared vs separate codebase)",
		Predicate:   hasWorker,
		BlockNames:  []string{"deploy-worker-process"},
	},
	{
		ID: "deploy-target-verification", Step: RecipeStepDeploy,
		Description: "Verify all runtime targets by plan shape",
		BlockNames:  []string{"deploy-target-verification"},
	},
	{
		// v8.83 §response-size-fix — feature-sweep-dev focused topic.
		// Previously unmapped in subStepToTopic, which caused the substep
		// guide to fall through to the full ~40 KB deploy monolith on
		// every completion response. The block has existed in recipe.md
		// since v18 (catches content-type-mismatch 200+text/html from the
		// nginx SPA fallback trap) but was never registered as a topic.
		ID: "feature-sweep-dev", Step: RecipeStepDeploy,
		Description: "Dev feature sweep — curl every api-surface feature's healthCheck, require 2xx + application/json",
		BlockNames:  []string{"feature-sweep-dev"},
	},
	{
		// v8.83 §response-size-fix — feature-sweep-stage focused topic.
		// Same fall-through pattern as feature-sweep-dev, for the stage
		// re-verify after cross-deploy.
		ID: "feature-sweep-stage", Step: RecipeStepDeploy,
		Description: "Stage feature sweep — re-curl every api-surface feature against the stage subdomain",
		BlockNames:  []string{"feature-sweep-stage"},
	},
	{
		ID: "subagent-brief", Step: RecipeStepDeploy,
		Description: "Feature sub-agent dispatch and brief",
		Predicate:   isShowcase,
		BlockNames:  []string{"dev-deploy-subagent-brief"},
		// No EagerAt — v8.84 dropped step-entry eager because the
		// `subagent` sub-step's focused guide already serves this topic
		// (subStepToTopic maps SubStepSubagent → "subagent-brief"). The
		// teaching lands in context the moment the agent enters the
		// subagent sub-step, which is immediately before the dispatch —
		// exactly where the NATS-credentials / Valkey-no-auth / S3-
		// apiUrl rules need to be. Step-entry injection pre-loaded the
		// brief 5+ tool calls early and fattened the step-entry envelope
		// past 50 KB (persist-to-disk threshold).
	},
	{
		ID: "where-commands-run", Step: RecipeStepDeploy,
		Description: "SSH vs zcp-side command execution model + zerops_dev_server lifecycle tool",
		BlockNames:  []string{"where-commands-run"},
		// EagerAt step-entry: the dev-server backgrounding pattern is
		// the single biggest operational cost driver across every recipe
		// run (v11: 541s, v15: 556s, v16: 249s — all hand-rolled SSH + `&`
		// calls hitting the 120s bash timeout). The tool-based fix
		// (`zerops_dev_server`) needs to land in context by default, not
		// be discovered after hitting the timeout. This is the ONE deploy
		// topic that legitimately belongs at step entry: the very first
		// substep (deploy-dev / start-processes) starts dev servers over
		// SSH, so the teaching must precede any sub-step work. Every
		// other deploy eager topic was moved to sub-step scope in v8.84.
		EagerAt: EagerStepEntry,
	},
	{
		ID: "browser-walk", Step: RecipeStepDeploy,
		Description: "Browser verification flow",
		Predicate:   isShowcase,
		BlockNames:  []string{"dev-deploy-browser-walk"},
	},
	{
		ID: "browser-commands", Step: RecipeStepDeploy,
		Description: "Browser tool command vocabulary and anti-patterns",
		Predicate:   isShowcase,
		BlockNames:  []string{"browser-command-reference"},
	},
	{
		ID: "deploy-execution-order", Step: RecipeStepDeploy,
		Description: "Deploy step execution order by recipe type",
		BlockNames:  []string{"deploy-execution-order"},
	},
	{
		ID: "stage-deploy", Step: RecipeStepDeploy,
		Description: "Stage cross-deploy flow",
		BlockNames:  []string{"stage-deployment-flow"},
	},
	{
		ID: "deploy-failures", Step: RecipeStepDeploy,
		Description: "Failure diagnosis reference",
		BlockNames:  []string{"reading-deploy-failures", "common-deployment-issues"},
	},
	{
		ID: "writer-subagent-brief", Step: RecipeStepDeploy,
		Description: "Writer sub-agent dispatch brief for README+CLAUDE.md composition (multi-codebase)",
		Predicate:   hasMultipleCodebases,
		BlockNames:  []string{"writer-subagent-brief"},
	},
	{
		ID: "fix-subagent-brief", Step: RecipeStepDeploy,
		Description: "Fix sub-agent brief for scoped check-failure iteration",
		BlockNames:  []string{"fix-subagent-brief"},
	},
	{
		// v8.81 §4.1 — content-fix sub-agent brief. Dispatched on retries
		// of `complete step=deploy` when content-quality checks failed.
		// The gate at engine_recipe.go surfaces a `content_fix_dispatch_required`
		// check; the agent fetches this topic to learn the dispatch shape
		// and file allowlist before running the Agent tool.
		ID: "content-fix-subagent-brief", Step: RecipeStepDeploy,
		Description: "Content-fix sub-agent brief for post-writer README/CLAUDE.md rewrite cycles (v8.81)",
		BlockNames:  []string{"content-fix-subagent-brief"},
	},
	{
		ID: "feature-subagent-mcp-schemas", Step: RecipeStepDeploy,
		Description: "Exact MCP tool parameter names/types for feature sub-agent dispatch",
		Predicate:   isShowcase,
		BlockNames:  []string{"feature-subagent-mcp-schemas"},
	},
	{
		// v8.86 §3.6a — execOnce semantics corrective. Lands eagerly at
		// the init-commands sub-step (where the agent writes & debugs
		// execOnce scripts) so the agent reaches for the correct mental
		// model before the wrong one settles in. Pairs with the
		// claude_md_no_burn_trap_folk content check at deploy-complete:
		// if the agent still ships "burn trap" phrasing in a README or
		// CLAUDE.md, that check fires and blocks the deploy step.
		ID: "execOnce-semantics", Step: RecipeStepDeploy,
		Description: "execOnce is keyed on appVersionId (fresh per deploy) — silent no-op comes from the script, not a burned lock",
		BlockNames:  []string{"execOnce-semantics"},
		EagerAt:     SubStepInitCommands,
	},
	{
		ID: "readme-fragments", Step: RecipeStepDeploy,
		Description: "Per-codebase README structure with extract fragments (post-verify `readmes` sub-step)",
		BlockNames:  []string{"readme-with-fragments"},
		// No EagerAt — v8.84 dropped step-entry eager because the
		// `readmes` sub-step's focused guide already serves this topic
		// (subStepToTopic maps SubStepReadmes → "readme-fragments").
		// The fragment marker template lands in context the moment the
		// agent enters the readmes sub-step — authoring-time, which is
		// exactly when the byte-literal marker format matters. v14's
		// agent-invented-`<!-- FRAGMENT:intro:start -->` failure
		// happened BEFORE the sub-step orchestration layer existed, when
		// the only injection site was step-entry or nothing. With the
		// sub-step focus, step-entry eager is redundant and 18 KB of
		// envelope bloat.
	},
	{
		// v8.82 §4.3 — six-surface teaching system overview. Landed as
		// teaching/coherence content — no check, no enforcement, just a
		// map of where each content fact belongs across zerops.yaml / IG
		// / gotchas / CLAUDE.md / env / root.
		//
		// v8.84 re-scoped from EagerStepEntry → SubStepReadmes. The map
		// is authoring-prep content — it orients the agent across six
		// surfaces before the `readmes` sub-step where most surfaces
		// land. At step entry (5+ tool calls before readmes), it was
		// occupying 4 KB of envelope before the teaching was actionable,
		// and its step-entry residency (alongside subagent-brief + readme-
		// fragments + where-commands-run) pushed deploy step-entry past
		// 50 KB. At sub-step scope, the map lands right before authorship.
		ID: "content-quality-overview", Step: RecipeStepDeploy,
		Description: "Six-surface teaching system — what goes where, author, step, rubric",
		BlockNames:  []string{"content-quality-overview"},
		EagerAt:     SubStepReadmes,
	},
}

// ──────────────────────────────────────────────────────────────────────
// Finalize step topics
// ──────────────────────────────────────────────────────────────────────

var recipeFinalizeTopics = []*GuidanceTopic{
	{
		ID: "env-comments", Step: RecipeStepFinalize,
		Description: "Comment writing instructions",
		BlockNames:  []string{"env-comment-rules"},
	},
	{
		ID: "env-comments-example", Step: RecipeStepFinalize,
		Description: "Complete env comment YAML example template",
		BlockNames:  []string{"env-comments-example"},
	},
	{
		ID: "showcase-service-keys", Step: RecipeStepFinalize,
		Description: "Service key lists by worker shape",
		Predicate:   isShowcase,
		BlockNames:  []string{"showcase-service-keys"},
	},
	{
		ID: "project-env-vars", Step: RecipeStepFinalize,
		Description: "projectEnvVariables for dual-runtime",
		Predicate:   isDualRuntime,
		BlockNames:  []string{"project-env-vars"},
	},
	{
		ID: "comment-style", Step: RecipeStepFinalize,
		Description: "Writing style reference for env comments",
		BlockNames:  []string{"comment-voice"},
	},
}

// ──────────────────────────────────────────────────────────────────────
// Close step topics
// ──────────────────────────────────────────────────────────────────────

var recipeCloseTopics = []*GuidanceTopic{
	{
		ID: "code-review-agent", Step: RecipeStepClose,
		Description: "Static code review sub-agent brief (FIND-only — structured findings JSON)",
		BlockNames:  []string{"code-review-subagent"},
	},
	{
		// v8.86 §3.4 — critical-fix sub-agent brief. Dispatched at the
		// close.critical-fix sub-step when code-review returned ≥1
		// critical or wrong finding. The sub-agent reads the findings
		// JSON, applies fixes (Edit/Write), commits per codebase,
		// redeploys each affected dev service, runs E2E verification,
		// cross-deploys to stage, and returns a structured verification
		// report. Keeps main agent at orchestration level.
		ID: "close-critical-fix-brief", Step: RecipeStepClose,
		Description: "Critical-fix sub-agent brief — applies fixes + redeploys + reverifies when code-review surfaces critical/wrong findings",
		Predicate:   isShowcase,
		BlockNames:  []string{"close-critical-fix-subagent"},
	},
	{
		ID: "close-browser-walk", Step: RecipeStepClose,
		Description: "Post-review browser verification",
		Predicate:   isShowcase,
		BlockNames:  []string{"close-browser-walk"},
	},
	{
		ID: "export-publish", Step: RecipeStepClose,
		Description: "Export and publish pipeline",
		BlockNames:  []string{"export-publish"},
	},
}

// ──────────────────────────────────────────────────────────────────────
// Resolution
// ──────────────────────────────────────────────────────────────────────

// stepToSectionName maps a recipe step constant to the <section name="...">
// tag in recipe.md.
// stepToSectionName maps a recipe step constant to the <section name="...">
// tag in recipe.md. Step constants happen to match section names exactly.
func stepToSectionName(step string) string {
	return step
}

// ResolveTopic returns the guidance content for a topic, filtered by plan.
// If the topic's predicate is false, returns an empty string (topic does not
// apply to this plan shape). For compound topics (multiple BlockNames), the
// blocks are concatenated with double newlines.
//
// The special "where-to-write" topic returns whichever of its two blocks
// matches the plan's codebase shape — it is the only topic with a
// multi-block OR semantic (vs AND for compound topics like zerops-yaml-rules).
func ResolveTopic(topicID string, plan *RecipePlan) (string, error) {
	topic, ok := topicRegistry[topicID]
	if !ok {
		return "", fmt.Errorf("unknown guidance topic %q", topicID)
	}
	if topic.Predicate != nil && !topic.Predicate(plan) {
		return "", nil // topic doesn't apply to this plan shape
	}

	md, err := content.GetWorkflow("recipe")
	if err != nil {
		return "", fmt.Errorf("load recipe.md: %w", err)
	}

	sectionName := stepToSectionName(topic.Step)
	sectionBody := ExtractSection(md, sectionName)
	if sectionBody == "" {
		return "", fmt.Errorf("section %q not found in recipe.md", sectionName)
	}

	blocks := ExtractBlocks(sectionBody)
	byName := make(map[string]string, len(blocks))
	for _, b := range blocks {
		byName[b.Name] = b.Body
	}

	// Special case: where-to-write has OR semantics — return whichever
	// block matches the plan shape.
	if topicID == "where-to-write" {
		if hasMultipleCodebases(plan) {
			if body := byName["where-to-write-files-multi"]; body != "" {
				return body, nil
			}
		}
		if body := byName["where-to-write-files-single"]; body != "" {
			return body, nil
		}
		return "", nil
	}

	var parts []string
	for _, blockName := range topic.BlockNames {
		if body := byName[blockName]; body != "" {
			parts = append(parts, body)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

// InjectEagerTopics returns the inlined content for every Eager topic in the
// given list whose predicate matches the plan. Each topic is rendered as a
// labeled section so the agent can tell which guidance came from which topic
// (and so a future agent can still call zerops_guidance topic="X" if it
// wants the canonical fetch path).
//
// Returns the empty string when no eager topics fire — callers should treat
// that as "skip the divider" rather than emit a stray separator.
//
// Used by buildGuide in recipe_guidance.go to land catastrophic-failure-class
// guidance directly in the step's detailedGuide instead of relying on the
// agent to fetch a flat list of optional topic markers in priority order.
func InjectEagerTopics(topics []*GuidanceTopic, plan *RecipePlan) string {
	return injectEagerTopicsAt(topics, plan, EagerStepEntry, "")
}

// InjectEagerTopicsForSubStep returns topics whose EagerAt matches subStep.
// The excludeID parameter lets the caller skip a topic that's already being
// served as the sub-step's primary focus (via subStepToTopic) so the body
// doesn't double-inline. Pass "" to include everything.
func InjectEagerTopicsForSubStep(topics []*GuidanceTopic, plan *RecipePlan, subStep, excludeID string) string {
	if subStep == "" {
		return ""
	}
	return injectEagerTopicsAt(topics, plan, subStep, excludeID)
}

func injectEagerTopicsAt(topics []*GuidanceTopic, plan *RecipePlan, scope, excludeID string) string {
	var parts []string
	for _, t := range topics {
		if t.EagerAt != scope {
			continue
		}
		if t.ID == excludeID {
			continue
		}
		if t.Predicate != nil && !t.Predicate(plan) {
			continue
		}
		body, err := ResolveTopic(t.ID, plan)
		if err != nil || strings.TrimSpace(body) == "" {
			continue
		}
		header := fmt.Sprintf("## Inlined: %s — `topic=\"%s\"`\n\n", t.Description, t.ID)
		parts = append(parts, header+strings.TrimSpace(body))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// topicExpansion defines a related topic that should be suggested when the
// agent fetches a particular topic.
type topicExpansion struct {
	TopicID   string
	Predicate func(*RecipePlan) bool // nil = always suggest
}

// topicExpansionRules maps a fetched topic to related topics. The expanded
// topics are suggested (not inlined) — the agent decides whether to fetch.
var topicExpansionRules = map[string][]topicExpansion{
	"zerops-yaml-rules": {
		{TopicID: "dual-runtime-urls", Predicate: isDualRuntime},
		{TopicID: "worker-setup", Predicate: hasWorker},
		{TopicID: "comment-anti-patterns"},
	},
	"deploy-flow": {
		{TopicID: "subagent-brief", Predicate: isShowcase},
		{TopicID: "deploy-execution-order"},
	},
	"browser-walk": {
		{TopicID: "browser-commands", Predicate: isShowcase},
	},
	"env-comments": {
		{TopicID: "env-comments-example"},
	},
	"smoke-test": {
		{TopicID: "code-quality"},
	},
}

// ExpandTopic returns related topics the agent should also fetch, filtered
// by predicate and excluding already-accessed topics.
func ExpandTopic(topicID string, plan *RecipePlan, accessed map[string]bool) []*GuidanceTopic {
	expansions, ok := topicExpansionRules[topicID]
	if !ok {
		return nil
	}
	var result []*GuidanceTopic
	for _, exp := range expansions {
		if accessed[exp.TopicID] {
			continue
		}
		if exp.Predicate != nil && !exp.Predicate(plan) {
			continue
		}
		if t := topicRegistry[exp.TopicID]; t != nil {
			result = append(result, t)
		}
	}
	return result
}

// LookupTopic returns the topic definition for a given ID, or nil if not found.
func LookupTopic(topicID string) *GuidanceTopic {
	return topicRegistry[topicID]
}

// AllTopicsForStep returns all topic definitions for a given recipe step.
func AllTopicsForStep(step string) []*GuidanceTopic {
	switch step {
	case RecipeStepGenerate:
		return recipeGenerateTopics
	case RecipeStepDeploy:
		return recipeDeployTopics
	case RecipeStepFinalize:
		return recipeFinalizeTopics
	case RecipeStepClose:
		return recipeCloseTopics
	default:
		return nil
	}
}
