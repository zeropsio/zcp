package workflow

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
)

// HA mode constants.
const (
	ModeHA    = "HA"
	ModeNonHA = "NON_HA"
)

// Dependency resolution constants.
const (
	ResolutionCreate = "CREATE"
	ResolutionExists = "EXISTS"
	ResolutionShared = "SHARED"
)

// validBootstrapModes is the set of valid BootstrapMode values (including empty for default).
var validBootstrapModes = map[string]bool{
	"": true, PlanModeStandard: true, PlanModeDev: true, PlanModeSimple: true,
}

// BootstrapTarget represents one runtime service and its dependencies in the bootstrap plan.
type BootstrapTarget struct {
	Runtime      RuntimeTarget `json:"runtime"`
	Dependencies []Dependency  `json:"dependencies,omitempty"`
}

// RuntimeTarget describes a runtime service to bootstrap.
type RuntimeTarget struct {
	DevHostname   string `json:"devHostname"`
	Type          string `json:"type"`
	IsExisting    bool   `json:"isExisting,omitempty"`
	BootstrapMode string `json:"bootstrapMode,omitempty"` // standard, dev, or simple
}

// EffectiveMode returns the bootstrap mode, defaulting to standard if empty.
func (r RuntimeTarget) EffectiveMode() string {
	if r.BootstrapMode == "" {
		return PlanModeStandard
	}
	return r.BootstrapMode
}

// StageHostname derives the stage hostname from the dev hostname.
// Returns empty for dev/simple modes or when the hostname doesn't end in "dev".
func (r RuntimeTarget) StageHostname() string {
	if r.EffectiveMode() != PlanModeStandard {
		return ""
	}
	if base, ok := strings.CutSuffix(r.DevHostname, "dev"); ok {
		return base + "stage"
	}
	return ""
}

// Dependency describes a service dependency for a bootstrap target.
type Dependency struct {
	Hostname   string `json:"hostname"`
	Type       string `json:"type"`
	Mode       string `json:"mode,omitempty"` // NON_HA or HA, defaults to NON_HA for managed services
	Resolution string `json:"resolution"`     // CREATE, EXISTS, SHARED
}

