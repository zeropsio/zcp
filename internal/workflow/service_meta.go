package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/zeropsio/zcp/internal/topology"
)

// ServiceMeta records bootstrap decisions for a service.
// ZCP's persistent knowledge — the API doesn't track mode, pairing, or strategy.
// The API is the source of truth for operational state (running, resources,
// envs).
//
// FirstDeployedAt is the durable "has this service seen a real code deploy"
// signal. Stamped from two events (plan phase A.3):
//
//  1. A deploy attempt recorded in the work session lands with SucceededAt
//     set — stamp here so the fact persists after the session closes.
//  2. Auto-adoption observes platform Status=ACTIVE on a pre-existing
//     service — stamp at adoption so services deployed before ZCP touched
//     them don't get stuck at "never deployed" (the fizzy-export bug).
//
// The old "stamp only on verify pass" behavior is gone; deploy success is a
// sufficient signal and verify-only stamping was masking legitimate
// Deployed=true cases for services that bypassed ZCP verify.
type ServiceMeta struct {
	Hostname string        `json:"hostname"`
	Mode     topology.Mode `json:"mode,omitempty"`
	// Stages is the pair's only stage list: every stage runtime built from
	// the dev half's repository, each with its zerops.yml setup (SetupName
	// empty until known). A single-part pair has one entry; dev/simple/
	// local-only metas have none. Metas written before Stages carried one
	// stageHostname/stageSetupName; parseMeta folds those into Stages[0] and
	// WriteServiceMeta mirrors Stages[0] back into them for an older zcp —
	// nothing else reads or writes them (spec-mate §2.8a.4).
	Stages []topology.StageRef `json:"stages,omitempty"`
	// stagesUnrecorded is a meta read without `stages`: written before them,
	// or by an older zcp that dropped them. StagesRebuildDue reads it.
	stagesUnrecorded bool
	// CodeVolume is a variant-B pair's shared code: the local-storage
	// service holding the repository, where every dev service mounts it,
	// and the pair's dev services beside Hostname. nil for every other pair.
	CodeVolume *topology.CodeVolume `json:"codeVolume,omitempty"`

	// Per-pair deploy dimensions (deploy-strategy decomposition; see
	// plans/archive/deploy-strategy-decomposition-2026-04-28.md §3.1 for
	// the orthogonality matrix). Three independent dimensions:
	// CloseDeployMode is what the develop workflow auto-does at close;
	// GitPushState is whether git-push capability is set up;
	// BuildIntegration is which ZCP-managed CI integration responds to
	// remote git pushes.
	CloseDeployMode          topology.CloseDeployMode `json:"closeDeployMode,omitempty"`
	CloseDeployModeConfirmed bool                     `json:"closeDeployModeConfirmed,omitempty"` // true after user explicitly confirms/sets close mode
	GitPushState             topology.GitPushState    `json:"gitPushState,omitempty"`
	// RemoteURL caches `git remote get-url origin`; a pair wired to HQ keeps
	// its repository in HQ here even while origin is GitHub (HQ.Origin).
	RemoteURL        string                    `json:"remoteUrl,omitempty"`
	BuildIntegration topology.BuildIntegration `json:"buildIntegration,omitempty"`
	// TrackedRef is the ref a target consumes — what stage or prod is built
	// from (GF-7, docs/spec-workflows.md §12.6). Recorded ONCE by
	// git-push-setup's confirm step: an explicit trackedRef input always
	// wins; otherwise the push source's current branch is detected (its
	// attached HEAD, else the remote's default branch), falling back to
	// "main" when neither resolves. Every reader (git-push's default
	// branch, the GitHub Actions template, the launch gate's remote-HEAD
	// compare) applies the SAME "main" fallback when this is empty
	// (pre-existing metas written before GF-7) — never re-detected here.
	TrackedRef string `json:"trackedRef,omitempty"`
	// BuildIntegrationVerifiedAt is the RFC3339 timestamp of the last
	// EARNED verification of the declared BuildIntegration — a checkable
	// signal ZCP observed, never the declaration itself:
	//   - actions: workflow file present in the push-source working tree
	//     at a moment the launch push-proof guaranteed clean tree + local
	//     HEAD == remote HEAD (so the file is on the pushed HEAD).
	//   - webhook: the platform integration-status read returned
	//     configured for the build target.
	// Empty = declared-but-unverified — the zero value old on-disk metas
	// load with (the SAFE direction: more warnings, never a new block).
	// Stamped ONLY on the publish-side launch gate; the read-side poll
	// path never mutates meta. BuildIntegration=none never carries it.
	BuildIntegrationVerifiedAt string `json:"buildIntegrationVerifiedAt,omitempty"`

	BootstrapSession string `json:"bootstrapSession"`
	BootstrappedAt   string `json:"bootstrappedAt"`
	FirstDeployedAt  string `json:"firstDeployedAt,omitempty"` // stamped on first observed deploy — via session or adoption

	// Setup-name canonical store. The zerops.yaml setup-block name ZCP
	// uses for this service's deploys, matching the same local-canonical
	// pattern as the per-pair deploy dimensions above.
	//
	// Empty = not yet discovered. First setup-sensitive operation runs
	// ResolveCanonicalSetup (internal/workflow/setup_resolver.go); on a
	// platform-source or local-yaml hit the field is populated; on total
	// miss the caller emits a requiresSetupInput structured blocker.
	// Updated by set-default-setup action or cascade write-back.
	//
	// PrimarySetupName applies to deploys targeting Hostname's half; each
	// stage's setup is its Stages entry's Setup.
	//
	// Plan: plans/setup-name-local-canonical-2026-05-27.md §F3.
	PrimarySetupName string `json:"primarySetupName,omitempty"`
	// ProdSetupName records WHICH zerops.yaml setup block production was
	// launched with — stamped by the launch finalize alongside the
	// post-launch back-reference. The launch setup cascade reads it
	// FIRST after explicit overrides, so a re-launch reuses the proven
	// identity instead of re-deriving (and ultimately re-falling-back to
	// the literal "prod" tail). F4 ledger completion.
	ProdSetupName string `json:"prodSetupName,omitempty"`

	// ServesHTTPByHost records, per host of this meta — Hostname and each
	// stage — whether the setup that host last deployed through ZCP declares
	// an HTTP surface; false is a known worker setup. A host without an entry
	// is unknown (adopted, externally deployed, not deployed yet): readers
	// fall back to its declared ports, never to another host's entry.
	ServesHTTPByHost map[string]bool `json:"servesHttpByHost,omitempty"`
	// ProvisionedFromGit marks a recipe-route runtime the platform cloned,
	// built, and DEPLOYED from buildFromGit at import — so the service is
	// already serving curated code when it reaches ACTIVE, NOT awaiting a first
	// deploy. DeriveDeployed reads it (joined with live status==ACTIVE) so the
	// develop branch selector doesn't send the agent to re-scaffold + re-deploy
	// an app that is already live (B4/F11). Classic metas never carry it.
	ProvisionedFromGit bool `json:"provisionedFromGit,omitempty"`

	// ProdLaunches is the post-launch back-reference: one entry per
	// production project this pair was promoted into. Written at launch
	// finalize (append-if-new keyed on ProdProjectID+ProdHostname); read
	// by develop-side surfaces so the source pair can say "this stage
	// feeds production X" and a later session knows production exists.
	// Non-secret (IDs + names + timestamp only). F4 ledger completion.
	ProdLaunches []ProdLaunchRef `json:"prodLaunches,omitempty"`

	// PublicAccess persists the user's public-access INTENT per hostname
	// (docs/spec-workflows.md §8 E9) — everything else about public access
	// (subdomain on/off, domains, listener) is read live (O3), never
	// persisted. Keyed by hostname rather than shared across the pair like
	// the other per-pair dimensions above: a standard pair's dev and stage
	// halves each carry their own entry, since one half can be publicly
	// exposed while the other isn't. Use PublicAccessFor/SetPublicAccess —
	// a missing entry means "never recorded", which defaults to auto, NOT
	// the zero value of PublicAccessRecord (whose Intent would be "").
	PublicAccess map[string]topology.PublicAccessRecord `json:"publicAccess,omitempty"`

	// Repo is the adopt-time baseline marker (docs/spec-workflows.md
	// §8 GLC-7, G2) — which appVersion was running at adoption and
	// whether adoption preserved existing history or created a snapshot.
	// Neither provenance proves what built that appVersion. nil for a service bootstrapped
	// fresh (no adopt step ran) or not yet adopted.
	Repo *topology.Repo `json:"repo,omitempty"`

	// HQ is the pair's repository in HQ, recorded when HQ gave it (SPEC
	// §3.2a): where its code lands through changes. Absent on every pair
	// whose remote is not this Mate's HQ — which is every pair outside a
	// Mate. Non-secret: an application id, a repository name, a branch name,
	// the change on record, and at most the Mate's own words about its change
	// until the change carries them. The Mate credential is never here (nor in
	// any other file ZCP writes) — it lives where git-push-setup put it, a
	// sensitive service env on the push source.
	HQ *HQRepoRef `json:"hq,omitempty"`

	// DevPool marks a pair whose dev half is a dev pool: Hostname then names a
	// pool, whose containers are leased one per branch — never one container
	// zcp may ssh, mount, deploy from or probe by that hostname (spec-workflows
	// §8 PS-3, §13). nil on every other meta. Read it through DevIsPool.
	DevPool *DevPoolRef `json:"devPool,omitempty"`
}

