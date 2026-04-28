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
	"github.com/zeropsio/zcp/internal/topology"
)

// StateEnvelope.Bootstrap is populated by bootstrap_guide_assembly.go's
// synthesisEnvelope helper, NOT by ComputeEnvelope. ComputeEnvelope leaves
// it nil; the bootstrap conductor builds a synthetic summary from the live
// BootstrapState on every per-step render. Same for StateEnvelope.Recipe.

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
		project     *platform.Project
		projectErr  error
		wg          sync.WaitGroup
	)

	if client != nil && projectID != "" {
		wg.Add(2)
		go func() { defer wg.Done(); services, servicesErr = client.ListServices(ctx, projectID) }()
		go func() { defer wg.Done(); project, projectErr = client.GetProject(ctx, projectID) }()
	}
	wg.Add(2)
	go func() { defer wg.Done(); metas, metasErr = ListServiceMetas(stateDir) }()
	go func() { defer wg.Done(); ws, wsErr = CurrentWorkSession(stateDir) }()
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
	// projectErr is intentionally non-fatal: project Name is cosmetic. A
	// missing project (deleted, permissions changed, stale projectID) should
	// still yield a renderable envelope with the ID alone.
	_ = projectErr

	var self *SelfService
	if rt.InContainer && rt.ServiceName != "" {
		self = &SelfService{Hostname: rt.ServiceName}
	}

	snapshots := buildServiceSnapshots(services, metas, ws, selfHostnameFromRT(rt))

	var wsSummary *WorkSessionSummary
	if ws != nil {
		wsSummary = buildWorkSessionSummary(ws)
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
		// Bootstrap and Recipe are left nil here — the bootstrap conductor
		// populates them on a per-render synthetic envelope (see
		// bootstrap_guide_assembly.go::synthesisEnvelope), not from disk.
		Generated: now.UTC(),
	}, nil
}

