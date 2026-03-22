package workflow

import (
	"context"
	"fmt"
	"os"
	"time"
)

// DeployStart creates a new deploy session with targets ordered by mode.
func (e *Engine) DeployStart(projectID, intent string, targets []DeployTarget, mode, strategy string) (*DeployResponse, error) {
	state, err := e.Start(projectID, WorkflowDeploy, intent)
	if err != nil {
		return nil, fmt.Errorf("deploy start: %w", err)
	}

	ds := NewDeployState(targets, mode, strategy)
	ds.Steps[0].Status = stepInProgress
	state.Deploy = ds

	if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return nil, fmt.Errorf("deploy start save: %w", err)
	}
	return ds.BuildResponse(state.SessionID, intent, state.Iteration, e.environment), nil
}

// DeployComplete completes the current deploy step.
// If checker is non-nil and fails, the step stays and the agent receives failure details.
func (e *Engine) DeployComplete(ctx context.Context, step, attestation string, checker DeployStepChecker) (*DeployResponse, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("deploy complete: %w", err)
	}
	if state.Deploy == nil || !state.Deploy.Active {
		return nil, fmt.Errorf("deploy complete: not active")
	}

	var checkResult *StepCheckResult
	if checker != nil {
		result, checkErr := checker(ctx, state.Deploy)
		if checkErr != nil {
			return nil, fmt.Errorf("deploy step check: %w", checkErr)
		}
		if result != nil && !result.Passed {
			resp := state.Deploy.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment)
			resp.CheckResult = result
			resp.Message = fmt.Sprintf("Step %q: %s — fix issues and retry", step, result.Summary)
			return resp, nil
		}
		checkResult = result
	}

	if err := state.Deploy.CompleteStep(step, attestation); err != nil {
		return nil, fmt.Errorf("deploy complete: %w", err)
	}

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if !state.Deploy.Active {
		if err := ResetSessionByID(e.stateDir, state.SessionID); err != nil {
			fmt.Fprintf(os.Stderr, "zcp: cleanup completed deploy session: %v\n", err)
		}
		e.completedState = state
		e.sessionID = ""
	} else if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return nil, fmt.Errorf("deploy complete save: %w", err)
	}
	resp := state.Deploy.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment)
	resp.CheckResult = checkResult
	return resp, nil
}

// DeploySkip skips the current deploy step.
func (e *Engine) DeploySkip(step, reason string) (*DeployResponse, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("deploy skip: %w", err)
	}
	if state.Deploy == nil || !state.Deploy.Active {
		return nil, fmt.Errorf("deploy skip: not active")
	}
	if err := state.Deploy.SkipStep(step, reason); err != nil {
		return nil, fmt.Errorf("deploy skip: %w", err)
	}

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return nil, fmt.Errorf("deploy skip save: %w", err)
	}
	return state.Deploy.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment), nil
}

// DeployStatus returns the current deploy progress with fresh guidance.
func (e *Engine) DeployStatus() (*DeployResponse, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("deploy status: %w", err)
	}
	if state.Deploy == nil {
		return nil, fmt.Errorf("deploy status: no deploy state")
	}
	return state.Deploy.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment), nil
}