// SetRepoBaseline records the adopt-time baseline marker for this meta.
func (m *ServiceMeta) SetRepoBaseline(appVersionID string, provenance topology.RepoProvenance) {
	m.Repo = &topology.Repo{BaselineAppVersion: appVersionID, Provenance: provenance}
}

// HQRepoRef is what a pair needs to keep working on its repository in HQ
// across sessions: which repository it is, and its change toward `main`.
// `main` moves only by a person's merge, so the Mate's work lands through a
// change.
type HQRepoRef struct {
	// AppID is the HQ application the repository belongs to — the one HQ held
	// the Mate in when it was wired. A Mate moved to another application is
	// wired again there (rewire), and its change in the old one stays there.
	AppID string `json:"appId"`
	// Repo is the repository's name in that application: the pair's dev
	// hostname, or the one a recipe names.
	Repo string `json:"repo"`
	// WiredAt is when the repository was wired, RFC3339.
	WiredAt string `json:"wiredAt,omitempty"`
	// CredentialFingerprint names the Mate credential the dev service holds
	// as GIT_TOKEN (hq.Client.CredentialFingerprint), never the credential:
	// the Mate enrolling again revokes it, and a pass renews what differs.
	CredentialFingerprint string `json:"credentialFingerprint,omitempty"`
	// Change is the number of the Mate's open change in Repo as the Mate's
	// own state in HQ last named it, 0 for none. Recorded rather than
	// re-derived: a pass reads what became of it from that state.
	Change int `json:"change,omitempty"`
	// Branch is the branch Change is bound to, by its short name, as HQ named
	// it — the work's place, a handed change's included; "" for none, or a
	// change recorded before branches were.
	Branch string `json:"branch,omitempty"`
	// ChangeDescription is what the Mate wrote about its change while HQ did
	// not take the edit: there only until the change carries it, and never
	// longer than the change it was written for.
	ChangeDescription *ChangeDescription `json:"changeDescription,omitempty"`
	// Origin is how the Mate's own state in HQ last said Repo's code is
	// kept; nil until zcp read it from an HQ that names its repositories.
	// Recorded so status names it without asking HQ.
	Origin *HQOrigin `json:"origin,omitempty"`
}

// HQOrigin is how a repository's code is kept: Mode is the origin's mode as
// HQ names it (hq.OriginOnGitHub, hq.OriginFollowed), HQOriginNone for no
// origin — HQ alone keeps the code — or "" from an HQ older than modes. URL
// is the origin's address, "" for none.
type HQOrigin struct {
	Mode string `json:"mode,omitempty"`
	URL  string `json:"url,omitempty"`
}

