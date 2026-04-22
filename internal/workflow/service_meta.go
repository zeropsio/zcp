package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// Strategy constants for deploy decisions.
const (
	StrategyPushDev = "push-dev"
	StrategyPushGit = "push-git"
	StrategyManual  = "manual"
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
	Hostname          string `json:"hostname"`
	Mode              string `json:"mode,omitempty"`
	StageHostname     string `json:"stageHostname,omitempty"`
	DeployStrategy    string `json:"deployStrategy,omitempty"`
	StrategyConfirmed bool   `json:"strategyConfirmed,omitempty"` // true after user explicitly confirms/sets strategy
	Environment       string `json:"environment,omitempty"`       // "container" or "local"
	BootstrapSession  string `json:"bootstrapSession"`
	BootstrappedAt    string `json:"bootstrappedAt"`
	FirstDeployedAt   string `json:"firstDeployedAt,omitempty"` // stamped on first observed deploy — via session or adoption
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

// IsAdopted reports whether this meta records an adopted service.
// Adopted = bootstrap-complete AND BootstrapSession empty (the convention written by
// writeBootstrapOutputs when IsExisting=true). Both guards matter: incomplete metas
// with an empty session are orphans, not adoptions.
func (m *ServiceMeta) IsAdopted() bool {
	return m.BootstrapSession == "" && m.IsComplete()
}

// Hostnames returns every hostname this meta represents.
// For container+standard that's [dev, stage]; for everything else just [m.Hostname].
func (m *ServiceMeta) Hostnames() []string {
	if m.StageHostname != "" {
		return []string{m.Hostname, m.StageHostname}
	}
	return []string{m.Hostname}
}

// PrimaryRole returns the deploy role of m.Hostname.
// Encapsulates the mode+environment+stage lookup so callers don't re-derive it.
func (m *ServiceMeta) PrimaryRole() string {
	mode := m.Mode
	if mode == "" {
		mode = PlanModeStandard
	}
	// Local+standard: m.Hostname holds the stage hostname (dev doesn't exist locally).
	if m.Environment == string(EnvLocal) && mode == PlanModeStandard {
		return DeployRoleStage
	}
	switch mode {
	case PlanModeDev:
		return DeployRoleDev
	case PlanModeSimple:
		return DeployRoleSimple
	}
	return DeployRoleDev
}

// RoleFor returns the deploy role of the given hostname within this meta's scope.
// Returns "" when the hostname is unrelated to this meta.
func (m *ServiceMeta) RoleFor(hostname string) string {
	if hostname == "" {
		return ""
	}
	if m.StageHostname != "" && hostname == m.StageHostname {
		return DeployRoleStage
	}
	if hostname == m.Hostname {
		return m.PrimaryRole()
	}
	return ""
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

// PruneServiceMetas removes service meta files that don't match any live hostname.
// A meta is kept if its Hostname OR StageHostname exists in liveHostnames.
// Returns the number of pruned entries.
func PruneServiceMetas(baseDir string, liveHostnames map[string]bool) int {
	metas, err := ListServiceMetas(baseDir)
	if err != nil || len(metas) == 0 {
		return 0
	}

	pruned := 0
	for _, m := range metas {
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
			pruned++
		}
	}
	return pruned
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

// findMetaForHostname returns the meta whose Hostname OR StageHostname matches.
// Container+standard mode stores the meta as {dev}.json with a StageHostname
// field, so a direct file lookup by the stage hostname misses. Fast path hits
// the direct file; slow path scans metas for a StageHostname match.
// Returns (nil, nil) when no meta tracks hostname.
func findMetaForHostname(stateDir, hostname string) (*ServiceMeta, error) {
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
