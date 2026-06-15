package workflow

import (
	"context"
	"maps"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
)

// StateEnvelope.Bootstrap is populated by bootstrap_guide_assembly.go's
// synthesisEnvelope helper, NOT by ComputeEnvelope. ComputeEnvelope leaves
// it nil; the bootstrap conductor builds a synthetic summary from the live
// BootstrapState on every per-step render.

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
		wsSummary = buildWorkSessionSummary(stateDir, ws)
	}

	phase := derivePhase(ws, stateDir)

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
	// Synthetic snapshots for local-only metas. Local-only projects have
	// no Zerops-side runtime to iterate, so without this tail the meta
	// would never produce a snapshot and atoms with `modes: [...,
	// local-only]` had no service to fire on. RuntimeClass stays empty
	// — there's no linked runtime to classify; runtime-gated atoms
	// intentionally don't match for local-only.
	for _, m := range metas {
		if m == nil || m.Mode != topology.PlanModeLocalOnly {
			continue
		}
		snap := ServiceSnapshot{
			Hostname:         m.Hostname,
			Mode:             m.Mode,
			Bootstrapped:     m.IsComplete(),
			CloseDeployMode:  m.CloseDeployMode,
			GitPushState:     m.GitPushState,
			BuildIntegration: m.BuildIntegration,
			RemoteURL:        m.RemoteURL,
			FeedsProduction:  prodLaunchRefsRender(m.ProdLaunches),
			SetupName:        m.PrimarySetupName,
		}
		normalizeDeployDims(&snap) // TOPO-1: heal empty dims (parity with buildOneSnapshot)
		out = append(out, snap)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hostname < out[j].Hostname })
	return out
}

// normalizeDeployDims fills the three orthogonal deploy dimensions with
// their canonical zero values when empty. The atom matcher
// (serviceSatisfiesAxes) uses slices.Contains over axis allowlists like
// [unconfigured,broken] / [none], which an empty string never satisfies —
// so an empty on-disk dimension silently suppressed the git-push-setup and
// build-integration atoms for exactly the local-stage mode they target
// (TOPO-1/WF-2/DELIV-2). Healing here (every read rebuilds a fresh
// snapshot) covers metas written before NewServiceMeta stamped the dims.
func normalizeDeployDims(snap *ServiceSnapshot) {
	if snap.CloseDeployMode == "" {
		snap.CloseDeployMode = topology.CloseModeUnset
	}
	if snap.GitPushState == "" {
		snap.GitPushState = topology.GitPushUnconfigured
	}
	if snap.BuildIntegration == "" {
		snap.BuildIntegration = topology.BuildIntegrationNone
	}
}