// HQOriginNone is HQOrigin.Mode for a repository with no origin: zcp's own
// word, HQ sends no origin at all.
const HQOriginNone = "none"

// ChangeDescription is a Mate's description of its change, kept until HQ
// takes it.
type ChangeDescription struct {
	// Text is the description, in markdown.
	Text string `json:"text"`
	// Title is the change's title the Mate gave with it, "" for none.
	Title string `json:"title,omitempty"`
	// Change is the change it was written for.
	Change int `json:"change,omitempty"`
}

// PublicAccessFor returns the persisted public-access record for hostname —
// {Intent: auto} when no entry was ever recorded (E9 default). hostname must
// be one of m.Hostnames(); an unrelated hostname also returns the auto
// default since there is nothing to look up.
func (m *ServiceMeta) PublicAccessFor(hostname string) topology.PublicAccessRecord {
	if m == nil || m.PublicAccess == nil {
		return topology.PublicAccessRecord{Intent: topology.PublicAccessAuto}
	}
	if rec, ok := m.PublicAccess[hostname]; ok {
		return rec
	}
	return topology.PublicAccessRecord{Intent: topology.PublicAccessAuto}
}

// SetPublicAccess records the public-access intent for hostname, allocating
// the map on first use.
func (m *ServiceMeta) SetPublicAccess(hostname string, rec topology.PublicAccessRecord) {
	if m.PublicAccess == nil {
		m.PublicAccess = make(map[string]topology.PublicAccessRecord, 2)
	}
	m.PublicAccess[hostname] = rec
}

// ProdLaunchRef is one production promotion record on the source pair.
type ProdLaunchRef struct {
	ProdProjectID   string `json:"prodProjectId"`
	ProdProjectName string `json:"prodProjectName,omitempty"`
	ProdHostname    string `json:"prodHostname,omitempty"`
	LaunchedAt      string `json:"launchedAt"`
}

// SetupNameFor returns the canonical zerops.yaml setup-block name for a
// target hostname. Pair-keyed: a stage hostname returns that stage's Setup;
// targetHostname == Hostname returns PrimarySetupName; any other hostname
// returns "" (caller must load that hostname's meta).
//
// Empty result on an in-scope hostname means "cache miss — run cascade."
// Callers that need a guaranteed value must invoke ResolveCanonicalSetup
// (see internal/workflow/setup_resolver.go from P1) rather than treating
// an empty return as authoritative.
func (m *ServiceMeta) SetupNameFor(targetHostname string) string {
	if m == nil {
		return ""
	}
	if stage, ok := m.StageFor(targetHostname); ok {
		return stage.SetupName
	}
	if targetHostname == m.Hostname {
		return m.PrimarySetupName
	}
	if d, ok := m.CodeVolume.DevService(targetHostname); ok {
		return d.SetupName
	}
	return ""
}

// WorkingDirFor is the container directory a deploy from hostname runs in:
// the code volume's mount for a dev service of a variant-B pair, whose
// repository lives there, else "" — the deploy's default, /var/www.
func (m *ServiceMeta) WorkingDirFor(hostname string) string {
	if m == nil || m.CodeVolume == nil {
		return ""
	}
	if hostname == m.Hostname {
		return m.CodeVolume.MountPath
	}
	if _, ok := m.CodeVolume.DevService(hostname); ok {
		return m.CodeVolume.MountPath
	}
	return ""
}

// HasStage reports whether the meta is a pair: it records at least one stage.
func (m *ServiceMeta) HasStage() bool {
	return m != nil && len(m.Stages) > 0
}

// StageFor returns the stage named hostname, and whether the pair has one.
func (m *ServiceMeta) StageFor(hostname string) (topology.StageRef, bool) {
	if m == nil || hostname == "" {
		return topology.StageRef{}, false
	}
	for _, s := range m.Stages {
		if s.Hostname == hostname {
			return s, true
		}
	}
	return topology.StageRef{}, false
}

// StageHostnames returns the hostname of every stage, in Stages order.
func (m *ServiceMeta) StageHostnames() []string {
	if m == nil {
		return nil
	}
	out := make([]string, 0, len(m.Stages))
	for _, s := range m.Stages {
		out = append(out, s.Hostname)
	}
	return out
}

// FirstStage is the pair's first stage, or the zero Stage when it has none.
// It serves the surfaces that still name one stage per pair — the envelope's
// stageHostname/stageSetupName, the build-integration target and the tier
// composer's input — which on a single-part pair is the pair's only stage.
func (m *ServiceMeta) FirstStage() topology.StageRef {
	if !m.HasStage() {
		return topology.StageRef{}
	}
	return m.Stages[0]
}

// SetStageSetup records setup as the zerops.yml setup of the stage named
// hostname. Reports whether the meta changed: false when the pair has no
// such stage or it already records that setup.
func (m *ServiceMeta) SetStageSetup(hostname, setup string) bool {
	if m == nil {
		return false
	}
	for i := range m.Stages {
		if m.Stages[i].Hostname == hostname {
			if m.Stages[i].SetupName == setup {
				return false
			}
			m.Stages[i].SetupName = setup
			return true
		}
	}
	return false
}

// ServesHTTPFor is host's own record of whether its deployed setup declares an
// HTTP surface; nil when host has none.
func (m *ServiceMeta) ServesHTTPFor(host string) *bool {
	if m == nil {
		return nil
	}
	serves, ok := m.ServesHTTPByHost[host]
	if !ok {
		return nil
	}
	return &serves
}

// SetServesHTTP records serves as host's own. Reports whether the meta
// changed: false when host already records that value.
func (m *ServiceMeta) SetServesHTTP(host string, serves bool) bool {
	if old, ok := m.ServesHTTPByHost[host]; ok && old == serves {
		return false
	}
	if m.ServesHTTPByHost == nil {
		m.ServesHTTPByHost = map[string]bool{}
	}
	m.ServesHTTPByHost[host] = serves
	return true
}

