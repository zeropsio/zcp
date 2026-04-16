package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

func showcasePlan(targets ...workflow.RecipeTarget) *workflow.RecipePlan {
	return &workflow.RecipePlan{
		Tier:    workflow.RecipeTierShowcase,
		Targets: targets,
	}
}

// TestServiceCoverage_API_FullCoverage_Pass — apidev gotchas mention
// every managed service category in the plan (db, cache, queue, storage,
// search). v20 apidev shape.
func TestServiceCoverage_API_FullCoverage_Pass(t *testing.T) {
	t.Parallel()
	plan := showcasePlan(
		workflow.RecipeTarget{Hostname: "apidev", Type: "nodejs@22", Role: "api"},
		workflow.RecipeTarget{Hostname: "db", Type: "postgresql@18"},
		workflow.RecipeTarget{Hostname: "redis", Type: "valkey@7.2"},
		workflow.RecipeTarget{Hostname: "queue", Type: "nats@2.12"},
		workflow.RecipeTarget{Hostname: "storage", Type: "object-storage"},
		workflow.RecipeTarget{Hostname: "search", Type: "meilisearch@1.20"},
	)
	kb := `### Gotchas
- **TypeORM client.query deprecation** — pg driver floods logs.
- **ioredis lazyConnect mandatory for Valkey** — empty AUTH rejection.
- **NATS credentials must be separate options** — AUTHORIZATION_VIOLATION otherwise.
- **Object Storage forcePathStyle** — MinIO needs path style addressing.
- **Meilisearch indexes ephemeral** — re-push on every deploy.
`
	checks := checkServiceCoverage(kb, plan, "apidev", false)
	if checks[0].Status != statusPass {
		t.Fatalf("expected pass; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestServiceCoverage_API_MissingS3_Fail — apidev gotchas omit
// storage. v18 apidev shape (4 gotchas, no S3 mention).
func TestServiceCoverage_API_MissingS3_Fail(t *testing.T) {
	t.Parallel()
	plan := showcasePlan(
		workflow.RecipeTarget{Hostname: "apidev", Type: "nodejs@22", Role: "api"},
		workflow.RecipeTarget{Hostname: "db", Type: "postgresql@18"},
		workflow.RecipeTarget{Hostname: "redis", Type: "valkey@7.2"},
		workflow.RecipeTarget{Hostname: "queue", Type: "nats@2.12"},
		workflow.RecipeTarget{Hostname: "storage", Type: "object-storage"},
		workflow.RecipeTarget{Hostname: "search", Type: "meilisearch@1.20"},
	)
	kb := `### Gotchas
- **TypeORM client.query deprecation** — pg driver issue.
- **ioredis lazyConnect mandatory for Valkey** — empty AUTH.
- **NATS credentials must be separate options** — AUTHORIZATION_VIOLATION.
- **Meilisearch indexes ephemeral** — re-push.
`
	checks := checkServiceCoverage(kb, plan, "apidev", false)
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail — storage uncovered; got %s — %s", checks[0].Status, checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, "storage") {
		t.Fatalf("detail must name 'storage': %s", checks[0].Detail)
	}
}

// TestServiceCoverage_Worker_DBQueueOnly_Pass — workerdev only needs
// to cover db + queue (typical worker pattern). No need to mention
// cache/storage/search even if the plan ships them.
func TestServiceCoverage_Worker_DBQueueOnly_Pass(t *testing.T) {
	t.Parallel()
	plan := showcasePlan(
		workflow.RecipeTarget{Hostname: "workerdev", Type: "nodejs@22", IsWorker: true},
		workflow.RecipeTarget{Hostname: "db", Type: "postgresql@18"},
		workflow.RecipeTarget{Hostname: "queue", Type: "nats@2.12"},
		workflow.RecipeTarget{Hostname: "redis", Type: "valkey@7.2"},
	)
	kb := `### Gotchas
- **NATS queue group prevents duplicate processing** — without queue, every replica receives every message.
- **Worker shares entities with API but must not run migrations** — TypeORM concurrent DDL deadlocks PostgreSQL.
`
	checks := checkServiceCoverage(kb, plan, "workerdev", true)
	if checks[0].Status != statusPass {
		t.Fatalf("expected pass; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestServiceCoverage_Worker_MissingDB_Fail — workerdev gotchas only
// cover queue, no db mention. Workers normally write results to db.
func TestServiceCoverage_Worker_MissingDB_Fail(t *testing.T) {
	t.Parallel()
	plan := showcasePlan(
		workflow.RecipeTarget{Hostname: "workerdev", Type: "nodejs@22", IsWorker: true},
		workflow.RecipeTarget{Hostname: "db", Type: "postgresql@18"},
		workflow.RecipeTarget{Hostname: "queue", Type: "nats@2.12"},
	)
	kb := `### Gotchas
- **NATS queue group prevents duplicate processing** — every replica receives every message without it.
- **NATS reconnect-forever pattern** — broker restart silently kills worker.
`
	checks := checkServiceCoverage(kb, plan, "workerdev", true)
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail; got %s — %s", checks[0].Status, checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, "db") {
		t.Fatalf("detail must name 'db': %s", checks[0].Detail)
	}
}

// TestServiceCoverage_Static_NoRequirement — appdev (static SPA) does
// not directly use managed services. Skip the check entirely.
func TestServiceCoverage_Static_NoRequirement(t *testing.T) {
	t.Parallel()
	plan := showcasePlan(
		workflow.RecipeTarget{Hostname: "appdev", Type: "static", Role: "app"},
		workflow.RecipeTarget{Hostname: "db", Type: "postgresql@18"},
		workflow.RecipeTarget{Hostname: "queue", Type: "nats@2.12"},
	)
	kb := `### Gotchas
- **Vite host gate** — without allowedHosts, the dev subdomain is rejected.
`
	target := workflow.RecipeTarget{Hostname: "appdev", Type: "static", Role: "app"}
	checks := checkServiceCoverage(kb, plan, target.Hostname, false)
	// Static base has no service-coverage requirement — but we still
	// derive expected from API targets in the plan; static frontend is
	// detected by Type and skipped.
	if len(checks) == 0 {
		// Acceptable: skip-with-no-check shape.
		return
	}
	if checks[0].Status != statusPass {
		t.Fatalf("static target should pass or no-op; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestServiceCoverage_NoManagedServices_NoOp — minimal recipe with no
// managed services at all. Nothing to cover.
func TestServiceCoverage_NoManagedServices_NoOp(t *testing.T) {
	t.Parallel()
	plan := showcasePlan(
		workflow.RecipeTarget{Hostname: "app", Type: "nodejs@22"},
	)
	kb := `### Gotchas
- **Bind 0.0.0.0 not localhost** — L7 balancer cannot route to 127.0.0.1.
`
	checks := checkServiceCoverage(kb, plan, "app", false)
	// No managed services in plan → no expectation.
	if len(checks) == 0 {
		return
	}
	if checks[0].Status != statusPass {
		t.Fatalf("no managed services → expected pass; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestServiceCoverage_NotShowcase_NoOp — minimal-tier recipes don't
// run the service-coverage check. Coverage is a showcase-only quality
// bar (showcase is the recipe class that exercises every category).
func TestServiceCoverage_NotShowcase_NoOp(t *testing.T) {
	t.Parallel()
	plan := &workflow.RecipePlan{
		Tier: workflow.RecipeTierMinimal,
		Targets: []workflow.RecipeTarget{
			{Hostname: "app", Type: "nodejs@22"},
			{Hostname: "db", Type: "postgresql@18"},
		},
	}
	kb := `### Gotchas
- **Bind 0.0.0.0** — L7 balancer.
`
	checks := checkServiceCoverage(kb, plan, "app", false)
	if len(checks) != 0 {
		t.Fatalf("minimal tier should no-op; got %+v", checks)
	}
}
