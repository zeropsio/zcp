package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// Engine orchestrates the workflow lifecycle.
type Engine struct {
	stateDir    string
	evidenceDir string
	sessionID   string
}

// NewEngine creates a new workflow engine rooted at baseDir.
func NewEngine(baseDir string) *Engine {
	return &Engine{
		stateDir:    baseDir,
		evidenceDir: filepath.Join(baseDir, "evidence"),
	}
}

// Start creates a new workflow session with auto-reset, exclusivity, and registry refresh.
func (e *Engine) Start(projectID, workflowName, intent string) (*WorkflowState, error) {
	if e.sessionID != "" {
		if existing, err := LoadSessionByID(e.stateDir, e.sessionID); err == nil {
			if existing.Phase == PhaseDone {
				if err := ResetSessionByID(e.stateDir, e.sessionID); err != nil {
					return nil, fmt.Errorf("start auto-reset: %w", err)
				}
				e.sessionID = ""
			} else {
				return nil, fmt.Errorf("start: active session %s in phase %s, reset first", e.sessionID, existing.Phase)
			}
		}
	}

	state, err := InitSessionAtomic(e.stateDir, projectID, workflowName, intent)
	if err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}
	e.sessionID = state.SessionID
	return state, nil
}

// Transition moves the workflow to the next phase.
// Returns (state, nil, nil) on success, (nil, gateResult, nil) on gate failure,
// or (nil, nil, err) on other errors.
func (e *Engine) Transition(phase Phase) (*WorkflowState, *GateResult, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, nil, fmt.Errorf("transition: %w", err)
	}
	if !IsValidTransition(state.Phase, phase) {
		return nil, nil, fmt.Errorf("transition: invalid %s → %s", state.Phase, phase)
	}
	result, err := CheckGate(state.Phase, phase, e.evidenceDir, state.SessionID)
	if err != nil {
		return nil, nil, fmt.Errorf("transition gate check: %w", err)
	}
	if !result.Passed {
		return nil, result, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	state.History = append(state.History, PhaseTransition{From: state.Phase, To: phase, At: now})
	state.Phase = phase
	state.UpdatedAt = now
	if err := saveStateAndUpdateRegistry(e.stateDir, e.sessionID, state, phase); err != nil {
		return nil, nil, fmt.Errorf("transition: %w", err)
	}
	return state, nil, nil
}

// RecordEvidence saves evidence for the current session.
func (e *Engine) RecordEvidence(ev *Evidence) error {
	state, err := e.loadState()
	if err != nil {
		return fmt.Errorf("record evidence: %w", err)
	}
	ev.SessionID = state.SessionID
	return SaveEvidence(e.evidenceDir, state.SessionID, ev)
}

// Reset clears the current session.
func (e *Engine) Reset() error {
	if e.sessionID == "" {
		return nil
	}
	err := ResetSessionByID(e.stateDir, e.sessionID)
	e.sessionID = ""
	return err
}

// Iterate archives evidence and resets to DEVELOP.
func (e *Engine) Iterate() (*WorkflowState, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("iterate: %w", err)
	}
	if state.Iteration >= maxIterations() {
		return nil, fmt.Errorf("iterate: max iterations reached (%d), reset session to continue", maxIterations())
	}
	return IterateSession(e.stateDir, e.evidenceDir, e.sessionID)
}

// HasActiveSession returns true if this engine has an active session.
func (e *Engine) HasActiveSession() bool {
	return e.sessionID != ""
}

// GetState returns the current workflow state.
func (e *Engine) GetState() (*WorkflowState, error) {
	return e.loadState()
}

// SessionID returns the current session ID.
func (e *Engine) SessionID() string {
	return e.sessionID
}

// ListActiveSessions returns all active sessions from the registry.
func (e *Engine) ListActiveSessions() ([]SessionEntry, error) {
	return ListSessions(e.stateDir)
}

