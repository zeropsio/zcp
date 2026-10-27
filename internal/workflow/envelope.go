package workflow

import (
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
)

// StateEnvelope captures all state needed to (a) synthesize knowledge,
// (b) produce the next-action plan. It is computed once per tool response
// and embedded verbatim in the response payload. Any two envelopes that
// serialize to the same JSON MUST produce the same synthesis — this is
// the compaction-safety invariant.
//
// Serialization invariant: slices are sorted by stable keys (services by
// hostname, attempts by time). encoding/json already sorts map keys
// alphabetically, so plain json.Marshal is deterministic once slice order
// is controlled at construction.
type StateEnvelope struct {
	Phase        Phase        `json:"phase"`
	Environment  Environment  `json:"environment"`
	IdleScenario IdleScenario `json:"idleScenario,omitempty"`
	// ExportStatus discriminates the sub-state of PhaseExportActive — see
	// topology.ExportStatus. Set by BuildExportEnvelope when the export
	// workflow handler routes through atom synthesis; zero-value (unset)
	// for non-export phases. Atoms filter on this via exportStatus:
	// frontmatter; service-scoped axes paired with this gate fire on the
	// single targetService snapshot the export envelope carries.
	ExportStatus topology.ExportStatus `json:"exportStatus,omitempty"`
	SelfService  *SelfService          `json:"selfService,omitempty"`
	Project      ProjectSummary        `json:"project"`
	Services     []ServiceSnapshot     `json:"services"`
	// Leases are the pool containers this project's zcp holds a lease on,
	// at the envelope's top level (a lease is per thread, not per service).
	// A pointer keeps "read" apart from "not read": nil — no reader, or a
	// project that could not hold a lease — drops the key; a pointer to an
	// empty slice is `"leases":[]` (read, none held). Additive to Mate's
	// contract (docs/spec-mate.md §2.8a.3). DP-3 (docs/spec-workflows.md §8):
	// a lease SECRET is never in the envelope — a snapshot carries the alias,
	// never the lease string.
	Leases      *[]LeaseSnapshot         `json:"leases,omitempty"`
	WorkSession *WorkSessionSummary      `json:"workSession,omitempty"`
	Bootstrap   *BootstrapSessionSummary `json:"bootstrap,omitempty"`
	Generated   time.Time                `json:"generated"`

	// HQChanges are a Mate's changes in HQ, per repository, as status names
	// them (RenderStatus's Changes section): zcp's own ledger and lease
	// records with each change's standing as the Mate's state names it now,
	// attached by status, off the envelope wire.
	HQChanges []HQRepoChanges `json:"-"`
	// HQChangesAsLastRead is HQChanges being the ledger's standing as last
	// told, HQ not answering status's read of the Mate's state.
	HQChangesAsLastRead bool `json:"-"`
}

