package workflow

import (
	"fmt"
	"regexp"
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
	Mode     string `json:"mode,omitempty"` // NON_HA or HA, required for managed services
}

var hostnameRe = regexp.MustCompile(`^[a-z0-9]{1,25}$`)

// ValidatePlanHostname checks that a hostname matches Zerops constraints.
func ValidatePlanHostname(hostname string) error {
	if hostname == "" {
		return fmt.Errorf("hostname is empty")
	}
	if len(hostname) > 25 {
		return fmt.Errorf("hostname %q exceeds 25 characters", hostname)
	}
	if !hostnameRe.MatchString(hostname) {
		return fmt.Errorf("hostname %q has invalid characters (only lowercase a-z and 0-9 allowed)", hostname)
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
func ValidateServicePlan(services []PlannedService, liveTypes []platform.ServiceStackType) error {
	if len(services) == 0 {
		return fmt.Errorf("plan must contain at least one service")
	}

	// Derive managed base names from live types when available.
	liveManaged := knowledge.ManagedBaseNames(liveTypes)

	seen := make(map[string]bool, len(services))
	for _, svc := range services {
		if err := ValidatePlanHostname(svc.Hostname); err != nil {
			return fmt.Errorf("service %q: %w", svc.Hostname, err)
		}
		if seen[svc.Hostname] {
			return fmt.Errorf("duplicate hostname %q", svc.Hostname)
		}
		seen[svc.Hostname] = true

		if svc.Type == "" {
			return fmt.Errorf("service %q has empty type", svc.Hostname)
		}

		// Type check against live catalog.
		if liveTypes != nil {
			if !typeExists(svc.Type, liveTypes) {
				return fmt.Errorf("service %q type %q not found in available service types", svc.Hostname, svc.Type)
			}
		}

		// Managed services require Mode.
		if isManagedTypeWithLive(svc.Type, liveManaged) {
			if svc.Mode == "" {
				return fmt.Errorf("service %q (type %q) requires mode (HA or NON_HA)", svc.Hostname, svc.Type)
			}
			if svc.Mode != "HA" && svc.Mode != "NON_HA" {
				return fmt.Errorf("service %q mode %q must be HA or NON_HA", svc.Hostname, svc.Mode)
			}
		}
	}
	return nil
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