// BootstrapStart creates a new session with bootstrap state and returns the first step.
func (e *Engine) BootstrapStart(projectID, intent string) (*BootstrapResponse, error) {
	state, err := e.Start(projectID, "bootstrap", intent)
	if err != nil {
		return nil, fmt.Errorf("bootstrap start: %w", err)
	}

	bs := NewBootstrapState()
	bs.Steps[0].Status = stepInProgress
	state.Bootstrap = bs

	if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return nil, fmt.Errorf("bootstrap start save: %w", err)
	}
	return bs.BuildResponse(state.SessionID, intent, state.Iteration), nil
}

// BootstrapComplete completes the current step and advances to the next.
func (e *Engine) BootstrapComplete(ctx context.Context, stepName string, attestation string, checker StepChecker) (*BootstrapResponse, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("bootstrap complete: %w", err)
	}
	if state.Bootstrap == nil || !state.Bootstrap.Active {
		return nil, fmt.Errorf("bootstrap complete: bootstrap not active")
	}

	if checker != nil {
		result, checkErr := checker(ctx, state.Bootstrap.Plan, state.Bootstrap)
		if checkErr != nil {
			return nil, fmt.Errorf("step check: %w", checkErr)
		}
		if result != nil && !result.Passed {
			resp := state.Bootstrap.BuildResponse(state.SessionID, state.Intent, state.Iteration)
			resp.CheckResult = result
			resp.Message = fmt.Sprintf("Step %q: %s — fix issues and retry", stepName, result.Summary)
			return resp, nil
		}
	}

	if err := state.Bootstrap.CompleteStep(stepName, attestation); err != nil {
		return nil, fmt.Errorf("bootstrap complete: %w", err)
	}

	if state.Bootstrap.CurrentStep < len(state.Bootstrap.Steps) {
		state.Bootstrap.Steps[state.Bootstrap.CurrentStep].Status = stepInProgress
	}

	// Write incremental service metas after provision step.
	if stepName == StepProvision {
		e.writeServiceMetas(state, MetaStatusProvisioned)
	}

	sessionID := e.sessionID // capture before auto-complete may clear it

	if !state.Bootstrap.Active {
		if err := e.autoCompleteBootstrap(state); err != nil {
			return nil, fmt.Errorf("bootstrap auto-complete: %w", err)
		}
	}

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveSessionState(e.stateDir, sessionID, state); err != nil {
		return nil, fmt.Errorf("bootstrap complete save: %w", err)
	}
	return state.Bootstrap.BuildResponse(state.SessionID, state.Intent, state.Iteration), nil
}

// BootstrapCompletePlan validates a structured plan, completes the "plan" step, and stores it.
func (e *Engine) BootstrapCompletePlan(targets []BootstrapTarget, liveTypes []platform.ServiceStackType, liveServices []platform.ServiceStack) (*BootstrapResponse, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("bootstrap complete plan: %w", err)
	}
	if state.Bootstrap == nil || !state.Bootstrap.Active {
		return nil, fmt.Errorf("bootstrap complete plan: bootstrap not active")
	}
	if state.Bootstrap.CurrentStepName() != StepDiscover {
		return nil, fmt.Errorf("bootstrap complete plan: current step is %q, not %q", state.Bootstrap.CurrentStepName(), StepDiscover)
	}

	defaulted, err := ValidateBootstrapTargets(targets, liveTypes, liveServices)
	if err != nil {
		return nil, fmt.Errorf("bootstrap complete plan: %w", err)
	}

	defaultedSet := make(map[string]bool, len(defaulted))
	for _, h := range defaulted {
		defaultedSet[h] = true
	}
	var parts []string
	for _, target := range targets {
		entry := target.Runtime.DevHostname + " (" + target.Runtime.Type + ")"
		parts = append(parts, entry)
		for _, dep := range target.Dependencies {
			depEntry := dep.Hostname + " (" + dep.Type
			if dep.Mode != "" {
				depEntry += ", " + dep.Mode
				if defaultedSet[dep.Hostname] {
					depEntry += " [defaulted]"
				}
			}
			depEntry += ")"
			parts = append(parts, depEntry)
		}
	}
	attestation := "Planned targets: " + strings.Join(parts, ", ")

	if err := state.Bootstrap.CompleteStep(StepDiscover, attestation); err != nil {
		return nil, fmt.Errorf("bootstrap complete plan: %w", err)
	}

	state.Bootstrap.Plan = &ServicePlan{
		Targets:   targets,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if state.Bootstrap.CurrentStep < len(state.Bootstrap.Steps) {
		state.Bootstrap.Steps[state.Bootstrap.CurrentStep].Status = stepInProgress
	}

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return nil, fmt.Errorf("bootstrap complete plan save: %w", err)
	}

	// Write incremental service metas with planned status.
	e.writeServiceMetas(state, MetaStatusPlanned)

	return state.Bootstrap.BuildResponse(state.SessionID, state.Intent, state.Iteration), nil
}

