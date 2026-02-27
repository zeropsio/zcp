package workflow

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// Engine orchestrates the workflow lifecycle.
type Engine struct {
	stateDir    string
	evidenceDir string
}

// NewEngine creates a new workflow engine rooted at baseDir.
// State: baseDir/zcp_state.json, Evidence: baseDir/evidence/
func NewEngine(baseDir string) *Engine {
	return &Engine{
		stateDir:    baseDir,
		evidenceDir: filepath.Join(baseDir, "evidence"),
	}
}

// Start creates a new workflow session.
func (e *Engine) Start(projectID, workflowName string, mode Mode, intent string) (*WorkflowState, error) {
	return InitSession(e.stateDir, projectID, workflowName, mode, intent)
}

// Transition moves the workflow to the next phase, checking gates.
func (e *Engine) Transition(phase Phase) (*WorkflowState, error) {
	state, err := LoadSession(e.stateDir)
	if err != nil {
		return nil, fmt.Errorf("transition: %w", err)
	}

	if !IsValidTransition(state.Phase, phase, state.Mode) {
		return nil, fmt.Errorf("transition: invalid %s → %s in mode %s", state.Phase, phase, state.Mode)
	}

	// Check gate.
	result, err := CheckGate(state.Phase, phase, state.Mode, e.evidenceDir, state.SessionID)
	if err != nil {
		return nil, fmt.Errorf("transition gate check: %w", err)
	}
	if !result.Passed {
		return nil, fmt.Errorf("transition: gate %s failed, missing evidence: %v", result.Gate, result.Missing)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	state.History = append(state.History, PhaseTransition{
		From: state.Phase,
		To:   phase,
		At:   now,
	})
	state.Phase = phase
	state.UpdatedAt = now

	if err := saveState(e.stateDir, state); err != nil {
		return nil, err
	}
	return state, nil
}

// RecordEvidence saves evidence for the current session.
func (e *Engine) RecordEvidence(ev *Evidence) error {
	state, err := LoadSession(e.stateDir)
	if err != nil {
		return fmt.Errorf("record evidence: %w", err)
	}
	ev.SessionID = state.SessionID
	return SaveEvidence(e.evidenceDir, state.SessionID, ev)
}

// Reset clears the current session.
func (e *Engine) Reset() error {
	return ResetSession(e.stateDir)
}

// Iterate archives evidence and resets to DEVELOP.
func (e *Engine) Iterate() (*WorkflowState, error) {
	return IterateSession(e.stateDir, e.evidenceDir)
}

// HasActiveSession returns true if there is a loadable workflow session.
func (e *Engine) HasActiveSession() bool {
	_, err := LoadSession(e.stateDir)
	return err == nil
}

// GetState returns the current workflow state.
func (e *Engine) GetState() (*WorkflowState, error) {
	return LoadSession(e.stateDir)
}

// BootstrapStart creates a new session with bootstrap state and returns the first step.
func (e *Engine) BootstrapStart(projectID string, mode Mode, intent string) (*BootstrapResponse, error) {
	state, err := InitSession(e.stateDir, projectID, "bootstrap", mode, intent)
	if err != nil {
		return nil, fmt.Errorf("bootstrap start: %w", err)
	}

	bs := NewBootstrapState()
	bs.Steps[0].Status = stepInProgress
	state.Bootstrap = bs

	if err := saveState(e.stateDir, state); err != nil {
		return nil, fmt.Errorf("bootstrap start save: %w", err)
	}
	return bs.BuildResponse(state.SessionID, mode, intent), nil
}

// BootstrapComplete completes the current step and returns the next.
func (e *Engine) BootstrapComplete(stepName, attestation string) (*BootstrapResponse, error) {
	state, err := LoadSession(e.stateDir)
	if err != nil {
		return nil, fmt.Errorf("bootstrap complete: %w", err)
	}
	if state.Bootstrap == nil || !state.Bootstrap.Active {
		return nil, fmt.Errorf("bootstrap complete: bootstrap not active")
	}

	if err := state.Bootstrap.CompleteStep(stepName, attestation); err != nil {
		return nil, fmt.Errorf("bootstrap complete: %w", err)
	}

	// Mark next step as in_progress.
	if state.Bootstrap.CurrentStep < len(state.Bootstrap.Steps) {
		state.Bootstrap.Steps[state.Bootstrap.CurrentStep].Status = stepInProgress
	}

	// Auto-complete: when all steps done, record evidence and transition to DONE.
	if !state.Bootstrap.Active {
		if err := e.autoCompleteBootstrap(state); err != nil {
			return nil, fmt.Errorf("bootstrap auto-complete: %w", err)
		}
	}

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveState(e.stateDir, state); err != nil {
		return nil, fmt.Errorf("bootstrap complete save: %w", err)
	}
	return state.Bootstrap.BuildResponse(state.SessionID, state.Mode, state.Intent), nil
}

// BootstrapCompletePlan validates a structured plan, completes the "plan" step, and stores the plan.
// liveTypes may be nil — type checking is skipped when unavailable.
func (e *Engine) BootstrapCompletePlan(services []PlannedService, liveTypes []platform.ServiceStackType) (*BootstrapResponse, error) {
	state, err := LoadSession(e.stateDir)
	if err != nil {
		return nil, fmt.Errorf("bootstrap complete plan: %w", err)
	}
	if state.Bootstrap == nil || !state.Bootstrap.Active {
		return nil, fmt.Errorf("bootstrap complete plan: bootstrap not active")
	}
	if state.Bootstrap.CurrentStepName() != "plan" {
		return nil, fmt.Errorf("bootstrap complete plan: current step is %q, not \"plan\"", state.Bootstrap.CurrentStepName())
	}

	defaulted, err := ValidateServicePlan(services, liveTypes)
	if err != nil {
		return nil, fmt.Errorf("bootstrap complete plan: %w", err)
	}

	// Build attestation from validated plan.
	defaultedSet := make(map[string]bool, len(defaulted))
	for _, h := range defaulted {
		defaultedSet[h] = true
	}
	var parts []string
	for _, svc := range services {
		entry := svc.Hostname + " (" + svc.Type
		if svc.Mode != "" {
			entry += ", " + svc.Mode
			if defaultedSet[svc.Hostname] {
				entry += " [defaulted]"
			}
		}
		entry += ")"
		parts = append(parts, entry)
	}
	attestation := "Planned services: " + strings.Join(parts, ", ")

	if err := state.Bootstrap.CompleteStep("plan", attestation); err != nil {
		return nil, fmt.Errorf("bootstrap complete plan: %w", err)
	}

	// Store plan in bootstrap state.
	state.Bootstrap.Plan = &ServicePlan{
		Services:  services,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Mark next step as in_progress.
	if state.Bootstrap.CurrentStep < len(state.Bootstrap.Steps) {
		state.Bootstrap.Steps[state.Bootstrap.CurrentStep].Status = stepInProgress
	}

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveState(e.stateDir, state); err != nil {
		return nil, fmt.Errorf("bootstrap complete plan save: %w", err)
	}
	return state.Bootstrap.BuildResponse(state.SessionID, state.Mode, state.Intent), nil
}

// BootstrapSkip skips the current step and returns the next.
func (e *Engine) BootstrapSkip(stepName, reason string) (*BootstrapResponse, error) {
	state, err := LoadSession(e.stateDir)
	if err != nil {
		return nil, fmt.Errorf("bootstrap skip: %w", err)
	}
	if state.Bootstrap == nil || !state.Bootstrap.Active {
		return nil, fmt.Errorf("bootstrap skip: bootstrap not active")
	}

	if err := state.Bootstrap.SkipStep(stepName, reason); err != nil {
		return nil, fmt.Errorf("bootstrap skip: %w", err)
	}

	// Mark next step as in_progress.
	if state.Bootstrap.CurrentStep < len(state.Bootstrap.Steps) {
		state.Bootstrap.Steps[state.Bootstrap.CurrentStep].Status = stepInProgress
	}

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveState(e.stateDir, state); err != nil {
		return nil, fmt.Errorf("bootstrap skip save: %w", err)
	}
	return state.Bootstrap.BuildResponse(state.SessionID, state.Mode, state.Intent), nil
}

// BootstrapStatus returns the current bootstrap progress (read-only).
func (e *Engine) BootstrapStatus() (*BootstrapResponse, error) {
	state, err := LoadSession(e.stateDir)
	if err != nil {
		return nil, fmt.Errorf("bootstrap status: %w", err)
	}
	if state.Bootstrap == nil {
		return nil, fmt.Errorf("bootstrap status: no bootstrap state")
	}
	return state.Bootstrap.BuildResponse(state.SessionID, state.Mode, state.Intent), nil
}
