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

// Show returns a textual summary of the project state and workflow recommendation.
func (e *Engine) Show(ctx context.Context, client platform.Client, projectID string) (string, error) {
	projState, err := DetectProjectState(ctx, client, projectID)
	if err != nil {
		return "", fmt.Errorf("show: %w", err)
	}

	// Check for existing session.
	existingState, loadErr := LoadSession(e.stateDir)

	var b strings.Builder
	fmt.Fprintf(&b, "Project State: %s\n", projState)

	if loadErr == nil {
		fmt.Fprintf(&b, "Active Session: %s (mode=%s, phase=%s, iteration=%d)\n",
			existingState.SessionID, existingState.Mode, existingState.Phase, existingState.Iteration)
		fmt.Fprintf(&b, "Intent: %s\n", existingState.Intent)
	} else {
		b.WriteString("No active session.\n")
	}

	b.WriteString("\nRecommended mode: ")
	switch projState {
	case StateFresh:
		b.WriteString("full (new project setup)\n")
	case StateConformant:
		b.WriteString("dev_only or hotfix (existing conformant project)\n")
	case StateNonConformant:
		b.WriteString("full (project needs restructuring)\n")
	}

	return b.String(), nil
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
		{"-dev", "-stage"},
		{"-dev", "-staging"},
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
