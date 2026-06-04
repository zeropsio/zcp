package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
// route=adopt directly from live services in the agent-named scope. Every scoped
// adoptable runtime becomes an isExisting target and every managed service a
// shared EXISTS dependency. Empty scope returns a candidate-list diagnostic
// instead of treating the whole project as implicit scope.
//
// It refuses to guess a dev/stage pairing — exactly two same-type adoptable
// runtimes return ErrAdoptPairingChoice with both plan templates instead of
// silently committing two independent dev containers (which would drop the
// dev→stage relationship later promote/launch flows depend on). One named
// runtime, or multiple named runtimes of mixed types, commit frictionlessly as
// independent dev containers.
func (e *Engine) BootstrapCompleteAdoptPlan(existing []platform.ServiceStack, scope []string, self runtime.Info, schemas *schema.Schemas) (*BootstrapResponse, error) {
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
	adoptable := e.deriveAdoptableServices(existing, metas, self)
	if len(adoptable) == 0 {
		return nil, fmt.Errorf("bootstrap adopt plan: no adoptable runtime services found — nothing to adopt")
	}
	adoptable, err = validateAdoptScope(scope, adoptable)
	if err != nil {
		return nil, fmt.Errorf("bootstrap adopt plan: %w", err)
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
	// Compare CANONICAL BARE FORM, not the raw composite name: the common
	// dev/stage pair is ubuntu/<rt> (dev) + alpine/<rt> (prod) — same bare type,
	// different OS base. Raw equality missed it and silently committed two
	// independent dev containers, which then dead-ended launch's git-push gate
	// (Wave-1 finding). CanonicalBareForm keeps the version, so different
	// versions (nodejs@22 vs nodejs@20) correctly stay unpaired. Honors the
	// composite-aware invariant (CLAUDE.md).
	if len(adoptable) == 2 &&
		topology.CanonicalBareForm(typeByHost[adoptable[0]]) == topology.CanonicalBareForm(typeByHost[adoptable[1]]) {
		return nil, adoptPairingChoice(adoptable[0], adoptable[1], typeByHost[adoptable[0]], typeByHost[adoptable[1]], deps)
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

func validateAdoptScope(scope, adoptable []string) ([]string, error) {
	available := append([]string(nil), adoptable...)
	sort.Strings(available)
	if len(scope) == 0 {
		return nil, fmt.Errorf("adopt scope is required — name the runtime service hostnames to adopt; available adoptable runtime services: %v", available)
	}

	adoptableSet := make(map[string]bool, len(adoptable))
	for _, h := range adoptable {
		adoptableSet[h] = true
	}

	seen := make(map[string]bool, len(scope))
	scoped := make([]string, 0, len(scope))
	var unknown []string
	for _, h := range scope {
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		if !adoptableSet[h] {
			unknown = append(unknown, h)
			continue
		}
		scoped = append(scoped, h)
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("adopt scope contains unknown or non-adoptable hostnames %v — available adoptable runtime services: %v", unknown, available)
	}
	if len(scoped) == 0 {
		return nil, fmt.Errorf("adopt scope is empty after deduplication — name at least one runtime service")
	}
	return scoped, nil
}

func (e *Engine) deriveAdoptableServices(existing []platform.ServiceStack, metas []*ServiceMeta, self runtime.Info) []string {
	metaByHost := ManagedRuntimeIndex(metas)
	aliveSessions := e.aliveSessionIDs()
	var out []string
	for _, svc := range existing {
		if !isAdoptableRuntimeService(svc, self) {
			continue
		}
		meta := metaByHost[svc.Name]
		if meta != nil && meta.IsComplete() {
			continue
		}
		if meta != nil && meta.BootstrapSession != "" && meta.BootstrapSession != e.sessionID && aliveSessions[meta.BootstrapSession] {
			continue
		}
		out = append(out, svc.Name)
	}
	return out
}

func (e *Engine) aliveSessionIDs() map[string]bool {
	sessions, err := ListSessions(e.stateDir)
	if err != nil {
		return nil
	}
	alive := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		if isProcessAlive(s.PID, s.StartTime) {
			alive[s.SessionID] = true
		}
	}
	return alive
}

// adoptPairingChoice renders the ErrAdoptPairingChoice diagnostic with two
// schema-valid, copy-pasteable plan templates (marshalled from real targets so the
// agent pastes-and-resends in one turn). devHost/stageHost order follows discovery
// order; the agent adjusts roles if needed.
//
// devType/stageType are the live per-host types — they share a CanonicalBareForm
// (that is why this pair triggered) but commonly differ in OS prefix
// (ubuntu/<rt> dev + alpine/<rt> prod). The prompt names each host's ACTUAL type
// and the independent template uses each host's own type, rather than rendering
// the dev-half type for both (Wave-3 regression the composite-aware guard exposed).
func adoptPairingChoice(devHost, stageHost, devType, stageType string, deps []Dependency) error {
	bare := topology.CanonicalBareForm(devType)
	pair := []BootstrapTarget{{
		Runtime:      RuntimeTarget{DevHostname: devHost, ExplicitStage: stageHost, Type: devType, BootstrapMode: topology.PlanModeStandard, IsExisting: true},
		Dependencies: deps,
	}}
	indep := []BootstrapTarget{
		{Runtime: RuntimeTarget{DevHostname: devHost, Type: devType, BootstrapMode: topology.PlanModeDev, IsExisting: true}, Dependencies: deps},
		{Runtime: RuntimeTarget{DevHostname: stageHost, Type: stageType, BootstrapMode: topology.PlanModeDev, IsExisting: true}, Dependencies: deps},
	}
	pairJSON, err := json.Marshal(pair)
	if err != nil {
		return fmt.Errorf("%w: %q (%s) and %q (%s) share runtime base %s — likely a dev/stage pair. Resubmit an explicit plan: a standard dev/stage pair (devHostname=%q, stageHostname=%q, bootstrapMode=standard, isExisting=true) OR two independent dev containers (bootstrapMode=dev each)",
			ErrAdoptPairingChoice, devHost, devType, stageHost, stageType, bare, devHost, stageHost)
	}
	indepJSON, err := json.Marshal(indep)
	if err != nil {
		return fmt.Errorf("%w: %q (%s) and %q (%s) share runtime base %s — likely a dev/stage pair. Resubmit an explicit plan: a standard dev/stage pair (devHostname=%q, stageHostname=%q, bootstrapMode=standard, isExisting=true) OR two independent dev containers (bootstrapMode=dev each)",
			ErrAdoptPairingChoice, devHost, devType, stageHost, stageType, bare, devHost, stageHost)
	}
	return fmt.Errorf("%w: %q (%s) and %q (%s) share runtime base %s — likely a dev/stage pair, which ZCP will not guess. Resubmit action=complete step=discover with ONE of these as an explicit plan:\n\n• dev/stage pair (cross-deploy promote — pick this if %q deploys to %q):\nplan=%s\n\n• two independent dev containers:\nplan=%s",
		ErrAdoptPairingChoice, devHost, devType, stageHost, stageType, bare, devHost, stageHost, string(pairJSON), string(indepJSON))
}
