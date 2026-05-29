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
	Hostname      string        `json:"hostname"`
	Mode          topology.Mode `json:"mode,omitempty"`
	StageHostname string        `json:"stageHostname,omitempty"`

	// Per-pair deploy dimensions (deploy-strategy decomposition; see
	// plans/archive/deploy-strategy-decomposition-2026-04-28.md §3.1 for
	// the orthogonality matrix). Three independent dimensions:
	// CloseDeployMode is what the develop workflow auto-does at close;
	// GitPushState is whether git-push capability is set up;
	// BuildIntegration is which ZCP-managed CI integration responds to
	// remote git pushes.
	CloseDeployMode          topology.CloseDeployMode  `json:"closeDeployMode,omitempty"`
	CloseDeployModeConfirmed bool                      `json:"closeDeployModeConfirmed,omitempty"` // true after user explicitly confirms/sets close mode
	GitPushState             topology.GitPushState     `json:"gitPushState,omitempty"`
	RemoteURL                string                    `json:"remoteUrl,omitempty"` // cache; runtime source of truth = `git remote get-url origin`
	BuildIntegration         topology.BuildIntegration `json:"buildIntegration,omitempty"`

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
	// PrimarySetupName applies to deploys targeting Hostname's half.
	// StageSetupName applies to cross-deploys targeting StageHostname
	// (pair shapes only; empty for non-pair modes).
	//
	// Plan: plans/setup-name-local-canonical-2026-05-27.md §F3.
	PrimarySetupName string `json:"primarySetupName,omitempty"`
	StageSetupName   string `json:"stageSetupName,omitempty"`
}

// SetupNameFor returns the canonical zerops.yaml setup-block name for a
// target hostname. Pair-keyed: targetHostname == StageHostname returns
// StageSetupName; targetHostname == Hostname returns PrimarySetupName;
// any other hostname returns "" (caller must load that hostname's meta).
//
// Empty result on an in-scope hostname means "cache miss — run cascade."
// Callers that need a guaranteed value must invoke ResolveCanonicalSetup
// (see internal/workflow/setup_resolver.go from P1) rather than treating
// an empty return as authoritative.
func (m *ServiceMeta) SetupNameFor(targetHostname string) string {
	if m == nil {
		return ""
	}
	if m.StageHostname != "" && targetHostname == m.StageHostname {
		return m.StageSetupName
	}
	if targetHostname == m.Hostname {
		return m.PrimarySetupName
	}
	return ""
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
// For container+standard and local+standard that's [Hostname, StageHostname];
// for everything else just [Hostname].
func (m *ServiceMeta) Hostnames() []string {
	if m.StageHostname != "" {
		return []string{m.Hostname, m.StageHostname}
	}
	return []string{m.Hostname}
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
// have no per-service deploy role; callers should use StageHostname
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
	if m.StageHostname != "" && hostname == m.StageHostname {
		return topology.DeployRoleStage
	}
	if hostname == m.Hostname {
		return m.PrimaryRole()
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
	// runtime named in m.StageHostname IS a legitimate deploy target —
	// treating it as "stage half, push from dev half" misleads the agent
	// into pushing from a non-existent Zerops service. The container
	// standard pair stays IsStageHalf for stage-hostname matches because
	// its dev half is a real Zerops service the agent can target.
	if m.Mode == topology.PlanModeLocalStage && hostname == m.StageHostname {
		return topology.PushSourceOK
	}
	if m.StageHostname != "" && hostname == m.StageHostname {
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

// WriteServiceMeta writes service metadata to baseDir/services/{hostname}.json.
func WriteServiceMeta(baseDir string, meta *ServiceMeta) error {
	dir := filepath.Join(baseDir, "services")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create services dir: %w", err)
	}

	data, err := json.MarshalIndent(meta, "", "  ")
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

// parseMeta deserializes a ServiceMeta from JSON.
// Single deserialization path — both ReadServiceMeta and ListServiceMetas
// route through this so any future field-level invariants land at one
// integration point.
func parseMeta(data []byte) (*ServiceMeta, error) {
	var meta ServiceMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
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
	dir := filepath.Join(baseDir, "services")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("list services dir: %w", err)
	}

	var metas []*ServiceMeta
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			return nil, fmt.Errorf("read service meta %s: %w", entry.Name(), readErr)
		}
		meta, unmarshalErr := parseMeta(data)
		if unmarshalErr != nil {
			return nil, fmt.Errorf("unmarshal service meta %s: %w", entry.Name(), unmarshalErr)
		}
		metas = append(metas, meta)
	}
	return metas, nil
}

// PruneServiceMetas removes service meta files that don't match any live
// hostname. A container-mode meta is kept if its Hostname OR StageHostname
// exists in liveHostnames.
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
// A hostname is known if it matches any meta's Hostname or StageHostname.
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

// FindServiceMeta returns the meta whose Hostname OR StageHostname matches
// — the disk-backed counterpart to ManagedRuntimeIndex. Honors the pair-keyed
// invariant (spec-workflows.md §8 E8): container+standard and local+standard
// store exactly one file per pair; a direct read by the non-primary hostname
// would miss. Fast path hits the direct file; slow path scans metas for a
// StageHostname match. Returns (nil, nil) when no meta tracks hostname.
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
		if m.StageHostname == hostname {
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