// IsComplete returns true if bootstrap finished for this service.
// BootstrappedAt is set only at bootstrap completion — empty means
// the service was provisioned but bootstrap didn't finish.
// Under Option A this marks infra readiness (services provisioned, mounted,
// env vars discoverable) — not code-deploy completion. See IsDeployed.
func (m *ServiceMeta) IsComplete() bool {
	return m.BootstrappedAt != ""
}

// IsDeployed returns true once the service has been observed to have a real
// code deploy (via session or adoption-at-ACTIVE). See ServiceMeta doc.
func (m *ServiceMeta) IsDeployed() bool {
	return m.FirstDeployedAt != ""
}

// RecordExternalDeploy stamps FirstDeployedAt on the meta for the given
// hostname WITHOUT requiring an active work session. Bridges deploys
// that happen outside the synchronous ZCP push path — git-push (where
// the platform build is async, post C2 audit closure), CI/CD outside
// MCP, custom platform calls — to MCP-tracked state. Once stamped,
// ServiceSnapshot.Deployed flips to true and develop atoms gated on
// `deployStates: [never-deployed]` (e.g. develop-record-external-deploy)
// stop firing for that service in subsequent envelope renders.
//
// Idempotent: when FirstDeployedAt is already set, returns the existing
// timestamp without rewriting. Stage hostnames resolve to the dev-keyed
// pair meta per pair-keyed invariant (§ E8) — stamping the stage half
// flips Deployed for both halves of a container+standard pair.
//
// Returns (stamped, firstDeployedAt, err):
//   - stamped: true only when this call wrote a fresh timestamp.
//   - firstDeployedAt: authoritative on-disk value (current or just-written),
//     empty when meta is missing.
//   - err: filesystem read/write failure. Missing meta returns (false, "", nil)
//     — meta-less services have nothing to stamp; not an error.
//
// Implementation defers to the unexported stampFirstDeployedAt helper used
// by RecordDeployAttempt, with a stat read first to distinguish "fresh stamp"
// from "no-op already stamped".
func RecordExternalDeploy(stateDir, hostname string) (bool, string, error) {
	meta, err := FindServiceMeta(stateDir, hostname)
	if err != nil {
		return false, "", fmt.Errorf("record external deploy: %w", err)
	}
	if meta == nil {
		return false, "", nil
	}
	if meta.FirstDeployedAt != "" {
		return false, meta.FirstDeployedAt, nil
	}
	if err := stampFirstDeployedAt(stateDir, hostname); err != nil {
		return false, "", err
	}
	stamped, err := FindServiceMeta(stateDir, hostname)
	if err != nil || stamped == nil {
		return true, "", err
	}
	return true, stamped.FirstDeployedAt, nil
}

// IsAdopted reports whether this meta records an adopted service.
// Adopted = bootstrap-complete AND BootstrapSession empty (the convention written by
// writeBootstrapOutputs when IsExisting=true). Both guards matter: incomplete metas
// with an empty session are orphans, not adoptions.
func (m *ServiceMeta) IsAdopted() bool {
	return m.BootstrapSession == "" && m.IsComplete()
}

// Hostnames returns every hostname this meta represents.
//
// Pair-keyed meta invariant (see docs/spec-workflows.md §8 E8): exactly one
// ServiceMeta file represents a runtime service — as a dev/stage pair
// (container+standard, local+standard) or a single hostname
// (dev/simple/local-only). Hostnames() is the canonical enumeration across the
// pair; use it (or ManagedRuntimeIndex for slice→map construction) anywhere you
// map hostnames to metas. Keying by m.Hostname alone violates the invariant and
// breaks scope validation, auto-close, and strategy resolution for stage
// hostnames.
//
// For container+standard and local+standard that's Hostname followed by every
// stage hostname; for everything else just [Hostname]. A variant-B pair's
// further dev services follow.
func (m *ServiceMeta) Hostnames() []string {
	return append(append([]string{m.Hostname}, m.StageHostnames()...), m.CodeVolume.DevServiceHostnames()...)
}

// ManagedRuntimeIndex builds a hostname → meta map honoring the pair-keyed
// invariant (docs/spec-workflows.md §8 E8). Every hostname a meta represents
// (via Hostnames()) resolves to the same *ServiceMeta pointer.
//
// The helper does not filter on IsComplete() or by Mode — callers layer their
// own predicates on top (e.g. scope validation keeps its runtime-class
// filter). Nil metas and metas with empty Hostname are skipped so lookups
// never poison on an empty key.
//
// This is the single canonical mechanism for hostname→meta mapping when the
// caller already holds a []*ServiceMeta slice (typically from
// ListServiceMetas). Inline reimplementations are a pair-keyed invariant
// violation — TestNoInlineManagedRuntimeIndex scans the codebase for the
// pattern and fails the build.
func ManagedRuntimeIndex(metas []*ServiceMeta) map[string]*ServiceMeta {
	out := make(map[string]*ServiceMeta, len(metas)*2)
	for _, m := range metas {
		if m == nil || m.Hostname == "" {
			continue
		}
		for _, h := range m.Hostnames() {
			out[h] = m
		}
	}
	return out
}

// PrimaryRole returns the deploy role of m.Hostname.
// Encapsulates the mode→role lookup so callers don't re-derive it.
// Local topologies (local-stage / local-only) are project-keyed — they
// have no per-service deploy role; callers should use the stage hostname
// directly for deploys on local-stage.
func (m *ServiceMeta) PrimaryRole() topology.Mode {
	mode := m.Mode
	if mode == "" {
		mode = topology.PlanModeStandard
	}
	switch mode {
	case topology.PlanModeSimple:
		return topology.DeployRoleSimple
	case topology.PlanModeDev, topology.PlanModeStandard, topology.ModeStage, topology.PlanModeLocalStage, topology.PlanModeLocalOnly:
		// Dev half of a standard pair and standalone dev both deploy as Dev.
		// Local topologies have no per-service role — the container-side
		// fallback keeps call sites that expect a non-empty role happy;
		// callers that care about local-only semantics gate on meta.Mode.
		return topology.DeployRoleDev
	}
	return topology.DeployRoleDev
}

