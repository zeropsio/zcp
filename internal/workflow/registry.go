package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	registryFileName = "registry.json"
	lockFileName     = ".registry.lock"
	registryVersion  = "1"
)

// Registry is the active sessions index persisted to registry.json.
type Registry struct {
	Version  string         `json:"version"`
	Sessions []SessionEntry `json:"sessions"`
}

// SessionEntry represents one active session in the registry.
type SessionEntry struct {
	SessionID string `json:"sessionId"`
	PID       int    `json:"pid"`
	Workflow  string `json:"workflow"`
	ProjectID string `json:"projectId"`
	Intent    string `json:"intent"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// RegisterSession adds an entry to the registry.
func RegisterSession(stateDir string, entry SessionEntry) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if entry.CreatedAt == "" {
		entry.CreatedAt = now
	}
	if entry.UpdatedAt == "" {
		entry.UpdatedAt = now
	}

	return withRegistryLock(stateDir, func(reg *Registry) (*Registry, error) {
		reg.Sessions = append(reg.Sessions, entry)
		return reg, nil
	})
}

// UnregisterSession removes an entry from the registry by session ID.
// No error if the session is not found.
func UnregisterSession(stateDir, sessionID string) error {
	return withRegistryLock(stateDir, func(reg *Registry) (*Registry, error) {
		filtered := reg.Sessions[:0]
		for _, s := range reg.Sessions {
			if s.SessionID != sessionID {
				filtered = append(filtered, s)
			}
		}
		reg.Sessions = filtered
		return reg, nil
	})
}

// ListSessions returns all sessions from the registry (read-only, no pruning).
func ListSessions(stateDir string) ([]SessionEntry, error) {
	reg, err := readRegistryShared(stateDir)
	if err != nil {
		return nil, err
	}
	result := make([]SessionEntry, len(reg.Sessions))
	copy(result, reg.Sessions)
	return result, nil
}

// ClassifySessions splits sessions into alive (PID running) and dead (PID not running).
func ClassifySessions(sessions []SessionEntry) (alive, dead []SessionEntry) {
	for _, s := range sessions {
		if isProcessAlive(s.PID) {
			alive = append(alive, s)
		} else {
			dead = append(dead, s)
		}
	}
	return alive, dead
}

// readRegistryShared reads the registry under a shared (read-only) file lock.
func readRegistryShared(stateDir string) (*Registry, error) {
	lockPath := filepath.Join(stateDir, lockFileName)
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		if os.IsNotExist(err) {
			return &Registry{Version: registryVersion}, nil
		}
		return nil, fmt.Errorf("registry lock open: %w", err)
	}
	defer lockFile.Close()

	if err := lockFileShared(lockFile); err != nil {
		return nil, fmt.Errorf("registry shared flock: %w", err)
	}
	defer unlockFile(lockFile)

	return readRegistry(stateDir)
}

// withRegistryLock acquires an exclusive file lock, reads the registry, calls fn, and writes back.
func withRegistryLock(stateDir string, fn func(*Registry) (*Registry, error)) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("registry mkdir: %w", err)
	}

	lockPath := filepath.Join(stateDir, lockFileName)
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("registry lock open: %w", err)
	}
	defer lockFile.Close()

	if err := lockFileExclusive(lockFile); err != nil {
		return fmt.Errorf("registry flock: %w", err)
	}
	defer unlockFile(lockFile)

	reg, err := readRegistry(stateDir)
	if err != nil {
		return err
	}

	updated, err := fn(reg)
	if err != nil {
		return err
	}

	return writeRegistry(stateDir, updated)
}

// readRegistry reads the registry from disk. Returns empty registry if file doesn't exist.
func readRegistry(stateDir string) (*Registry, error) {
	path := filepath.Join(stateDir, registryFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Registry{Version: registryVersion}, nil
		}
		return nil, fmt.Errorf("registry read: %w", err)
	}

	var reg Registry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("registry unmarshal: %w", err)
	}
	return &reg, nil
}

// writeRegistry atomically writes the registry to disk.
func writeRegistry(stateDir string, reg *Registry) error {
	reg.Version = registryVersion
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("registry marshal: %w", err)
	}

	target := filepath.Join(stateDir, registryFileName)
	tmp, err := os.CreateTemp(stateDir, ".registry-*.tmp")
	if err != nil {
		return fmt.Errorf("registry temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("registry write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("registry close: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("registry rename: %w", err)
	}
	return nil
}

// cleanOrphanedFiles removes session files that are not associated with any live session.
func cleanOrphanedFiles(stateDir string, liveIDs map[string]bool) {
	sessDir := filepath.Join(stateDir, sessionsDirName)
	entries, err := os.ReadDir(sessDir)
	if err == nil {
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() && len(name) > 5 && name[len(name)-5:] == ".json" {
				id := name[:len(name)-5]
				if !liveIDs[id] {
					_ = os.Remove(filepath.Join(sessDir, name))
				}
			}
		}
	}
}

// pruneDeadSessions removes entries with dead PIDs or entries older than 24h.
func pruneDeadSessions(sessions []SessionEntry) []SessionEntry {
	cutoff := time.Now().Add(-24 * time.Hour)
	alive := sessions[:0]
	for _, s := range sessions {
		if !isProcessAlive(s.PID) {
			continue
		}
		if t, err := time.Parse(time.RFC3339, s.CreatedAt); err == nil && t.Before(cutoff) {
			continue
		}
		alive = append(alive, s)
	}
	return alive
}
