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
- **PostgreSQL pool saturation under concurrent writes** — pg driver floods logs.
- **Valkey lazyConnect mandatory without AUTH** — empty AUTH rejection.
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
- **PostgreSQL pool saturation under concurrent writes** — pg driver issue.
- **Valkey lazyConnect mandatory without AUTH** — empty AUTH.
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

// TestServiceCoverage_TypeORM_DoesNotSatisfyDB — a gotcha that mentions
// only ORM client-library names (TypeORM, Prisma) without a platform
// service brand or Zerops env-var prefix must NOT satisfy db coverage.
// Rationale: the reform is framework-agnostic; ORM names are Node-
// ecosystem tokens that unfairly pass Node recipes and unfairly fail
// Rails/Django/PHP recipes.
func TestServiceCoverage_TypeORM_DoesNotSatisfyDB(t *testing.T) {
	t.Parallel()
	plan := showcasePlan(
		workflow.RecipeTarget{Hostname: "apidev", Type: "nodejs@22", Role: "api"},
		workflow.RecipeTarget{Hostname: "db", Type: "postgresql@18"},
	)
	kb := `### Gotchas
- **TypeORM synchronize must be off in production** — Schema drift and deadlocks.
- **Prisma migrate deploy is the only safe migration path** — every replica racing drop_if_exists deadlocks the pool.
`
	checks := checkServiceCoverage(kb, plan, "apidev", false)
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail — only TypeORM/Prisma mentioned, no db brand or env-var; got %s — %s", checks[0].Status, checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, "db") {
		t.Fatalf("detail must name 'db': %s", checks[0].Detail)
	}
}

// TestServiceCoverage_Ioredis_DoesNotSatisfyCache — ioredis client-
// library name without Valkey/Redis brand or env-var must not satisfy
// cache coverage.
func TestServiceCoverage_Ioredis_DoesNotSatisfyCache(t *testing.T) {
	t.Parallel()
	plan := showcasePlan(
		workflow.RecipeTarget{Hostname: "apidev", Type: "nodejs@22", Role: "api"},
		workflow.RecipeTarget{Hostname: "redis", Type: "valkey@7.2"},
	)
	kb := `### Gotchas
- **ioredis lazyConnect is mandatory** — connects synchronously on module load otherwise and crashes.
`
	checks := checkServiceCoverage(kb, plan, "apidev", false)
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail — only ioredis mentioned, no cache brand or env-var; got %s — %s", checks[0].Status, checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, "cache") {
		t.Fatalf("detail must name 'cache': %s", checks[0].Detail)
	}
}

// TestServiceCoverage_Keydb_DoesNotSatisfyCache — keydb is a Redis-
// compatible fork but not a Zerops-managed service type (Zerops offers
// valkey, not keydb). Must not satisfy cache coverage.
func TestServiceCoverage_Keydb_DoesNotSatisfyCache(t *testing.T) {
	t.Parallel()
	plan := showcasePlan(
		workflow.RecipeTarget{Hostname: "apidev", Type: "nodejs@22", Role: "api"},
		workflow.RecipeTarget{Hostname: "cache", Type: "valkey@7.2"},
	)
	kb := `### Gotchas
- **keydb fork cache semantics** — some commands behave differently, crashes on certain patterns.
`
	checks := checkServiceCoverage(kb, plan, "apidev", false)
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail — only keydb mentioned, no Valkey/Redis brand or env-var; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestServiceCoverage_ValkeyBrand_SatisfiesCache — the service brand
// itself (Valkey) satisfies cache coverage post-cleanup.
func TestServiceCoverage_ValkeyBrand_SatisfiesCache(t *testing.T) {
	t.Parallel()
	plan := showcasePlan(
		workflow.RecipeTarget{Hostname: "apidev", Type: "nodejs@22", Role: "api"},
		workflow.RecipeTarget{Hostname: "cache", Type: "valkey@7.2"},
	)
	kb := `### Gotchas
- **Valkey no-auth on Zerops requires empty-AUTH tolerance in client** — otherwise the client rejects and crashes.
`
	checks := checkServiceCoverage(kb, plan, "apidev", false)
	if checks[0].Status != statusPass {
		t.Fatalf("expected pass — Valkey brand mentioned; got %s — %s", checks[0].Status, checks[0].Detail)
	}
}

// TestServiceCoverage_DbEnvVar_SatisfiesDb — ${db_hostname} env-var
// prefix satisfies db coverage without needing a brand name.
func TestServiceCoverage_DbEnvVar_SatisfiesDb(t *testing.T) {
	t.Parallel()
	plan := showcasePlan(
		workflow.RecipeTarget{Hostname: "apidev", Type: "nodejs@22", Role: "api"},
		workflow.RecipeTarget{Hostname: "db", Type: "postgresql@18"},
	)
	kb := `### Gotchas
- **Use ${db_hostname} not localhost** — localhost points to the dev container, not the managed db; connection refused otherwise.
`
	checks := checkServiceCoverage(kb, plan, "apidev", false)
	if checks[0].Status != statusPass {
		t.Fatalf("expected pass — ${db_ env-var prefix mentioned; got %s — %s", checks[0].Status, checks[0].Detail)
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