// RoleFor returns the deploy role of the given hostname within this meta's scope.
// Returns "" when the hostname is unrelated to this meta.
func (m *ServiceMeta) RoleFor(hostname string) topology.Mode {
	if hostname == "" {
		return ""
	}
	if _, ok := m.StageFor(hostname); ok {
		return topology.DeployRoleStage
	}
	if hostname == m.Hostname {
		return m.PrimaryRole()
	}
	if _, ok := m.CodeVolume.DevService(hostname); ok {
		return topology.DeployRoleDev
	}
	return ""
}

// ModeFor returns the envelope-Mode of `hostname` projected through this
// pair-keyed meta. Lifts the role returned by RoleFor into the Mode
// vocabulary (see topology/aliases.go) so callers feeding mode-sensitive
// predicates — topology.IsDeferredStart, atom modes:[...] gates, etc. —
// read the correct half of a pair-keyed meta.
//
// Why this is distinct from m.Mode: a standard-pair ServiceMeta carries
// Mode=ModeStandard for both halves; reading m.Mode directly when
// hostname is the stage half projects dev-half semantics (zsc-noop,
// deferred-start) onto a stage runtime that runs run.start. Three
// deploy/subdomain sites previously misread pair-keyed metas this way
// (deploy_poll.go::resolveDeployTargetTopology,
// deploy_subdomain.go::maybeAutoEnableSubdomain,
// subdomain.go::skipDeferredStartProbe) — every mode-sensitive next-
// action / probe-skip decision on the stage half. The compute_envelope
// path already had this projection (formerly a private
// resolveEnvelopeMode helper); promoting it to a method here unifies
// the contract across the envelope and deploy/subdomain surfaces.
//
// Returns "" when hostname is unrelated to this meta. Local-stage's
// stage half projects as ModeLocalStage (not ModeStage) so atoms /
// predicates that key on local-* keep matching when target is the
// stage hostname of a local-stage pair.
func (m *ServiceMeta) ModeFor(hostname string) topology.Mode {
	if m == nil {
		return ""
	}
	// Local topologies are project-keyed: m.Hostname is the project name,
	// not a deployable runtime. Asking for the Mode of the project name on
	// a local-only / local-stage meta is meaningless — no runtime to
	// project. RoleFor falls through to PrimaryRole which returns
	// DeployRoleDev as a defensive fallback (so non-Mode-aware callers
	// don't see ""), but that fallback is misleading at this layer:
	// callers feeding mode-sensitive predicates need "" so they branch
	// through the "no meta available" path instead of treating the
	// project name as a dev runtime. Stage hostname targets on a
	// local-stage meta still project correctly via the Stage branch
	// below — only the project-name case is short-circuited here.
	if hostname == m.Hostname &&
		(m.Mode == topology.PlanModeLocalOnly || m.Mode == topology.PlanModeLocalStage) {
		return ""
	}
	// The pool half of a pool-keyed pair is no dev runtime either: a pool's
	// containers are leased, and no mode-sensitive guidance or probe (dev
	// server, zsc noop, deferred start, a modes: atom) applies to the pool.
	// It projects as a service with no meta, as a pool always did; readers
	// that need the pair ask DevIsPool. Its stages keep the stage mode below.
	if hostname == m.Hostname && m.DevIsPool() {
		return ""
	}
	// RoleFor's contract narrows the return surface to DeployRoleStage /
	// Simple / Dev / "" — the remaining topology.Mode enum values
	// (PlanModeStandard, PlanModeLocalStage, PlanModeLocalOnly) are
	// never returned. They're listed alongside DeployRoleDev below to
	// keep the exhaustive linter coverage explicit; the m.Mode check
	// inside the branch handles the only reachable input (DeployRoleDev).
	switch m.RoleFor(hostname) {
	case topology.DeployRoleStage:
		if m.Mode == topology.PlanModeLocalStage {
			return topology.ModeLocalStage
		}
		return topology.ModeStage
	case topology.DeployRoleSimple:
		return topology.ModeSimple
	case topology.DeployRoleDev,
		topology.PlanModeStandard,
		topology.PlanModeLocalStage,
		topology.PlanModeLocalOnly:
		if m.Mode == topology.PlanModeStandard {
			return topology.ModeStandard
		}
		return topology.ModeDev
	}
	return ""
}

// PushSourceCheckFor classifies whether hostname is a source-of-push within
// this meta's pair scope, returning the discriminating reason so callers can
// render reason-specific remediation. Replaced the boolean IsPushSourceFor
// once handlers grew the need to distinguish "stage half — push from dev"
// from "mode unsupported — expand to ModeStandard first" from "unknown
// host" — all of which the boolean form collapsed onto a single false,
// producing nonsensical "X instead of X" error messages for standalone
// ModeDev services where input.Service == meta.Hostname.
//
// Reads meta.Mode directly (PlanMode values alias topology.Mode values, so
// topology.IsPushSource classifies them correctly) rather than going through
// ModeFor — that projection answers "what mode is THIS hostname relative
// to the pair?" and short-circuits to "" for project-name targets on
// local-* metas (no deployable runtime there). PushSourceCheckFor needs
// the meta-level mode regardless of which hostname the caller is asking
// about, so reading m.Mode straight is the correct shape here.
// Stage-hostname check is the explicit pair-half
// carve-out.
//
// Used by handleGitPush + handleGitPushSetup to reject targetService that
// is not a source-of-push, returning a remediation pointing at the correct
// dev hostname OR a mode-expansion pointer OR a meta-scope-mismatch hint.
func (m *ServiceMeta) PushSourceCheckFor(hostname string) topology.PushSourceResult {
	if m == nil || hostname == "" {
		return topology.PushSourceUnknownHost
	}
	// Local-stage carve-out: the dev half is the user's CWD (m.Hostname is
	// the project name, not a Zerops service). The Zerops-side stage
	// runtime named in m.Stages IS a legitimate deploy target —
	// treating it as "stage half, push from dev half" misleads the agent
	// into pushing from a non-existent Zerops service. The container
	// standard pair stays IsStageHalf for stage-hostname matches because
	// its dev half is a real Zerops service the agent can target.
	_, isStage := m.StageFor(hostname)
	if m.Mode == topology.PlanModeLocalStage && isStage {
		return topology.PushSourceOK
	}
	if isStage {
		return topology.PushSourceIsStageHalf
	}
	if hostname != m.Hostname {
		return topology.PushSourceUnknownHost
	}
	if !topology.IsPushSource(m.Mode) {
		return topology.PushSourceModeUnsupported
	}
	return topology.PushSourceOK
}