// deriveIdleScenario classifies the idle phase into one of four sub-cases
// based on service composition. Returns "" for non-idle phases. Partitions
// services the same way planIdle does: managed services don't count toward
// any runtime bucket (they are data stores, not deploy targets).
//
// Priority: incomplete > bootstrapped > adopt > empty.
//   - Incomplete wins because a ServiceMeta tagged to a prior session
//     signals an interrupted bootstrap; atoms gated here surface resume.
//   - Bootstrapped + adopt continue per the original logic.
//   - Empty when no runtime services exist.
//
// Stale ServiceMetas (disk records whose live counterpart is gone) used to
// route to a dedicated `orphan` scenario; E3 (engine plan 2026-04-27) made
// orphan cleanup a transparent side-effect of bootstrap-start, so an
// orphan-only project now collapses to IdleEmpty.
func deriveIdleScenario(phase Phase, services []ServiceSnapshot) IdleScenario {
	if phase != PhaseIdle {
		return ""
	}
	// Managed deps don't drive the runtime buckets (bootstrap/adopt/resume) —
	// the bucket counts only consider runtime services.
	var bootstrapped, adoptable, resumable int
	for _, svc := range services {
		if svc.RuntimeClass == topology.RuntimeManaged {
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

// selfHostnameFromRT returns the container's own hostname or "" locally.
// Split out so derivePhase / buildServiceSnapshots can share one source.
func selfHostnameFromRT(rt runtime.Info) string {
	if rt.InContainer {
		return rt.ServiceName
	}
	return ""
}

// buildServiceSnapshots turns (platform services, local metas, session history)
// into the envelope's Services field. Skips system containers and the
// self-service. Output is sorted by hostname for determinism.
//
// ws is optional — when nil, Deployed falls back purely to platform signals.
// When present, a service with a recorded successful deploy in the session
// history is marked Deployed even if platform state hasn't caught up.
func buildServiceSnapshots(
	services []platform.ServiceStack,
	metas []*ServiceMeta,
	ws *WorkSession,
	selfHostname string,
) []ServiceSnapshot {
	metaByHost := ManagedRuntimeIndex(metas)

	out := make([]ServiceSnapshot, 0, len(services))
	for _, svc := range services {
		if svc.IsSystem() {
			continue
		}
		if selfHostname != "" && svc.Name == selfHostname {
			continue
		}
		out = append(out, buildOneSnapshot(svc, metaByHost[svc.Name], ws))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hostname < out[j].Hostname })
	return out
}

func buildOneSnapshot(svc platform.ServiceStack, meta *ServiceMeta, ws *WorkSession) ServiceSnapshot {
	typeVersion := svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
	snap := ServiceSnapshot{
		Hostname:     svc.Name,
		TypeVersion:  typeVersion,
		Status:       svc.Status,
		RuntimeClass: classifyEnvelopeRuntime(typeVersion),
	}
	if meta != nil && meta.IsComplete() {
		snap.Bootstrapped = true
		snap.Deployed = DeriveDeployed(svc.Name, svc.Status, meta, ws)
		snap.Mode = resolveEnvelopeMode(meta, svc.Name)
		snap.CloseDeployMode = meta.CloseDeployMode
		if snap.CloseDeployMode == "" {
			snap.CloseDeployMode = topology.CloseModeUnset
		}
		snap.GitPushState = meta.GitPushState
		snap.BuildIntegration = meta.BuildIntegration
		snap.RemoteURL = meta.RemoteURL
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

// StatusActive is the platform Status string that indicates a service is
// running. Re-declared at package level (rather than importing from
// internal/tools) so workflow-internal deploy-state derivation has no
// outside dependency.
const StatusActive = "ACTIVE"

// DeriveDeployed answers "has this service ever received a real code deploy?"
// Three signals, OR-composed:
//
//  1. meta.FirstDeployedAt — persistent stamp from a prior successful deploy
//     (recorded by RecordDeployAttempt). Survives session closure; this is
//     the authoritative signal for ZCP-driven flows after the first cycle.
//  2. HasSuccessfulDeployFor — current session has recorded a successful
//     deploy attempt. Covers the window between the deploy landing and the
//     stamp reaching meta (same tick, but belt-and-suspenders).
//  3. meta.IsAdopted() AND platform.Status == ACTIVE — services that were
//     running before ZCP touched them (the fizzy-export case). Auto-adoption
//     also stamps FirstDeployedAt so this path is primarily a fallback for
//     legacy metas written before the stamping code shipped.
//
// Fresh ZCP bootstrap (non-empty BootstrapSession) with empty
// FirstDeployedAt and no session-recorded deploy correctly reports
// Deployed=false, so the develop first-deploy branch fires even though
// the platform may show Status=ACTIVE (startWithoutCode trap).
//
// hostname must match the platform service name. meta is the local record
// for that hostname (or its paired dev hostname); nil → Deployed=false.
// ws is optional; when nil only signals 1 and 3 apply.
func DeriveDeployed(hostname, status string, meta *ServiceMeta, ws *WorkSession) bool {
	if meta != nil && meta.IsDeployed() {
		return true
	}
	if HasSuccessfulDeployFor(ws, hostname) {
		return true
	}
	if meta != nil && meta.IsAdopted() && status == StatusActive {
		return true
	}
	return false
}

// classifyEnvelopeRuntime maps a service type to the envelope's RuntimeClass
// vocabulary. Distinct from `verify.classifyRuntime` — verify's enum is
// check-dispatch oriented (Worker = dynamic-without-ports), while the
// envelope models managed services as a first-class class (not "skip all
// checks") and collapses worker/dynamic into one class because no atom
// needs the port distinction.
func classifyEnvelopeRuntime(typeVersion string) topology.RuntimeClass {
	if typeVersion == "" {
		return topology.RuntimeUnknown
	}
	if topology.IsManagedService(typeVersion) {
		return topology.RuntimeManaged
	}
	lower := strings.ToLower(typeVersion)
	if strings.HasPrefix(lower, "php-apache") || strings.HasPrefix(lower, "php-nginx") {
		return topology.RuntimeImplicitWeb
	}
	if strings.HasPrefix(lower, "static") || strings.HasPrefix(lower, "nginx") {
		return topology.RuntimeStatic
	}
	return topology.RuntimeDynamic
}

// resolveEnvelopeMode maps a service's (meta, hostname) pair to the envelope
// Mode enum. Role-based so the dev half and stage half of a standard pair
// get distinct modes (ModeStandard vs ModeStage) even though they share a
// single ServiceMeta record. Dev-only services get ModeDev; simple stays
// ModeSimple. meta.RoleFor already encodes the mode+environment+hostname
// lookup — we reuse it here instead of duplicating the rules.
func resolveEnvelopeMode(meta *ServiceMeta, hostname string) topology.Mode {
	if meta == nil {
		return ""
	}
	switch meta.RoleFor(hostname) {
	case topology.DeployRoleStage:
		return topology.ModeStage
	case topology.DeployRoleSimple:
		return topology.ModeSimple
	case topology.DeployRoleDev, topology.PlanModeStandard, topology.PlanModeLocalStage, topology.PlanModeLocalOnly:
		// PrimaryRole returns Dev for both PlanModeDev (standalone dev) and
		// PlanModeStandard's dev half. Split them here so standard-only atoms
		// don't fire for dev-only services and vice versa. Local topologies
		// carry their own Mode values that project unchanged.
		if meta.Mode == topology.PlanModeStandard {
			return topology.ModeStandard
		}
		return topology.ModeDev
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

// deployAttemptsToInfo projects persisted deploy attempts into the envelope
// shape. Carries Setup/Strategy unconditionally (informational on both
// success and failure) and Reason/FailureClass only when the attempt
// failed — the LLM treats absence of those fields as "this attempt
// succeeded; nothing to recover from".
func deployAttemptsToInfo(attempts []DeployAttempt) []AttemptInfo {
	out := make([]AttemptInfo, 0, len(attempts))
	for i, a := range attempts {
		info := AttemptInfo{
			At:        parseOrZero(firstNonEmpty(a.SucceededAt, a.AttemptedAt)),
			Success:   a.SucceededAt != "",
			Iteration: i + 1,
			Setup:     a.Setup,
			Strategy:  a.Strategy,
		}
		if !info.Success {
			info.Reason = a.Error
			info.FailureClass = a.FailureClass
		}
		out = append(out, info)
	}
	return out
}

// verifyAttemptsToInfo projects persisted verify attempts into the envelope
// shape. Summary is the brief outcome string and is preserved on both pass
// (e.g. "healthy") and fail (the failing check name + detail). Reason and
// FailureClass duplicate Summary on failure so render/plan code can branch
// on the same fields used for deploy attempts.
func verifyAttemptsToInfo(attempts []VerifyAttempt) []AttemptInfo {
	out := make([]AttemptInfo, 0, len(attempts))
	for i, a := range attempts {
		info := AttemptInfo{
			At:        parseOrZero(firstNonEmpty(a.PassedAt, a.AttemptedAt)),
			Success:   a.Passed,
			Iteration: i + 1,
			Summary:   a.Summary,
		}
		if !info.Success {
			info.Reason = a.Summary
			info.FailureClass = a.FailureClass
		}
		out = append(out, info)
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
