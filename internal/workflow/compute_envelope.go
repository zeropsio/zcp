package workflow

import (
	"context"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// ComputeEnvelope is the single entry point for computing state. Every
// workflow-aware tool handler calls this.
//
// I/O parallelism: the three dependent reads (platform ListServices, local
// ServiceMeta list, WorkSession load) are independent and run concurrently.
// ComputeEnvelope itself is deterministic given the same inputs — callers
// relying on compaction-safety should hold the client and stateDir stable.
//
// Errors: when the platform client is unconfigured or no project is bound,
// returns `{Phase: idle, ...}` with empty services — this is the literal
// envelope of "no project yet", not a fallback.
func ComputeEnvelope(
	ctx context.Context,
	client platform.Client,
	stateDir string,
	projectID string,
	rt runtime.Info,
	now time.Time,
) (StateEnvelope, error) {
	env := DetectEnvironment(rt)

	var (
		services    []platform.ServiceStack
		servicesErr error
		metas       []*ServiceMeta
		metasErr    error
		ws          *WorkSession
		wsErr       error
		bs          *BootstrapSession
		bsErr       error
		project     *platform.Project
		projectErr  error
		wg          sync.WaitGroup
	)

	if client != nil && projectID != "" {
		wg.Add(2)
		go func() { defer wg.Done(); services, servicesErr = client.ListServices(ctx, projectID) }()
		go func() { defer wg.Done(); project, projectErr = client.GetProject(ctx, projectID) }()
	}
	wg.Add(3)
	go func() { defer wg.Done(); metas, metasErr = ListServiceMetas(stateDir) }()
	go func() { defer wg.Done(); ws, wsErr = CurrentWorkSession(stateDir) }()
	go func() { defer wg.Done(); bs, bsErr = LoadBootstrapSession(stateDir, os.Getpid()) }()
	wg.Wait()

	if servicesErr != nil {
		return StateEnvelope{}, servicesErr
	}
	if metasErr != nil {
		return StateEnvelope{}, metasErr
	}
	if wsErr != nil {
		return StateEnvelope{}, wsErr
	}
	if bsErr != nil {
		return StateEnvelope{}, bsErr
	}
	// projectErr is intentionally non-fatal: project Name is cosmetic. A
	// missing project (deleted, permissions changed, stale projectID) should
	// still yield a renderable envelope with the ID alone.
	_ = projectErr

	var self *SelfService
	if rt.InContainer && rt.ServiceName != "" {
		self = &SelfService{Hostname: rt.ServiceName}
	}

	snapshots := buildServiceSnapshots(services, metas, selfHostnameFromRT(rt))

	var wsSummary *WorkSessionSummary
	if ws != nil {
		wsSummary = buildWorkSessionSummary(ws)
	}

	var bsSummary *BootstrapSessionSummary
	if bs != nil {
		bsSummary = buildBootstrapSessionSummary(bs)
	}

	phase := derivePhase(ws, snapshots, stateDir)

	projectSummary := ProjectSummary{ID: projectID}
	if project != nil {
		projectSummary.Name = project.Name
	}

	return StateEnvelope{
		Phase:        phase,
		Environment:  env,
		IdleScenario: deriveIdleScenario(phase, snapshots),
		SelfService:  self,
		Project:      projectSummary,
		Services:     snapshots,
		WorkSession:  wsSummary,
		Bootstrap:    bsSummary,
		Recipe:       recipeSummaryFromBootstrap(bs),
		Generated:    now.UTC(),
	}, nil
}

// deriveIdleScenario classifies the idle phase into one of four sub-cases
// based on service composition. Returns "" for non-idle phases. Partitions
// services the same way planIdle does: managed services don't count toward
// any bucket (they are data stores, not deploy targets).
//
// Priority: incomplete > bootstrapped > adopt > empty. Incomplete wins
// because a ServiceMeta tagged to a prior session signals an interrupted
// bootstrap — resuming is the only clean recovery path, and atoms gated on
// this scenario surface the resume option before anything else.
func deriveIdleScenario(phase Phase, services []ServiceSnapshot) IdleScenario {
	if phase != PhaseIdle {
		return ""
	}
	var bootstrapped, adoptable, resumable int
	for _, svc := range services {
		if svc.RuntimeClass == RuntimeManaged {
			continue
		}
		if svc.Resumable {
			resumable++
			continue
		}
		if svc.Bootstrapped {
			bootstrapped++
			continue
		}
		adoptable++
	}
	if resumable > 0 {
		return IdleIncomplete
	}
	if bootstrapped > 0 {
		return IdleBootstrapped
	}
	if adoptable > 0 {
		return IdleAdopt
	}
	return IdleEmpty
}

// buildBootstrapSessionSummary adapts the persisted BootstrapSession into its
// envelope projection. Route is the primary axis for atom filtering; Closed
// tells renderers whether the bootstrap has wrapped up but its session file
// has not yet been GC'd.
func buildBootstrapSessionSummary(bs *BootstrapSession) *BootstrapSessionSummary {
	return &BootstrapSessionSummary{
		Route:       bs.Route,
		Intent:      bs.Intent,
		RecipeMatch: bs.RecipeMatch,
		Closed:      bs.ClosedAt != nil,
	}
}

// recipeSummaryFromBootstrap mirrors bs.RecipeMatch into the envelope's top-
// level Recipe field. Kept in sync with Bootstrap.RecipeMatch so existing
// renderers that only inspect Recipe still see the match; Bootstrap.Route is
// what atoms filter on.
func recipeSummaryFromBootstrap(bs *BootstrapSession) *RecipeSessionSummary {
	if bs == nil || bs.RecipeMatch == nil {
		return nil
	}
	return &RecipeSessionSummary{
		Slug:       bs.RecipeMatch.Slug,
		Confidence: bs.RecipeMatch.Confidence,
	}
}

// selfHostnameFromRT returns the container's own hostname or "" locally.
// Split out so derivePhase / buildServiceSnapshots can share one source.
func selfHostnameFromRT(rt runtime.Info) string {
	if rt.InContainer {
		return rt.ServiceName
	}
	return ""
}

// buildServiceSnapshots turns (platform services, local metas) into the
// envelope's Services field. Skips system containers and the self-service.
// Output is sorted by hostname for determinism.
func buildServiceSnapshots(
	services []platform.ServiceStack,
	metas []*ServiceMeta,
	selfHostname string,
) []ServiceSnapshot {
	metaByHost := make(map[string]*ServiceMeta, len(metas))
	for _, m := range metas {
		if m == nil {
			continue
		}
		metaByHost[m.Hostname] = m
		if m.StageHostname != "" {
			metaByHost[m.StageHostname] = m
		}
	}

	out := make([]ServiceSnapshot, 0, len(services))
	for _, svc := range services {
		if svc.IsSystem() {
			continue
		}
		if selfHostname != "" && svc.Name == selfHostname {
			continue
		}
		out = append(out, buildOneSnapshot(svc, metaByHost[svc.Name]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hostname < out[j].Hostname })
	return out
}

func buildOneSnapshot(svc platform.ServiceStack, meta *ServiceMeta) ServiceSnapshot {
	typeVersion := svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
	snap := ServiceSnapshot{
		Hostname:     svc.Name,
		TypeVersion:  typeVersion,
		Status:       svc.Status,
		RuntimeClass: classifyEnvelopeRuntime(typeVersion),
	}
	if meta != nil && meta.IsComplete() {
		snap.Bootstrapped = true
		snap.Deployed = meta.IsDeployed()
		snap.Mode = resolveEnvelopeMode(meta, svc.Name)
		snap.Strategy = DeployStrategy(meta.DeployStrategy)
		if snap.Strategy == "" {
			snap.Strategy = StrategyUnset
		}
		if meta.StageHostname != "" && svc.Name == meta.Hostname {
			snap.StageHostname = meta.StageHostname
		}
	}
	// Incomplete meta with BootstrapSession tag = resumable. Fires even when
	// Bootstrapped == false because the session already owns this slot; any
	// downstream workflow choosing "adopt" would clash with the in-flight
	// session's metadata.
	if meta != nil && !meta.IsComplete() && meta.BootstrapSession != "" {
		snap.Resumable = true
	}
	return snap
}

// classifyEnvelopeRuntime maps a service type to the envelope's RuntimeClass
// vocabulary. Distinct from `verify.classifyRuntime` — verify's enum is
// check-dispatch oriented (Worker = dynamic-without-ports), while the
// envelope models managed services as a first-class class (not "skip all
// checks") and collapses worker/dynamic into one class because no atom
// needs the port distinction.
func classifyEnvelopeRuntime(typeVersion string) RuntimeClass {
	if typeVersion == "" {
		return RuntimeUnknown
	}
	if IsManagedService(typeVersion) {
		return RuntimeManaged
	}
	lower := strings.ToLower(typeVersion)
	if strings.HasPrefix(lower, "php-apache") || strings.HasPrefix(lower, "php-nginx") {
		return RuntimeImplicitWeb
	}
	if strings.HasPrefix(lower, "static") || strings.HasPrefix(lower, "nginx") {
		return RuntimeStatic
	}
	return RuntimeDynamic
}

// resolveEnvelopeMode maps a service's (meta, hostname) pair to the envelope
// Mode enum. Role-based so the dev half and stage half of a standard pair
// get distinct modes (ModeStandard vs ModeStage) even though they share a
// single ServiceMeta record. Dev-only services get ModeDev; simple stays
// ModeSimple. meta.RoleFor already encodes the mode+environment+hostname
// lookup — we reuse it here instead of duplicating the rules.
func resolveEnvelopeMode(meta *ServiceMeta, hostname string) Mode {
	if meta == nil {
		return ""
	}
	switch meta.RoleFor(hostname) {
	case DeployRoleStage:
		return ModeStage
	case DeployRoleSimple:
		return ModeSimple
	case DeployRoleDev:
		// PrimaryRole returns Dev for both PlanModeDev (standalone dev) and
		// PlanModeStandard's dev half. Split them here so standard-only atoms
		// don't fire for dev-only services and vice versa.
		if meta.Mode == PlanModeStandard || (meta.Mode == "" && meta.StageHostname != "") {
			return ModeStandard
		}
		return ModeDev
	}
	return ""
}

// buildWorkSessionSummary adapts the persisted WorkSession into its envelope
// projection. Attempts are re-encoded with typed time fields and an iteration
// counter derived from slice index.
func buildWorkSessionSummary(ws *WorkSession) *WorkSessionSummary {
	summary := &WorkSessionSummary{
		Intent:      ws.Intent,
		Services:    append([]string(nil), ws.Services...),
		CreatedAt:   parseOrZero(ws.CreatedAt),
		CloseReason: ws.CloseReason,
	}
	if ws.ClosedAt != "" {
		t := parseOrZero(ws.ClosedAt)
		summary.ClosedAt = &t
	}
	if len(ws.Deploys) > 0 {
		summary.Deploys = make(map[string][]AttemptInfo, len(ws.Deploys))
		for host, attempts := range ws.Deploys {
			summary.Deploys[host] = deployAttemptsToInfo(attempts)
		}
	}
	if len(ws.Verifies) > 0 {
		summary.Verifies = make(map[string][]AttemptInfo, len(ws.Verifies))
		for host, attempts := range ws.Verifies {
			summary.Verifies[host] = verifyAttemptsToInfo(attempts)
		}
	}
	return summary
}

func deployAttemptsToInfo(attempts []DeployAttempt) []AttemptInfo {
	out := make([]AttemptInfo, 0, len(attempts))
	for i, a := range attempts {
		out = append(out, AttemptInfo{
			At:        parseOrZero(firstNonEmpty(a.SucceededAt, a.AttemptedAt)),
			Success:   a.SucceededAt != "",
			Iteration: i + 1,
		})
	}
	return out
}

func verifyAttemptsToInfo(attempts []VerifyAttempt) []AttemptInfo {
	out := make([]AttemptInfo, 0, len(attempts))
	for i, a := range attempts {
		out = append(out, AttemptInfo{
			At:        parseOrZero(firstNonEmpty(a.PassedAt, a.AttemptedAt)),
			Success:   a.Passed,
			Iteration: i + 1,
		})
	}
	return out
}

// derivePhase implements §4 phase rules:
//
//   - WorkSession present AND ClosedAt set AND CloseReason == auto-complete →
//     develop-closed-auto
//   - WorkSession present AND open → develop-active
//   - Bootstrap or recipe session registered for this PID → bootstrap-active /
//     recipe-active (looked up via registry)
//   - Otherwise → idle
//
// The registry lookup is best-effort: a registry read failure degrades to
// idle rather than erroring, because the envelope must always be producible.
func derivePhase(ws *WorkSession, _ []ServiceSnapshot, stateDir string) Phase {
	if ws != nil {
		if ws.ClosedAt != "" && ws.CloseReason == CloseReasonAutoComplete {
			return PhaseDevelopClosed
		}
		if ws.ClosedAt == "" {
			return PhaseDevelopActive
		}
	}
	if phase := infraPhaseForPID(stateDir); phase != "" {
		return phase
	}
	return PhaseIdle
}

// infraPhaseForPID returns bootstrap-active / recipe-active when a non-work
// session is registered for the running PID. Returns "" when none exists.
func infraPhaseForPID(stateDir string) Phase {
	if stateDir == "" {
		return ""
	}
	sessions, err := ListSessions(stateDir)
	if err != nil {
		return ""
	}
	pid := os.Getpid()
	for _, s := range sessions {
		if s.PID != pid {
			continue
		}
		switch s.Workflow {
		case WorkflowBootstrap:
			return PhaseBootstrapActive
		case WorkflowRecipe:
			return PhaseRecipeActive
		}
	}
	return ""
}

// parseOrZero converts a persisted RFC3339 timestamp to time.Time, returning
// the zero value for an empty or malformed input. Zero is the documented
// sentinel for "no timestamp" throughout the envelope.
func parseOrZero(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