// LeaseSnapshot is one pool-container lease as the envelope sees it: what the
// agent needs to address the container (alias, pool, branch, container
// hostname) and its reconciled state. It never carries the lease string.
type LeaseSnapshot struct {
	Alias             string `json:"alias"`
	Pool              string `json:"pool"`
	Branch            string `json:"branch"`
	ContainerHostname string `json:"containerHostname"`
	// State is the reconciled state the lease reader reports (held, lost,
	// unknown-holder, … — rendered verbatim, never enumerated here).
	State string `json:"state"`
	// Unverified marks a state read from zcp's own record because the pool's
	// orchestrator did not answer: last known, not confirmed.
	Unverified bool `json:"unverified"`
	// AcquiredAt is when zcp acquired the lease (RFC 3339, UTC), "" when it
	// holds no record of it (an unknown holder): the key a client rebinds by
	// when the bound lease of a branch ends and another stays held.
	AcquiredAt string `json:"acquiredAt,omitempty"`
	// Repo is the repository the lease's group is keyed by beside Branch
	// (docs/spec-workflows.md §8 DP-17) — canonical and credential-free
	// (topology.RepoIdentity) — so a client binds and rebinds a conversation
	// by repository and branch, never by branch alone (docs/spec-mate.md §6.3);
	// absent for a lease zcp holds no record of, or an older record naming none.
	Repo string `json:"repo,omitempty"`
	// Part is the part the lease's pool serves — its dev server's name, else
	// the pool-keyed pair's stage setup — and Stage the stage hostname that
	// pair pairs with; both absent where zcp knows neither (WithParts,
	// docs/spec-mate.md §2.8a.3).
	Part  string `json:"part,omitempty"`
	Stage string `json:"stage,omitempty"`
	// DevServers is what the lease's supervisor last reported — fresh on a
	// call that read it (zerops_dev_server on the alias, an acquire), keyed
	// by alias the way a dev service's devServers[] is keyed by its hostname
	// (WithParts), otherwise the last-known read from zcp's lease record —
	// and DevServersReadAt when that read was (RFC 3339, UTC); both absent
	// while no zcp call has read it.
	DevServers       []topology.DevServerState `json:"devServers,omitempty"`
	DevServersReadAt string                    `json:"devServersReadAt,omitempty"`
	// Activity and Unsaved are what a zerops_lease call read of the lease's
	// checkout (docs/spec-workflows.md §8 DP-21), only on that call
	// (WithLeaseActivity) — absent means not read in this call, never "no
	// activity" or "nothing unsaved". ComputeEnvelope drops any a lease
	// reader sets.
	Activity *topology.LeaseActivity `json:"activity,omitempty"`
	Unsaved  *topology.LeaseUnsaved  `json:"unsaved,omitempty"`
	// NotAnswering is why the last zcp call that ran a command in the lease's
	// container found it not answering — its process limit, or ssh not
	// reaching it — as zcp's lease record keeps it until a later call reaches
	// the container again; absent when that call reached it, or none ran.
	// ComputeEnvelope takes it from the record on every envelope, never from
	// an ssh read of its own (docs/spec-mate.md §2.8a.3).
	NotAnswering string `json:"notAnswering,omitempty"`
}

// LeaseList is the leases the envelope carries, nil when none were read or
// none are held.
func (e StateEnvelope) LeaseList() []LeaseSnapshot {
	if e.Leases == nil {
		return nil
	}
	return *e.Leases
}

// IdleScenario discriminates the sub-cases of PhaseIdle so atoms can
// filter on a single mutually-exclusive value instead of racing on overlapping
// service-count heuristics. Empty when Phase != idle.
type IdleScenario string

const (
	IdleEmpty        IdleScenario = "empty"        // no user services at all
	IdleBootstrapped IdleScenario = "bootstrapped" // at least one bootstrapped service
	IdleAdopt        IdleScenario = "adopt"        // only unmanaged runtimes, none bootstrapped
	// IdleIncomplete fires when at least one ServiceMeta is incomplete AND
	// carries a non-empty BootstrapSession — a prior bootstrap session that
	// was interrupted before writing BootstrappedAt. Resume takes priority
	// over adopt because ZCP already owns the service slot via that session.
	IdleIncomplete IdleScenario = "incomplete"
	// IdleDevPool fires when no app runtime exists — nothing to bootstrap,
	// adopt or resume — but at least one Swarm pool does: a part's dev
	// environment, worked on in a container leased per branch, never through
	// bootstrap or adopt (docs/spec-workflows.md §8 DP-29).
	IdleDevPool IdleScenario = "dev-pool"
)

// DeployState marks a bootstrapped service's deploy progression. The
// develop workflow branches on this: never-deployed services enter the
// first-deploy branch (scaffold + write + first deploy + stamp FirstDeployedAt);
// deployed services run the normal edit-loop branch.
type DeployState string

const (
	DeployStateNeverDeployed DeployState = "never-deployed"
	DeployStateDeployed      DeployState = "deployed"
)

// Phase enumerates the lifecycle states the envelope can describe.
type Phase string

