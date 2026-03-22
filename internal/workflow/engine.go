package workflow

import (
	"context"
	"fmt"
	"maps"
	"os"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
)

// Engine orchestrates the workflow lifecycle.
type Engine struct {
	stateDir       string
	sessionID      string
	completedState *WorkflowState // holds final state after session file is cleaned up
	environment    Environment
	knowledge      knowledge.Provider
}

// NewEngine creates a new workflow engine rooted at baseDir.
func NewEngine(baseDir string, env Environment, kp knowledge.Provider) *Engine {
	return &Engine{
		stateDir:    baseDir,
		environment: env,
		knowledge:   kp,
	}
}

// Environment returns the detected execution environment.
func (e *Engine) Environment() Environment {
	return e.environment
}

// Start creates a new workflow session with auto-reset, exclusivity, and registry refresh.
func (e *Engine) Start(projectID, workflowName, intent string) (*WorkflowState, error) {
	if e.sessionID != "" {
		if existing, err := LoadSessionByID(e.stateDir, e.sessionID); err == nil {
			bootstrapDone := existing.Bootstrap != nil && !existing.Bootstrap.Active
			deployDone := existing.Deploy != nil && !existing.Deploy.Active
			cicdDone := existing.CICD != nil && !existing.CICD.Active
			if bootstrapDone || deployDone || cicdDone {
				if err := ResetSessionByID(e.stateDir, e.sessionID); err != nil {
					return nil, fmt.Errorf("start auto-reset: %w", err)
				}
				e.sessionID = ""
			} else {
				return nil, fmt.Errorf("start: active session %s, reset first", e.sessionID)
			}
		}
	}

	state, err := InitSessionAtomic(e.stateDir, projectID, workflowName, intent)
	if err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}
	e.sessionID = state.SessionID
	e.completedState = nil
	return state, nil
}

// Reset clears the current session.
func (e *Engine) Reset() error {
	e.completedState = nil
	if e.sessionID == "" {
		return nil
	}
	err := ResetSessionByID(e.stateDir, e.sessionID)
	e.sessionID = ""
	if err != nil {
		return fmt.Errorf("reset session: %w", err)
	}
	return nil
}

// Iterate resets bootstrap steps and increments the counter.
func (e *Engine) Iterate() (*WorkflowState, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("iterate: %w", err)
	}
	if state.Iteration >= maxIterations() {
		return nil, fmt.Errorf("iterate: max iterations reached (%d), reset session to continue", maxIterations())
	}
	return IterateSession(e.stateDir, e.sessionID)
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

// StateDir returns the engine's state directory path.
func (e *Engine) StateDir() string {
	return e.stateDir
}

// ListActiveSessions returns all active sessions from the registry.
func (e *Engine) ListActiveSessions() ([]SessionEntry, error) {
	return ListSessions(e.stateDir)
}

// BootstrapStart creates a new session with bootstrap state and returns the first step.
func (e *Engine) BootstrapStart(projectID, intent string) (*BootstrapResponse, error) {
	state, err := e.Start(projectID, WorkflowBootstrap, intent)
	if err != nil {
		return nil, fmt.Errorf("bootstrap start: %w", err)
	}

	bs := NewBootstrapState()
	bs.Steps[0].Status = stepInProgress
	state.Bootstrap = bs

	if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return nil, fmt.Errorf("bootstrap start save: %w", err)
	}
	return bs.BuildResponse(state.SessionID, intent, state.Iteration, e.environment, e.knowledge), nil
}

