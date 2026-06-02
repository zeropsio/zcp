package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
)

// AdoptCandidate represents a live service for auto-adoption.
type AdoptCandidate struct {
	Hostname string
	Type     string
}

// isControlPlaneType returns true for ZCP's own service type.
func isControlPlaneType(serviceType string) bool {
	return strings.HasPrefix(strings.ToLower(serviceType), "zcp")
}

// InferServicePairing builds BootstrapTargets from live services for adoption.
// Filters out managed and control-plane types; every remaining runtime
// becomes its own target with `BootstrapMode: PlanModeDev`. Managed
// services become EXISTS-resolution dependencies shared across targets.
//
// Why no pairing: earlier revisions inferred `{base}dev` + `{base}stage`
// pairs from hostname suffixes. Service names are now arbitrary strings,
// so the heuristic silently misclassified repos with non-conforming
// names (e.g. `frontend-app` + `frontend-app-prod`) and overrode the
// author's intent when they wanted `appdev`+`appstage` adopted as two
// independent services. Users who want a dev/stage pair adopted as
// PlanModeStandard write that into the bootstrap plan explicitly
// (BootstrapMode=standard, ExplicitStage=<hostname>).
//
// liveManaged: managed-service base names from the schema-derived catalog
// (schema.Schemas.ManagedBaseNames). When non-empty it overrides the static
// prefix list so a managed type the static list misses is still classified.
// Pass nil to use the static topology fallback.
func InferServicePairing(candidates []AdoptCandidate, liveManaged map[string]bool) []BootstrapTarget {
	var runtimes []AdoptCandidate
	var managed []AdoptCandidate
	for _, c := range candidates {
		if isControlPlaneType(c.Type) {
			continue
		}
		if isManagedTypeWithLive(c.Type, liveManaged) {
			managed = append(managed, c)
			continue
		}
		runtimes = append(runtimes, c)
	}

	if len(runtimes) == 0 {
		return nil
	}

	// Shared dependencies from managed services.
	deps := make([]Dependency, len(managed))
	for i, m := range managed {
		deps[i] = Dependency{
			Hostname:   m.Hostname,
			Type:       m.Type,
			Resolution: "EXISTS",
		}
	}

	targets := make([]BootstrapTarget, 0, len(runtimes))
	for _, r := range runtimes {
		targets = append(targets, BootstrapTarget{
			Runtime: RuntimeTarget{
				DevHostname:   r.Hostname,
				Type:          r.Type,
				IsExisting:    true,
				BootstrapMode: topology.PlanModeDev,
			},
			Dependencies: deps,
		})
	}
	return targets
}

// ErrAdoptPairingChoice is returned by BootstrapCompleteAdoptPlan when exactly two
// adoptable runtimes share a type — the canonical dev/stage shape ZCP refuses to
// guess. The error message carries copy-pasteable standard-pair and independent-dev
// plan templates so the agent resubmits one as an explicit plan in a single
// round-trip (never the schema-fuzzing it would otherwise do).
var ErrAdoptPairingChoice = errors.New("adopt: ambiguous dev/stage pairing")