const (
	PhaseIdle            Phase = "idle"
	PhaseBootstrapActive Phase = "bootstrap-active"
	PhaseDevelopActive   Phase = "develop-active"
	PhaseDevelopClosed   Phase = "develop-closed-auto"
	PhaseStrategySetup   Phase = "strategy-setup"
	PhaseExportActive    Phase = "export-active"
	// PhaseLaunchProductionActive fires when the launch-production
	// workflow is mid-flow on a source project. The workflow handler
	// emits its own atom guidance per LaunchProductionStatus; BuildPlan
	// returns an empty Plan so callers know there's nothing to suggest
	// outside the handler's response (same pattern as ExportActive).
	PhaseLaunchProductionActive Phase = "launch-production-active"
	// PhasePoolMigrateActive fires when zerops_workflow workflow="pool-migrate"
	// walks a part's dev service onto a dev pool. Like launch-production, the
	// handler emits its own atom guidance per step and BuildPlan returns an
	// empty Plan (docs/spec-workflows.md §13).
	PhasePoolMigrateActive Phase = "pool-migrate-active"
)

// SelfService names the ZCP host service when running in container environment.
type SelfService struct {
	Hostname string `json:"hostname"`
}

// ProjectSummary identifies the project the envelope describes.
type ProjectSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ServiceSnapshot is one service's point-in-time state inside the envelope.
//
// Per-pair deploy dimensions (CloseDeployMode / GitPushState /
// BuildIntegration / RemoteURL / TrackedRef) project ServiceMeta state onto
// the envelope so atoms filter on the orthogonal post-decomposition
// vocabulary. See plan
// `plans/archive/deploy-strategy-decomposition-2026-04-28.md` §3.1 for
// the orthogonality matrix.
type ServiceSnapshot struct {
	Hostname     string                `json:"hostname"`
	TypeVersion  string                `json:"typeVersion"`
	RuntimeClass topology.RuntimeClass `json:"runtimeClass"`
	Status       string                `json:"status"`
	// DeployHistory is the recovery-shape classification (`none` · `failed`
	// · `ok`) atom selection gates on via AxisVector.DeployHistories (spec-
	// workflows.md §8 R1/R3). Only meaningful when Status is
	// READY_TO_DEPLOY/FAILED; empty is the safe default (treated as "ok" by
	// effectiveDeployHistory in synthesize.go) until a caller populates it
	// from ops.ComputeRecoveryState.
	DeployHistory string        `json:"deployHistory,omitempty"`
	Bootstrapped  bool          `json:"bootstrapped"`
	Deployed      bool          `json:"deployed,omitempty"`
	Resumable     bool          `json:"resumable,omitempty"`
	Mode          topology.Mode `json:"mode,omitempty"`

	CloseDeployMode  topology.CloseDeployMode  `json:"closeDeployMode,omitempty"`
	GitPushState     topology.GitPushState     `json:"gitPushState,omitempty"`
	BuildIntegration topology.BuildIntegration `json:"buildIntegration,omitempty"`
	RemoteURL        string                    `json:"remoteUrl,omitempty"`
	// TrackedRef mirrors ServiceMeta.TrackedRef (GF-7, docs/spec-workflows.md
	// §12.6) — empty when no confirm has recorded one yet (readers apply the
	// "main" fallback independently; the envelope shows the raw recorded
	// value, never the derived default).
	TrackedRef string `json:"trackedRef,omitempty"`
	// FeedsProduction lists the production projects this pair was
	// promoted into ("name (projectID)") — the F4 post-launch
	// back-reference projected for render, so develop-side status can
	// say "this stage feeds production X".
	FeedsProduction []string `json:"feedsProduction,omitempty"`

	// StageHostname names the pair's first stage on the dev half's snapshot —
	// the one stage the envelope wire carries per pair. Stages holds every
	// stage of the pair, off the wire until the envelope contract names
	// them; read both through PairStageHostnames.
	StageHostname string              `json:"stageHostname,omitempty"`
	Stages        []topology.StageRef `json:"-"`

	// DevIsPool marks the snapshot of a pool that is a pair's dev half
	// (ServiceMeta.DevIsPool): it keys the pair, carries its stages and HQ
	// record, and has no Mode — no dev-runtime guidance applies to a pool.
	// Pure readers ask it before treating the pair's dev half as a container
	// (BuildPlan, Resolve). Off the wire: a client tells the pool by its
	// runtimeClass and the pair by its stages.
	DevIsPool bool `json:"-"`

	// HQRepo and HQOrigin name, on a wired pair's dev half, its repository in
	// HQ and how its code is kept (HQRepoRef.Origin, nil until read). Off the
	// envelope wire: status renders them.
	HQRepo   string    `json:"-"`
	HQOrigin *HQOrigin `json:"-"`
	// HQCheckoutOrigin is that half's checkout's origin as status read it
	// (HQOriginNone for none; "" unread), and HQOriginPending where the
	// wiring points it when that differs — the move develop's start makes.
	// HQOriginBlocked says why the wiring cannot name that target (no
	// organisation to address HQ's repository by).
	HQCheckoutOrigin string `json:"-"`
	HQOriginPending  string `json:"-"`
	HQOriginBlocked  string `json:"-"`

	// Setup-name projection from ServiceMeta (canonical store on disk).
	// SetupName mirrors meta.SetupNameFor(Hostname) for this half;
	// StageSetupName mirrors the first stage's setup on the dev half's
	// snapshot (empty when no pair). Populated by ComputeEnvelope from the
	// loaded meta; downstream consumers (DeployIntent.Resolve, launch
	// composer, build-integration generator) read from snapshot so they
	// stay pure and don't re-touch disk per call. Empty fields signal
	// cache-miss → cascade discovery + write-back via
	// ResolveCanonicalSetup. Plan: plans/setup-name-local-canonical-2026-05-27.md
	// §ServiceMeta schema.
	SetupName      string `json:"setupName,omitempty"`
	StageSetupName string `json:"stageSetupName,omitempty"`

	// WireStages and DevServers are the dev half's parts as the envelope
	// carries them from field-order step (3) of docs/spec-mate.md §2.8a.3;
	// only WithParts sets them. DevServers is absent when the call did not
	// read the supervisor — never "none running".
	WireStages []topology.StageWire      `json:"stages,omitempty"`
	DevServers []topology.DevServerState `json:"devServers,omitempty"`

	// StandIn marks, inside a Mate, a dev service pool-migrate holds as its
	// repository's stand-in (ServiceMeta.PoolMigrateStandIn): {for:
	// "pool-migrate"} beside its stages and dev servers, nil otherwise
	// (docs/spec-workflows.md §13 PM-14, docs/spec-mate.md §2.8a.3).
	StandIn *topology.StandIn `json:"standIn,omitempty"`

	// MultiPart is the mechanical multi-part signal of a dev half's repo
	// (ops.ReadPartsSignal): its zerops.yaml declares more than one part,
	// its .zcp/dev-servers.json more than one server, or workspace markers
	// name several deployable packages. Read by the `parts:` atom axis; off
	// the wire. False when not read.
	MultiPart bool `json:"-"`

	// DeclaresBase is true when the zerops.yaml this service deploys from
	// already declares a build.base or run.base (ops.PartsSignal, read on the
	// dev half and carried to each stage of its pair). The repo has chosen
	// its OS then, and the envelope never asks to change it. Off the wire;
	// false when not read.
	DeclaresBase bool `json:"-"`

	// Repo is the live repo state for this service (docs/spec-workflows.md
	// §8 GLC-7, G1/G2) — nil for managed/unknown-class services (they never
	// carry a working tree) and for any runtime service ApplyRepoStatus
	// wasn't given a live read for. Populated by the tools layer via
	// ApplyRepoStatus, never persisted on ServiceMeta or the bootstrap
	// session — a fresh read every envelope computation.
	Repo *RepoStatus `json:"repo,omitempty"`

	// Rollback names this service's GF-8 rollback candidates (docs/spec-
	// workflows.md §8 R2 generalised / §12.6 GF-8) — ids only, so an
	// agent reads eligible `zerops_deploy targetService=<h> appVersion=<id>`
	// targets straight off action="status" instead of probing
	// zerops_deploy with a fake id or round-tripping zerops_events. Nil
	// for managed/unknown-class services and for any runtime service
	// ApplyRollbackInfo wasn't given a live read for. Populated by the
	// tools layer via ApplyRollbackInfo, never persisted — a fresh read
	// every envelope computation.
	Rollback *RollbackInfo `json:"rollback,omitempty"`
}

