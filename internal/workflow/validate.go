package workflow

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
)

// ServicePlan is the structured plan submitted during the bootstrap "plan" step.
type ServicePlan struct {
	Services  []PlannedService `json:"services"`
	CreatedAt string           `json:"createdAt"`
}

// PlannedService describes one service in the bootstrap plan.
type PlannedService struct {
	Hostname string `json:"hostname"`
	Type     string `json:"type"`
	Mode     string `json:"mode,omitempty"` // NON_HA or HA, defaults to NON_HA for managed services
}

// ValidatePlanHostname checks that a hostname matches Zerops constraints.
// Delegates to platform.ValidateHostname for canonical validation rules.
func ValidatePlanHostname(hostname string) error {
	if err := platform.ValidateHostname(hostname); err != nil {
		return fmt.Errorf("%s", err.Message)
	}
	return nil
}

// isManagedTypeWithLive checks if a service type requires a Mode field.
// Uses live API categories when available, falls back to static prefixes.
func isManagedTypeWithLive(serviceType string, liveManaged map[string]bool) bool {
	base, _, _ := strings.Cut(serviceType, "@")
	if len(liveManaged) > 0 {
		return liveManaged[base]
	}
	return isManagedService(serviceType)
}

// ValidateServicePlan validates a list of planned services against constraints.
// liveTypes may be nil — type checking is skipped when unavailable.
// Returns the list of hostnames that had mode auto-defaulted to NON_HA.
// Collects all errors and returns them as a batch.
func ValidateServicePlan(services []PlannedService, liveTypes []platform.ServiceStackType) ([]string, error) {
	if len(services) == 0 {
		return nil, fmt.Errorf("plan must contain at least one service")
	}

	// Derive managed base names from live types when available.
	liveManaged := knowledge.ManagedBaseNames(liveTypes)

	var errs []string
	var defaulted []string
	seen := make(map[string]bool, len(services))
	for i, svc := range services {
		if err := ValidatePlanHostname(svc.Hostname); err != nil {
			errs = append(errs, fmt.Sprintf("service %q: %v", svc.Hostname, err))
			continue
		}
		if seen[svc.Hostname] {
			errs = append(errs, fmt.Sprintf("duplicate hostname %q", svc.Hostname))
			continue
		}
		seen[svc.Hostname] = true

		if svc.Type == "" {
			errs = append(errs, fmt.Sprintf("service %q has empty type", svc.Hostname))
			continue
		}

		// Type check against live catalog.
		if liveTypes != nil {
			if !typeExists(svc.Type, liveTypes) {
				errs = append(errs, fmt.Sprintf("service %q type %q not found in available service types", svc.Hostname, svc.Type))
				continue
			}
		}

		// Managed services: defaults to NON_HA for managed services.
		if isManagedTypeWithLive(svc.Type, liveManaged) {
			if svc.Mode == "" {
				services[i].Mode = "NON_HA"
				defaulted = append(defaulted, svc.Hostname)
			} else if svc.Mode != "HA" && svc.Mode != "NON_HA" {
				errs = append(errs, fmt.Sprintf("service %q mode %q must be HA or NON_HA", svc.Hostname, svc.Mode))
			}
		}
	}

	if len(errs) > 0 {
		if len(errs) == 1 {
			return nil, fmt.Errorf("%s", errs[0])
		}
		return nil, fmt.Errorf("%d validation errors:\n- %s", len(errs), strings.Join(errs, "\n- "))
	}
	return defaulted, nil
}

// typeExists checks if a version name exists in the live type catalog.
func typeExists(versionName string, types []platform.ServiceStackType) bool {
	for _, st := range types {
		for _, v := range st.Versions {
			if v.Name == versionName {
				return true
			}
		}
	}
	return false
}