// NewServiceMeta returns a fresh ServiceMeta with the three orthogonal
// deploy dimensions stamped to their canonical zero values
// (CloseModeUnset / GitPushUnconfigured / BuildIntegrationNone). Fresh-meta
// writers construct through this so no dimension is ever left empty on
// disk — an empty dimension silently suppressed the git-push-setup +
// build-integration atom chain because the matcher's slices.Contains over
// the axis allowlists never matches "" (TOPO-1/WF-2/DELIV-2). Callers set
// the remaining fields (Stages, BootstrappedAt, …) on the result.
func NewServiceMeta(hostname string, mode topology.Mode) *ServiceMeta {
	return &ServiceMeta{
		Hostname:         hostname,
		Mode:             mode,
		CloseDeployMode:  topology.CloseModeUnset,
		GitPushState:     topology.GitPushUnconfigured,
		BuildIntegration: topology.BuildIntegrationNone,
	}
}

// WriteServiceMeta writes service metadata to baseDir/services/{hostname}.json.
func WriteServiceMeta(baseDir string, meta *ServiceMeta) error {
	foldLegacyCloseMode(meta)
	dir := filepath.Join(baseDir, "services")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create services dir: %w", err)
	}

	data, err := json.MarshalIndent(struct {
		*ServiceMeta
		legacyStageKeys
	}{meta, mirrorFirstStage(meta)}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal service meta: %w", err)
	}

	path := filepath.Join(dir, meta.Hostname+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename service meta: %w", err)
	}
	return nil
}

// serviceMetaLockName is the flock file serializing ServiceMeta read-modify-write.
// Distinct from the registry's .registry.lock so a ServiceMeta update can never
// same-fd-deadlock against a registry-lock scope; the two are never held nested.
const serviceMetaLockName = ".services.lock"

// ErrSkipWrite signals from an Update/UpsertServiceMeta mutate closure that no
// change was made and the write should be skipped (preserves no-op fast paths).
var ErrSkipWrite = errors.New("service meta: no change, skip write")

// ErrServiceMetaNotFound is returned by UpdateServiceMeta when no meta exists
// for the hostname (use UpsertServiceMeta for create-or-update).
var ErrServiceMetaNotFound = errors.New("service meta not found")

// UpdateServiceMeta performs a locked read-modify-write on the pair-keyed
// ServiceMeta for hostname. Under the .services.lock flock it re-reads the meta
// FRESH (pair-aware via FindServiceMeta — a stage-half hostname resolves to its
// dev-keyed file), applies mutate, and writes it back atomically. This is the
// single path that makes concurrent updates to orthogonal ServiceMeta fields
// (close-mode / git-push / build-integration / first-deploy) safe: without it
// each handler did read-whole→mutate-one→write-whole with no lock, so a
// parallel tool_use turn lost-updated orthogonal fields (XCUT-1). mutate returns
// ErrSkipWrite to skip the write when nothing changed.
//
// CLAUDE.md "no mutex during I/O" sanctioned exception: a read-modify-write
// transaction is correct only if no other writer interleaves between the read
// and the write, so the flock is held across the (bounded, local-file) read+
// write. The rule's hazard (blocking on slow/unbounded, e.g. network, I/O) does
// not apply to small local JSON.
func UpdateServiceMeta(stateDir, hostname string, mutate func(*ServiceMeta) error) error {
	return withFileLock(filepath.Join(stateDir, serviceMetaLockName), func() error {
		meta, err := FindServiceMeta(stateDir, hostname)
		if err != nil {
			return fmt.Errorf("update service meta: read %q: %w", hostname, err)
		}
		if meta == nil {
			return fmt.Errorf("update service meta %q: %w", hostname, ErrServiceMetaNotFound)
		}
		if err := mutate(meta); err != nil {
			if errors.Is(err, ErrSkipWrite) {
				return nil
			}
			return err
		}
		return WriteServiceMeta(stateDir, meta)
	})
}

// UpsertServiceMeta is the create-or-update sibling of UpdateServiceMeta for
// constructive writers (bootstrap provision, local auto-adopt). Under the
// .services.lock it reads the pair-keyed meta (nil → a zero meta with Hostname
// pre-set), passes (meta, existed) to mutate, then writes. Use existed to
// implement create-if-absent (return ErrSkipWrite when existed) or merge-onto-
// existing. Keeps the construct/merge atomic with the existence check.
func UpsertServiceMeta(stateDir, hostname string, mutate func(meta *ServiceMeta, existed bool) error) error {
	return withFileLock(filepath.Join(stateDir, serviceMetaLockName), func() error {
		meta, err := FindServiceMeta(stateDir, hostname)
		if err != nil {
			return fmt.Errorf("upsert service meta: read %q: %w", hostname, err)
		}
		existed := meta != nil
		if !existed {
			meta = &ServiceMeta{Hostname: hostname}
		}
		if err := mutate(meta, existed); err != nil {
			if errors.Is(err, ErrSkipWrite) {
				return nil
			}
			return err
		}
		return WriteServiceMeta(stateDir, meta)
	})
}

