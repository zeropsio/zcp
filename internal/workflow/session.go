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
	legacyStateFile = "zcp_state.json"
	stateVersion    = "1"
)

// InitSession creates a new workflow session, persists it to sessions/{id}.json,
// and registers it in the registry. Cleans up legacy zcp_state.json if found.
func InitSession(stateDir, projectID, workflowName, intent string) (*WorkflowState, error) {
	cleanupLegacyState(stateDir)

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
		Phase:     PhaseInit,
		Iteration: 0,
		Intent:    intent,
		CreatedAt: now,
		UpdatedAt: now,
		History:   []PhaseTransition{},
	}

	if err := saveSessionState(stateDir, sessionID, state); err != nil {
		return nil, err
	}

	entry := SessionEntry{
		SessionID: sessionID,
		PID:       os.Getpid(),
		Workflow:  workflowName,
		ProjectID: projectID,
		Phase:     PhaseInit,
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

// IterateSession archives evidence, resets phase to DEVELOP, and increments the counter.
func IterateSession(stateDir, evidenceDir, sessionID string) (*WorkflowState, error) {
	state, err := LoadSessionByID(stateDir, sessionID)
	if err != nil {
		return nil, fmt.Errorf("iterate session: %w", err)
	}

	nextIter := state.Iteration + 1

	if err := ArchiveEvidence(evidenceDir, state.SessionID, nextIter); err != nil {
		return nil, fmt.Errorf("iterate session archive: %w", err)
	}

	prevPhase := state.Phase
	state.Phase = PhaseDevelop
	state.Iteration = nextIter
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	state.History = append(state.History, PhaseTransition{
		From: prevPhase,
		To:   PhaseDevelop,
		At:   state.UpdatedAt,
	})

	if err := saveSessionState(stateDir, sessionID, state); err != nil {
		return nil, err
	}
	return state, nil
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

// cleanupLegacyState removes the old singleton zcp_state.json if found.
func cleanupLegacyState(stateDir string) {
	path := filepath.Join(stateDir, legacyStateFile)
	_ = os.Remove(path)
}

// generateSessionID creates a random session ID.
func generateSessionID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