func buildOneSnapshot(svc platform.ServiceStack, meta *ServiceMeta, ws *WorkSession) ServiceSnapshot {
	typeVersion := svc.ServiceStackTypeInfo.ServiceStackTypeVersionName
	snap := ServiceSnapshot{
		Hostname:     svc.Name,
		TypeVersion:  typeVersion,
		Status:       svc.Status,
		RuntimeClass: topology.RuntimeClassFor(typeVersion),
	}
	if meta != nil && meta.IsComplete() {
		snap.Bootstrapped = true
		snap.Deployed = DeriveDeployed(svc.Name, svc.Status, meta, ws)
		snap.Mode = meta.ModeFor(svc.Name)
		snap.CloseDeployMode = meta.CloseDeployMode
		snap.GitPushState = meta.GitPushState
		snap.BuildIntegration = meta.BuildIntegration
		// TOPO-1: heal empty deploy dimensions to their canonical zero
		// values so the atom matcher (serviceSatisfiesAxes / slices.Contains,
		// which "" never satisfies) fires the git-push-setup + build-
		// integration atoms for local-stage metas written before the
		// NewServiceMeta constructor stamped them. Previously only
		// CloseDeployMode was normalized here; GitPushState/BuildIntegration
		// were copied raw, so the atom chain silently never fired.
		normalizeDeployDims(&snap)
		snap.RemoteURL = meta.RemoteURL
		snap.FeedsProduction = prodLaunchRefsRender(meta.ProdLaunches)
		if meta.StageHostname != "" && svc.Name == meta.Hostname {
			snap.StageHostname = meta.StageHostname
		}
		// Setup-name projection from meta (canonical store on disk).
		// SetupName via SetupNameFor picks the right field for this
		// hostname (Primary for Hostname, Stage for StageHostname).
		// StageSetupName projects the paired half's value when this
		// snapshot represents the dev/primary side of a pair.
		snap.SetupName = meta.SetupNameFor(svc.Name)
		if meta.StageHostname != "" && svc.Name == meta.Hostname {
			snap.StageSetupName = meta.StageSetupName
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
	// B4/F11: a recipe-buildFromGit runtime is deployed by the platform at
	// import — once it reaches ACTIVE it is serving curated code, not awaiting
	// a first deploy. Mirrors the adopted signal above; classic metas never
	// carry ProvisionedFromGit, so this can't false-positive a startWithoutCode
	// dev container (whose status is RUNNING/READY_TO_DEPLOY, not ACTIVE, until
	// real code lands).
	if meta != nil && meta.ProvisionedFromGit && status == StatusActive {
		return true
	}
	return false
}

// classifyEnvelopeRuntime is preserved as a thin alias for topology's
// authoritative classifier so callers in this package keep their
// envelope-shaped naming. New call sites should use topology.RuntimeClassFor
// directly. The envelope's RuntimeClass and verify's classifyRuntime remain
// distinct enums — verify treats Worker (dynamic-without-ports) as its own
// class for check-dispatch, while the envelope collapses worker/dynamic.
func classifyEnvelopeRuntime(typeVersion string) topology.RuntimeClass {
	return topology.RuntimeClassFor(typeVersion)
}

// buildWorkSessionSummary adapts the persisted WorkSession into its envelope
// projection. Attempts are re-encoded with typed time fields and an iteration
// counter derived from slice index.
func buildWorkSessionSummary(stateDir string, ws *WorkSession) *WorkSessionSummary {
	summary := &WorkSessionSummary{
		Intent:    ws.Intent,
		Services:  append([]string(nil), ws.Services...),
		CreatedAt: parseOrZero(ws.CreatedAt),
	}
	if len(ws.Roles) > 0 {
		summary.Roles = make(map[string]string, len(ws.Roles))
		maps.Copy(summary.Roles, ws.Roles)
	}
	// Close state (persisted explicit/iteration-cap, or DERIVED auto-complete)
	// from the single resolver so phase + summary + annotations never disagree.
	if closed, closedAt, reason := DeriveCloseState(stateDir, ws); closed {
		t := parseOrZero(closedAt)
		summary.ClosedAt = &t
		summary.CloseReason = reason
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

// Focus is the precedence-resolved primary slot for the current PID, computed
// from DISK (the registry's infra sessions + the work-session file) — NEVER
// from an in-memory engine session pointer. ONE resolver (ResolveLifecycle)
// feeds both the envelope (derivePhase) and the dispatcher's status routing,
// killing the SPINE-1 split where the two read opposite precedence from
// different sources (envelope work-first on disk vs dispatcher infra-first on
// in-memory e.sessionID).
type Focus int

const (
	// FocusIdle — no infra session and no open/auto-closed work session.
	FocusIdle Focus = iota
	// FocusWork — a develop work session is the primary (open, or auto-closed
	// and awaiting the explicit close+next).
	FocusWork
	// FocusBootstrap — an infra-layer session foregrounds work.
	FocusBootstrap
)

// ResolveLifecycle returns the focus for the current PID per the focus rule
// (spec-work-session.md §5.3/§6.2): an infra-layer session (bootstrap)
// FOREGROUNDS the work session — infra wins, else an open/auto-closed work
// session, else idle. `ws` is the already-loaded work session for this PID
// (nil if none); the infra slot is read from the registry on disk. The
// registry read is best-effort (a failure degrades to no-infra) so the
// envelope is always producible.
func ResolveLifecycle(stateDir string, ws *WorkSession) Focus {
	if infraPhaseForPID(stateDir) == PhaseBootstrapActive {
		return FocusBootstrap
	}
	if ws != nil && (ws.ClosedAt == "" || ws.CloseReason == CloseReasonAutoComplete) {
		return FocusWork
	}
	return FocusIdle
}

// derivePhase projects the resolved Focus onto the Phase enum. Infra-first per
// the focus rule (the SPINE-1 fix): an open work session that coexists with a
// bootstrap session now resolves to the infra phase (was develop-active
// under the old work-first ordering), matching the dispatcher.
func derivePhase(ws *WorkSession, stateDir string) Phase {
	switch ResolveLifecycle(stateDir, ws) {
	case FocusBootstrap:
		return PhaseBootstrapActive
	case FocusWork:
		// develop-closed-auto is DERIVED (all declared services deployed+verified),
		// never stamped — so phase agrees with the summary and annotations.
		if closed, _, _ := DeriveCloseState(stateDir, ws); closed {
			return PhaseDevelopClosed
		}
		return PhaseDevelopActive
	case FocusIdle:
		return PhaseIdle
	}
	return PhaseIdle // unreachable: Focus is exhaustively handled above
}

// infraPhaseForPID returns bootstrap-active when a non-work session is
// registered for the running PID. Returns "" when none exists.
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
		// ListSessions does NOT prune; gate on two-state liveness so a dead
		// predecessor's bootstrap entry whose PID was recycled to THIS
		// process does not foreground a ghost infra phase over the real work
		// session (parity with ClassifySessions / checkHostnameLocks / the P6
		// identity story). isProcessAlive biases alive on an unreadable clock.
		if s.PID != pid || !isProcessAlive(s.PID, s.StartTime) {
			continue
		}
		if s.Workflow == WorkflowBootstrap {
			return PhaseBootstrapActive
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

// prodLaunchRefsRender formats the F4 back-references for snapshot
// projection: "name (projectID)" per promotion, stable order.
func prodLaunchRefsRender(refs []ProdLaunchRef) []string {
	if len(refs) == 0 {
		return nil
	}
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		label := r.ProdProjectName
		if label == "" {
			label = "production"
		}
		out = append(out, label+" ("+r.ProdProjectID+")")
	}
	return out
}
