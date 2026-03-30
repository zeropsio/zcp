package workflow

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
