package workflow

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	stateFileName = "zcp_state.json"
	stateVersion  = "1"
)

// InitSession creates a new workflow session and persists it to disk.
// Returns an error if a session already exists (call ResetSession first).
func InitSession(stateDir, projectID string, mode Mode, intent string) (*WorkflowState, error) {
	if _, err := LoadSession(stateDir); err == nil {
		return nil, fmt.Errorf("init session: active session exists, reset first")
	}

	sessionID, err := generateSessionID()
	if err != nil {
		return nil, fmt.Errorf("init session: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	state := &WorkflowState{
		Version:   stateVersion,
		SessionID: sessionID,
		ProjectID: projectID,
		Mode:      mode,
		Phase:     PhaseInit,
		Iteration: 0,
		Intent:    intent,
		CreatedAt: now,
		UpdatedAt: now,
		History:   []PhaseTransition{},
	}

	if err := saveState(stateDir, state); err != nil {
		return nil, err
	}
	return state, nil
}

// LoadSession reads the workflow state from disk.
func LoadSession(stateDir string) (*WorkflowState, error) {
	path := filepath.Join(stateDir, stateFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	var state WorkflowState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("load session unmarshal: %w", err)
	}
	return &state, nil
}

// ResetSession removes the state file.
func ResetSession(stateDir string) error {
	path := filepath.Join(stateDir, stateFileName)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reset session: %w", err)
	}
	return nil
}

// IterateSession archives evidence, resets phase to DEVELOP, and increments the counter.
func IterateSession(stateDir, evidenceDir string) (*WorkflowState, error) {
	state, err := LoadSession(stateDir)
	if err != nil {
		return nil, fmt.Errorf("iterate session: %w", err)
	}

	nextIter := state.Iteration + 1

	// Archive evidence for the current iteration.
	if err := ArchiveEvidence(evidenceDir, state.SessionID, nextIter); err != nil {
		return nil, fmt.Errorf("iterate session archive: %w", err)
	}

	state.Phase = PhaseDevelop
	state.Iteration = nextIter
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	state.History = append(state.History, PhaseTransition{
		From: state.Phase,
		To:   PhaseDevelop,
		At:   state.UpdatedAt,
	})

	if err := saveState(stateDir, state); err != nil {
		return nil, err
	}
	return state, nil
}

// saveState atomically writes the state to disk.
func saveState(stateDir string, state *WorkflowState) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("save state mkdir: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("save state marshal: %w", err)
	}

	target := filepath.Join(stateDir, stateFileName)
	tmp, err := os.CreateTemp(stateDir, ".state-*.tmp")
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

// generateSessionID creates a random session ID.
func generateSessionID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
