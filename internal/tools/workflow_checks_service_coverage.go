package tools

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// checkServiceCoverage enforces that a codebase's gotchas mention each
// managed-service category the codebase exercises. Recipes are
// standalone artifacts — the predecessor-as-floor check no longer
// penalizes overlap (per the v8.78 reform); what matters is that the
// showcase recipe covers every category it ships, with at least one
// gotcha per category.
//
// Heuristic per codebase:
//
//   - Static / serve-only targets (appdev): no requirement (the SPA
//     doesn't directly use managed services).
//   - Worker target: db + queue if those categories exist in the plan
//     (the typical worker pattern: consume from queue, write results
//     to db).
//   - Any other runtime target (apidev / main): every managed-service
//     category present in the plan.
//
// "Mention" is a substring match against the category's brand names
// (`PostgreSQL`, `Valkey`, `NATS`, `Object Storage`/`MinIO`/`S3`,
// `Meilisearch`) plus the Zerops service-discovery env-var prefix
// (`${db_`, `${redis_`, `${queue_`, `${storage_`, `${search_`).
//
// Showcase-tier only — minimal/hello-world recipes are exempt because
// they intentionally exercise a narrow surface.
func checkServiceCoverage(kbContent string, plan *workflow.RecipePlan, hostname string, isWorker bool) []workflow.StepCheck {
	if plan == nil || plan.Tier != workflow.RecipeTierShowcase {
		return nil
	}
	if kbContent == "" {
		return nil
	}
	// Static / serve-only frontends have no managed-service obligations.
	if targetIsStatic(plan, hostname) {
		return nil
	}
	expected := expectedCoverageForCodebase(plan, isWorker)
	if len(expected) == 0 {
		return nil
	}
	low := strings.ToLower(kbContent)
	var missing []string
	for _, cat := range expected {
		if categoryMentioned(low, cat) {
			continue
		}
		missing = append(missing, cat)
	}
	checkName := hostname + "_service_coverage"
	if len(missing) == 0 {
		return []workflow.StepCheck{{Name: checkName, Status: statusPass}}
	}
	sort.Strings(missing)
	return []workflow.StepCheck{{
		Name:   checkName,
		Status: statusFail,
		Detail: fmt.Sprintf(
			"%s gotchas leave managed-service category(ies) uncovered: %s. Each managed service in the plan that this codebase exercises must have at least one gotcha that names the service brand (PostgreSQL/Valkey/NATS/Object Storage/MinIO/Meilisearch) OR the Zerops env-var pattern (${X_hostname}, ${X_user}, etc.). Predecessor-cloned gotchas are fine — what matters is full coverage of THIS recipe's service surface, not net-additive vs the predecessor.",
			hostname, strings.Join(missing, ", "),
		),
	}}
}

// serviceCategory canonicalizes a managed-service Zerops type into the
// coverage category used in failure messages and brand-token lookup.
// Returns "" for non-managed types.
func serviceCategory(serviceType string) string {
	t := strings.ToLower(serviceType)
	t = strings.SplitN(t, "@", 2)[0]
	switch t {
	case "postgresql", "mysql", "mariadb":
		return "db"
	case "valkey", "redis", "keydb":
		return "cache"
	case "nats", "kafka", "rabbitmq":
		return "queue"
	case "object-storage", "shared-storage":
		return "storage"
	case "meilisearch", "elasticsearch", "typesense":
		return "search"
	case "mailpit", "mailhog":
		return "mail"
	}
	return ""
}

// categoryBrands lists the brand-name and env-var-prefix tokens that
// signal a gotcha mentions a given category. Substring match,
// case-insensitive.
var categoryBrands = map[string][]string{
	"db":      {"postgresql", "postgres", "mysql", "mariadb", "${db_", "${pg_", "${postgres_", "typeorm", "prisma", "${database_"},
	"cache":   {"valkey", "redis", "keydb", "ioredis", "${redis_", "${cache_", "${valkey_"},
	"queue":   {"nats", "kafka", "rabbitmq", "${queue_", "${nats_", "${kafka_", "${rabbitmq_"},
	"storage": {"object storage", "object-storage", "minio", " s3 ", "s3-compatible", "${storage_", "${s3_", "${minio_"},
	"search":  {"meilisearch", "elasticsearch", "typesense", "${search_", "${meilisearch_", "${elastic_"},
	"mail":    {"mailpit", "mailhog", "smtp", "${mail_", "${smtp_"},
}

func categoryMentioned(lowered string, category string) bool {
	for _, brand := range categoryBrands[category] {
		if strings.Contains(lowered, brand) {
			return true
		}
	}
	return false
}

// expectedCoverageForCodebase derives the set of category names this
// codebase is expected to cover.
func expectedCoverageForCodebase(plan *workflow.RecipePlan, isWorker bool) []string {
	seen := map[string]bool{}
	var cats []string
	for _, t := range plan.Targets {
		cat := serviceCategory(t.Type)
		if cat == "" || seen[cat] {
			continue
		}
		seen[cat] = true
		cats = append(cats, cat)
	}
	if !isWorker {
		sort.Strings(cats)
		return cats
	}
	// Workers: heuristic db + queue subset of what the plan ships.
	var workerCats []string
	for _, c := range []string{"db", "queue"} {
		if seen[c] {
			workerCats = append(workerCats, c)
		}
	}
	sort.Strings(workerCats)
	return workerCats
}

// targetIsStatic returns true when the named target's Type is a serve-
// only base (static, nginx) — the frontend has no business code at
// runtime and therefore no service-coverage requirement.
func targetIsStatic(plan *workflow.RecipePlan, hostname string) bool {
	for _, t := range plan.Targets {
		if t.Hostname != hostname {
			continue
		}
		base := strings.SplitN(strings.ToLower(t.Type), "@", 2)[0]
		switch base {
		case "static", "nginx":
			return true
		}
		return false
	}
	return false
}
