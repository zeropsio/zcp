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
	StrategyCICD    = "ci-cd"
	StrategyManual  = "manual"
)

// ServiceMeta records bootstrap decisions for a service.
// ZCP's persistent knowledge — the API doesn't track mode, pairing, or strategy.
// The API is the source of truth for operational state (running, resources, envs).
type ServiceMeta struct {
	Hostname         string `json:"hostname"`
	Mode             string `json:"mode,omitempty"`
	StageHostname    string `json:"stageHostname,omitempty"`
	DeployStrategy   string `json:"deployStrategy,omitempty"`
	Environment      string `json:"environment,omitempty"` // "container" or "local"
	BootstrapSession string `json:"bootstrapSession"`
	BootstrappedAt   string `json:"bootstrappedAt"`
}

// IsComplete returns true if bootstrap finished for this service.
// BootstrappedAt is set only at bootstrap completion — empty means
// the service was provisioned but bootstrap didn't finish.
func (m *ServiceMeta) IsComplete() bool {
	return m.BootstrappedAt != ""
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

	var meta ServiceMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal service meta: %w", err)
	}
	return &meta, nil
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
		var meta ServiceMeta
		if unmarshalErr := json.Unmarshal(data, &meta); unmarshalErr != nil {
			return nil, fmt.Errorf("unmarshal service meta %s: %w", entry.Name(), unmarshalErr)
		}
		metas = append(metas, &meta)
	}
	return metas, nil
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
