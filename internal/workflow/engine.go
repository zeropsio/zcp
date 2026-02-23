package workflow

import (
	"context"
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
func (e *Engine) Start(projectID string, mode Mode, intent string) (*WorkflowState, error) {
	return InitSession(e.stateDir, projectID, mode, intent)
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

// GetState returns the current workflow state.
func (e *Engine) GetState() (*WorkflowState, error) {
	return LoadSession(e.stateDir)
}

// BootstrapStart creates a new session with bootstrap state and returns the first step.
func (e *Engine) BootstrapStart(projectID string, mode Mode, intent string) (*BootstrapResponse, error) {
	state, err := InitSession(e.stateDir, projectID, mode, intent)
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

// autoCompleteBootstrap records evidence and transitions through all phases to DONE.
func (e *Engine) autoCompleteBootstrap(state *WorkflowState) error {
	now := time.Now().UTC().Format(time.RFC3339)

	// Collect attestations by category for evidence.
	attestations := make(map[string]string)
	for _, step := range state.Bootstrap.Steps {
		if step.Attestation != "" {
			attestations[step.Name] = step.Attestation
		}
	}

	// Map steps to evidence types.
	evidenceMap := map[string][]string{
		"recipe_review":   {"detect", "plan", "load-knowledge"},
		"discovery":       {"discover-envs"},
		"dev_verify":      {"deploy", "verify"},
		"deploy_evidence": {"deploy"},
		"stage_verify":    {"verify", "report"},
	}

	for evType, steps := range evidenceMap {
		var parts []string
		for _, s := range steps {
			if a, ok := attestations[s]; ok {
				parts = append(parts, s+": "+a)
			}
		}
		attestation := "auto-recorded from bootstrap steps"
		if len(parts) > 0 {
			attestation = strings.Join(parts, "; ")
		}
		ev := &Evidence{
			SessionID:        state.SessionID,
			Timestamp:        now,
			VerificationType: "attestation",
			Attestation:      attestation,
			Type:             evType,
			Passed:           1,
		}
		if err := SaveEvidence(e.evidenceDir, state.SessionID, ev); err != nil {
			return fmt.Errorf("auto-evidence %s: %w", evType, err)
		}
	}

	// Transition through all phases, checking gates at each step.
	seq := PhaseSequence(state.Mode)
	for i := 1; i < len(seq); i++ {
		result, err := CheckGate(seq[i-1], seq[i], state.Mode, e.evidenceDir, state.SessionID)
		if err != nil {
			return fmt.Errorf("auto-complete gate %s→%s: %w", seq[i-1], seq[i], err)
		}
		if !result.Passed {
			return fmt.Errorf("auto-complete blocked at gate %s: missing=%v failures=%v",
				result.Gate, result.Missing, result.Failures)
		}
		state.History = append(state.History, PhaseTransition{
			From: seq[i-1],
			To:   seq[i],
			At:   now,
		})
	}
	if len(seq) > 0 {
		state.Phase = seq[len(seq)-1]
	}

	return nil
}

// runtimeServiceTypes lists service type prefixes that are considered runtime (not managed).
var managedServicePrefixes = []string{
	"postgresql", "mariadb", "mysql", "mongodb", "valkey", "redis",
	"keydb", "elasticsearch", "meilisearch", "rabbitmq", "kafka",
	"objectstorage", "sharedstorage",
}

// DetectProjectState determines the project state based on service inventory.
func DetectProjectState(ctx context.Context, client platform.Client, projectID string) (ProjectState, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return "", fmt.Errorf("detect project state: %w", err)
	}

	// Filter to runtime services only.
	var runtimeServices []platform.ServiceStack
	for _, svc := range services {
		if !isManagedService(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
			runtimeServices = append(runtimeServices, svc)
		}
	}

	if len(runtimeServices) == 0 {
		return StateFresh, nil
	}

	// Check for dev/stage naming pattern.
	if hasDevStagePattern(runtimeServices) {
		return StateConformant, nil
	}

	return StateNonConformant, nil
}

// isManagedService checks if a service type is a managed (non-runtime) service.
func isManagedService(serviceType string) bool {
	lower := strings.ToLower(serviceType)
	for _, prefix := range managedServicePrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// hasDevStagePattern checks if any service names follow the dev/stage naming convention.
func hasDevStagePattern(services []platform.ServiceStack) bool {
	names := make(map[string]bool, len(services))
	for _, svc := range services {
		names[svc.Name] = true
	}

	suffixes := []struct{ dev, stage string }{
		{"dev", "stage"},
	}

	for name := range names {
		for _, sf := range suffixes {
			if base, ok := strings.CutSuffix(name, sf.dev); ok {
				if names[base+sf.stage] {
					return true
				}
			}
		}
	}
	return false
}
