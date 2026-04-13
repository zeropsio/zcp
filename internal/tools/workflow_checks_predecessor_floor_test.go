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
// showcase tier is three net-new stems. A single net-new gotcha alongside
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

// TestCheckKnowledgeBaseExceedsPredecessor_TwoNetNewNowFails replays v11's
// apidev pattern: 4 clones of the predecessor + 2 net-new gotchas. At the
// old floor of 2 this was a marginal pass; the v11 session analysis showed
// the output still read as scaffold-quality rather than showcase-quality
// because the narration stopped one gotcha short of the v7 baseline of 3.
// Locks in the tightening from 2 → 3 so the regression cannot come back.
func TestCheckKnowledgeBaseExceedsPredecessor_TwoNetNewNowFails(t *testing.T) {
	t.Parallel()
	readme := readmeWithGotchas(
		"No `.env` files on Zerops",
		"TypeORM `synchronize: true` in production",
		"NestJS listens on `localhost` by default",
		"`ts-node` needs devDependencies",
		"CORS with dual-runtime frontend",
		"S3 path-style addressing required",
	)
	checks := checkKnowledgeBaseExceedsPredecessor(readme, showcaseTierPlan(), nestjsMinimalPredecessorStems)
	if len(checks) == 0 || checks[0].Status != "fail" {
		t.Errorf("expected fail with 2 net-new at new floor of 3, got: %+v", checks)
	}
}

// TestCheckKnowledgeBaseAuthenticity_V12SyntheticMix replays the v12
// nestjs-showcase API README: 5 gotchas that all clear the net-new floor
// (tokens don't overlap nestjs-minimal) but only 1 is authentic (forcePathStyle
// is a real platform trap). The other 4 are scaffold-self-referential
// narration — CORS env shadowing invented by the scaffold's own naming,
// afterInsert hooks describing the scaffold's own seed script, lazy NATS
// connection describing a design choice, Valkey ioredis store configuration
// documenting the scaffold's own library pick. The authenticity check must
// fail so retries reach the v7 quality bar.
func TestCheckKnowledgeBaseAuthenticity_V12SyntheticMix(t *testing.T) {
	t.Parallel()
	readme := "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n\n" +
		"### Gotchas\n\n" +
		"- **forcePathStyle: true for S3/MinIO** — Zerops object storage uses MinIO which requires path-style S3 URLs. The AWS SDK defaults to virtual-hosted style which fails with DNS resolution errors.\n" +
		"- **CORS origin must match the frontend URL** — the API sets FRONTEND_URL from the project-level STAGE_APP_URL. Use a different env var name (FRONTEND_URL) to avoid shadowing the project-level var.\n" +
		"- **Meilisearch index must be seeded explicitly** — TypeORM's afterInsert hooks don't fire during raw SQL seeding. The seed script must call the Meilisearch addDocuments API after inserting records.\n" +
		"- **NATS lazy connection pattern** — the NATS client connects on first publish, not at module init. This prevents the API from crashing at startup if the NATS service is still initializing.\n" +
		"- **Valkey cache-manager store configuration** — the cache-manager library for NestJS requires an ioredis-backed store adapter. Valkey is Redis-compatible but the connection must use plain TCP for internal traffic on Zerops.\n" +
		"\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n"
	checks := checkKnowledgeBaseExceedsPredecessor(readme, showcaseTierPlan(), nestjsMinimalPredecessorStems)
	var authenticity *workflow.StepCheck
	for i := range checks {
		if checks[i].Name == "knowledge_base_authenticity" {
			authenticity = &checks[i]
			break
		}
	}
	if authenticity == nil {
		t.Fatalf("expected knowledge_base_authenticity check in results, got: %+v", checks)
	}
	if authenticity.Status != "fail" {
		t.Errorf("v12 synthetic mix should fail authenticity, got status=%q detail=%q", authenticity.Status, authenticity.Detail)
	}
}

// TestCheckKnowledgeBaseAuthenticity_V7Style replays v7-style authentic
// gotchas: every entry names a concrete Zerops constraint or a failure
// mode. The check must pass — this is the quality bar for showcase
// recipes written to the new scaffold-minimal + feature-subagent design.
func TestCheckKnowledgeBaseAuthenticity_V7Style(t *testing.T) {
	t.Parallel()
	readme := "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n\n" +
		"### Gotchas\n\n" +
		"- **forcePathStyle: true for S3/MinIO** — Zerops object storage uses MinIO which requires path-style URLs. AWS SDK defaults to virtual-hosted style which fails with DNS resolution errors.\n" +
		"- **Trust proxy and bind 0.0.0.0** — Zerops terminates SSL at the L7 balancer. Without trust proxy Express sees every request as plain HTTP, breaking protocol detection and secure cookies.\n" +
		"- **execOnce on multi-container deploys** — migrations acquire advisory locks and must not race across horizontal containers. zsc execOnce guarantees exactly-one execution for a given appVersionId.\n" +
		"- **Vite dev server host-check** — Vite 6+ blocks requests from unrecognized hosts. The allowedHosts setting is required or the dev server returns Blocked request for the Zerops subdomain.\n" +
		"- **No .env files on Zerops** — Zerops injects all environment variables as OS-level env vars. Creating a .env file shadows the platform-injected values.\n" +
		"\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n"
	checks := checkKnowledgeBaseExceedsPredecessor(readme, showcaseTierPlan(), nestjsMinimalPredecessorStems)
	var authenticity *workflow.StepCheck
	for i := range checks {
		if checks[i].Name == "knowledge_base_authenticity" {
			authenticity = &checks[i]
			break
		}
	}
	if authenticity == nil {
		t.Fatalf("expected knowledge_base_authenticity check in results, got: %+v", checks)
	}
	if authenticity.Status != "pass" {
		t.Errorf("v7-style authentic gotchas should pass authenticity, got status=%q detail=%q", authenticity.Status, authenticity.Detail)
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
