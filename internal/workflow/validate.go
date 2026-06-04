package workflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
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

// validBootstrapModes is the set of valid BootstrapMode values. Empty is NOT
// valid: bootstrapMode is required on every runtime target. The earlier
// empty→standard default trapped agents who omitted mode for a single dev
// container — they hit "standard mode requires explicit stageHostname" with
// no link to the actual cause (missing mode).
//
//nolint:gochecknoglobals // enum-set table; value-only, not mutated.
var validBootstrapModes = map[topology.Mode]bool{
	topology.PlanModeStandard: true, topology.PlanModeDev: true, topology.PlanModeSimple: true,
}

// ValidBootstrapModes returns the accepted bootstrapMode values. Single owner
// for the bootstrap-plan mode enum — tool-schema drift tests pin the schema
// prose against this set so a new mode cannot go unsurfaced.
func ValidBootstrapModes() []string {
	out := make([]string, 0, len(validBootstrapModes))
	for m := range validBootstrapModes {
		out = append(out, string(m))
	}
	sort.Strings(out) // deterministic — map iteration order is not stable
	return out
}

// BootstrapTarget represents one runtime service and its dependencies in the bootstrap plan.
type BootstrapTarget struct {
	Runtime      RuntimeTarget `json:"runtime"`
	Dependencies []Dependency  `json:"dependencies,omitempty"`
}

// flattenedRuntimeFields are RuntimeTarget keys that agents reflexively place
// at the top level of a plan target. json.Unmarshal silently drops unknown
// keys, so a flatten goes undetected and the agent later sees a confusing
// mode/stage error. UnmarshalJSON catches them at parse time with an
// actionable diagnostic.
//
//nolint:gochecknoglobals // value-only enum table.
var flattenedRuntimeFields = []string{"bootstrapMode", "stageHostname", "devHostname", "type", "isExisting"}

// UnmarshalJSON hard-rejects flattened RuntimeTarget fields at the top level
// of a plan target. The actionable diagnostic returns the complete corrected
// target JSON (with leaked keys folded into "runtime") so the agent can
// paste-and-resend in one turn instead of hand-reconstructing the nested
// shape from a one-field example. All flattened fields are reported in one
// error so the fix lands in one round-trip.
func (t *BootstrapTarget) UnmarshalJSON(data []byte) error {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	var leaked []string
	for _, key := range flattenedRuntimeFields {
		if _, ok := probe[key]; ok {
			leaked = append(leaked, key)
		}
	}
	if len(leaked) > 0 {
		return fmt.Errorf("plan target: flattened fields [%s] must nest inside runtime. Resubmit this target as:\n%s",
			strings.Join(leaked, ", "), renderCorrectedTarget(probe, leaked))
	}
	type alias BootstrapTarget
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*t = BootstrapTarget(a)
	return nil
}

// renderCorrectedTarget folds leaked top-level keys into "runtime" and
// returns the marshalled corrected target shape. Existing nested runtime
// fields take precedence when a key appears in both flat and nested form
// (presence of flat is the bug; the nested copy reflects the agent's
// considered intent). Best-effort: marshal errors fall back to a static
// nested-shape stub so the error still names the corrected shape.
func renderCorrectedTarget(probe map[string]json.RawMessage, leaked []string) string {
	runtimeMap := make(map[string]json.RawMessage)
	if raw, ok := probe["runtime"]; ok {
		_ = json.Unmarshal(raw, &runtimeMap)
	}
	for _, key := range leaked {
		if _, exists := runtimeMap[key]; !exists {
			runtimeMap[key] = probe[key]
		}
	}
	runtimeBytes, err := json.Marshal(runtimeMap)
	if err != nil {
		return `{"runtime":{<leaked keys + existing runtime fields>}}`
	}
	corrected := map[string]json.RawMessage{"runtime": runtimeBytes}
	if deps, ok := probe["dependencies"]; ok {
		corrected["dependencies"] = deps
	}
	out, err := json.MarshalIndent(corrected, "", "  ")
	if err != nil {
		return `{"runtime":{<leaked keys + existing runtime fields>}}`
	}
	return string(out)
}

