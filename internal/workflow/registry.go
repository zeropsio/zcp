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

	// flockRetries × flockInterval = max wait time for registry lock (~5s).
	flockRetries  = 50
	flockInterval = 100 * time.Millisecond
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
	StartTime string `json:"startTime,omitempty"` // (pid,startTime) identity — detects recycled PIDs; omitempty round-trips old registries
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

// updateRegistryPID updates the PID for a session in the registry.
// Used during auto-recovery to claim a session from a dead process. Writes the
// claiming process's (pid,startTime) so a later liveness check identifies the
// new owner exactly.
func updateRegistryPID(stateDir, sessionID string, pid int, startTime string) error {
	return withRegistryLock(stateDir, func(reg *Registry) (*Registry, error) {
		for i := range reg.Sessions {
			if reg.Sessions[i].SessionID == sessionID {
				reg.Sessions[i].PID = pid
				reg.Sessions[i].StartTime = startTime
				reg.Sessions[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				break
			}
		}
		return reg, nil
	})
}

// CurrentProcessStartTime returns this process's start-time string (or "" when
// unreadable), stamped into new/claimed sessions so a later liveness check can
// detect a recycled PID. Exported so the tools layer can stamp its own session
// entries with the same value.
func CurrentProcessStartTime() string {
	st, _ := processStartTime(os.Getpid())
	return st
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
		if isProcessAlive(s.PID, s.StartTime) {
			alive = append(alive, s)
		} else {
			dead = append(dead, s)
		}
	}
	return alive, dead
}

// InFlightBootstrapHostnames returns the SET of runtime hostnames that an
// ALIVE (process-running) bootstrap session is actively provisioning — i.e.
// the session has REACHED the provision step, so its `zerops_import` has run
// (or is running) and the service exists on the platform but is not yet
// meta-stamped (the window between import and the provision-step partial-meta
// write, writeProvisionMetas). discover classifies such a service
// `AdoptionBootstrapping` (silent, no warning) instead of firing a false
// adopt/resume warning on a service the agent just created.
//
// The provision-reached gate is load-bearing: a session still at the discover
// step has NOT imported anything, so a pre-existing same-named live service is
// genuinely adoptable (or a name collision) and MUST NOT be suppressed.
// Returns a set (not hostname→session) because the silent classification needs
// no session ID, which also sidesteps last-write-wins ambiguity when two alive
// sessions plan the same hostname.
func InFlightBootstrapHostnames(stateDir string) map[string]bool {
	out := map[string]bool{}
	if stateDir == "" {
		return out
	}
	sessions, err := ListSessions(stateDir)
	if err != nil {
		return out
	}
	alive, _ := ClassifySessions(sessions)
	for _, s := range alive {
		state, err := LoadSessionByID(stateDir, s.SessionID)
		if err != nil || state == nil || state.Bootstrap == nil || state.Bootstrap.Plan == nil {
			continue
		}
		if !bootstrapReachedProvision(state.Bootstrap) {
			continue
		}
		for _, t := range state.Bootstrap.Plan.Targets {
			if h := t.Runtime.DevHostname; h != "" {
				out[h] = true
			}
			if h := t.Runtime.StageHostname(); h != "" {
				out[h] = true
			}
		}
	}
	return out
}

// bootstrapReachedProvision reports whether the bootstrap has advanced to (or
// past) the provision step — the point at which zerops_import runs. Before
// provision the session has touched nothing on the platform.
func bootstrapReachedProvision(b *BootstrapState) bool {
	for i, step := range b.Steps {
		if step.Name == StepProvision {
			return b.CurrentStep >= i
		}
	}
	return false
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

// withFileLock runs fn while holding an exclusive cross-process flock on
// lockPath (creating the lock file if needed). It is the single state-dir
// serialization primitive — both the registry (withRegistryLock) and the
// ServiceMeta read-modify-write (UpdateServiceMeta) use it, on DISTINCT lock
// files (.registry.lock vs .services.lock).
//
// HAZARD: flock is per open-file-description, so a second OpenFile+flock on the
// SAME path from one process blocks on itself (deadlock). Never nest the same
// lock file; and never hold both the registry lock and the services lock at
// once (today no code path does — keep it that way).
func withFileLock(lockPath string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return fmt.Errorf("lock mkdir: %w", err)
	}
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("lock open: %w", err)
	}
	defer lockFile.Close()

	if err := lockFileExclusive(lockFile); err != nil {
		return fmt.Errorf("flock: %w", err)
	}
	defer unlockFile(lockFile)

	return fn()
}

// withRegistryLock acquires an exclusive file lock, reads the registry, calls fn, and writes back.
func withRegistryLock(stateDir string, fn func(*Registry) (*Registry, error)) error {
	return withFileLock(filepath.Join(stateDir, lockFileName), func() error {
		reg, err := readRegistry(stateDir)
		if err != nil {
			return err
		}
		updated, err := fn(reg)
		if err != nil {
			return err
		}
		return writeRegistry(stateDir, updated)
	})
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
		if !isProcessAlive(s.PID, s.StartTime) {
			continue
		}
		if t, err := time.Parse(time.RFC3339, s.CreatedAt); err == nil && t.Before(cutoff) {
			continue
		}
		alive = append(alive, s)
	}
	return alive
}