// BootstrapComplete completes the current step and advances to the next.
// Step advancement depends on checker results, not attestation content.
// If checker is nil or passes, step advances. If checker fails, step stays
// and the agent receives the failure details to fix and retry.
func (e *Engine) BootstrapComplete(ctx context.Context, stepName string, attestation string, checker StepChecker) (*BootstrapResponse, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("bootstrap complete: %w", err)
	}
	if state.Bootstrap == nil || !state.Bootstrap.Active {
		return nil, fmt.Errorf("bootstrap complete: bootstrap not active")
	}

	// Defense-in-depth: non-discover steps require a plan from the discover step.
	if stepName != StepDiscover && state.Bootstrap.Plan == nil {
		return nil, fmt.Errorf("bootstrap complete: step %q requires plan from discover step", stepName)
	}

	var checkResult *StepCheckResult
	if checker != nil {
		result, checkErr := checker(ctx, state.Bootstrap.Plan, state.Bootstrap)
		if checkErr != nil {
			return nil, fmt.Errorf("step check: %w", checkErr)
		}
		if result != nil && !result.Passed {
			resp := state.Bootstrap.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment, e.knowledge)
			resp.CheckResult = result
			resp.Message = fmt.Sprintf("Step %q: %s — fix issues and retry", stepName, result.Summary)
			return resp, nil
		}
		checkResult = result
	}

	if err := state.Bootstrap.CompleteStep(stepName, attestation); err != nil {
		return nil, fmt.Errorf("bootstrap complete: %w", err)
	}

	// Write partial metas after provision (no BootstrappedAt = incomplete).
	if stepName == StepProvision {
		e.writeProvisionMetas(state)
	}

	sessionID := e.sessionID // capture before outputs may reference it

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if !state.Bootstrap.Active {
		e.writeBootstrapOutputs(state)
		if err := ResetSessionByID(e.stateDir, state.SessionID); err != nil {
			fmt.Fprintf(os.Stderr, "zcp: cleanup completed session: %v\n", err)
		}
		e.completedState = state
		e.sessionID = ""
	} else if err := saveSessionState(e.stateDir, sessionID, state); err != nil {
		return nil, fmt.Errorf("bootstrap complete save: %w", err)
	}
	resp := state.Bootstrap.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment, e.knowledge)
	resp.CheckResult = checkResult
	return resp, nil
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

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return nil, fmt.Errorf("bootstrap complete plan save: %w", err)
	}

	return state.Bootstrap.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment, e.knowledge), nil
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

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveSessionState(e.stateDir, e.sessionID, state); err != nil {
		return nil, fmt.Errorf("bootstrap skip save: %w", err)
	}
	return state.Bootstrap.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment, e.knowledge), nil
}

// BootstrapStoreStrategies saves per-hostname deploy strategies to the active bootstrap state.
func (e *Engine) BootstrapStoreStrategies(strategies map[string]string) error {
	state, err := e.loadState()
	if err != nil {
		return fmt.Errorf("store strategies: %w", err)
	}
	if state.Bootstrap == nil {
		return fmt.Errorf("store strategies: no bootstrap state")
	}
	if state.Bootstrap.Strategies == nil {
		state.Bootstrap.Strategies = make(map[string]string, len(strategies))
	}
	maps.Copy(state.Bootstrap.Strategies, strategies)
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return saveSessionState(e.stateDir, e.sessionID, state)
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

// BootstrapStatus returns the current bootstrap progress with full guidance for context recovery.
func (e *Engine) BootstrapStatus() (*BootstrapResponse, error) {
	state, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("bootstrap status: %w", err)
	}
	if state.Bootstrap == nil {
		return nil, fmt.Errorf("bootstrap status: no bootstrap state")
	}
	return state.Bootstrap.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment, e.knowledge), nil
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

	if err := saveSessionState(e.stateDir, sessionID, state); err != nil {
		return nil, fmt.Errorf("resume: %w", err)
	}
	e.sessionID = sessionID
	return state, nil
}

// --- Deploy workflow engine methods ---

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

// --- CI/CD workflow engine methods ---

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

// loadState loads state for the current session.
func (e *Engine) loadState() (*WorkflowState, error) {
	if e.sessionID == "" {
		if e.completedState != nil {
			return e.completedState, nil
		}
		return nil, fmt.Errorf("no active session")
	}
	return LoadSessionByID(e.stateDir, e.sessionID)
}