// PairStageHostnames returns the stage hostnames of the pair whose dev half
// this snapshot is: every stage when Stages is populated, else the one
// StageHostname names, else none.
func (s ServiceSnapshot) PairStageHostnames() []string {
	if len(s.Stages) > 0 {
		out := make([]string, 0, len(s.Stages))
		for _, st := range s.Stages {
			out = append(out, st.Hostname)
		}
		return out
	}
	if s.StageHostname != "" {
		return []string{s.StageHostname}
	}
	return nil
}

// WithParts is the envelope as zcp sends it from field-order step (3) of
// docs/spec-mate.md §2.8a.3: every dev half carries stages[] in place of
// stageHostname/stageSetupName — a pool that is a pair's dev half too, the
// pool keying the pair — and devServers[] where this call read its
// supervisor (keyed by dev hostname). The receiver is left as it was; a
// second application keeps the stages the first one projected.
func (e StateEnvelope) WithParts(devServers map[string][]topology.DevServerState) StateEnvelope {
	var leases []LeaseSnapshot
	if e.Leases != nil {
		leases = make([]LeaseSnapshot, len(*e.Leases))
		copy(leases, *e.Leases)
		for i := range leases {
			// A fresh read replaces the last-known one; a lease this call
			// did not read keeps it.
			if states, read := devServers[leases[i].Alias]; read {
				leases[i].DevServers, leases[i].DevServersReadAt = states, devServersReadAt(states)
			}
		}
	}
	services := make([]ServiceSnapshot, len(e.Services))
	copy(services, e.Services)
	for i := range services {
		s := &services[i]
		stages := s.Stages
		if len(stages) == 0 && s.StageHostname != "" {
			stages = []topology.StageRef{{Hostname: s.StageHostname, SetupName: s.StageSetupName}}
		}
		if len(stages) > 0 {
			s.WireStages = make([]topology.StageWire, 0, len(stages))
			for _, st := range stages {
				part := stagePart(st, nil)
				if s.DevIsPool {
					part = stagePart(st, poolDevServers(leases, s.Hostname))
				}
				s.WireStages = append(s.WireStages, topology.StageWire{Hostname: st.Hostname, Setup: st.SetupName, Part: part})
			}
		}
		s.StageHostname, s.StageSetupName = "", ""
		s.DevServers = devServers[s.Hostname]
	}
	e.Services = services
	if e.Leases != nil {
		for i := range leases {
			leases[i].Part, leases[i].Stage = leasePart(leases[i], services)
		}
		e.Leases = &leases
	}
	return e
}

