package tools

import (
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

// nestjsMinimalPredecessorStems mirrors the Gotchas section of
// internal/knowledge/recipes/nestjs-minimal.md. Lifted verbatim to lock the
// test against the real predecessor content the chain-injection code feeds
// the agent at generate time.
var nestjsMinimalPredecessorStems = []string{
	"No `.env` files on Zerops",
	"TypeORM `synchronize: true` in production",
	"NestJS listens on `localhost` by default",
	"`ts-node` needs devDependencies",
}

func showcaseTierPlan() *workflow.RecipePlan {
	return &workflow.RecipePlan{
		Framework:   "nestjs",
		Tier:        workflow.RecipeTierShowcase,
		Slug:        "nestjs-showcase",
		RuntimeType: "nodejs@24",
	}
}

func minimalTierPlan() *workflow.RecipePlan {
	return &workflow.RecipePlan{
		Framework:   "nestjs",
		Tier:        workflow.RecipeTierMinimal,
		Slug:        "nestjs-minimal",
		RuntimeType: "nodejs@24",
	}
}

// TestCheckKnowledgeBaseExceedsPredecessor_V10ClonePattern replays the exact
// v10 apidev knowledge-base: four stems that all re-state the nestjs-minimal
// predecessor gotchas with cosmetic rewording. The check must fail hard,
// because the showcase provisions redis/queue/storage/search and has nothing
// to say about any of them.
func TestCheckKnowledgeBaseExceedsPredecessor_V10ClonePattern(t *testing.T) {
	t.Parallel()
	readme := readmeWithGotchas(
		"No .env files on Zerops.",
		"TypeORM `synchronize: true` must never run in the application process.",
		"NestJS listens on `localhost` by default.",
		"ts-node requires devDependencies.",
	)
	checks := checkKnowledgeBaseExceedsPredecessor(readme, showcaseTierPlan(), nestjsMinimalPredecessorStems)
	if len(checks) == 0 {
		t.Fatal("expected a check result, got none")
	}
	fail := checks[0]
	if fail.Name != "knowledge_base_exceeds_predecessor" {
		t.Errorf("check name = %q, want knowledge_base_exceeds_predecessor", fail.Name)
	}
	if fail.Status != "fail" {
		t.Errorf("status = %q, want fail (all 4 stems clone the predecessor)", fail.Status)
	}
}

// TestCheckKnowledgeBaseExceedsPredecessor_V7Pattern replays v7's apidev
// knowledge-base: 3 predecessor clones + 3 net-new gotchas narrated from the
// actual build (Meilisearch ESM, auto-indexing skips, MinIO forcePathStyle).
// The check must pass — this is the quality bar.
func TestCheckKnowledgeBaseExceedsPredecessor_V7Pattern(t *testing.T) {
	t.Parallel()
	readme := readmeWithGotchas(
		"No `.env` files on Zerops",
		"`synchronize: true` rewrites schemas on every startup",
		"NestJS listens on 127.0.0.1 by default",
		"Meilisearch SDK is ESM-only",
		"Auto-indexing skips on redeploy seed runs",
		"MinIO needs `forcePathStyle: true` and an explicit region",
	)
	checks := checkKnowledgeBaseExceedsPredecessor(readme, showcaseTierPlan(), nestjsMinimalPredecessorStems)
	if len(checks) == 0 {
		t.Fatal("expected a check result, got none")
	}
	pass := checks[0]
	if pass.Name != "knowledge_base_exceeds_predecessor" {
		t.Errorf("check name = %q", pass.Name)
	}
	if pass.Status != "pass" {
		t.Errorf("status = %q, want pass (3 net-new gotchas clears the floor)", pass.Status)
	}
}

// TestCheckKnowledgeBaseExceedsPredecessor_MinimalTierSkipped keeps the check
// scoped to showcase tier. Minimal recipes inherit from hello-world tiers
// whose Gotchas sections are tiny or absent, so requiring "net-new gotchas"
// at that level would produce noise failures on legitimately small recipes.
func TestCheckKnowledgeBaseExceedsPredecessor_MinimalTierSkipped(t *testing.T) {
	t.Parallel()
	readme := readmeWithGotchas("No .env files on Zerops.")
	checks := checkKnowledgeBaseExceedsPredecessor(readme, minimalTierPlan(), nestjsMinimalPredecessorStems)
	if len(checks) != 0 {
		t.Errorf("expected no checks emitted for minimal tier, got %d: %+v", len(checks), checks)
	}
}

// TestCheckKnowledgeBaseExceedsPredecessor_EmptyPredecessorSkipped — when the
// predecessor recipe has no Gotchas section (or the chain injection couldn't
// find one), the check is a no-op. The existing missing-fragment check
// covers "knowledge-base absent entirely"; this check layer only fires when
// there is a predecessor baseline to compare against.
func TestCheckKnowledgeBaseExceedsPredecessor_EmptyPredecessorSkipped(t *testing.T) {
	t.Parallel()
	readme := readmeWithGotchas("Anything at all")
	checks := checkKnowledgeBaseExceedsPredecessor(readme, showcaseTierPlan(), nil)
	if len(checks) != 0 {
		t.Errorf("expected no checks without predecessor baseline, got %d", len(checks))
	}
}

// TestCheckKnowledgeBaseExceedsPredecessor_OneNetNewIsTooFew — the floor for
// showcase tier is two net-new stems. A single net-new gotcha alongside
// three clones is not enough; the showcase added 4 managed services but has
// commentary for one of them at best.
func TestCheckKnowledgeBaseExceedsPredecessor_OneNetNewIsTooFew(t *testing.T) {
	t.Parallel()
	readme := readmeWithGotchas(
		"No .env files on Zerops.",
		"TypeORM `synchronize: true` must never run.",
		"NestJS listens on `localhost`.",
		"Meilisearch SDK is ESM-only",
	)
	checks := checkKnowledgeBaseExceedsPredecessor(readme, showcaseTierPlan(), nestjsMinimalPredecessorStems)
	if len(checks) == 0 || checks[0].Status != "fail" {
		t.Errorf("expected fail with 1 net-new, got: %+v", checks)
	}
}

// readmeWithGotchas builds a minimal README.md containing only the
// knowledge-base fragment with the provided bolded gotcha stems. It is the
// smallest fixture that exercises ExtractGotchaStems via the fragment
// extractor — other README concerns (intro, integration-guide) are tested
// elsewhere.
func readmeWithGotchas(stems ...string) string {
	const header = "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n\n### Gotchas\n\n"
	const footer = "\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n"
	capacity := len(header) + len(footer)
	for _, s := range stems {
		capacity += len("- **") + len(s) + len("** — body text.\n")
	}
	b := make([]byte, 0, capacity)
	b = append(b, header...)
	for _, s := range stems {
		b = append(b, "- **"...)
		b = append(b, s...)
		b = append(b, "** — body text.\n"...)
	}
	b = append(b, footer...)
	return string(b)
}
