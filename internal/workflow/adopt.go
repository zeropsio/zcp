package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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
// Filters out managed, control-plane, pool and Box types; every remaining runtime
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
		if isControlPlaneType(c.Type) || topology.IsPoolOrSandboxType(c.Type) {
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
// It refuses to guess a dev/stage pairing — any two scoped adoptable runtimes
// sharing a runtime base return ErrAdoptPairingChoice with plan templates
// instead of silently committing independent dev containers (which would drop
// the dev→stage relationship later promote/launch flows depend on). One named
// runtime, or named runtimes of different bases, commit frictionlessly as
// independent dev containers. A variant-B pair is derived with its stages
// before the question is asked (adoptVariantB).
func (e *Engine) BootstrapCompleteAdoptPlan(existing []platform.ServiceStack, scope []string, self runtime.Info, schemas *schema.Schemas, volumeSetups []AdoptVolumeSetup) (*BootstrapResponse, error) {
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

	// Variant B: dev services of one repository sharing a local-storage
	// volume that already holds it are one pair, recorded again.
	variantB, adoptable := adoptVariantB(adoptable, typeByHost, volumeSetups)

	// Managed services become shared EXISTS dependencies on every target.
	var deps []Dependency
	for _, svc := range existing {
		t := svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
		if variantB != nil && svc.Name == variantB.Runtime.CodeVolume.Hostname {
			continue
		}
		if !svc.IsSystem() && topology.IsManagedService(t) {
			deps = append(deps, Dependency{Hostname: svc.Name, Type: t, Resolution: ResolutionExists})
		}
	}

	// Pairing guard: any two adoptable runtimes sharing a runtime base may be a
	// dev service and its stage. ZCP refuses to commit a guess — two independent
	// dev services, or a stage recorded as a dev service of its own outside its
	// pair (§8 E8) — and surfaces the plan shapes to choose from.
	if err := adoptPairingQuestion(adoptable, typeByHost, deps); err != nil {
		return nil, err
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
	if variantB != nil {
		variantB.Dependencies = deps
		targets = append([]BootstrapTarget{*variantB}, targets...)
	}
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

// adoptPairingQuestion refuses the adoptable runtimes left for the ordinary
// derivation when any two of them share a runtime base, compared in
// canonical bare form: the common dev/stage pair is ubuntu/<rt> (dev) +
// alpine/<rt> (prod) — same bare type, different OS base — while different
// versions (nodejs@22 vs nodejs@20) stay apart. Exactly two runtimes get
// adoptPairingChoice, more get adoptPairingChoices, each with plan templates
// covering every runtime. Nil when no two share a base.
func adoptPairingQuestion(adoptable []string, typeByHost map[string]string, deps []Dependency) error {
	bases := map[string]int{}
	shared := false
	for _, h := range adoptable {
		base := topology.CanonicalBareForm(typeByHost[h])
		bases[base]++
		shared = shared || bases[base] > 1
	}
	if !shared {
		return nil
	}
	if len(adoptable) == 2 {
		return adoptPairingChoice(adoptable[0], adoptable[1], typeByHost[adoptable[0]], typeByHost[adoptable[1]], deps)
	}
	return adoptPairingChoices(adoptable, typeByHost, deps)
}

// adoptPairingChoices renders the ErrAdoptPairingChoice diagnostic for more
// than two runtimes, some of which share a runtime base: two schema-valid,
// copy-pasteable plan templates covering every runtime. The pairs template
// pairs, within one base, each stage hostname with the runtime named like it
// (`apistage` → `apidev` or `api`), and a base of exactly two runtimes named
// alike by neither in discovery order; every other runtime is a dev
// container of its own, as every runtime is in the independent template.
func adoptPairingChoices(adoptable []string, typeByHost map[string]string, deps []Dependency) error {
	stageOf := map[string]string{}
	paired := map[string]bool{}
	byBase := map[string][]string{}
	var bases []string
	for _, h := range adoptable {
		base := topology.CanonicalBareForm(typeByHost[h])
		if _, seen := byBase[base]; !seen {
			bases = append(bases, base)
		}
		byBase[base] = append(byBase[base], h)
	}
	for _, base := range bases {
		group := byBase[base]
		for _, stage := range group {
			if !IsStageHostname(stage) || paired[stage] {
				continue
			}
			stem := strings.TrimSuffix(stage, stageHostnameSuffix)
			for _, dev := range group {
				if dev != stage && !paired[dev] && (dev == stem+devHostnameSuffix || dev == stem) {
					stageOf[dev], paired[dev], paired[stage] = stage, true, true
					break
				}
			}
		}
		if len(group) == 2 && !paired[group[0]] && !paired[group[1]] {
			stageOf[group[0]], paired[group[0]], paired[group[1]] = group[1], true, true
		}
	}
	var pairs, indep []BootstrapTarget
	var shared []string
	for _, base := range bases {
		if group := byBase[base]; len(group) > 1 {
			shared = append(shared, quotedList(group)+" share runtime base "+base)
		}
	}
	for _, h := range adoptable {
		indep = append(indep, BootstrapTarget{Runtime: RuntimeTarget{DevHostname: h, Type: typeByHost[h], BootstrapMode: topology.PlanModeDev, IsExisting: true}, Dependencies: deps})
		if stage, ok := stageOf[h]; ok {
			pairs = append(pairs, BootstrapTarget{Runtime: RuntimeTarget{DevHostname: h, ExplicitStage: stage, Type: typeByHost[h], BootstrapMode: topology.PlanModeStandard, IsExisting: true}, Dependencies: deps})
			continue
		}
		if !paired[h] {
			pairs = append(pairs, BootstrapTarget{Runtime: RuntimeTarget{DevHostname: h, Type: typeByHost[h], BootstrapMode: topology.PlanModeDev, IsExisting: true}, Dependencies: deps})
		}
	}
	pairsJSON, perr := json.Marshal(pairs)
	indepJSON, ierr := json.Marshal(indep)
	if perr != nil || ierr != nil {
		return fmt.Errorf("%w: %s — any two of one base may be a dev service and its stage, which ZCP will not guess; resubmit an explicit plan naming each pair (bootstrapMode=standard, stageHostname) and each independent dev container (bootstrapMode=dev), isExisting=true", ErrAdoptPairingChoice, strings.Join(shared, "; "))
	}
	return fmt.Errorf("%w: %s — any two of one base may be a dev service and its stage, which ZCP will not guess. Resubmit action=complete step=discover with ONE of these as an explicit plan, its pairs adjusted to what deploys to what:\n\n• dev/stage pairs (cross-deploy promote — each stage paired with the runtime named like it):\nplan=%s\n\n• independent dev containers:\nplan=%s",
		ErrAdoptPairingChoice, strings.Join(shared, "; "), string(pairsJSON), string(indepJSON))
}

// quotedList is hostnames quoted and comma-separated.
func quotedList(hosts []string) string {
	quoted := make([]string, len(hosts))
	for i, h := range hosts {
		quoted[i] = fmt.Sprintf("%q", h)
	}
	return strings.Join(quoted, ", ")
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

// AdoptPair records an existing dev/stage pair as adopted — the meta the
// adopt route's close writes for a standard target with isExisting
// (writeBootstrapOutputs, through the same targetMeta/writeTargetMeta), so a
// pair a Mate's stand-up adopted from its recipe passes the adoption gate and
// reads in every envelope exactly as one adopted by hand. Two things differ
// from the route, both because the caller read them from the pair's recipe:
// the setups the halves build with (the recipe route's literal-setup rule,
// Gate R — a group names them after the pair, so no convention finds them),
// and no bootstrap session is opened or closed around the write.
//
// Idempotent: a pair already recorded complete keeps everything it earned
// since (mergeExistingMeta) — its wiring, its first deploy, a setup somebody
// chose. date is the BootstrappedAt a fresh record gets.
//
// Refuses a target that is no standard pair, and one naming a service that
// another meta already records — a stage half recorded as a service of its
// own, or a dev half or stage another pair names, a variant-B pair's further
// dev service among them: folding it into this pair would leave two metas for
// one runtime, which the pair-keyed invariant forbids (E8).
func AdoptPair(stateDir string, target BootstrapTarget, date string) error {
	rt := target.Runtime
	stages := rt.StageRefs()
	if rt.DevHostname == "" || len(stages) == 0 {
		return fmt.Errorf("adopt pair: %q is not a dev/stage pair (bootstrapMode %q, stage %q)", rt.DevHostname, rt.EffectiveMode(), rt.ExplicitStage)
	}
	halves := append(rt.StageHostnames(), rt.CodeVolume.DevServiceHostnames()...)
	for _, half := range append([]string{rt.DevHostname}, halves...) {
		other, err := FindServiceMeta(stateDir, half)
		if err != nil {
			return fmt.Errorf("adopt pair %s: %w", rt.DevHostname, err)
		}
		switch {
		case other == nil || other.Hostname == rt.DevHostname:
		case other.Hostname == half:
			return fmt.Errorf("adopt pair %s: its half %s is already recorded as a service of its own", rt.DevHostname, half)
		default:
			return fmt.Errorf("adopt pair %s: %s is already a service of the pair %s", rt.DevHostname, half, other.Hostname)
		}
	}
	target.Runtime.IsExisting = true
	meta := targetMeta(target, rt.DevHostname, topology.PlanModeStandard, stages, "", date)
	meta.PrimarySetupName = rt.PrimarySetupName
	meta.Stages = slices.Clone(stages)
	if err := writeTargetMeta(stateDir, target, meta); err != nil {
		return fmt.Errorf("adopt pair %s: %w", rt.DevHostname, err)
	}
	return nil
}

// AdoptVolumeSetup is a setup of a repository's zerops.yaml that mounts a
// local-storage volume of the project (run.volume), read through the scoped
// runtimes holding that file, in file order.
type AdoptVolumeSetup struct {
	Setup     string
	RunBase   string
	Volume    string
	MountPath string
	// WorksIn says the setup's run works in MountPath rather than in /var/www
	// (ops.ZeropsYmlEntry.WorksIn).
	WorksIn bool
	// Content is what each runtime holding the file finds at MountPath, by
	// hostname, for the runtimes of RunBase (ops.CodeVolumeContent); a
	// runtime absent from it proved nothing.
	Content map[string]topology.VolumeContent
	// FileSetups are every setup the zerops.yaml declaring this one declares —
	// the repository's other setups, its stages' among them.
	FileSetups []string
}

// adoptVariantB recognises a variant-B project (docs/spec-mate.md §2.8a.4a)
// among the adoptable runtimes: several dev services of one repository
// mounting one local-storage volume that already holds that repository are
// one pair, recorded with its code volume again — a project whose record was
// lost, which pool migration then moves onto one dev pool per part. Each
// setup that mounts the volume runs on one adoptable runtime
// (adoptVolumeDevServices); a setup no runtime takes leaves the topology
// alone. The volume is a code volume only when proven: every setup works in
// the mount, and every one of those runtimes finds this repository's
// checkout there. zcp sets up no new code volume, so a volume holding nothing
// yet or an empty folder — which adopting would make one, its first deploy
// filling it — is no code volume, and neither is a data volume (a Laravel app
// and worker sharing storage): those dev services adopt as separate services.
// The pair's stages are the adoptable stages of its repository
// (adoptVariantBStages); with any it is a standard pair, else a dev one. It
// returns the target holding the code volume (nil when there is none) and the
// adoptable runtimes left for the ordinary derivation.
func adoptVariantB(adoptable []string, typeByHost map[string]string, setups []AdoptVolumeSetup) (*BootstrapTarget, []string) {
	byVolume := map[string][]AdoptVolumeSetup{}
	var order []string
	for _, s := range setups {
		if s.Volume == "" {
			continue
		}
		if _, seen := byVolume[s.Volume]; !seen {
			order = append(order, s.Volume)
		}
		byVolume[s.Volume] = append(byVolume[s.Volume], s)
	}
	for _, volume := range order {
		mounts := byVolume[volume]
		mountPath := mounts[0].MountPath
		if len(mounts) < 2 || topology.CodeVolumeMountPathError(mountPath) != "" ||
			slices.ContainsFunc(mounts, func(m AdoptVolumeSetup) bool { return m.MountPath != mountPath || !m.WorksIn }) {
			continue
		}
		hosts := adoptVolumeDevServices(mounts, adoptable, typeByHost)
		if hosts == nil {
			continue
		}
		refs := make([]topology.DevServiceRef, len(mounts))
		for i, m := range mounts {
			refs[i] = topology.DevServiceRef{Hostname: hosts[i], SetupName: m.Setup}
		}
		var rest []string
		for _, h := range adoptable {
			if !slices.Contains(hosts, h) {
				rest = append(rest, h)
			}
		}
		stages, rest := adoptVariantBStages(rest, hosts, mounts)
		rt := RuntimeTarget{
			DevHostname:      hosts[0],
			Type:             typeByHost[hosts[0]],
			IsExisting:       true,
			BootstrapMode:    topology.PlanModeDev,
			PrimarySetupName: refs[0].SetupName,
			CodeVolume:       &topology.CodeVolume{Hostname: volume, MountPath: mountPath, DevServices: refs[1:]},
		}
		switch {
		case len(stages) == 1:
			rt.BootstrapMode = topology.PlanModeStandard
			rt.ExplicitStage, rt.StageSetupName = stages[0].Hostname, stages[0].SetupName
		case len(stages) > 1:
			rt.BootstrapMode = topology.PlanModeStandard
			rt.Stages = stages
		}
		return &BootstrapTarget{Runtime: rt}, rest
	}
	return nil, adoptable
}

// adoptVolumeDevServices is the runtime each setup mounting a volume runs on,
// in the setups' order: an adoptable runtime of the setup's run.base finding
// the repository's checkout at the mount — the one named after the setup (the
// platform's default setup of a service is its hostname), else the only one
// no other setup takes. A base the stages or other runtimes share never makes
// the choice ambiguous, as they hold no checkout there. Nil when a setup has
// no such runtime, or more than one with none named after it.
func adoptVolumeDevServices(mounts []AdoptVolumeSetup, adoptable []string, typeByHost map[string]string) []string {
	hosts := make([]string, len(mounts))
	taken := map[string]bool{}
	holding := func(m AdoptVolumeSetup) []string {
		var out []string
		for _, h := range adoptable {
			if !taken[h] && m.Content[h] == topology.VolumeCheckout &&
				topology.CanonicalBaseName(typeByHost[h]) == topology.CanonicalBaseName(m.RunBase) {
				out = append(out, h)
			}
		}
		return out
	}
	for i, m := range mounts {
		if slices.Contains(holding(m), m.Setup) {
			hosts[i], taken[m.Setup] = m.Setup, true
		}
	}
	for progress := true; progress; {
		progress = false
		for i, m := range mounts {
			if hosts[i] != "" {
				continue
			}
			if c := holding(m); len(c) == 1 {
				hosts[i], taken[c[0]], progress = c[0], true, true
			}
		}
	}
	if slices.Contains(hosts, "") {
		return nil
	}
	return hosts
}

// adoptVariantBStages takes the variant-B pair's stages from the adoptable
// runtimes left: every stage hostname (IsStageHostname) named like one of the
// pair's dev services (`apistage` → `apidev` or `api`, as the stand-up pairs
// them), or one the repository's zerops.yaml declares a setup named after —
// the setup it records then. The one named like the dev half comes first,
// then the scope's order. It returns the stages and the runtimes left.
func adoptVariantBStages(rest, devServices []string, mounts []AdoptVolumeSetup) ([]topology.StageRef, []string) {
	declared := map[string]bool{}
	for _, m := range mounts {
		for _, f := range m.FileSetups {
			declared[f] = true
		}
	}
	var stages []topology.StageRef
	var left []string
	for _, h := range rest {
		stem := strings.TrimSuffix(h, stageHostnameSuffix)
		if !IsStageHostname(h) || !declared[h] && !slices.Contains(devServices, stem+devHostnameSuffix) && !slices.Contains(devServices, stem) {
			left = append(left, h)
			continue
		}
		ref := topology.StageRef{Hostname: h}
		if declared[h] {
			ref.SetupName = h
		}
		stages = append(stages, ref)
	}
	named := strings.TrimSuffix(devServices[0], devHostnameSuffix) + stageHostnameSuffix
	if i := slices.IndexFunc(stages, func(s topology.StageRef) bool { return s.Hostname == named }); i > 0 {
		first := stages[i]
		stages = append([]topology.StageRef{first}, slices.Delete(stages, i, i+1)...)
	}
	return stages, left
}