// BootstrapCompleteAdoptPlan derives and commits the discover-step plan for
// route=adopt directly from live services, so the agent authors nothing: every
// adoptable runtime (per the canonical adoptableServices classifier) becomes an
// isExisting target and every managed service a shared EXISTS dependency.
//
// It refuses to guess a dev/stage pairing — exactly two same-type adoptable
// runtimes return ErrAdoptPairingChoice with both plan templates instead of
// silently committing two independent dev containers (which would drop the
// dev→stage relationship later promote/launch flows depend on). One runtime, or
// multiple of mixed types, commit frictionlessly as independent dev containers.
func (e *Engine) BootstrapCompleteAdoptPlan(existing []platform.ServiceStack, self runtime.Info, schemas *schema.Schemas) (*BootstrapResponse, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("bootstrap adopt plan: %w", err)
	}
	if state.Bootstrap == nil || !state.Bootstrap.Active {
		return nil, fmt.Errorf("bootstrap adopt plan: bootstrap not active")
	}
	if state.Bootstrap.Route != BootstrapRouteAdopt {
		return nil, fmt.Errorf("bootstrap adopt plan: auto-derive plan is adopt-route only (route=%q); submit an explicit plan", state.Bootstrap.Route)
	}
	if state.Bootstrap.CurrentStepName() != StepDiscover {
		return nil, fmt.Errorf("bootstrap adopt plan: current step is %q, not %q", state.Bootstrap.CurrentStepName(), StepDiscover)
	}

	metas, err := ListServiceMetas(e.stateDir)
	if err != nil {
		return nil, fmt.Errorf("bootstrap adopt plan: %w", err)
	}
	adoptable := adoptableServices(existing, metas, self)
	if len(adoptable) == 0 {
		return nil, fmt.Errorf("bootstrap adopt plan: no adoptable runtime services found — nothing to adopt")
	}

	typeByHost := make(map[string]string, len(existing))
	for _, svc := range existing {
		typeByHost[svc.Name] = svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
	}

	// Managed services become shared EXISTS dependencies on every target.
	var deps []Dependency
	for _, svc := range existing {
		t := svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
		if !svc.IsSystem() && topology.IsManagedService(t) {
			deps = append(deps, Dependency{Hostname: svc.Name, Type: t, Resolution: ResolutionExists})
		}
	}

	// Pairing guard: two same-type adoptable runtimes are the canonical dev/stage
	// shape. ZCP refuses to commit a guess — surface both plan shapes to choose.
	if len(adoptable) == 2 && typeByHost[adoptable[0]] == typeByHost[adoptable[1]] {
		return nil, adoptPairingChoice(adoptable[0], adoptable[1], typeByHost[adoptable[0]], deps)
	}

	candidates := make([]AdoptCandidate, 0, len(adoptable)+len(deps))
	for _, h := range adoptable {
		candidates = append(candidates, AdoptCandidate{Hostname: h, Type: typeByHost[h]})
	}
	for _, d := range deps {
		candidates = append(candidates, AdoptCandidate{Hostname: d.Hostname, Type: d.Type})
	}
	var managed map[string]bool
	if schemas != nil {
		managed = schemas.ManagedBaseNames()
	}
	targets := InferServicePairing(candidates, managed)
	if len(targets) == 0 {
		return nil, fmt.Errorf("bootstrap adopt plan: no adoptable runtime services found — nothing to adopt")
	}

	resp, err := e.completePlanWithTargets(state, targets, schemas, existing)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(targets))
	for _, tgt := range targets {
		names = append(names, tgt.Runtime.DevHostname)
	}
	msg := "Auto-derived adoption plan from discovery: adopted " + strings.Join(names, ", ") + " (isExisting)"
	if len(deps) > 0 {
		depNames := make([]string, len(deps))
		for i, d := range deps {
			depNames[i] = d.Hostname
		}
		msg += "; managed deps " + strings.Join(depNames, ", ") + " attached as EXISTS"
	}
	resp.Message = msg + ".\n\n" + resp.Message
	return resp, nil
}

// adoptPairingChoice renders the ErrAdoptPairingChoice diagnostic with two
// schema-valid, copy-pasteable plan templates (marshalled from real targets so the
// agent pastes-and-resends in one turn). devHost/stageHost order follows discovery
// order; the agent adjusts roles if needed.
func adoptPairingChoice(devHost, stageHost, svcType string, deps []Dependency) error {
	pair := []BootstrapTarget{{
		Runtime:      RuntimeTarget{DevHostname: devHost, ExplicitStage: stageHost, Type: svcType, BootstrapMode: topology.PlanModeStandard, IsExisting: true},
		Dependencies: deps,
	}}
	indep := []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: devHost, Type: svcType, BootstrapMode: topology.PlanModeDev, IsExisting: true}, Dependencies: deps},
		{Runtime: RuntimeTarget{DevHostname: stageHost, Type: svcType, BootstrapMode: topology.PlanModeDev, IsExisting: true}, Dependencies: deps},
	}
	pairJSON, err := json.Marshal(pair)
	if err != nil {
		return fmt.Errorf("%w: %q and %q are both %s — likely a dev/stage pair. Resubmit an explicit plan: a standard dev/stage pair (devHostname=%q, stageHostname=%q, bootstrapMode=standard, isExisting=true) OR two independent dev containers (bootstrapMode=dev each)",
			ErrAdoptPairingChoice, devHost, stageHost, svcType, devHost, stageHost)
	}
	indepJSON, err := json.Marshal(indep)
	if err != nil {
		return fmt.Errorf("%w: %q and %q are both %s — likely a dev/stage pair. Resubmit an explicit plan: a standard dev/stage pair (devHostname=%q, stageHostname=%q, bootstrapMode=standard, isExisting=true) OR two independent dev containers (bootstrapMode=dev each)",
			ErrAdoptPairingChoice, devHost, stageHost, svcType, devHost, stageHost)
	}
	return fmt.Errorf("%w: %q and %q are both %s — likely a dev/stage pair, which ZCP will not guess. Resubmit action=complete step=discover with ONE of these as an explicit plan:\n\n• dev/stage pair (cross-deploy promote — pick this if %q deploys to %q):\nplan=%s\n\n• two independent dev containers:\nplan=%s",
		ErrAdoptPairingChoice, devHost, stageHost, svcType, devHost, stageHost, string(pairJSON), string(indepJSON))
}
