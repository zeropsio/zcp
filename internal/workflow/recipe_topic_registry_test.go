package workflow

import (
	"testing"

	"github.com/zeropsio/zcp/internal/content"
)

// TestTopicRegistry_AllTopicBlocksExist asserts that every BlockName referenced
// by a topic in the registry actually exists as a <block> tag in the
// corresponding section of recipe.md. A registry entry pointing at a renamed
// or deleted block would silently return empty content via ResolveTopic.
func TestTopicRegistry_AllTopicBlocksExist(t *testing.T) {
	t.Parallel()

	steps := []string{
		RecipeStepGenerate, RecipeStepDeploy,
		RecipeStepFinalize, RecipeStepClose,
	}
	for _, step := range steps {
		topics := AllTopicsForStep(step)
		if len(topics) == 0 {
			continue
		}
		// Build a set of block names actually present in the section.
		sectionBlocks := blockNamesForStep(t, step)
		for _, topic := range topics {
			for _, bn := range topic.BlockNames {
				if !sectionBlocks[bn] {
					t.Errorf("topic %q (step %q) references block %q which does not exist in recipe.md", topic.ID, step, bn)
				}
			}
		}
	}
}

// TestTopicRegistry_PredicateParity asserts that each topic's predicate
// matches the predicate on its source block(s) in the section catalog.
// A mismatch would mean the topic is gated differently from the block it
// wraps — the topic might return content that the monolithic guide would
// not (or vice versa).
func TestTopicRegistry_PredicateParity(t *testing.T) {
	t.Parallel()

	shapes := []struct {
		name string
		plan *RecipePlan
	}{
		{"hello-world", fixtureForShape(ShapeHelloWorld)},
		{"backend-minimal", fixtureForShape(ShapeBackendMinimal)},
		{"fullstack-showcase", fixtureForShape(ShapeFullStackShowcase)},
		{"dual-runtime-showcase", fixtureForShape(ShapeDualRuntimeShowcase)},
	}

	steps := []string{
		RecipeStepGenerate, RecipeStepDeploy,
		RecipeStepFinalize, RecipeStepClose,
	}
	for _, step := range steps {
		catalog := catalogForStep(step)
		if len(catalog) == 0 {
			continue
		}
		catalogPred := make(map[string]func(*RecipePlan) bool, len(catalog))
		for _, sb := range catalog {
			catalogPred[sb.Name] = sb.Predicate
		}

		topics := AllTopicsForStep(step)
		for _, topic := range topics {
			for _, shape := range shapes {
				topicAllowed := topic.Predicate == nil || topic.Predicate(shape.plan)
				// A topic is allowed if any of its blocks would be allowed
				// in the monolithic composition.
				anyBlockAllowed := false
				for _, bn := range topic.BlockNames {
					pred := catalogPred[bn]
					if pred == nil || pred(shape.plan) {
						anyBlockAllowed = true
						break
					}
				}
				// If topic says "allowed" but no block in the catalog is
				// allowed, the topic would return content that the
				// monolithic guide would not. Flag it.
				if topicAllowed && !anyBlockAllowed {
					t.Errorf("topic %q allowed for shape %q but none of its blocks are allowed in the catalog (step %q)",
						topic.ID, shape.name, step)
				}
			}
		}
	}
}

// TestTopicRegistry_NoDuplicateIDs ensures no two topics share the same ID.
func TestTopicRegistry_NoDuplicateIDs(t *testing.T) {
	t.Parallel()
	seen := make(map[string]string) // ID → step
	steps := []string{
		RecipeStepGenerate, RecipeStepDeploy,
		RecipeStepFinalize, RecipeStepClose,
	}
	for _, step := range steps {
		for _, topic := range AllTopicsForStep(step) {
			if prev, ok := seen[topic.ID]; ok {
				t.Errorf("duplicate topic ID %q: first in step %q, again in step %q", topic.ID, prev, step)
			}
			seen[topic.ID] = step
		}
	}
}