// RuntimeTarget describes a runtime service to bootstrap.
//
// Os is a BC-only field (Sunday-release 2026-05-18 deprecated splitting
// type vs OS). The canonical shape is the composite Type identifier
// (`alpine/nodejs@22`); legacy callers sending `type: nodejs@22` plus
// `os: alpine` are normalized at validation time. New code should
// populate Type with the composite identifier directly and leave Os
// empty.
type RuntimeTarget struct {
	DevHostname   string        `json:"devHostname"`
	Type          string        `json:"type"`
	Os            string        `json:"os,omitempty"` // BC: legacy os sibling, normalized into Type
	IsExisting    bool          `json:"isExisting,omitempty"`
	BootstrapMode topology.Mode `json:"bootstrapMode"`           // required: standard, dev, or simple
	ExplicitStage string        `json:"stageHostname,omitempty"` // explicit stage hostname for standard mode
	// StageType is the stage half's runtime type when it differs from Type
	// (cross-type pair, e.g. dev nodejs → stage static). Empty means the
	// stage half is the same type as Type. Derive-only (DeriveRecipePlan sets
	// it from the recipe shape); agent-authored plans leave it empty.
	StageType string `json:"stageType,omitempty"`
	// ServesHTTP records whether this runtime serves HTTP, known from the
	// recipe shape's RoleKind at parse time (false for a zeropsSetup:worker).
	// nil = unknown (agent-authored / adopt plans leave it nil and let deploy
	// discover it from HasPorts). Derive-only; flows to ServiceMeta.ServesHTTP
	// so verify classifies a worker correctly on the first call. (R3-P4)
	ServesHTTP *bool `json:"servesHttp,omitempty"`
	// PrimarySetupName / StageSetupName carry the recipe runtime's LITERAL
	// zeropsSetup string for the primary (dev/lone) and stage halves — the
	// single owner of the setup name (a worker's is "worker", not the "prod"
	// the old mode→convention table returned). Empty on agent-authored/adopt
	// plans. Derive-only; flows to ServiceMeta.{Primary,Stage}SetupName. (R3-P4)
	PrimarySetupName string `json:"primarySetupName,omitempty"`
	StageSetupName   string `json:"stageSetupName,omitempty"`
}

// StageEffectiveType returns the stage half's runtime type: StageType when set
// (cross-type pair, e.g. nodejs dev → static stage), else Type. Consumers that
// validate/check/snapshot the stage half MUST use this — reading Type directly
// mis-types a cross-type stage as the dev type (R3-P4 / Codex #4).
func (r RuntimeTarget) StageEffectiveType() string {
	if r.StageType != "" {
		return r.StageType
	}
	return r.Type
}

// EffectiveMode returns the bootstrap mode. Empty is no longer mapped to
// standard — it indicates a missing required field caught by
// ValidateBootstrapTargets.
func (r RuntimeTarget) EffectiveMode() topology.Mode {
	return r.BootstrapMode
}

