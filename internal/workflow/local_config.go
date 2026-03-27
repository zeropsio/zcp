package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const localConfigFile = "local.json"

// LocalConfig holds local development configuration persisted in .zcp/state/.
type LocalConfig struct {
	Port    int    `json:"port"`
	EnvFile string `json:"envFile"`
}

// WriteLocalConfig writes the local config to .zcp/state/local.json.
func WriteLocalConfig(stateDir string, config *LocalConfig) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal local config: %w", err)
	}
	path := filepath.Join(stateDir, localConfigFile)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write local config: %w", err)
	}
	return nil
}

// ReadLocalConfig reads the local config from .zcp/state/local.json.
// Returns nil if the file doesn't exist.
func ReadLocalConfig(stateDir string) (*LocalConfig, error) {
	path := filepath.Join(stateDir, localConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil //nolint:nilnil // nil,nil = not found, by design
		}
		return nil, fmt.Errorf("read local config: %w", err)
	}
	var config LocalConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse local config: %w", err)
	}
	return &config, nil
}
