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

	// Per-pair deploy dimensions (deploy-strategy decomposition Phase 1/2;
	// see plans/deploy-strategy-decomposition-2026-04-28.md §3.1 for the
	// orthogonality matrix). Three orthogonal dimensions replace the
	// conflated DeployStrategy + PushGitTrigger pair: CloseDeployMode is
	// what the develop workflow auto-does at close, GitPushState is whether
	// git-push capability is set up, BuildIntegration is which ZCP-managed
	// CI integration responds to remote git pushes.
	CloseDeployMode          topology.CloseDeployMode  `json:"closeDeployMode,omitempty"`
	CloseDeployModeConfirmed bool                      `json:"closeDeployModeConfirmed,omitempty"` // true after user explicitly confirms/sets close mode
	GitPushState             topology.GitPushState     `json:"gitPushState,omitempty"`
	RemoteURL                string                    `json:"remoteUrl,omitempty"` // cache; runtime source of truth = `git remote get-url origin`
	BuildIntegration         topology.BuildIntegration `json:"buildIntegration,omitempty"`

	// Legacy strategy fields (deprecated; deleted in Phase 10 of the
	// decomposition plan after one migrate cycle). migrateOldMeta runs on
	// every parseMeta call and normalizes these into the new dimensions
	// when the new fields are unset; reads always see populated new fields.
	DeployStrategy    topology.DeployStrategy `json:"deployStrategy,omitempty"`
	PushGitTrigger    topology.PushGitTrigger `json:"pushGitTrigger,omitempty"`    // valid only when DeployStrategy==push-git
	StrategyConfirmed bool                    `json:"strategyConfirmed,omitempty"` // true after user explicitly confirms/sets strategy

	BootstrapSession string `json:"bootstrapSession"`
	BootstrappedAt   string `json:"bootstrappedAt"`
	FirstDeployedAt  string `json:"firstDeployedAt,omitempty"` // stamped on first observed deploy — via session or adoption
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
// hostname WITHOUT requiring an active work session. Bridges manual
// deployers (zcli, CI/CD outside MCP, custom platform calls) to MCP-tracked
// state — once stamped, ServiceSnapshot.Deployed flips to true and develop
// atoms gated on `deployStates: [deployed]` start firing for that service.
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

