package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Strategy constants for deploy decisions.
const (
	StrategyPushDev = "push-dev"
	StrategyPushGit = "push-git"
	StrategyManual  = "manual"
)

// ServiceMeta records bootstrap decisions for a service.
// ZCP's persistent knowledge — the API doesn't track mode, pairing, or strategy.
// The API is the source of truth for operational state (running, resources, envs).
type ServiceMeta struct {
	Hostname          string `json:"hostname"`
	Mode              string `json:"mode,omitempty"`
	StageHostname     string `json:"stageHostname,omitempty"`
	DeployStrategy    string `json:"deployStrategy,omitempty"`
	StrategyConfirmed bool   `json:"strategyConfirmed,omitempty"` // true after user explicitly confirms/sets strategy
	Environment       string `json:"environment,omitempty"`       // "container" or "local"
	BootstrapSession  string `json:"bootstrapSession"`
	BootstrappedAt    string `json:"bootstrappedAt"`
}

// IsComplete returns true if bootstrap finished for this service.
// BootstrappedAt is set only at bootstrap completion — empty means
// the service was provisioned but bootstrap didn't finish.
func (m *ServiceMeta) IsComplete() bool {
	return m.BootstrappedAt != ""
}

// EffectiveStrategy returns the deploy strategy as set by the user.
// Handles backward compatibility: old bootstrap metas wrote "push-dev" with
// StrategyConfirmed=false as a default. These are treated as empty (not user-chosen).
// After the fix, bootstrap writes empty DeployStrategy, making this a no-op for new metas.
func (m *ServiceMeta) EffectiveStrategy() string {
	if m.DeployStrategy == StrategyPushDev && !m.StrategyConfirmed {
		return "" // old bootstrap default, not a user choice
	}
	return m.DeployStrategy
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
		if liveHostnames[m.Hostname] || liveHostnames[m.StageHostname] {
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
	meta, _ := ReadServiceMeta(stateDir, hostname)
	if meta != nil {
		return true
	}
	// Check if it's a stage hostname of any meta.
	metas, _ := ListServiceMetas(stateDir)
	for _, m := range metas {
		if m.StageHostname == hostname {
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

// DeleteServiceMeta removes the service metadata file for the given hostname.
// Returns nil if the file does not exist (idempotent).
func DeleteServiceMeta(baseDir, hostname string) error {
	path := filepath.Join(baseDir, "services", hostname+".json")
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("delete service meta: %w", err)
	}
	return nil
}