// rekeyServiceMeta moves a pair from the file keyed from onto the file keyed
// to, inside one .services.lock: it reads both files as they are now (either
// may be absent), asks rekey for the meta to keep under to, writes it, then
// removes from's file. Writing before removing means a cut leaves both files,
// never neither — a pair is never left without its record, and the next call
// finishes the move (rekey sees both and decides). The caller holds whatever
// pair locks fence the pair's other writers.
func rekeyServiceMeta(stateDir, from, to string, rekey func(fromMeta, toMeta *ServiceMeta) (*ServiceMeta, error)) (*ServiceMeta, error) {
	var out *ServiceMeta
	err := withFileLock(filepath.Join(stateDir, serviceMetaLockName), func() error {
		fromMeta, err := ReadServiceMeta(stateDir, from)
		if err != nil {
			return fmt.Errorf("rekey service meta: read %q: %w", from, err)
		}
		toMeta, err := ReadServiceMeta(stateDir, to)
		if err != nil {
			return fmt.Errorf("rekey service meta: read %q: %w", to, err)
		}
		next, err := rekey(fromMeta, toMeta)
		if err != nil {
			return err
		}
		if next == nil || next.Hostname != to {
			return fmt.Errorf("rekey service meta %q → %q: the pair to keep is not keyed by %q", from, to, to)
		}
		if err := WriteServiceMeta(stateDir, next); err != nil {
			return err
		}
		// Not DeleteServiceMeta: it takes the lock this function already holds.
		if err := os.Remove(filepath.Join(stateDir, "services", from+".json")); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("rekey service meta: remove %q: %w", from, err)
		}
		out = next
		return nil
	})
	return out, err
}

// parseMeta deserializes a ServiceMeta from JSON.
// Single deserialization path — both ReadServiceMeta and ListServiceMetas
// route through this so any future field-level invariants land at one
// integration point.
func parseMeta(data []byte) (*ServiceMeta, error) {
	var meta ServiceMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	if err := foldLegacyStage(data, &meta); err != nil {
		return nil, err
	}
	if err := foldLegacyServesHTTP(data, &meta); err != nil {
		return nil, err
	}
	foldLegacyCloseMode(&meta)
	return &meta, nil
}

// legacyStageKeys are the single-stage keys a meta carried before Stages.
// This zcp reads them only to migrate a meta without stages, and writes them
// only as the mirror of Stages[0]: the release a container restart pulls
// back knows nothing else, and its first write keeps them and drops stages
// (spec-mate §2.8a.4).
type legacyStageKeys struct {
	StageHostname  string `json:"stageHostname,omitempty"`
	StageSetupName string `json:"stageSetupName,omitempty"`
}

// mirrorFirstStage is meta's first stage in the old keys; empty for a meta
// without stages.
func mirrorFirstStage(meta *ServiceMeta) legacyStageKeys {
	first := meta.FirstStage()
	return legacyStageKeys{StageHostname: first.Hostname, StageSetupName: first.SetupName}
}

// foldLegacyStage is the one-way lazy migration of a meta written without
// Stages — before them, or by an older zcp that dropped them: its single
// stageHostname/stageSetupName become Stages[0]. Applied at the single
// deserialization path only; a meta that records Stages keeps them and its
// mirrored old keys are not read.
func foldLegacyStage(data []byte, meta *ServiceMeta) error {
	if len(meta.Stages) > 0 {
		return nil
	}
	meta.stagesUnrecorded = true
	var legacy legacyStageKeys
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	if legacy.StageHostname != "" {
		meta.Stages = []topology.StageRef{{Hostname: legacy.StageHostname, SetupName: legacy.StageSetupName}}
	}
	return nil
}

// legacyServesHTTP is the single serves-HTTP key a meta carried before
// ServesHTTPByHost: the answer of the meta's last deploy, whichever host ran it.
type legacyServesHTTP struct {
	ServesHTTP *bool `json:"servesHttp,omitempty"`
}

// foldLegacyServesHTTP is the one-way lazy migration of that key, applied
// after foldLegacyStage. A meta without stages wrote it only for Hostname, so
// it becomes Hostname's entry; a pair's names no host and is not read — each
// of its hosts falls back to its declared ports until it deploys again.
func foldLegacyServesHTTP(data []byte, meta *ServiceMeta) error {
	if meta.ServesHTTPByHost != nil || meta.HasStage() {
		return nil
	}
	var legacy legacyServesHTTP
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	if legacy.ServesHTTP != nil {
		meta.SetServesHTTP(meta.Hostname, *legacy.ServesHTTP)
	}
	return nil
}

// foldLegacyCloseMode is the one-way lazy migration of the retired
// CloseModeGitPush value (spec-git-delivery-target §3/§9): close-mode now
// records done-ness OWNERSHIP only (auto/manual/unset); the delivery
// mechanism derives from the ladder (topology.DeriveDeliveryState — a
// configured GitPushState makes push the terminal act regardless of
// close-mode). Applied at the single deserialization path AND at write,
// so any read sees the folded value and the next write persists it —
// idempotent, no sweep needed.
func foldLegacyCloseMode(meta *ServiceMeta) {
	if meta.CloseDeployMode == topology.CloseModeGitPush {
		meta.CloseDeployMode = topology.CloseModeAuto
	}
}

// ReadServiceMeta reads service metadata from baseDir/services/{hostname}.json.
// Returns nil, nil if the file does not exist.
func ReadServiceMeta(baseDir, hostname string) (*ServiceMeta, error) {
	path := filepath.Join(baseDir, "services", hostname+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil //nolint:nilnil // nil,nil = not found, by design
		}
		return nil, fmt.Errorf("read service meta: %w", err)
	}

	meta, err := parseMeta(data)
	if err != nil {
		return nil, fmt.Errorf("unmarshal service meta: %w", err)
	}
	return meta, nil
}

