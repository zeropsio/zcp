package workflow

import (
	"cmp"
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
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
// BootstrapState on every per-step render.

// LeaseReader reports the project's pool-container leases, reconciled against
// the pool orchestrators. It is injected (WithLeaseReader) so workflow does not
// import ops. A reader error means the orchestrator could not be asked:
// ComputeEnvelope then keeps zcp's own lease records' last known state and
// marks it unverified. A reader reports reconciled state only: whatever
// activity or unsaved work its snapshots carry is dropped, as those come from
// WithLeaseActivity alone.
type LeaseReader func(ctx context.Context) ([]LeaseSnapshot, error)

// LeaseRead is what a zerops_lease call read of one lease's checkout
// (docs/spec-workflows.md §8 DP-21): its activity and its unsaved work, each
// nil when the call did not read it.
type LeaseRead struct {
	Activity *topology.LeaseActivity
	Unsaved  *topology.LeaseUnsaved
}

// EnvelopeOption tunes ComputeEnvelope's inputs beyond the platform client.
type EnvelopeOption func(*envelopeConfig)

type envelopeConfig struct {
	leases       LeaseReader
	leaseReads   map[string]LeaseRead
	leasesAlways bool
}

// WithLeaseReader injects the reader that fills StateEnvelope.Leases. Without
// it the envelope carries no leases.
func WithLeaseReader(r LeaseReader) EnvelopeOption {
	return func(c *envelopeConfig) { c.leases = r }
}

// WithLeasesAlwaysRead asks the reader whatever the project holds — the
// zerops_lease call's own envelope, whose leases are what the call is about:
// a release of the last lease of a pool no pair is keyed by carries the
// emptied list, never an absent key a client would keep its stale list for.
func WithLeasesAlwaysRead() EnvelopeOption {
	return func(c *envelopeConfig) { c.leasesAlways = true }
}

// WithLeaseActivity lays what this call read of its leases' checkouts onto
// leases[] (docs/spec-mate.md §2.8a.3), keyed by alias the way WithParts keys
// devServers: a lease the call read carries its activity and unsaved work,
// every other lease neither, and a read of an alias leases[] does not list
// adds nothing. Only the zerops_lease call that made the reads passes it; the
// lease reader never reads a checkout, so no other tool's envelope pays for
// one.
func WithLeaseActivity(reads map[string]LeaseRead) EnvelopeOption {
	return func(c *envelopeConfig) { c.leaseReads = reads }
}

// ComputeEnvelope is the single entry point for computing state. Every
// workflow-aware tool handler calls this.
//
// I/O parallelism: the three dependent reads (platform ListServicesDirect,
// local ServiceMeta list, WorkSession load) are independent and run
// concurrently. The service list is the DIRECT read, not the ES search: it
// is lag-free (a just-imported service is in the envelope at once) and it is
// the only list whose active app version names its source — which is how a
// runtime imported startWithoutCode reads as not deployed (HasDeployedCode).
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
	opts ...EnvelopeOption,
) (StateEnvelope, error) {
	var cfg envelopeConfig
	for _, opt := range opts {
		opt(&cfg)
	}
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
		go func() { defer wg.Done(); services, servicesErr = client.ListServicesDirect(ctx, projectID) }()
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
	if rt.MateEnabled {
		markStandIns(snapshots, metas)
	}

	var wsSummary *WorkSessionSummary
	if ws != nil {
		wsSummary = buildWorkSessionSummary(stateDir, ws)
	}

	phase := derivePhase(ws, stateDir)

	projectSummary := ProjectSummary{ID: projectID}
	if project != nil {
		projectSummary.Name = project.Name
	}

	leases := readLeases(ctx, cfg, stateDir, metas)

	return StateEnvelope{
		Phase:        phase,
		Environment:  env,
		IdleScenario: deriveIdleScenario(phase, snapshots),
		SelfService:  self,
		Project:      projectSummary,
		Services:     snapshots,
		Leases:       leases,
		WorkSession:  wsSummary,
		// Bootstrap and Recipe are left nil here — the bootstrap conductor
		// populates them on a per-render synthetic envelope (see
		// bootstrap_guide_assembly.go::synthesisEnvelope), not from disk.
		Generated: now.UTC(),
	}, nil
}

