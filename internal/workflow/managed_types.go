package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// managedServicePrefixes is the static fallback for managed service classification.
// Used when live API types are unavailable. Source of truth: Zerops API categories.
// Does NOT include phantom types that don't exist on Zerops (mysql, mongodb, redis).
var managedServicePrefixes = []string{
	"postgresql", "mariadb", "valkey",
	"keydb", "elasticsearch", "meilisearch", "rabbitmq", "kafka",
	"nats", "clickhouse", "qdrant", "typesense",
	"object-storage", "shared-storage",
}

// isManagedService checks if a service type is a managed (non-runtime) service.
func isManagedService(serviceType string) bool {
	lower := strings.ToLower(serviceType)
	for _, prefix := range managedServicePrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// DetectProjectState determines the project state based on service inventory.
func DetectProjectState(ctx context.Context, client platform.Client, projectID string) (ProjectState, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return "", fmt.Errorf("detect project state: %w", err)
	}

	// Filter to runtime services only.
	var runtimeServices []platform.ServiceStack
	for _, svc := range services {
		if !isManagedService(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
			runtimeServices = append(runtimeServices, svc)
		}
	}

	if len(runtimeServices) == 0 {
		return StateFresh, nil
	}

	// Check for dev/stage naming pattern.
	if hasDevStagePattern(runtimeServices) {
		return StateConformant, nil
	}

	return StateNonConformant, nil
}

// hasDevStagePattern checks if any service names follow the dev/stage naming convention.
func hasDevStagePattern(services []platform.ServiceStack) bool {
	names := make(map[string]bool, len(services))
	for _, svc := range services {
		names[svc.Name] = true
	}

	suffixes := []struct{ dev, stage string }{
		{"dev", "stage"},
	}

	for name := range names {
		for _, sf := range suffixes {
			if base, ok := strings.CutSuffix(name, sf.dev); ok {
				if names[base+sf.stage] {
					return true
				}
			}
		}
	}
	return false
}
