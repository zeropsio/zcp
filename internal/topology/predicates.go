package topology

import "strings"

// managedServicePrefixes is the static fallback for managed service classification.
// Used when live API types are unavailable. Source of truth: Zerops API categories.
// Does NOT include phantom types that don't exist on Zerops (mysql, mongodb, redis).
var managedServicePrefixes = []string{
	"postgresql", "mariadb", "valkey",
	"keydb", "elasticsearch", "meilisearch", "rabbitmq", "kafka",
	"nats", "clickhouse", "qdrant", "typesense",
	"object-storage", "shared-storage",
}

// IsManagedService checks if a service type is a managed (non-runtime) service.
func IsManagedService(serviceType string) bool {
	lower := strings.ToLower(serviceType)
	for _, prefix := range managedServicePrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// ManagedServicePrefixes returns a copy of the static managed-service prefix
// list. Exposed for callers that need to iterate the canonical set
// (e.g. coverage tests that pin every prefix has a kind mapping).
func ManagedServicePrefixes() []string {
	out := make([]string, len(managedServicePrefixes))
	copy(out, managedServicePrefixes)
	return out
}

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

// IsPushSource returns true when the Mode value names a service that can
// act as the source of a git-push or zcli-push operation. Used by handler
// validation (handleGitPush rejects targetService whose mode is not a push
// source) and by atom rendering (push-source-only atoms filter on this
// predicate via the `modes:` axis).
//
// True for: ModeStandard (dev half of standard pair), ModeSimple (single
// container service), ModeLocalStage (local CWD as source paired with
// Zerops stage), ModeLocalOnly (local CWD with no Zerops link).
//
// False for: ModeStage (build target, not source) and ModeDev (the legacy
// dev-only mode — invalid combo with push-git per the deploy-strategy
// decomposition).
//
// Spec: `plans/deploy-strategy-decomposition-2026-04-28.md` §3.2.
func IsPushSource(m Mode) bool {
	switch m {
	case ModeStandard, ModeSimple, ModeLocalStage, ModeLocalOnly:
		return true
	case ModeDev, ModeStage:
		return false
	}
	return false
}