// devServersReadAt is when states were read (RFC 3339, UTC): the newest read
// time among them, "" for none.
func devServersReadAt(states []topology.DevServerState) string {
	var at time.Time
	for _, s := range states {
		if s.ReadAt.After(at) {
			at = s.ReadAt
		}
	}
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339)
}

// leasePart is the part lease's pool serves and the stage its pool-keyed pair
// pairs with: the part is the lease's dev server's name (the one named after
// the stage's setup when it runs several), else the pair's stage setup;
// absent where zcp knows neither.
func leasePart(lease LeaseSnapshot, services []ServiceSnapshot) (part, stage string) {
	var ref topology.StageRef
	for _, s := range services {
		if s.DevIsPool && s.Hostname == lease.Pool && len(s.Stages) > 0 {
			ref = s.Stages[0]
		}
	}
	return serverPart(lease.DevServers, ref.SetupName, ref.SetupName), ref.Hostname
}

// stagePart is the part stage builds: the name of the dev server servers run
// for it (a pool-keyed pair's leases'), else its setup, else its hostname
// without the `stage` suffix.
func stagePart(stage topology.StageRef, servers []topology.DevServerState) string {
	fallback := stage.SetupName
	if fallback == "" {
		fallback = strings.TrimSuffix(stage.Hostname, "stage")
		if fallback == "" {
			fallback = stage.Hostname
		}
	}
	return serverPart(servers, stage.SetupName, fallback)
}

