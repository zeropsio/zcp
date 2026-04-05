package workflow

import "strings"

// Service-type name constants — shared across template/dispatch helpers that
// would otherwise trip goconst on repeated literals.
const (
	svcPostgreSQL  = "postgresql"
	svcMariaDB     = "mariadb"
	svcMeilisearch = "meilisearch"
)

// managedServicePrefixes is the static fallback for managed service classification.
// Used when live API types are unavailable. Source of truth: Zerops API categories.
// Does NOT include phantom types that don't exist on Zerops (mysql, mongodb, redis).
var managedServicePrefixes = []string{
	svcPostgreSQL, svcMariaDB, "valkey",
	"keydb", "elasticsearch", svcMeilisearch, "rabbitmq", "kafka",
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
