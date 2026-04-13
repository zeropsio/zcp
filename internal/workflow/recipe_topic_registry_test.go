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
