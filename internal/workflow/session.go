package workflow

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const defaultMaxIterations = 10

// WorkflowBootstrap is the workflow name for bootstrap sessions.
const WorkflowBootstrap = "bootstrap"

func maxIterations() int {
	if v := os.Getenv("ZCP_MAX_ITERATIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultMaxIterations
}

const (
	sessionsDirName = "sessions"
	stateVersion    = "1"
)

// InitSession creates a new workflow session, persists it to sessions/{id}.json,
// and registers it in the registry.
func InitSession(stateDir, projectID, workflowName, intent string) (*WorkflowState, error) {
	sessionID, err := generateSessionID()
	if err != nil {
		return nil, fmt.Errorf("init session: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	state := &WorkflowState{
		Version:   stateVersion,
		SessionID: sessionID,
		PID:       os.Getpid(),
		ProjectID: projectID,
		Workflow:  workflowName,
		Iteration: 0,
		Intent:    intent,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := saveSessionState(stateDir, sessionID, state); err != nil {
		return nil, fmt.Errorf("init session: %w", err)
	}

	entry := SessionEntry{
		SessionID: sessionID,
		PID:       os.Getpid(),
		Workflow:  workflowName,
		ProjectID: projectID,
		Intent:    intent,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := RegisterSession(stateDir, entry); err != nil {
		return nil, fmt.Errorf("init session register: %w", err)
	}

	return state, nil
}

// LoadSessionByID reads a per-session state file from sessions/{sessionID}.json.
func LoadSessionByID(stateDir, sessionID string) (*WorkflowState, error) {
	path := sessionFilePath(stateDir, sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load session %s: %w", sessionID, err)
	}
	var state WorkflowState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("load session %s unmarshal: %w", sessionID, err)
	}
	return &state, nil
}

// ResetSessionByID removes the per-session state file and unregisters from the registry.
func ResetSessionByID(stateDir, sessionID string) error {
	path := sessionFilePath(stateDir, sessionID)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reset session %s: %w", sessionID, err)
	}
	return UnregisterSession(stateDir, sessionID)
}

// IterateSession resets bootstrap steps and increments the counter.
func IterateSession(stateDir, sessionID string) (*WorkflowState, error) {
	state, err := LoadSessionByID(stateDir, sessionID)
	if err != nil {
		return nil, fmt.Errorf("iterate session: %w", err)
	}

	state.Iteration++
	if state.Bootstrap != nil {
		state.Bootstrap.ResetForIteration()
	}
	if state.Deploy != nil {
		state.Deploy.ResetForIteration()
	}
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := saveSessionState(stateDir, sessionID, state); err != nil {
		return nil, fmt.Errorf("iterate session: %w", err)
	}
	return state, nil
}

// SaveSessionState atomically writes state to sessions/{sessionID}.json.
// Exported for cross-package test access; internal callers use saveSessionState.
func SaveSessionState(stateDir, sessionID string, state *WorkflowState) error {
	return saveSessionState(stateDir, sessionID, state)
}

// saveSessionState atomically writes state to sessions/{sessionID}.json.
func saveSessionState(stateDir, sessionID string, state *WorkflowState) error {
	sessDir := filepath.Join(stateDir, sessionsDirName)
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		return fmt.Errorf("save state mkdir: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("save state marshal: %w", err)
	}

	target := sessionFilePath(stateDir, sessionID)
	tmp, err := os.CreateTemp(sessDir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("save state temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("save state write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("save state close: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("save state rename: %w", err)
	}
	return nil
}

// sessionFilePath returns the path for a per-session state file.
func sessionFilePath(stateDir, sessionID string) string {
	return filepath.Join(stateDir, sessionsDirName, sessionID+".json")
}

// InitSessionAtomic creates a new workflow session atomically within a single
// registry lock scope. It prunes dead sessions, checks bootstrap exclusivity
// (if workflowName == WorkflowBootstrap), creates the session state file, and
// appends the registry entry — all in one lock acquisition.
func InitSessionAtomic(stateDir, projectID, workflowName, intent string) (*WorkflowState, error) {
	sessionID, err := generateSessionID()
	if err != nil {
		return nil, fmt.Errorf("init session atomic: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	state := &WorkflowState{
		Version:   stateVersion,
		SessionID: sessionID,
		PID:       os.Getpid(),
		ProjectID: projectID,
		Workflow:  workflowName,
		Iteration: 0,
		Intent:    intent,
		CreatedAt: now,
		UpdatedAt: now,
	}

	err = withRegistryLock(stateDir, func(reg *Registry) (*Registry, error) {
		// Prune dead sessions.
		reg.Sessions = pruneDeadSessions(reg.Sessions)

		// Check bootstrap exclusivity.
		if workflowName == WorkflowBootstrap {
			for _, s := range reg.Sessions {
				if s.Workflow == WorkflowBootstrap {
					return reg, fmt.Errorf("init session atomic: bootstrap already active (session %s, PID %d)", s.SessionID, s.PID)
				}
			}
		}

		// Write state file while holding the lock.
		if err := saveSessionState(stateDir, sessionID, state); err != nil {
			return reg, err
		}

		// Append to registry directly (no separate RegisterSession call).
		reg.Sessions = append(reg.Sessions, SessionEntry{
			SessionID: sessionID,
			PID:       os.Getpid(),
			Workflow:  workflowName,
			ProjectID: projectID,
			Intent:    intent,
			CreatedAt: now,
			UpdatedAt: now,
		})

		return reg, nil
	})
	if err != nil {
		return nil, err
	}

	return state, nil
}

// generateSessionID creates a random session ID.
func generateSessionID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