// TestResolveTopic_BasicResolution exercises ResolveTopic for a few
// representative topics to ensure blocks are found and concatenated.
func TestResolveTopic_BasicResolution(t *testing.T) {
	t.Parallel()

	plan := fixtureForShape(ShapeDualRuntimeShowcase)

	tests := []struct {
		topicID     string
		plan        *RecipePlan
		expectEmpty bool
		minLen      int // minimum expected content length
	}{
		{"container-state", plan, false, 50},
		{"zerops-yaml-rules", plan, false, 200},
		{"dual-runtime-urls", plan, false, 100},
		{"deploy-flow", plan, false, 200},
		{"smoke-test", plan, false, 30},
		{"env-comments", plan, false, 100},
		{"comment-style", plan, false, 100},
		{"code-review-agent", plan, false, 100},
		// Predicate-gated: dual-runtime-urls should be empty for hello-world
		{"dual-runtime-urls", fixtureForShape(ShapeHelloWorld), true, 0},
		// showcase-service-keys should be empty for non-showcase
		{"showcase-service-keys", fixtureForShape(ShapeBackendMinimal), true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.topicID, func(t *testing.T) {
			t.Parallel()
			content, err := ResolveTopic(tt.topicID, tt.plan)
			if err != nil {
				t.Fatalf("ResolveTopic(%q): %v", tt.topicID, err)
			}
			if tt.expectEmpty && content != "" {
				t.Errorf("expected empty content for %q, got %d bytes", tt.topicID, len(content))
			}
			if !tt.expectEmpty && len(content) < tt.minLen {
				t.Errorf("topic %q content too short: %d bytes, expected at least %d", tt.topicID, len(content), tt.minLen)
			}
		})
	}
}

// TestRecipeTopicRegistry_WhereCommandsRun_AppliesToMainAgent asserts
// the where-commands-run block carries the main-agent-scope framing
// plus the git-traversal example that the v21 post-mortem identified
// as the root cause of the 3 parallel 120 s zcp-side git-add hangs.
// A brief that only speaks to "the sub-agent" fails to govern the main
// agent, which is the agent running git operations after scaffold.
func TestRecipeTopicRegistry_WhereCommandsRun_AppliesToMainAgent(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	body, err := ResolveTopic("where-commands-run", plan)
	if err != nil {
		t.Fatalf("resolve where-commands-run: %v", err)
	}
	wants := []string{
		"main agent",
		"sub-agent",
		"git add",
		"SSHFS",
		"EACCES",
		"120",
	}
	for _, w := range wants {
		if !stringsContains(body, w) {
			t.Errorf("where-commands-run body missing %q", w)
		}
	}
}

// TestRecipeTopicRegistry_WriterSubagentBrief_Registered asserts the
// writer-subagent-brief topic resolves to a brief that instructs the
// sub-agent to use Write (not Bash) and scope to README + CLAUDE.md
// files. §3.6 of v21 postmortem.
func TestRecipeTopicRegistry_WriterSubagentBrief_Registered(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	body, err := ResolveTopic("writer-subagent-brief", plan)
	if err != nil {
		t.Fatalf("resolve writer-subagent-brief: %v", err)
	}
	for _, s := range []string{"README + CLAUDE.md writer", "No Bash", "Write tool"} {
		if !stringsContains(body, s) {
			t.Errorf("writer-subagent-brief body missing %q", s)
		}
	}
}

// TestRecipeTopicRegistry_FixSubagentBrief_Registered asserts the
// fix-subagent-brief topic resolves and carries scope-restrictions.
func TestRecipeTopicRegistry_FixSubagentBrief_Registered(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	body, err := ResolveTopic("fix-subagent-brief", plan)
	if err != nil {
		t.Fatalf("resolve fix-subagent-brief: %v", err)
	}
	for _, s := range []string{"Files you MAY edit", "Files you MUST NOT edit", "2 KB"} {
		if !stringsContains(body, s) {
			t.Errorf("fix-subagent-brief body missing %q", s)
		}
	}
}

// v8.81 §4.1 — content-fix sub-agent brief. Dispatched on retries of
// `complete step=deploy` after content-quality checks fail; absorbs the
// v22-class Phase-4 rewrite cycle that otherwise leaks into main context.
func TestRecipeTopicRegistry_ContentFixSubagentBrief_Registered(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	body, err := ResolveTopic("content-fix-subagent-brief", plan)
	if err != nil {
		t.Fatalf("resolve content-fix-subagent-brief: %v", err)
	}
	for _, s := range []string{
		"post-writer content-quality repair",
		"content_fix_dispatch_required",
		"Files you MAY edit",
		"Files you MUST NOT touch",
		"inline-fix acknowledged",
		"gotcha_causal_anchor",
		"recipe_architecture_narrative",
	} {
		if !stringsContains(body, s) {
			t.Errorf("content-fix-subagent-brief body missing %q", s)
		}
	}
}