// serverPart is the part servers run: the one server's name, or among
// several the one named setup, else the first; fallback with none.
func serverPart(servers []topology.DevServerState, setup, fallback string) string {
	if len(servers) == 0 {
		return fallback
	}
	for _, s := range servers {
		if setup != "" && s.Name == setup {
			return s.Name
		}
	}
	return servers[0].Name
}

// poolDevServers is the dev servers zcp last knew a lease of pool to run —
// the first such lease's, by alias order — none when it knows none.
func poolDevServers(leases []LeaseSnapshot, pool string) []topology.DevServerState {
	for _, l := range leases {
		if l.Pool == pool && len(l.DevServers) > 0 {
			return l.DevServers
		}
	}
	return nil
}

// RollbackInfo is the per-service rollback-candidate id summary (docs/
// spec-workflows.md §8 R2 / §12.6 GF-8). Active is empty when no
// appVersion is currently active (a never-deployed service). Backup lists
// every BACKUP (rollback-eligible) appVersion id, newest first — the
// SAME candidates ops.AppVersionCandidates and zerops_events'
// `appVersions` section carry, reduced to ids to keep the envelope small
// (spec-mate.md §1 reducer rule).
type RollbackInfo struct {
	Active string   `json:"active,omitempty"`
	Backup []string `json:"backup,omitempty"`
}

// RepoStatus is the per-service repository state. Present/Head are read live.
// Baseline is the appVersion observed at adoption; it and Provenance come from
// ServiceMeta.Repo and remain unchanged when HEAD advances. Neither field proves
// a relationship between the current HEAD and the running appVersion.
type RepoStatus struct {
	Present    bool                    `json:"present"`
	Head       string                  `json:"head,omitempty"`
	Baseline   string                  `json:"baseline,omitempty"`
	Provenance topology.RepoProvenance `json:"provenance,omitempty"`
	// RepoState classifies the working tree at read time —
	// "clean" | "dirty" | "merging" | "rebasing" | "detached" (docs/
	// spec-workflows.md §12.6 GF-12). Empty when Present is false.
	RepoState string `json:"repoState,omitempty"`
}

// ApplyRepoStatus attaches a live repo status to each service snapshot
// that has one in statuses (keyed by hostname), skipping every service that
// is not an app runtime (managed, unknown, pool, Box) even if the caller
// mistakenly supplied one — a repo block only ever makes sense for a service
// that runs application code in one addressable container.
func ApplyRepoStatus(services []ServiceSnapshot, statuses map[string]RepoStatus) {
	for i := range services {
		if !services[i].RuntimeClass.IsAppRuntime() {
			continue
		}
		st, ok := statuses[services[i].Hostname]
		if !ok {
			continue
		}
		copied := st
		services[i].Repo = &copied
	}
}

// ApplyRollbackInfo attaches per-service rollback candidate ids to each
// service snapshot that has one in infos (keyed by hostname), skipping every
// service that is not an app runtime (managed, unknown, pool, Box) even if
// the caller mistakenly supplied one — rollback only ever makes sense for a
// runtime service with app-version history.
func ApplyRollbackInfo(services []ServiceSnapshot, infos map[string]RollbackInfo) {
	for i := range services {
		if !services[i].RuntimeClass.IsAppRuntime() {
			continue
		}
		info, ok := infos[services[i].Hostname]
		if !ok {
			continue
		}
		copied := info
		services[i].Rollback = &copied
	}
}