// IsPushSourceFor returns true when hostname is a source-of-push within this
// meta's pair scope. False for stage hostnames (build target, never source)
// and for ModeDev services (legacy dev-only mode incompatible with push-git
// per the deploy-strategy decomposition §3.2).
//
// Reads meta.Mode directly (PlanMode values alias topology.Mode values, so
// topology.IsPushSource classifies them correctly) rather than going through
// resolveEnvelopeMode — the envelope projection collapses local-* meta modes
// onto ModeDev for atom-rendering purposes, which loses the push-source
// information needed here. Stage-hostname check is the explicit pair-half
// carve-out.
//
// Used by handleGitPush + handleLocalGitPush (deploy-decomp P4) to reject
// targetService that is not a source-of-push, returning a remediation
// pointing at the correct dev hostname.
func (m *ServiceMeta) IsPushSourceFor(hostname string) bool {
	if m == nil || hostname == "" {
		return false
	}
	if m.StageHostname != "" && hostname == m.StageHostname {
		return false
	}
	if hostname != m.Hostname {
		return false
	}
	return topology.IsPushSource(m.Mode)
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

// parseMeta deserializes a ServiceMeta from JSON.
// Single deserialization path — both ReadServiceMeta and ListServiceMetas use this.
//
// migrateOldMeta runs on every parseMeta result so every read path (including
// FindServiceMeta and any consumer of ManagedRuntimeIndex) sees the new
// per-pair dimensions populated, even on legacy on-disk metas that pre-date
// the deploy-strategy decomposition (plan
// plans/deploy-strategy-decomposition-2026-04-28.md Phase 2). Hooking only
// ReadServiceMeta would leave router/envelope (which uses ListServiceMetas
// → ManagedRuntimeIndex) seeing un-migrated metas — confirmed during
// Codex PRE-WORK as the failure mode the parseMeta integration prevents.
func parseMeta(data []byte) (*ServiceMeta, error) {
	var meta ServiceMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	migrateOldMeta(&meta)
	return &meta, nil
}

// migrateOldMeta normalizes a freshly-parsed ServiceMeta into the
// post-decomposition shape. Reads the legacy DeployStrategy / PushGitTrigger
// / StrategyConfirmed fields and populates the new CloseDeployMode /
// CloseDeployModeConfirmed / GitPushState / BuildIntegration fields when
// the new ones are unset. Idempotent — running it twice is a no-op because
// every branch is guarded on "new field is empty".
//
// Mapping (per plan §3.4 Scenario F):
//
//   - DeployStrategy → CloseDeployMode
//     "" / "unset" → CloseModeUnset
//     "push-dev"   → CloseModeAuto
//     "push-git"   → CloseModeGitPush
//     "manual"     → CloseModeManual
//   - PushGitTrigger → BuildIntegration
//     "" / "unset" → BuildIntegrationNone
//     "webhook"    → BuildIntegrationWebhook
//     "actions"    → BuildIntegrationActions
//   - StrategyConfirmed → CloseDeployModeConfirmed (true→true; false leaves as-is)
//   - GitPushState heuristic:
//     was push-git AND FirstDeployedAt set → GitPushConfigured
//     was push-git AND no FirstDeployedAt  → GitPushUnknown (probe needed)
//     anything else                         → GitPushUnconfigured
//   - RemoteURL stays empty (data lost; fills on next push or probe).
//
// Removed in Phase 10 of the decomposition plan once the legacy fields
// are deleted.
func migrateOldMeta(meta *ServiceMeta) {
	if meta == nil {
		return
	}
	if meta.CloseDeployMode == "" {
		switch meta.DeployStrategy {
		case topology.StrategyPushDev:
			meta.CloseDeployMode = topology.CloseModeAuto
		case topology.StrategyPushGit:
			meta.CloseDeployMode = topology.CloseModeGitPush
		case topology.StrategyManual:
			meta.CloseDeployMode = topology.CloseModeManual
		case topology.StrategyUnset:
			meta.CloseDeployMode = topology.CloseModeUnset
		default:
			// Empty-string zero value (legacy metas pre-StrategyUnset).
			meta.CloseDeployMode = topology.CloseModeUnset
		}
	}
	if !meta.CloseDeployModeConfirmed && meta.StrategyConfirmed {
		meta.CloseDeployModeConfirmed = true
	}
	if meta.BuildIntegration == "" {
		switch meta.PushGitTrigger {
		case topology.TriggerWebhook:
			meta.BuildIntegration = topology.BuildIntegrationWebhook
		case topology.TriggerActions:
			meta.BuildIntegration = topology.BuildIntegrationActions
		case topology.TriggerUnset:
			meta.BuildIntegration = topology.BuildIntegrationNone
		default:
			// Empty-string zero value (legacy metas pre-TriggerUnset).
			meta.BuildIntegration = topology.BuildIntegrationNone
		}
	}
	if meta.GitPushState == "" {
		switch {
		case meta.DeployStrategy == topology.StrategyPushGit && meta.FirstDeployedAt != "":
			meta.GitPushState = topology.GitPushConfigured
		case meta.DeployStrategy == topology.StrategyPushGit:
			meta.GitPushState = topology.GitPushUnknown
		default:
			meta.GitPushState = topology.GitPushUnconfigured
		}
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

// PruneServiceMetas removes service meta files that don't match any live hostname.
// A meta is kept if its Hostname OR StageHostname exists in liveHostnames.
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
// Returns nil if the file does not exist (idempotent).
func DeleteServiceMeta(baseDir, hostname string) error {
	path := filepath.Join(baseDir, "services", hostname+".json")
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("delete service meta: %w", err)
	}
	return nil
}
