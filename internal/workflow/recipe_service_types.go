package workflow

import "strings"

// Service-type capability predicates. The template layer dispatches on these
// directly — there is NO intermediate "role" abstraction. The agent submits
// {hostname, type, isWorker} and every template decision flows from the type
// and its platform category.

// ServiceSupportsMode returns true if the service type supports mode: HA/NON_HA.
// Managed services (databases, caches, search, shared-storage, messaging) do.
// Object-storage does NOT — it's always internally replicated.
func ServiceSupportsMode(serviceType string) bool {
	return IsManagedService(serviceType) && !IsObjectStorageType(serviceType)
}

// ServiceSupportsAutoscaling returns true if the type supports verticalAutoscaling.
// All services except object-storage and shared-storage.
func ServiceSupportsAutoscaling(serviceType string) bool {
	return !IsObjectStorageType(serviceType) && !IsSharedStorageType(serviceType)
}

// IsObjectStorageType returns true for object-storage services.
// Object storage has no mode, no verticalAutoscaling — needs objectStorageSize instead.
func IsObjectStorageType(serviceType string) bool {
	return strings.HasPrefix(strings.ToLower(serviceType), "object-storage")
}

// IsSharedStorageType returns true for shared-storage services.
// Shared storage supports mode but NOT verticalAutoscaling.
func IsSharedStorageType(serviceType string) bool {
	return strings.HasPrefix(strings.ToLower(serviceType), "shared-storage")
}

// IsUtilityType returns true for utility services deployed from external repos.
// These are standalone apps (ubuntu-based) with their own buildFromGit URL and
// their own zerops.yaml in a zerops-recipe-apps repo. Currently: mailpit.
func IsUtilityType(serviceType string) bool {
	return strings.HasPrefix(strings.ToLower(serviceType), "mailpit")
}

// IsRuntimeType returns true for Zerops runtime types — the categories where
// user code executes (php-nginx, nodejs, go, bun, python, rust, nginx, static, ...).
// Defined by exclusion: not managed and not a utility. Runtime services need
// zeropsSetup + buildFromGit pointing at the recipe-app repo.
func IsRuntimeType(serviceType string) bool {
	return !IsManagedService(serviceType) && !IsUtilityType(serviceType)
}

// Canonical recipe setup names. Minimal recipes use dev+prod; showcase recipes
// add worker. All bootstrap/deploy guidance and checkers reference them.
const (
	RecipeSetupDev    = "dev"    // development workspace — idle start, full source, no healthCheck
	RecipeSetupProd   = "prod"   // production/stage/simple — real start, healthCheck
	RecipeSetupWorker = "worker" // showcase only — background job processor, no HTTP
)

// RecipeRole values for repo routing and comment generation.
// These do NOT affect template dispatch — type predicates remain authoritative.
const (
	RecipeRoleApp = "app" // frontend or single-app service (default)
	RecipeRoleAPI = "api" // backend API service (dual-runtime recipes)
)

// RecipeSetupForMode maps a bootstrap plan mode to its canonical recipe setup name.
// Standard and dev modes use the dev workspace entry; simple mode uses the prod entry.
func RecipeSetupForMode(mode Mode) string {
	if mode == PlanModeSimple {
		return RecipeSetupProd
	}
	return RecipeSetupDev
}

// SharesAppCodebase returns true when a worker target explicitly declares that
// it shares its codebase with another runtime target via SharesCodebaseWith.
//
// Semantics:
//   - true (shared codebase, opt-in): one repo, two processes (e.g. Laravel +
//     Horizon, Rails + Sidekiq). No workerdev service — the app's dev container
//     runs both processes via SSH. zeropsSetup is "worker" (a third setup in the
//     host target's shared zerops.yaml). buildFromGit inherits the host target's
//     repo (resolved by findTarget in writeRuntimeBuildFromGit).
//   - false (separate codebase, DEFAULT): own repo, own zerops.yaml with dev+prod
//     setups, own dev+stage service pair. buildFromGit points at {slug}-worker.
//
// The previous implementation used a runtime-match heuristic (same base runtime
// ⇒ shared codebase) which made the 3-repo case (e.g. Svelte frontend + NestJS
// API + NestJS worker in three separate repos with independent deploy lifecycles)
// literally unexpressible. The explicit field flips the default to separate and
// requires the agent to opt into sharing with a concrete hostname reference.
func SharesAppCodebase(target RecipeTarget) bool {
	return target.IsWorker && target.SharesCodebaseWith != ""
}

// TargetHostsSharedWorker returns true when the given non-worker runtime target's
// zerops.yaml must contain a `setup: worker` block — i.e., when at least one
// worker target in the plan explicitly names THIS target in SharesCodebaseWith.
// Separate-codebase workers have their own zerops.yaml and are not hosted by
// any app target.
func TargetHostsSharedWorker(target RecipeTarget, plan *RecipePlan) bool {
	if plan == nil || target.IsWorker || !IsRuntimeType(target.Type) {
		return false
	}
	for _, t := range plan.Targets {
		if t.IsWorker && t.SharesCodebaseWith == target.Hostname {
			return true
		}
	}
	return false
}

// findTarget returns a pointer to the named target in the plan, or nil if absent.
// Used by the template layer to resolve a shared worker's host target (for repo
// suffix inheritance). Lookup is by exact hostname match.
func findTarget(plan *RecipePlan, hostname string) *RecipeTarget {
	if plan == nil || hostname == "" {
		return nil
	}
	for i := range plan.Targets {
		if plan.Targets[i].Hostname == hostname {
			return &plan.Targets[i]
		}
	}
	return nil
}

// recipeSetupName returns the zeropsSetup name for a recipe RUNTIME service.
// The setup name depends on whether the worker shares the app codebase:
//   - "dev"    → dev entry (env 0-1 SSHFS mount)
//   - "worker" → shared-codebase worker in prod (shared zerops.yaml's worker setup)
//   - "prod"   → HTTP app in prod, OR separate-codebase worker (own zerops.yaml's prod setup)
func recipeSetupName(target RecipeTarget, isDev bool) string {
	if isDev {
		return RecipeSetupDev
	}
	// Shared-codebase worker: the shared zerops.yaml has a dedicated "worker" setup.
	if SharesAppCodebase(target) {
		return RecipeSetupWorker
	}
	// App, or separate-codebase worker (its own zerops.yaml's prod setup).
	return RecipeSetupProd
}

// serviceTypeKind returns a human-readable category label for comment generation.
// This is the ONLY place service types are grouped by category — it is not used
// for any template dispatch (dispatch uses the capability predicates above).
// Empty string for runtime types (callers fall back to the exact type name via
// dataServiceTypeName for those).
func serviceTypeKind(serviceType string) string {
	base, _, _ := strings.Cut(strings.ToLower(serviceType), "@")
	switch base {
	case svcPostgreSQL, svcMariaDB, "clickhouse":
		return kindDatabase
	case "valkey", "keydb":
		return kindCache
	case "elasticsearch", svcMeilisearch, "qdrant", "typesense":
		return kindSearchEngine
	case "object-storage", "shared-storage":
		return kindStorage
	case "nats", "kafka", "rabbitmq":
		return kindMessaging
	case "mailpit":
		return kindMailCatcher
	}
	return ""
}

// utilityBuildFromGitURL returns the buildFromGit URL for a utility service.
// Convention: zerops-recipe-apps/{type-base}-app.
func utilityBuildFromGitURL(serviceType string) string {
	base, _, _ := strings.Cut(strings.ToLower(serviceType), "@")
	return RecipeAppRepoBase + base + "-app"
}