// WorkSessionSummary mirrors the persistent WorkSession at envelope build time.
type WorkSessionSummary struct {
	Intent   string   `json:"intent"`
	Services []string `json:"services"`
	// Roles mirrors WorkSession.Roles (hostname → required|deferred|out-of-scope).
	// Empty / absent entry = required. Render uses it to count the completion
	// denominator over required services only and to surface deferred /
	// out-of-scope services as reminders rather than blockers (RC-B).
	Roles       map[string]string        `json:"roles,omitempty"`
	CreatedAt   time.Time                `json:"createdAt"`
	ClosedAt    *time.Time               `json:"closedAt,omitempty"`
	CloseReason string                   `json:"closeReason,omitempty"`
	Deploys     map[string][]AttemptInfo `json:"deploys,omitempty"`
	Verifies    map[string][]AttemptInfo `json:"verifies,omitempty"`
}

// AttemptInfo is a single deploy/verify record as projected into the
// envelope. Carries enough diagnostic data that `action="status"`
// reconstructs why an attempt failed without a separate logs round-trip.
//
// `Reason` is the failure message (empty when Success=true). `FailureClass`
// is a typed category populated at the record site where the failure mode
// is known from the operation result; it is informational — the LLM reads
// `Reason` for human content, while `BuildPlan` may key off `FailureClass`
// to phrase a targeted Primary rationale.
//
// `Setup` and `Strategy` are deploy-only; `Summary` is verify-only (the
// brief outcome string, e.g. "healthy" or the failing check name). The
// projection drops empty values via `omitempty` to keep envelope JSON
// stable across attempt types.
type AttemptInfo struct {
	At           time.Time             `json:"at"`
	Success      bool                  `json:"success"`
	Iteration    int                   `json:"iteration"`
	Setup        string                `json:"setup,omitempty"`
	Strategy     string                `json:"strategy,omitempty"`
	Reason       string                `json:"reason,omitempty"`
	FailureClass topology.FailureClass `json:"failureClass,omitempty"`
	Summary      string                `json:"summary,omitempty"`
	// SHA and AppVersionID mirror DeployAttempt's fields for a zcp deploy
	// attempt (docs/spec-workflows.md §4.9) — set for an explicit
	// deploy-from-commit AND for a working-tree deploy whose source had a
	// git repo with a reachable HEAD. Deploy-only, both empty when the
	// source had no repo at all, and for every verify attempt.
	SHA          string `json:"sha,omitempty"`
	AppVersionID string `json:"appVersionId,omitempty"`
	// Dirty mirrors DeployAttempt.Dirty: true when this attempt shipped
	// uncommitted changes on top of SHA. Deploy-only.
	Dirty bool `json:"dirty,omitempty"`
}

// BootstrapSessionSummary is the bootstrap projection on the envelope used
// for atom filtering. ComputeEnvelope leaves it nil; the bootstrap conductor
// builds a synthetic instance per render in
// bootstrap_guide_assembly.go::synthesisEnvelope from the live BootstrapState.
// Its presence signals that atoms should target a specific bootstrap route
// (recipe/classic/adopt).
//
// Step names the current bootstrap step the agent is working on (discover,
// provision, generate, deploy, close) so atoms can scope themselves to a
// single step. Empty string means "step not applicable" (used outside the
// active-bootstrap conductor).
type BootstrapSessionSummary struct {
	Route       BootstrapRoute `json:"route"`
	Step        string         `json:"step,omitempty"`
	Intent      string         `json:"intent,omitempty"`
	RecipeMatch *RecipeMatch   `json:"recipeMatch,omitempty"`
	Closed      bool           `json:"closed,omitempty"`
}
