package workflow

import (
	"fmt"
	"os"
	"time"
)

// CICDStart creates a new CI/CD setup session.
func (e *Engine) CICDStart(projectID, intent string, hostnames []string) (*CICDResponse, error) {
	state, err := e.Start(projectID, WorkflowCICD, intent)
	if err != nil {
		return nil, fmt.Errorf("cicd start: %w", err)
	}

	cs := NewCICDState(hostnames)
	cs.Steps[0].Status = stepInProgress
	state.CICD = cs

	if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return nil, fmt.Errorf("cicd start save: %w", err)
	}
	return cs.BuildResponse(state.SessionID, intent, e.environment, e.knowledge), nil
}

// CICDComplete completes the current CI/CD step.
func (e *Engine) CICDComplete(step, attestation, provider string) (*CICDResponse, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("cicd complete: %w", err)
	}
	if state.CICD == nil || !state.CICD.Active {
		return nil, fmt.Errorf("cicd complete: not active")
	}

	// Set provider on choose step completion.
	if step == CICDStepChoose && provider != "" {
		if err := state.CICD.SetProvider(provider); err != nil {
			return nil, fmt.Errorf("cicd complete: %w", err)
		}
	}

	if err := state.CICD.CompleteStep(step, attestation); err != nil {
		return nil, fmt.Errorf("cicd complete: %w", err)
	}

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if !state.CICD.Active {
		if err := ResetSessionByID(e.stateDir, state.SessionID); err != nil {
			fmt.Fprintf(os.Stderr, "zcp: cleanup completed cicd session: %v\n", err)
		}
		e.completedState = state
		e.sessionID = ""
	} else if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return nil, fmt.Errorf("cicd complete save: %w", err)
	}
	return state.CICD.BuildResponse(state.SessionID, state.Intent, e.environment, e.knowledge), nil
}

// CICDStatus returns the current CI/CD progress with fresh guidance.
func (e *Engine) CICDStatus() (*CICDResponse, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("cicd status: %w", err)
	}
	if state.CICD == nil {
		return nil, fmt.Errorf("cicd status: no cicd state")
	}
	return state.CICD.BuildResponse(state.SessionID, state.Intent, e.environment, e.knowledge), nil
}
