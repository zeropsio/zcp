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
//
//nolint:gochecknoglobals // enum-set table; value-only, not mutated.
var validBootstrapModes = map[Mode]bool{
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
	BootstrapMode Mode   `json:"bootstrapMode,omitempty"` // standard, dev, or simple
	ExplicitStage string `json:"stageHostname,omitempty"` // explicit stage hostname override for standard mode
}

// EffectiveMode returns the bootstrap mode, defaulting to standard if empty.
func (r RuntimeTarget) EffectiveMode() Mode {
	if r.BootstrapMode == "" {
		return PlanModeStandard
	}
	return r.BootstrapMode
}

// StageHostname returns the stage hostname for standard mode. ExplicitStage
// is the only source: service names are arbitrary strings, so the old
// `{base}dev → {base}stage` derivation silently misclassified repos with
// non-conforming hostnames. Returns empty for dev/simple modes OR when
// standard mode was requested without ExplicitStage; the latter is a
// caller bug — ValidateBootstrapTargets catches it with a hard error.
func (r RuntimeTarget) StageHostname() string {
	if r.EffectiveMode() != PlanModeStandard {
		return ""
	}
	return r.ExplicitStage
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

// RuntimeBase returns the base runtime name (before @) of the first target.
func (p *ServicePlan) RuntimeBase() string {
	if p == nil || len(p.Targets) == 0 {
		return ""
	}
	base, _, _ := strings.Cut(p.Targets[0].Runtime.Type, "@")
	return base
}

// DependencyTypes returns unique dependency types across all targets.
func (p *ServicePlan) DependencyTypes() []string {
	if p == nil {
		return nil
	}
	seen := make(map[string]bool)
	var types []string
	for _, t := range p.Targets {
		for _, d := range t.Dependencies {
			if !seen[d.Type] {
				seen[d.Type] = true
				types = append(types, d.Type)
			}
		}
	}
	return types
}

// IsAllExisting returns true when every target runtime has IsExisting=true
// and every dependency has resolution EXISTS. This signals a pure adoption
// plan where no new services need to be created.
func (p *ServicePlan) IsAllExisting() bool {
	if p == nil || len(p.Targets) == 0 {
		return false
	}
	for _, t := range p.Targets {
		if !t.Runtime.IsExisting {
			return false
		}
		for _, d := range t.Dependencies {
			if d.Resolution != ResolutionExists {
				return false
			}
		}
	}
	return true
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
	return IsManagedService(serviceType)
}

// ValidateBootstrapTargets validates a list of bootstrap targets against constraints.
// liveTypes may be nil — type checking is skipped when unavailable.
// liveServices may be nil — CREATE/EXISTS checks are skipped when unavailable.
// Returns the list of dependency hostnames that had mode auto-defaulted to NON_HA.
func ValidateBootstrapTargets(targets []BootstrapTarget, liveTypes []platform.ServiceStackType, liveServices []platform.ServiceStack) ([]string, error) {
	// Empty targets allowed for managed-only projects (no runtime services).
	if len(targets) == 0 {
		return nil, nil
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

		// Standard-mode targets must carry an explicit stageHostname.
		// Hostnames are arbitrary strings; ZCP refuses to guess a stage
		// pair from dev-hostname structure.
		var stageHostname string
		if rt.EffectiveMode() == PlanModeStandard {
			stageHostname = rt.StageHostname()
			if stageHostname == "" {
				errs = append(errs, fmt.Sprintf("target %q: standard mode requires explicit stageHostname", rt.DevHostname))
				continue
			}
			if err := ValidatePlanHostname(stageHostname); err != nil {
				errs = append(errs, fmt.Sprintf("target %q: stageHostname %q: %v", rt.DevHostname, stageHostname, err))
				continue
			}
		}

		// Runtime hostname collision check — symmetric with the dependency
		// resolution check below. Extracted because inline-ing the two-pair
		// (dev + stage) × two-direction (classic/adopt) matrix pushes the
		// enclosing function over the maintainability-index lint threshold.
		if liveServices != nil {
			if collisionErr := runtimeCollisionError(rt, stageHostname, liveServiceNames); collisionErr != "" {
				errs = append(errs, collisionErr)
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

// runtimeCollisionError returns a diagnostic string when a target's runtime
// hostnames conflict with (or disagree with) the project's live service set,
// or the empty string when the target is consistent. Pairs the dev and stage
// checks so callers only pay one cost.
//
// Classic plan + hostname already live → "exists, use adopt or pick a
// non-colliding hostname". Adopt plan + hostname missing → "isExisting=true
// but not found". Recipe route: see bootstrap-recipe-match atom for the
// rename flow — the error wording stays generic because the same function
// serves all routes.
func runtimeCollisionError(rt RuntimeTarget, stageHostname string, liveServiceNames map[string]bool) string {
	if liveServiceNames[rt.DevHostname] && !rt.IsExisting {
		return fmt.Sprintf("target %q: runtime already exists in project — set isExisting=true to adopt it, or pick a non-colliding devHostname (recipe route: ZCP rewrites the import YAML using your plan's hostnames)", rt.DevHostname)
	}
	if !liveServiceNames[rt.DevHostname] && rt.IsExisting {
		return fmt.Sprintf("target %q: isExisting=true but runtime not found in project", rt.DevHostname)
	}
	if stageHostname == "" {
		return ""
	}
	if liveServiceNames[stageHostname] && !rt.IsExisting {
		return fmt.Sprintf("target %q: stage runtime %q already exists — set isExisting=true to adopt, or pick a non-colliding stageHostname (recipe route: ZCP rewrites the import YAML using your plan's hostnames)", rt.DevHostname, stageHostname)
	}
	if !liveServiceNames[stageHostname] && rt.IsExisting {
		return fmt.Sprintf("target %q: isExisting=true but stage runtime %q not found in project", rt.DevHostname, stageHostname)
	}
	return ""
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