// leaseStateHeld is the state of a lease zcp holds — the reader's verbatim
// state for a reserved container whose record is zcp's own.
const leaseStateHeld = "held"

// leaseStateRecorded is the state of a lease read from zcp's own record: the record is
// only written by a successful acquire, so "held" is the last known state.
const leaseStateRecorded = leaseStateHeld

// readLeases fills StateEnvelope.Leases (nil: not read). The reader is asked
// only when zcp could hold a lease the envelope is for — zcp has a lease
// record, or a pair is keyed by its dev pool (metas) — or the call asks always
// (WithLeasesAlwaysRead). A project without either costs nothing, a pool an
// app calls as its own infrastructure included: its reserved containers are
// zerops_lease action=status's to list. When the reader cannot answer
// (the orchestrator answers only inside the project), the records' last known
// state is reported, marked unverified; an envelope never fails over it.
// Only the non-secret fields of a record are ever decoded (DP-3). Each lease
// carries the activity and unsaved work of reads (WithLeaseActivity) and no
// other, and why it is not answering from its record alone.
func readLeases(ctx context.Context, cfg envelopeConfig, stateDir string, metas []*ServiceMeta) *[]LeaseSnapshot {
	reader, reads := cfg.leases, cfg.leaseReads
	if reader == nil {
		return nil
	}
	records := readLeaseRecordSnapshots(stateDir)
	if len(records) == 0 && !cfg.leasesAlways && !slices.ContainsFunc(metas, (*ServiceMeta).DevIsPool) {
		return nil
	}
	leases, err := reader(ctx)
	if err != nil {
		leases = records
	} else {
		// The records as the reader left them: a lease it found not answering
		// is recorded so by now, and a record it dropped carries nothing.
		records = readLeaseRecordSnapshots(stateDir)
	}
	notAnswering := make(map[string]string, len(records))
	known := make(map[string]LeaseSnapshot, len(records))
	for _, r := range records {
		notAnswering[r.Alias] = r.NotAnswering
		known[r.Alias] = r
	}
	// Read: a reader that answered (or records that stood in) with nothing is
	// the empty array on the wire, never an absent key.
	leases = append([]LeaseSnapshot{}, leases...)
	for i := range leases {
		read := reads[leases[i].Alias]
		leases[i].Activity, leases[i].Unsaved = read.Activity, read.Unsaved
		leases[i].NotAnswering = notAnswering[leases[i].Alias]
		// Every envelope carries the last-known dev servers of a lease zcp
		// records, with when they were read; a reader names none of its own.
		if r, ok := known[leases[i].Alias]; ok && leases[i].DevServers == nil &&
			(leases[i].AcquiredAt == "" || r.AcquiredAt == "" || leases[i].AcquiredAt == r.AcquiredAt) {
			leases[i].DevServers, leases[i].DevServersReadAt = r.DevServers, r.DevServersReadAt
		}
	}
	slices.SortStableFunc(leases, func(a, b LeaseSnapshot) int {
		if c := cmp.Compare(a.Pool, b.Pool); c != 0 {
			return c
		}
		return cmp.Compare(leaseNumber(a.Alias), leaseNumber(b.Alias))
	})
	return &leases
}

// leaseNumber is the container number at the end of an alias (<pool>-<n>).
func leaseNumber(alias string) int {
	i := strings.LastIndex(alias, "-")
	n, err := strconv.Atoi(alias[i+1:])
	if err != nil {
		return 0
	}
	return n
}

// leaseAliasRe is the shape of a lease alias (spec-workflows.md §8 DP-1); it
// keeps a record from naming a path outside the lease directory.
var leaseAliasRe = regexp.MustCompile(`^[a-z][a-z0-9]{0,39}-[0-9]+$`)

// leaseRecordFields are the non-secret fields of a lease record
// (.zcp/state/leases/<alias>.json, spec-workflows.md §8 DP-2). The record's
// lease string is deliberately not a field here: it is never decoded.
type leaseRecordFields struct {
	PoolHostname      string `json:"poolHostname"`
	ContainerNumber   int64  `json:"containerNumber"`
	ContainerHostname string `json:"containerHostname"`
	Branch            string `json:"branch"`
	Repo              string `json:"repo"`
	AcquiredAt        string `json:"acquiredAt"`
	NotAnswering      string `json:"notAnswering"`
	// DevServers and DevServersReadAt are the last read of the lease's
	// supervisor any zcp call made, and when.
	DevServers       []topology.DevServerState `json:"devServers"`
	DevServersReadAt string                    `json:"devServersReadAt"`
}

