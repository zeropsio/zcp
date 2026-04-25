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

// defaultMaxIterations aligns with the 3-tier escalation ladder in
// BuildIterationDelta: tiers 1-2 diagnose, 3-4 systematic check, 5 stop.
// Cap at 5 so the STOP tier fires exactly once instead of repeating.
const defaultMaxIterations = 5

// WorkflowBootstrap is the workflow name for bootstrap sessions.
const WorkflowBootstrap = "bootstrap"

// WorkflowDevelop is the workflow name for develop sessions.
const WorkflowDevelop = "develop"

// Deploy step constants.
const (
	DeployStepPrepare = "prepare"
	DeployStepExecute = "execute"
	DeployStepVerify  = "verify"
)

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

// IterateSession increments the iteration counter. Recipe resets its
// generate/deploy/finalize steps; bootstrap never iterates under Option A
// (infrastructure verification hard-stops and escalates to the user).
func IterateSession(stateDir, sessionID string) (*WorkflowState, error) {
	state, err := LoadSessionByID(stateDir, sessionID)
	if err != nil {
		return nil, fmt.Errorf("iterate session: %w", err)
	}

	state.Iteration++
	if state.Recipe != nil {
		state.Recipe.ResetForIteration()
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
	return atomicWriteJSON(filepath.Join(stateDir, sessionsDirName), ".state-*.tmp", sessionFilePath(stateDir, sessionID), state)
}

// atomicWriteJSON marshals v and writes it to target via a temp file + rename,
// creating dir if needed. Callers own the filename; this helper only manages
// directory creation and atomic replacement.
func atomicWriteJSON(dir, tmpPattern, target string, v any) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp, err := os.CreateTemp(dir, tmpPattern)
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename: %w", err)
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
		// Prune dead sessions and clean orphaned session files.
		reg.Sessions = pruneDeadSessions(reg.Sessions)
		liveIDs := make(map[string]bool, len(reg.Sessions)+1)
		for _, s := range reg.Sessions {
			liveIDs[s.SessionID] = true
		}
		liveIDs[sessionID] = true // about to be created
		cleanOrphanedFiles(stateDir, liveIDs)

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
