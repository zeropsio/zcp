package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// managedServicePrefixes lists service type prefixes for managed (non-runtime) services.
// Used by both isManagedService (project state detection) and isManagedType (plan validation).
// Source of truth: matches Zerops API type names (hyphenated: "object-storage", "shared-storage").
var managedServicePrefixes = []string{
	"postgresql", "mariadb", "mysql", "mongodb", "valkey", "redis",
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