// StageHostname returns the stage hostname for standard mode. ExplicitStage
// is the only source: service names are arbitrary strings, so the old
// `{base}dev → {base}stage` derivation silently misclassified repos with
// non-conforming hostnames. Returns empty for dev/simple modes OR when
// standard mode was requested without ExplicitStage; the latter is a
// caller bug — ValidateBootstrapTargets catches it with a hard error.
func (r RuntimeTarget) StageHostname() string {
	if r.EffectiveMode() != topology.PlanModeStandard {
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
// Uses the schema-derived managed-base set when available, falls back to
// static topology classification.
//
// Both sides key on topology.CanonicalBaseName (OS prefix, mode suffix, and
// version stripped, storage spellings normalized), so a bare-form plan dep
// (`postgresql@18`) matches a composite-only schema set (`postgresql:single@18`
// → `postgresql`) and `sharedstorage:ha` matches `shared-storage` — without
// this symmetric keying the dep would silently miss the set and skip mode
// defaulting / HA validation downstream.
func isManagedTypeWithLive(serviceType string, liveManaged map[string]bool) bool {
	if len(liveManaged) > 0 {
		return liveManaged[topology.CanonicalBaseName(serviceType)]
	}
	return topology.IsManagedService(serviceType)
}

// ValidateBootstrapTargets validates a list of bootstrap targets against constraints.
// schemas may be nil — type existence checking is skipped when unavailable
// (managed detection then falls back to static topology).
// liveServices may be nil — CREATE/EXISTS checks are skipped when unavailable.
// Returns the list of dependency hostnames that had mode auto-defaulted to NON_HA.
//
// Composite of plan validation rules; each rule is independently extractable
// but the validation loop pattern is the cohesive shape — splitting would
// scatter the "iterate every target, iterate every dep, validate each
// constraint" structure across helpers.
func ValidateBootstrapTargets(targets []BootstrapTarget, schemas *schema.Schemas, liveServices []platform.ServiceStack) ([]string, error) {
	// Empty targets allowed for managed-only projects (no runtime services).
	if len(targets) == 0 {
		return nil, nil
	}

	var liveManaged map[string]bool
	if schemas != nil {
		liveManaged = schemas.ManagedBaseNames()
	}

	// Build set of live service hostnames for CREATE/EXISTS validation.
	liveServiceNames := make(map[string]bool, len(liveServices))
	for _, svc := range liveServices {
		liveServiceNames[svc.Name] = true
	}

	// Collect all CREATE + EXISTS hostnames across targets for SHARED
	// validation. SHARED is the plan-level "this dep is shared across
	// targets, don't redefine it" marker — the OWNER target can be
	// either CREATE (greenfield) or EXISTS (adopt scenarios). Pre-fix
	// the validator required CREATE specifically, which broke adopt-
	// only plans where multiple existing runtimes share live managed
	// deps (Karel's Laravel showcase: workerstage shares db/redis/
	// search/storage with appdev pair — all four targets IsExisting=
	// true, so no CREATE exists to anchor SHARED → 4 false-positive
	// validation errors).
	sharedAnchors := make(map[string]bool)
	for _, target := range targets {
		for _, dep := range target.Dependencies {
			if dep.Resolution == ResolutionCreate || dep.Resolution == ResolutionExists {
				sharedAnchors[dep.Hostname] = true
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

		// Validate bootstrap mode (required, no default).
		if rt.BootstrapMode == "" {
			errs = append(errs, fmt.Sprintf("target %q: runtime.bootstrapMode is required: dev, simple, or standard", rt.DevHostname))
			continue
		}
		if !validBootstrapModes[rt.BootstrapMode] {
			errs = append(errs, fmt.Sprintf("target %q: invalid bootstrapMode %q (must be standard, dev, or simple)", rt.DevHostname, rt.BootstrapMode))
			continue
		}

		// Validate runtime type against live catalog.
		if rt.Type == "" {
			errs = append(errs, fmt.Sprintf("target %q has empty type", rt.DevHostname))
			continue
		}
		if schemas != nil && !schemas.HasServiceType(rt.Type) {
			errs = append(errs, fmt.Sprintf("target %q type %q not found in available service types", rt.DevHostname, rt.Type))
			continue
		}

		// Standard-mode targets must carry an explicit stageHostname.
		// Hostnames are arbitrary strings; ZCP refuses to guess a stage
		// pair from dev-hostname structure. The error hints at the dev-mode
		// alternative since "I want a single dev container" is the most
		// common reason an agent omits stage and hits this check.
		var stageHostname string
		if rt.EffectiveMode() == topology.PlanModeStandard {
			stageHostname = rt.StageHostname()
			if stageHostname == "" {
				errs = append(errs, fmt.Sprintf(`target %q: standard mode requires explicit stageHostname; for a single mutable dev container use bootstrapMode="dev"`, rt.DevHostname))
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
			if schemas != nil && !schemas.HasServiceType(dep.Type) {
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
				if !sharedAnchors[dep.Hostname] {
					errs = append(errs, fmt.Sprintf("target %q dependency %q: SHARED resolution requires another target to declare it (CREATE for greenfield, EXISTS for adopt)", rt.DevHostname, dep.Hostname))
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
// Classic plan + hostname already live → primary recovery is "pick a
// non-colliding hostname to create alongside"; adoption is the alternative
// when the user's intent is actually to attach to the running service.
// Adopt plan + hostname missing → "isExisting=true but not found". Recipe
// route: see bootstrap-recipe-match atom for the rename flow — the error
// wording stays generic because the same function serves all routes.
//
// Re-importing the existing service (`zerops_import override=true`) is
// intentionally NOT suggested here: it is destructive (replaces the running
// stack, deployed code, env vars, filesystem state) and reserved for
// explicit user requests, not a default conflict-recovery path.
func runtimeCollisionError(rt RuntimeTarget, stageHostname string, liveServiceNames map[string]bool) string {
	if liveServiceNames[rt.DevHostname] && !rt.IsExisting {
		return fmt.Sprintf("target %q: runtime already exists in project — pick a non-colliding devHostname to create a parallel service (existing one stays running), or set isExisting=true to adopt (attach ZCP tracking to the running service, no infra change). Recipe route: ZCP rewrites the import YAML using your plan's hostnames.", rt.DevHostname)
	}
	if !liveServiceNames[rt.DevHostname] && rt.IsExisting {
		return fmt.Sprintf("target %q: isExisting=true but runtime not found in project", rt.DevHostname)
	}
	if stageHostname == "" {
		return ""
	}
	if liveServiceNames[stageHostname] && !rt.IsExisting {
		return fmt.Sprintf("target %q: stage runtime %q already exists — pick a non-colliding stageHostname to create a parallel pair (existing one stays running), or set isExisting=true to adopt (attach ZCP tracking to the running pair). Recipe route: ZCP rewrites the import YAML using your plan's hostnames.", rt.DevHostname, stageHostname)
	}
	if !liveServiceNames[stageHostname] && rt.IsExisting {
		return fmt.Sprintf("target %q: isExisting=true but stage runtime %q not found in project", rt.DevHostname, stageHostname)
	}
	return ""
}
