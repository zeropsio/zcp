package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ServiceMeta records decisions made during bootstrap for a service.
// These are historical records, NOT state — the API is the source of truth.
type ServiceMeta struct {
	Hostname         string            `json:"hostname"`
	Type             string            `json:"type"`
	Mode             string            `json:"mode,omitempty"`
	StageHostname    string            `json:"stageHostname,omitempty"`
	DeployFlow       string            `json:"deployFlow,omitempty"`
	Dependencies     []string          `json:"dependencies,omitempty"`
	BootstrapSession string            `json:"bootstrapSession"`
	BootstrappedAt   string            `json:"bootstrappedAt"`
	Decisions        map[string]string `json:"decisions,omitempty"`
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