// ServicePlan is the structured plan submitted during the bootstrap "plan" step.
type ServicePlan struct {
	Targets   []BootstrapTarget `json:"targets"`
	CreatedAt string            `json:"createdAt"`
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

// ValidateBootstrapTargets validates a list of bootstrap targets against constraints.
// liveTypes may be nil — type checking is skipped when unavailable.
// liveServices may be nil — CREATE/EXISTS checks are skipped when unavailable.
// Returns the list of dependency hostnames that had mode auto-defaulted to NON_HA.
func ValidateBootstrapTargets(targets []BootstrapTarget, liveTypes []platform.ServiceStackType, liveServices []platform.ServiceStack) ([]string, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("plan must contain at least one target")
	}

	liveManaged := knowledge.ManagedBaseNames(liveTypes)

	// Build set of live service hostnames for CREATE/EXISTS validation.
	liveServiceNames := make(map[string]bool, len(liveServices))
	for _, svc := range liveServices {
		liveServiceNames[svc.Name] = true
	}

	// Collect all CREATE hostnames across targets for SHARED validation.
	createHostnames := make(map[string]bool)
	for _, target := range targets {
		for _, dep := range target.Dependencies {
			if dep.Resolution == ResolutionCreate {
				createHostnames[dep.Hostname] = true
			}
		}
	}

	var errs []string
	var defaulted []string

	for i, target := range targets {
		rt := target.Runtime

		// Validate dev hostname.
		if err := ValidatePlanHostname(rt.DevHostname); err != nil {
			errs = append(errs, fmt.Sprintf("target %q: %v", rt.DevHostname, err))
			continue
		}

		// Validate bootstrap mode.
		if !validBootstrapModes[rt.BootstrapMode] {
			errs = append(errs, fmt.Sprintf("target %q: invalid bootstrapMode %q (must be standard, dev, or simple)", rt.DevHostname, rt.BootstrapMode))
			continue
		}

		// Validate runtime type against live catalog.
		if rt.Type == "" {
			errs = append(errs, fmt.Sprintf("target %q has empty type", rt.DevHostname))
			continue
		}
		if liveTypes != nil && !typeExists(rt.Type, liveTypes) {
			errs = append(errs, fmt.Sprintf("target %q type %q not found in available service types", rt.DevHostname, rt.Type))
			continue
		}

		// H7: validate derived stage hostname length for standard mode.
		if rt.EffectiveMode() == PlanModeStandard {
			stageHostname := rt.StageHostname()
			if stageHostname == "" {
				errs = append(errs, fmt.Sprintf("target %q: dev hostname must end in 'dev' for standard mode (or set bootstrapMode)", rt.DevHostname))
				continue
			}
			if err := ValidatePlanHostname(stageHostname); err != nil {
				errs = append(errs, fmt.Sprintf("target %q: derived stage hostname %q: %v", rt.DevHostname, stageHostname, err))
				continue
			}
		}

		// Validate dependencies.
		depSeen := make(map[string]bool, len(target.Dependencies))
		for j, dep := range target.Dependencies {
			if err := ValidatePlanHostname(dep.Hostname); err != nil {
				errs = append(errs, fmt.Sprintf("target %q dependency %q: %v", rt.DevHostname, dep.Hostname, err))
				continue
			}
			if depSeen[dep.Hostname] {
				errs = append(errs, fmt.Sprintf("target %q: duplicate dependency hostname %q", rt.DevHostname, dep.Hostname))
				continue
			}
			depSeen[dep.Hostname] = true

			if dep.Type == "" {
				errs = append(errs, fmt.Sprintf("target %q dependency %q has empty type", rt.DevHostname, dep.Hostname))
				continue
			}
			if liveTypes != nil && !typeExists(dep.Type, liveTypes) {
				errs = append(errs, fmt.Sprintf("target %q dependency %q type %q not found in available service types", rt.DevHostname, dep.Hostname, dep.Type))
				continue
			}

			// Normalize resolution to uppercase (LLMs send mixed case).
			targets[i].Dependencies[j].Resolution = strings.ToUpper(dep.Resolution)
			dep = targets[i].Dependencies[j]

			// Resolution validation.
			switch dep.Resolution {
			case ResolutionCreate:
				if liveServices != nil && liveServiceNames[dep.Hostname] {
					errs = append(errs, fmt.Sprintf("target %q dependency %q: CREATE but service already exists", rt.DevHostname, dep.Hostname))
					continue
				}
			case ResolutionExists:
				if liveServices != nil && !liveServiceNames[dep.Hostname] {
					errs = append(errs, fmt.Sprintf("target %q dependency %q: EXISTS but service not found in project", rt.DevHostname, dep.Hostname))
					continue
				}
			case ResolutionShared:
				if !createHostnames[dep.Hostname] {
					errs = append(errs, fmt.Sprintf("target %q dependency %q: SHARED resolution requires another target to CREATE it", rt.DevHostname, dep.Hostname))
					continue
				}
			default:
				errs = append(errs, fmt.Sprintf("target %q dependency %q: invalid resolution %q (must be CREATE, EXISTS, or SHARED)", rt.DevHostname, dep.Hostname, dep.Resolution))
				continue
			}

			// Normalize mode to uppercase (LLMs send mixed case).
			if dep.Mode != "" {
				targets[i].Dependencies[j].Mode = strings.ToUpper(dep.Mode)
				dep = targets[i].Dependencies[j]
			}

			// Mode defaulting for managed services.
			if isManagedTypeWithLive(dep.Type, liveManaged) {
				if dep.Mode == "" {
					targets[i].Dependencies[j].Mode = ModeNonHA
					defaulted = append(defaulted, dep.Hostname)
				} else if dep.Mode != ModeHA && dep.Mode != ModeNonHA {
					errs = append(errs, fmt.Sprintf("target %q dependency %q mode %q must be HA or NON_HA", rt.DevHostname, dep.Hostname, dep.Mode))
				}
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