// BootstrapSkip skips the current step and returns the next.
func (e *Engine) BootstrapSkip(stepName, reason string) (*BootstrapResponse, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("bootstrap skip: %w", err)
	}
	if state.Bootstrap == nil || !state.Bootstrap.Active {
		return nil, fmt.Errorf("bootstrap skip: bootstrap not active")
	}

	if err := state.Bootstrap.SkipStep(stepName, reason); err != nil {
		return nil, fmt.Errorf("bootstrap skip: %w", err)
	}

	if state.Bootstrap.CurrentStep < len(state.Bootstrap.Steps) {
		state.Bootstrap.Steps[state.Bootstrap.CurrentStep].Status = stepInProgress
	}

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return nil, fmt.Errorf("bootstrap skip save: %w", err)
	}
	return state.Bootstrap.BuildResponse(state.SessionID, state.Intent, state.Iteration), nil
}

// StoreDiscoveredEnvVars saves discovered env var names for a service hostname.
func (e *Engine) StoreDiscoveredEnvVars(hostname string, vars []string) error {
	state, err := e.loadState()
	if err != nil {
		return fmt.Errorf("store discovered env vars: %w", err)
	}
	if state.Bootstrap == nil {
		return fmt.Errorf("store discovered env vars: no bootstrap state")
	}
	if state.Bootstrap.DiscoveredEnvVars == nil {
		state.Bootstrap.DiscoveredEnvVars = make(map[string][]string)
	}
	state.Bootstrap.DiscoveredEnvVars[hostname] = vars

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return saveSessionState(e.stateDir, e.sessionID, state)
}

// BootstrapStatus returns the current bootstrap progress.
func (e *Engine) BootstrapStatus() (*BootstrapResponse, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("bootstrap status: %w", err)
	}
	if state.Bootstrap == nil {
		return nil, fmt.Errorf("bootstrap status: no bootstrap state")
	}
	return state.Bootstrap.BuildResponse(state.SessionID, state.Intent, state.Iteration), nil
}

// Resume takes over an abandoned session (dead PID) by updating PID to current process.
func (e *Engine) Resume(sessionID string) (*WorkflowState, error) {
	state, err := LoadSessionByID(e.stateDir, sessionID)
	if err != nil {
		return nil, fmt.Errorf("resume: %w", err)
	}
	if isProcessAlive(state.PID) {
		return nil, fmt.Errorf("resume: session %s still active (PID %d)", sessionID, state.PID)
	}

	state.PID = os.Getpid()
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveStateAndUpdateRegistry(e.stateDir, sessionID, state, state.Phase); err != nil {
		return nil, fmt.Errorf("resume: %w", err)
	}
	e.sessionID = sessionID
	return state, nil
}

// loadState loads state for the current session.
func (e *Engine) loadState() (*WorkflowState, error) {
	if e.sessionID == "" {
		return nil, fmt.Errorf("no active session")
	}
	return LoadSessionByID(e.stateDir, e.sessionID)
}