// readLeaseRecordSnapshots reads zcp's lease records as unverified "held"
// snapshots. A record that cannot be read, or whose alias does not match its
// file name, names no lease and is skipped.
func readLeaseRecordSnapshots(stateDir string) []LeaseSnapshot {
	if stateDir == "" {
		return nil
	}
	dir := filepath.Join(stateDir, "leases")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []LeaseSnapshot
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var r leaseRecordFields
		if json.Unmarshal(raw, &r) != nil {
			continue
		}
		alias := r.PoolHostname + "-" + strconv.FormatInt(r.ContainerNumber, 10)
		if alias+".json" != e.Name() || !leaseAliasRe.MatchString(alias) {
			continue
		}
		out = append(out, LeaseSnapshot{
			Alias: alias, Pool: r.PoolHostname, Branch: r.Branch,
			ContainerHostname: r.ContainerHostname, State: leaseStateRecorded, Unverified: true,
			AcquiredAt: r.AcquiredAt, Repo: topology.RepoIdentity(r.Repo), NotAnswering: r.NotAnswering,
			DevServers: r.DevServers, DevServersReadAt: r.DevServersReadAt,
		})
	}
	return out
}

// deriveIdleScenario classifies the idle phase into one of five sub-cases
// based on service composition. Returns "" for non-idle phases. Partitions
// services the same way planIdle does: managed services don't count toward
// any runtime bucket (they are data stores, not deploy targets).
//
// Priority: incomplete > bootstrapped > adopt > dev-pool > empty.
//   - Incomplete wins because a ServiceMeta tagged to a prior session
//     signals an interrupted bootstrap; atoms gated here surface resume.
//   - Bootstrapped + adopt continue per the original logic.
//   - Dev-pool when no app runtime exists but a Swarm pool does: its work is
//     a lease (docs/spec-workflows.md §8 DP-29), never bootstrap or adopt.
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
	// Managed deps, pools and Boxes don't drive the runtime buckets
	// (bootstrap/adopt/resume) — the bucket counts only consider runtime
	// services; a pool or Box is never an adoptable target (PS-2).
	var bootstrapped, adoptable, resumable, pools int
	for _, svc := range services {
		if svc.RuntimeClass == topology.RuntimePool {
			pools++
			continue
		}
		if svc.RuntimeClass == topology.RuntimeManaged ||
			svc.RuntimeClass == topology.RuntimeSandbox {
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
	if pools > 0 {
		return IdleDevPool
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
			TrackedRef:       m.TrackedRef,
			FeedsProduction:  prodLaunchRefsRender(m.ProdLaunches),
			SetupName:        m.PrimarySetupName,
		}
		normalizeDeployDims(&snap) // TOPO-1: heal empty dims (parity with buildOneSnapshot)
		out = append(out, snap)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hostname < out[j].Hostname })
	return out
}

