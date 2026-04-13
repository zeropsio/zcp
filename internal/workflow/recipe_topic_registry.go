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
	{
		ID: "readme-fragments", Step: RecipeStepGenerate,
		Description: "README structure with extract fragments",
		BlockNames:  []string{"readme-with-fragments"},
	},
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
	},
	{
		ID: "where-commands-run", Step: RecipeStepDeploy,
		Description: "SSH vs zcp-side command execution model",
		BlockNames:  []string{"where-commands-run"},
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
