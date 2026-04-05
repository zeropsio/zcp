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

// recipeSetupName returns the zeropsSetup name for a recipe RUNTIME service:
//   - "dev"    → the dev entry that env 0-1 mounts on SSHFS
//   - "worker" → background/queue worker runtime in prod
//   - "prod"   → the HTTP-serving primary app runtime in prod
//
// Not valid for managed or utility services (they use different setup names
// or no setup at all).
func recipeSetupName(isWorker, isDev bool) string {
	if isDev {
		return "dev"
	}
	if isWorker {
		return "worker"
	}
	return "prod"
}

// serviceTypeKind returns a human-readable category label for comment generation.
// This is the ONLY place service types are grouped by category — it is not used
// for any template dispatch (dispatch uses the capability predicates above).
// Empty string for runtime types (callers fall back to the exact type name via
// dataServiceTypeName for those).
func serviceTypeKind(serviceType string) string {
	base, _, _ := strings.Cut(strings.ToLower(serviceType), "@")
	switch base {
	case "postgresql", "mariadb", "clickhouse": //nolint:goconst // service-type literals, not worth extracting
		return "database"
	case "valkey", "keydb":
		return "cache"
	case "elasticsearch", "meilisearch", "qdrant", "typesense": //nolint:goconst // service-type literals
		return "search engine"
	case "object-storage", "shared-storage":
		return "storage"
	case "nats", "kafka", "rabbitmq":
		return "messaging"
	case "mailpit":
		return "mail catcher"
	}
	return ""
}

// utilityBuildFromGitURL returns the buildFromGit URL for a utility service.
// Convention: zerops-recipe-apps/{type-base}-app.
func utilityBuildFromGitURL(serviceType string) string {
	base, _, _ := strings.Cut(strings.ToLower(serviceType), "@")
	return RecipeAppRepoBase + base + "-app"
}