// markStandIns marks each snapshot of a dev service pool-migrate holds as its
// repository's stand-in (ServiceMeta.PoolMigrateStandIn): a Mate's client
// shows it as the step on the way it is (docs/spec-workflows.md §13 PM-14).
func markStandIns(snapshots []ServiceSnapshot, metas []*ServiceMeta) {
	byHost := ManagedRuntimeIndex(metas)
	for i := range snapshots {
		if byHost[snapshots[i].Hostname].PoolMigrateStandIn(snapshots[i].Hostname) {
			snapshots[i].StandIn = &topology.StandIn{For: topology.StandInPoolMigrate}
		}
	}
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
	if snap.RuntimeClass != topology.RuntimeManaged {
		// Untracked runtime: zcp has no record of its own, so the platform
		// is the whole answer — false for one imported without code.
		snap.Deployed = svc.HasDeployedCode()
	}
	if meta != nil && meta.IsComplete() {
		snap.Bootstrapped = true
		snap.Deployed = DeriveDeployed(svc.Name, &svc, meta, ws)
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
		snap.TrackedRef = meta.TrackedRef
		snap.FeedsProduction = prodLaunchRefsRender(meta.ProdLaunches)
		// Setup-name projection from meta (canonical store on disk).
		// SetupName via SetupNameFor picks the right field for this
		// hostname (Primary for Hostname, the stage's own for a stage).
		// The dev/primary side of a pair carries its stages; the wire
		// fields StageHostname/StageSetupName name the first one.
		snap.SetupName = meta.SetupNameFor(svc.Name)
		// A pool keying its pair is the pair's dev half (its Mode is "":
		// ModeFor projects no dev-runtime mode onto a pool); it carries the
		// stages and the HQ record below like any dev half.
		snap.DevIsPool = meta.DevIsPool() && svc.Name == meta.Hostname
		if meta.HQ != nil && svc.Name == meta.Hostname {
			snap.HQRepo, snap.HQOrigin = meta.HQ.Repo, meta.HQ.Origin
		}
		if meta.HasStage() && svc.Name == meta.Hostname {
			first := meta.FirstStage()
			snap.StageHostname = first.Hostname
			snap.StageSetupName = first.SetupName
			snap.Stages = slices.Clone(meta.Stages)
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
// Four signals, OR-composed:
//
//  1. meta.FirstDeployedAt — persistent stamp from a prior successful deploy
//     (recorded by RecordDeployAttempt). Survives session closure; this is
//     the authoritative signal for ZCP-driven flows after the first cycle.
//  2. HasSuccessfulDeployFor — current session has recorded a successful
//     deploy attempt. Covers the window between the deploy landing and the
//     stamp reaching meta (same tick, but belt-and-suspenders).
//  3. meta.IsAdopted() AND the live service is ACTIVE with deployed code
//     (platform.ServiceStack.HasDeployedCode) — services that were running
//     before ZCP touched them (the fizzy-export case). ACTIVE alone is not
//     enough: a runtime imported startWithoutCode is ACTIVE too, holding
//     only a placeholder version, and adopting it must leave it
//     never-deployed so develop runs its first deploy (the Beviro trial,
//     2026-09-29: a Mate's recipe runtimes read "active and deployed").
//  4. meta.ProvisionedFromGit AND live ACTIVE — see below.
//
// Fresh ZCP bootstrap (non-empty BootstrapSession) with empty
// FirstDeployedAt and no session-recorded deploy correctly reports
// Deployed=false, so the develop first-deploy branch fires even though
// the platform may show Status=ACTIVE (startWithoutCode trap).
//
// hostname must match the platform service name. live is the service as
// the platform lists it, nil when it is not live. meta is the local record
// for that hostname (or its paired dev hostname); nil → Deployed=false.
// ws is optional; when nil only signals 1, 3 and 4 apply.
//
// The stamp is the pair's, shared by its dev half and every stage, so it
// cannot speak for a service the platform still lists READY_TO_DEPLOY
// without deployed code: no deploy has landed there, unless this session
// recorded one (the list lags the deploy).
func DeriveDeployed(hostname string, live *platform.ServiceStack, meta *ServiceMeta, ws *WorkSession) bool {
	if HasSuccessfulDeployFor(ws, hostname) {
		return true
	}
	if live != nil && live.Status == platform.ServiceStatusReadyToDeploy && !live.HasDeployedCode() {
		return false
	}
	if meta != nil && meta.IsDeployed() {
		return true
	}
	active := live != nil && live.Status == StatusActive
	if meta != nil && meta.IsAdopted() && active && live.HasDeployedCode() {
		return true
	}
	// B4/F11: a recipe-buildFromGit runtime is deployed by the platform at
	// import — once it reaches ACTIVE it is serving curated code, not awaiting
	// a first deploy. Classic metas never carry ProvisionedFromGit.
	if meta != nil && meta.ProvisionedFromGit && active {
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
			At:           parseOrZero(firstNonEmpty(a.SucceededAt, a.AttemptedAt)),
			Success:      a.SucceededAt != "",
			Iteration:    i + 1,
			Setup:        a.Setup,
			Strategy:     a.Strategy,
			SHA:          a.SHA,
			AppVersionID: a.AppVersionID,
			Dirty:        a.Dirty,
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