// ListServiceMetas reads all service metadata files from baseDir/services/.
// Returns an empty slice if the directory does not exist or is empty.
func ListServiceMetas(baseDir string) ([]*ServiceMeta, error) {
	dir, names, err := serviceMetaFiles(baseDir)
	if err != nil {
		return nil, err
	}
	var metas []*ServiceMeta
	for _, name := range names {
		data, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			return nil, fmt.Errorf("read service meta %s: %w", name, readErr)
		}
		meta, unmarshalErr := parseMeta(data)
		if unmarshalErr != nil {
			return nil, fmt.Errorf("unmarshal service meta %s: %w", name, unmarshalErr)
		}
		metas = append(metas, meta)
	}
	return metas, nil
}

// serviceMetaFiles is the services directory and the name of every pair
// record in it; none when the directory does not exist yet.
func serviceMetaFiles(baseDir string) (string, []string, error) {
	dir := filepath.Join(baseDir, "services")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return dir, nil, nil
		}
		return dir, nil, fmt.Errorf("list services dir: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			names = append(names, entry.Name())
		}
	}
	return dir, names, nil
}

// PruneServiceMetas removes service meta files that don't match any live
// hostname. A container-mode meta is kept if its Hostname OR any stage
// hostname exists in liveHostnames.
//
// Local-mode metas (Mode = local-only / local-stage) are skipped entirely —
// they're project-keyed (Hostname = project.Name set by LocalAutoAdopt), not
// service-keyed, so the live-hostnames predicate never holds. Pruning them
// silently orphaned every local meta on every develop_start, surfacing as
// spurious ADOPT_REQUIRED rejections after a successful auto-adopt.
//
// Returns the sorted list of deleted primary hostnames so callers can surface
// the cleanup transparently (e.g. bootstrap-start's `cleanedUpOrphanMetas`).
func PruneServiceMetas(baseDir string, liveHostnames map[string]bool) []string {
	metas, err := ListServiceMetas(baseDir)
	if err != nil || len(metas) == 0 {
		return nil
	}

	var deleted []string
	for _, m := range metas {
		if m == nil {
			continue
		}
		// F5: skip an empty-Hostname meta (partial/corrupt write). Without
		// this guard DeleteServiceMeta(baseDir, "") removes a non-existent
		// services/.json — os.Remove tolerates ErrNotExist as success — and
		// the empty Hostname leaks into the returned list as `[""]`. Mirrors
		// the empty-Hostname skip in ManagedRuntimeIndex.
		if m.Hostname == "" {
			continue
		}
		if m.Mode == topology.PlanModeLocalOnly || m.Mode == topology.PlanModeLocalStage {
			continue
		}
		keep := false
		for _, h := range m.Hostnames() {
			if liveHostnames[h] {
				keep = true
				break
			}
		}
		if keep {
			continue
		}
		if err := DeleteServiceMeta(baseDir, m.Hostname); err == nil {
			deleted = append(deleted, m.Hostname)
		}
	}
	sort.Strings(deleted)
	return deleted
}

// IsKnownService checks if a hostname is tracked by any ServiceMeta.
// A hostname is known if it matches any meta's Hostname or stage hostname.
// Returns false when stateDir is empty or no metas exist (permissive on error).
func IsKnownService(stateDir, hostname string) bool {
	if stateDir == "" || hostname == "" {
		return false
	}
	// Direct match by filename (fast path).
	if meta, _ := ReadServiceMeta(stateDir, hostname); meta != nil {
		return true
	}
	// Check if it's a stage hostname of any meta.
	metas, _ := ListServiceMetas(stateDir)
	for _, m := range metas {
		if slices.Contains(m.Hostnames(), hostname) {
			return true
		}
	}
	return false
}

// cleanIncompleteMetasForSession removes ServiceMeta files that were created
// by the given session but never completed (BootstrappedAt is empty).
// Best-effort — errors are silently ignored.
func cleanIncompleteMetasForSession(stateDir, sessionID string) {
	if stateDir == "" || sessionID == "" {
		return
	}
	metas, err := ListServiceMetas(stateDir)
	if err != nil {
		return
	}
	for _, m := range metas {
		if m.BootstrapSession == sessionID && !m.IsComplete() {
			_ = DeleteServiceMeta(stateDir, m.Hostname)
		}
	}
}

// FindServiceMeta returns the meta whose Hostname, a stage hostname or a
// variant-B dev service matches
// — the disk-backed counterpart to ManagedRuntimeIndex. Honors the pair-keyed
// invariant (spec-workflows.md §8 E8): container+standard and local+standard
// store exactly one file per pair; a direct read by the non-primary hostname
// would miss. Fast path hits the direct file; slow path scans metas for a
// stage or dev-service match. Returns (nil, nil) when no meta tracks hostname.
//
// Use this from tool-layer handlers when you have a hostname but not a
// pre-loaded meta slice. For slice→map construction, use ManagedRuntimeIndex.
func FindServiceMeta(stateDir, hostname string) (*ServiceMeta, error) {
	if meta, err := ReadServiceMeta(stateDir, hostname); err != nil || meta != nil {
		return meta, err
	}
	metas, err := ListServiceMetas(stateDir)
	if err != nil {
		return nil, err
	}
	for _, m := range metas {
		if _, ok := m.StageFor(hostname); ok {
			return m, nil
		}
		if _, ok := m.CodeVolume.DevService(hostname); ok {
			return m, nil
		}
	}
	return nil, nil //nolint:nilnil // not-found sentinel
}

// DeleteServiceMeta removes the service metadata file for the given hostname.
// Returns nil if the file does not exist (idempotent). Holds the .services.lock
// so a delete cannot race a concurrent UpdateServiceMeta/UpsertServiceMeta
// read-modify-write (which would otherwise resurrect a just-deleted meta, or
// drop a just-written one) — the single canonical deleter, so all callers are
// serialized against the locked writers (XCUT-1).
func DeleteServiceMeta(baseDir, hostname string) error {
	return withFileLock(filepath.Join(baseDir, serviceMetaLockName), func() error {
		path := filepath.Join(baseDir, "services", hostname+".json")
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("delete service meta: %w", err)
		}
		return nil
	})
}