// TestRecipeTopicRegistry_FeatureSubagentMCPSchemas_Registered — the
// inlined MCP schema reference for the feature sub-agent. §3.6b.
func TestRecipeTopicRegistry_FeatureSubagentMCPSchemas_Registered(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	body, err := ResolveTopic("feature-subagent-mcp-schemas", plan)
	if err != nil {
		t.Fatalf("resolve feature-subagent-mcp-schemas: %v", err)
	}
	for _, s := range []string{"serviceHostname", "waitSeconds", "noHttpProbe", "-32602 invalid params"} {
		if !stringsContains(body, s) {
			t.Errorf("feature-subagent-mcp-schemas body missing %q", s)
		}
	}
}

// TestRecipeTopicRegistry_ContentQualityOverview_Registered — v8.82 §4.3
// asserts the content-quality-overview topic resolves, is eager, and carries
// the six-surface map + boundary rules + anti-patterns. The agent reads this
// at deploy-step start to build a coherent mental model of all six content
// surfaces before authoring any of them.
func TestRecipeTopicRegistry_ContentQualityOverview_Registered(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	topic := LookupTopic("content-quality-overview")
	if topic == nil {
		t.Fatal("content-quality-overview not registered")
	}
	if !topic.Eager {
		t.Fatal("content-quality-overview must be Eager so the map lands in context before authorship")
	}
	if topic.Step != RecipeStepDeploy {
		t.Fatalf("content-quality-overview must live on the deploy step; got %q", topic.Step)
	}
	body, err := ResolveTopic("content-quality-overview", plan)
	if err != nil {
		t.Fatalf("resolve content-quality-overview: %v", err)
	}
	// Content anchors — the block names all six surfaces and the key
	// boundary rules. If any of these go missing, the map is broken.
	wants := []string{
		"six-surface",
		"zerops.yaml",
		"Integration Guide",
		"Gotchas",
		"import.yaml",
		"CLAUDE.md",
		"Root README",
		// Rubric references
		"causal-anchor",
		"predecessor-floor",
		// Boundary rules
		"Platform facts",
		"repo-local ops",
		// Anti-patterns
		"Anti-patterns",
	}
	for _, w := range wants {
		if !stringsContains(body, w) {
			t.Errorf("content-quality-overview body missing %q", w)
		}
	}
}

// TestInjectEagerTopics_ContentQualityOverview_InDeploy asserts that the
// content-quality-overview topic reaches the deploy-step eager injection
// regardless of shape. It's a teaching overview — every shape needs the
// mental map before authoring the README.
func TestInjectEagerTopics_ContentQualityOverview_InDeploy(t *testing.T) {
	t.Parallel()
	for _, shape := range []RecipeShape{
		ShapeHelloWorld, ShapeBackendMinimal,
		ShapeFullStackShowcase, ShapeDualRuntimeShowcase,
	} {
		plan := fixtureForShape(shape)
		got := InjectEagerTopics(recipeDeployTopics, plan)
		if !stringsContains(got, "content-quality-overview") {
			t.Errorf("shape %q did not receive content-quality-overview eager injection", shape)
		}
	}
}

// TestResolveTopic_UnknownTopic verifies error on unknown topic.
func TestResolveTopic_UnknownTopic(t *testing.T) {
	t.Parallel()
	_, err := ResolveTopic("nonexistent-topic", nil)
	if err == nil {
		t.Fatal("expected error for unknown topic")
	}
}

