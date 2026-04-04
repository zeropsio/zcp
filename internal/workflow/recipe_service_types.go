package workflow

import "strings"

// Service type capabilities — determines which import.yaml fields are valid
// for a given Zerops service type. These are type-based (not role-based)
// because the same role can map to different service types with different
// field support (e.g., "storage" role → object-storage or shared-storage).

// ServiceSupportsMode returns true if the service type supports mode: HA/NON_HA.
// Managed services (db, cache, search, messaging) support mode.
// Object-storage does NOT — it's always replicated internally.
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
// These are standalone apps (not part of the recipe codebase) with their own
// buildFromGit URL and zerops.yaml. Currently: mailpit.
func IsUtilityType(serviceType string) bool {
	return strings.HasPrefix(strings.ToLower(serviceType), "mailpit")
}

// recipeSetupName returns the zeropsSetup name for a recipe service.
// Convention: dev services use "dev", prod workers use "worker", prod apps use "prod".
func recipeSetupName(role string, isDev bool) string {
	if isDev {
		return "dev"
	}
	if role == RecipeRoleWorker {
		return "worker"
	}
	return "prod"
}

// managedServiceKind returns a human-readable kind label for comments.
func managedServiceKind(role string) string {
	switch role {
	case "db":
		return "database"
	case "cache": //nolint:goconst // role string, not a shared constant
		return "cache"
	case "search": //nolint:goconst // role string, not a shared constant
		return "search engine"
	default:
		return "service"
	}
}

// utilityBuildFromGitURL returns the buildFromGit URL for a utility service.
// Convention: zerops-recipe-apps/{type-base}-app.
func utilityBuildFromGitURL(serviceType string) string {
	base, _, _ := strings.Cut(strings.ToLower(serviceType), "@")
	return RecipeAppRepoBase + base + "-app"
}
