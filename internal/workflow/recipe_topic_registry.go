package workflow

import (
	"fmt"
	"sort"
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

	// Eager promotes a topic from "fetch on demand" to "inlined into the
	// step's detailedGuide". Set true for topics whose content prevents a
	// catastrophic-failure-class bug that the agent will otherwise
	// rediscover during deploy. v13 demonstrated that a flat list of
	// optional fetch markers does not get fetched in priority order — the
	// dev-server-hostcheck topic was offered, ignored, then rediscovered
	// 45 minutes later as a 403 from the appdev subdomain. Eager topics
	// land in context whether the agent fetches them or not.
	//
	// Use sparingly. Eager topics inflate the per-step guidance budget;
	// only mark topics that have repeatedly cost a real recipe run.
	Eager bool

	// IncludePriorDiscoveries — v8.96 §6.3. When true, buildSubStepGuide
	// prepends a "Prior discoveries" block (downstream-scoped facts from
	// upstream subagents) to the resolved topic content. Use for
	// delegation briefs whose subagent would otherwise re-investigate
	// framework / tooling surfaces an upstream subagent already
	// characterized. Default false: the writer subagent reads the facts
	// log directly via the v8.95 manifest contract; scaffold runs first
	// so it has no upstream facts.
	IncludePriorDiscoveries bool
}

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
		// Eager: v7 documented this gotcha, v8-v12 inlined it via the
		// chain-recipe predecessor for nestjs-minimal recipes, but v13
		// (a fresh showcase from a runtime where the predecessor lacks
		// the gotcha) re-discovered it as a 403 from appdev 45 minutes
		// into deploy. Eager-injecting at generate time means the
		// scaffold sub-agent brief includes the allowedHosts setting
		// regardless of whether the agent fetches the topic explicitly.
		Eager: true,
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
		// Eager: v13 fetched 10 generate topics but skipped this one and
		// composed scaffold briefs from its own understanding instead.
		// The brief contains the explicit DO-NOT-WRITE list that keeps
		// the scaffold narrow — without it the sub-agents drift into
		// writing item CRUD, cache demos, and search forms at scaffold
		// time, which is the exact v10/v11 contract-mismatch failure mode
		// the bare-baseline design exists to prevent.
		Eager: true,
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
		ID: "subagent-brief", Step: RecipeStepDeploy,
		Description: "Feature sub-agent dispatch and brief",
		Predicate:   isShowcase,
		BlockNames:  []string{"dev-deploy-subagent-brief"},
		// v8.96 §6.3 — feature subagent benefits from scaffold-recorded
		// framework quirks (Meilisearch SDK shape, cache-manager TTL
		// semantics, svelte-check / typescript compat). When prepended,
		// the brief carries upstream subagents' downstream-scoped facts
		// so the feature subagent doesn't re-archaeology the same APIs.
		IncludePriorDiscoveries: true,
		// NOT eager — v8.90. The brief is delivered via substep-complete:
		// the mapping subStepToTopic(deploy, subagent) == "subagent-brief"
		// means this block lands in the response to `complete substep=
		// init-commands` (the advance into `subagent` is what triggers the
		// delivery). v25 evidence: eager injection placed the brief in
		// context 30+ minutes before the feature-sub-agent dispatch
		// happened; the main agent then dispatched without first calling
		// complete substep, so the substep brief arrived after the work
		// was done. Substep-scoped delivery re-binds the brief to the
		// phase where the delegation actually occurs.
		//
		// The v13 regression that originally motivated eager injection
		// (agent fetched the topic at post-generate then forgot to act on
		// its rules) is addressed by substep-ordered delivery: the brief
		// arrives in the response IMMEDIATELY before dispatch, not 30+
		// minutes earlier.
	},
	{
		ID: "fact-recording-mandatory", Step: RecipeStepDeploy,
		Description: "Mandatory zerops_record_fact usage during deploy — primary input for the content-authoring sub-agent at readmes substep",
		BlockNames:  []string{"fact-recording-mandatory"},
		// Eager — v8.94. This is the prompt-level pressure that makes the
		// content-authoring sub-agent's input (the facts log) substantive.
		// Unlike the two substep-scoped briefs that arrive at a single
		// boundary, fact recording is mandatory from the FIRST deploy
		// substep (deploy-dev) through the LAST — every substep the agent
		// reaches needs the guidance in context. v28 evidence: 3 voluntary
		// calls without this pressure; the authoring path then had to
		// reconstruct events from the run transcript, which caused the
		// content-quality failures v8.94 is addressing. Eager is the right
		// setting for universal, per-substep prompt pressure.
		Eager: true,
	},
	{
		ID: "where-commands-run", Step: RecipeStepDeploy,
		Description: "SSH vs zcp-side command execution model + zerops_dev_server lifecycle tool",
		BlockNames:  []string{"where-commands-run"},
		// Eager: the dev-server backgrounding pattern is the single
		// biggest operational cost driver across every recipe run
		// (v11: 541s, v15: 556s, v16: 249s — all hand-rolled SSH + `&`
		// calls hitting the 120s bash timeout). The tool-based fix
		// (`zerops_dev_server`) needs to land in context by default,
		// not be discovered after hitting the timeout. Promoting this
		// topic to eager inlines the SSH vs dev_server distinction
		// whether the agent fetches it or not.
		//
		// v8.90: subagent-brief and readme-fragments de-eagered;
		// where-commands-run STAYS eager because the SSH/zcp boundary
		// applies from substep=deploy-dev onwards and the
		// zerops_dev_server tool discipline needs to be in context at
		// every substep, not just the one whose mapping returns it.
		Eager: true,
	},
	{
		ID: "feature-sweep-dev", Step: RecipeStepDeploy,
		Description: "Dev feature sweep — curl every api-surface feature's healthCheck, require 2xx + application/json",
		BlockNames:  []string{"feature-sweep-dev"},
	},
	{
		ID: "feature-sweep-stage", Step: RecipeStepDeploy,
		Description: "Stage feature sweep — re-curl every api-surface feature against the stage subdomain",
		BlockNames:  []string{"feature-sweep-stage"},
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
		ID: "readme-fragments", Step: RecipeStepDeploy,
		Description: "Per-codebase README structure with extract fragments (marker format reference)",
		BlockNames:  []string{"readme-with-fragments"},
		// NOT eager. v8.94: the primary `readmes` substep delivery moved to
		// content-authoring-brief (which embeds the surface contracts,
		// classification taxonomy, and citation map). readme-fragments
		// remains a stable on-demand reference for fragment marker format —
		// cited from the content-authoring brief and from checker error
		// messages — so it is NOT remapped onto SubStepReadmes. Agents fetch
		// it via zerops_guidance topic="readme-fragments" when they need the
		// byte-literal marker shape.
	},
	{
		ID: "content-authoring-brief", Step: RecipeStepDeploy,
		Description: "Fresh-context content-authoring sub-agent brief — surface contracts, fact classification, citation map, counter-examples",
		Predicate:   isShowcase,
		BlockNames:  []string{"content-authoring-brief"},
		// NOT eager — v8.94. Delivered via substep-complete:
		// subStepToTopic(deploy, readmes) == "content-authoring-brief" puts
		// this block in the response to `complete substep=feature-sweep-stage`
		// (the advance into `readmes` triggers the delivery). v28 evidence:
		// the main agent that spent 85 min debugging wrote self-narrative
		// gotchas (what confused me) instead of reader-facing content
		// (what will surprise a fresh developer). The new brief replaces the
		// READMEs substep guidance with a surface-contract + classification-
		// taxonomy + citation-map rubric that the sub-agent evaluates every
		// fact against before routing it to a surface. The shape of the
		// delivery matches v8.90's subagent-brief / readme-fragments pattern
		// (NOT eager, substep-scoped) so the brief arrives immediately before
		// dispatch and governs the sub-agent's output.
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
		Description: "Static code review sub-agent brief",
		BlockNames:  []string{"code-review-subagent"},
		// v8.96 §6.3 — code-review benefits from feature/scaffold
		// tooling observations (e.g. svelte-check@4 incompatible with
		// typescript@6) so it doesn't flag a non-bug as a STYLE finding.
		IncludePriorDiscoveries: true,
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
	sectionBody := extractSection(md, sectionName)
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
	var parts []string
	for _, t := range topics {
		if !t.Eager {
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

// AllTopicIDs returns every registered guidance topic ID sorted
// alphabetically. Cx-GUIDANCE-TOPIC-REGISTRY (v35 F-5 close): surfaced
// on the recipe-start response so the main agent has a closed universe
// of valid topic IDs instead of pattern-matching plausible-sounding
// ones from its own reasoning (v35 at 07:29:50-51: three hallucinated
// topic lookups in succession — `dual-runtime-consumption`,
// `client-code-observable-failure`, `init-script-loud-failure`).
func AllTopicIDs() []string {
	ids := make([]string, 0, len(topicRegistry))
	for id := range topicRegistry {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// NearestTopicIDs returns up to k topic IDs closest to query by
// Levenshtein distance, tie-broken lexicographically. Empty query or
// k≤0 returns nil. Used to replace bare "unknown topic" errors with
// actionable suggestions per Cx-GUIDANCE-TOPIC-REGISTRY.
//
// Rationale: edit-distance handles both typos (dual-runtime-consumtion
// → dual-runtime-consumption) and near-synonyms (init-script → on-
// container-smoke-test) well enough for triage. Full embedding-based
// similarity is overkill for a < 100-topic registry.
func NearestTopicIDs(query string, k int) []string {
	if query == "" || k <= 0 {
		return nil
	}
	type scored struct {
		id       string
		distance int
	}
	ids := AllTopicIDs()
	scoredIDs := make([]scored, 0, len(ids))
	for _, id := range ids {
		scoredIDs = append(scoredIDs, scored{id: id, distance: levenshtein(query, id)})
	}
	sort.SliceStable(scoredIDs, func(i, j int) bool {
		if scoredIDs[i].distance != scoredIDs[j].distance {
			return scoredIDs[i].distance < scoredIDs[j].distance
		}
		return scoredIDs[i].id < scoredIDs[j].id
	})
	if k > len(scoredIDs) {
		k = len(scoredIDs)
	}
	out := make([]string, k)
	for i := 0; i < k; i++ {
		out[i] = scoredIDs[i].id
	}
	return out
}

// levenshtein computes the edit distance between two strings using the
// standard dynamic-programming matrix. O(len(a)*len(b)) time, O(len(b))
// space. Sufficient for topic IDs (< 60 chars each).
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			// min of: delete, insert, substitute
			curr[j] = prev[j] + 1
			if v := curr[j-1] + 1; v < curr[j] {
				curr[j] = v
			}
			if v := prev[j-1] + cost; v < curr[j] {
				curr[j] = v
			}
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

// TopicIncludesPriorDiscoveries reports whether the topic with the given
// ID opts into the v8.96 "Prior discoveries" block prepend. Returns false
// for unknown topic IDs so a renamed topic doesn't accidentally inject a
// stray block.
func TopicIncludesPriorDiscoveries(topicID string) bool {
	t := topicRegistry[topicID]
	return t != nil && t.IncludePriorDiscoveries
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
