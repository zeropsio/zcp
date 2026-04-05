package workflow

import (
	"fmt"
	"strings"
)

// DeriveRole computes the canonical recipe role for a service from its Zerops
// type. The role is an internal template-dispatch key — it is NOT submitted by
// the agent, because it is fully determined by the service type:
//
//   - Utility types (mailpit, ...)                               → mail
//   - Managed DBs (postgresql, mariadb, clickhouse)              → db
//   - Caches (valkey, keydb)                                     → cache
//   - Search engines (elasticsearch, meilisearch, qdrant, ...)   → search
//   - Storage (object-storage, shared-storage)                   → storage
//   - Messaging (nats, kafka, rabbitmq)                          → mail
//   - Runtime types (php-nginx, nodejs, go, ...)                 → app | worker
//
// For runtime types, isWorker distinguishes the HTTP-serving primary app
// (isWorker=false, the default) from background/queue workers (isWorker=true).
// For all non-runtime types, isWorker is ignored.
//
// Returns an error if the service type has no role mapping — which means
// either an unknown managed type or a type outside all known categories.
// Validation surfaces this to the agent at plan submission time.
func DeriveRole(serviceType string, isWorker bool) (string, error) {
	if serviceType == "" {
		return "", fmt.Errorf("empty service type")
	}

	// Utility first: mailpit is ubuntu-based (looks like a runtime) but behaves
	// as a messaging service with its own buildFromGit URL.
	if IsUtilityType(serviceType) {
		return RecipeRoleMail, nil
	}

	base, _, _ := strings.Cut(strings.ToLower(serviceType), "@")

	if IsManagedService(serviceType) {
		switch {
		case managedRolePrefixes["db"][base]:
			return RecipeRoleDB, nil
		case managedRolePrefixes["cache"][base]:
			return RecipeRoleCache, nil
		case managedRolePrefixes["search"][base]:
			return RecipeRoleSearch, nil
		case managedRolePrefixes["storage"][base]:
			return RecipeRoleStorage, nil
		case managedRolePrefixes["mail"][base]:
			return RecipeRoleMail, nil
		default:
			// Managed but not categorized — new managed type added to the
			// platform that recipe_role_derive doesn't know about yet.
			return "", fmt.Errorf("managed service type %q has no role mapping — add to managedRolePrefixes", serviceType)
		}
	}

	// Everything else is a runtime type (build/run container). The agent's
	// only role choice: is this the primary HTTP-serving app, or a worker?
	if isWorker {
		return RecipeRoleWorker, nil
	}
	return RecipeRoleApp, nil
}

// managedRolePrefixes maps canonical role → set of lowercase type-prefixes
// (the part before '@'). Updated when Zerops adds new managed service types.
// Prefixes here MUST also appear in managedServicePrefixes (managed_types.go) —
// otherwise IsManagedService returns false and the derivation falls through
// to the runtime branch.
var managedRolePrefixes = map[string]map[string]bool{
	"db": {
		"postgresql": true,
		"mariadb":    true,
		"clickhouse": true,
	},
	"cache": {
		"valkey": true,
		"keydb":  true,
	},
	"search": {
		"elasticsearch": true,
		"meilisearch":   true,
		"qdrant":        true,
		"typesense":     true,
	},
	"storage": {
		"object-storage": true,
		"shared-storage": true,
	},
	"mail": {
		"nats":     true,
		"kafka":    true,
		"rabbitmq": true,
	},
}