// TestInjectEagerTopics_GenerateShowcase covers the v14 eager-topic
// promotion: topics that guard catastrophic-failure-class bugs are inlined
// into the generate guide directly, so the agent does not need to fetch them
// explicitly. v13 shipped with dev-server-hostcheck and scaffold-subagent-brief
// as optional fetch topics; the Sonnet agent selected 10 other topics and
// skipped both, then rediscovered Vite allowedHosts as a 403 mid-deploy and
// composed scaffold briefs without the DO-NOT-WRITE list. The eager
// injection makes those two topics arrive whether the agent fetches them
// or not.
func TestInjectEagerTopics_GenerateShowcase(t *testing.T) {
	t.Parallel()

	// A dual-runtime showcase with a static frontend target triggers
	// hasBundlerDevServer (frontend runs a Vite dev server) and
	// hasMultipleCodebases (scaffold-subagent-brief predicate). Both
	// eager topics should resolve to non-empty content.
	plan := &RecipePlan{
		Framework: "nestjs",
		Tier:      RecipeTierShowcase,
		Slug:      "nestjs-showcase",
		Targets: []RecipeTarget{
			{Hostname: "api", Type: "nodejs@22", Role: RecipeRoleAPI},
			{Hostname: "app", Type: "static", DevBase: "nodejs@22", Role: RecipeRoleApp},
		},
	}
	got := InjectEagerTopics(recipeGenerateTopics, plan)
	if got == "" {
		t.Fatal("expected eager topic content for nestjs-showcase plan, got empty")
	}

	wants := []string{
		"dev-server-hostcheck",    // topic ID must be referenced in header
		"scaffold-subagent-brief", // topic ID must be referenced in header
		"allowedHosts",            // content from dev-server-host-check block
		"health-dashboard-only",   // content from scaffold-subagent-brief block
		// v17 regression guard: scaffold sub-agents MUST be told that
		// `/var/www/{hostname}/` is an SSHFS MOUNT on zcp and every
		// executable command must ssh into the target container. v17
		// failed because the brief lacked this and all three scaffold
		// subagents ran `cd /var/www/{hostname} && npm install` on zcp,
		// producing root-owned node_modules and broken absolute-path
		// symlinks that required 16 min of recovery work over ssh.
		"SSHFS network mount",
		"Executable commands",
		"write surface, not an execution surface",
		// v8.81 regression guards: the three recurrence-class service-client
		// traps that v21 AND v22 both hit as runtime CRITs despite being
		// documented in the prior run's published README. Gotchas-in-README
		// are post-mortem; the scaffold brief is the preventative.
		"Recurrence-class service-client traps",
		// NATS URL-embedded-credentials forbid
		"NATS credentials MUST be passed as separate",
		"TypeError: Invalid URL",
		"ConnectionOptions",
		// S3 endpoint preference
		"storage_apiUrl",
		"301 redirects",
		"forcePathStyle",
		// Dev-start vs buildCommands contract
		"dev-start",
		"ts-node",
		"node dist/main.js",
		"post_spawn_exit",
	}
	for _, w := range wants {
		if !stringsContains(got, w) {
			t.Errorf("eager injection missing %q.\nGot:\n%s", w, got)
		}
	}
}

// TestInjectEagerTopics_MinimalTierSkipsShowcaseTopics — scaffold-subagent-brief
// is gated on isShowcase + hasMultipleCodebases. A minimal hello-world plan
// fails both predicates and must not receive that eager injection. The
// dev-server-hostcheck topic still fires if the framework is a bundler
// framework, so this test uses a non-bundler framework to isolate the
// minimal-tier case.
func TestInjectEagerTopics_MinimalTierSkipsShowcaseTopics(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{
		Framework: "nestjs",
		Tier:      RecipeTierMinimal,
		Slug:      "nestjs-hello-world",
		Targets: []RecipeTarget{
			{Hostname: "api", Type: "nodejs@22"},
		},
	}
	got := InjectEagerTopics(recipeGenerateTopics, plan)
	if stringsContains(got, "scaffold-subagent-brief") {
		t.Errorf("minimal-tier plan should not receive scaffold-subagent-brief eager injection, got:\n%s", got)
	}
}

func stringsContains(haystack, needle string) bool {
	return len(needle) == 0 || indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// ── helpers ──

func blockNamesForStep(t *testing.T, step string) map[string]bool {
	t.Helper()
	md := loadRecipeMD(t)
	sectionName := stepToSectionName(step)
	body := ExtractSection(md, sectionName)
	if body == "" {
		t.Fatalf("section %q not found", sectionName)
	}
	blocks := ExtractBlocks(body)
	names := make(map[string]bool, len(blocks))
	for _, b := range blocks {
		if b.Name != "" {
			names[b.Name] = true
		}
	}
	return names
}

func catalogForStep(step string) []sectionBlock {
	switch step {
	case RecipeStepGenerate:
		return recipeGenerateBlocks
	case RecipeStepDeploy:
		return recipeDeployBlocks
	case RecipeStepFinalize:
		return recipeFinalizeBlocks
	case RecipeStepClose:
		return recipeCloseBlocks
	default:
		return nil
	}
}

func loadRecipeMD(t *testing.T) string {
	t.Helper()
	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}
	return md
}
