package workflow

import (
	"context"
	"fmt"
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
// Auto-recovers the active session from disk if the previous process died
// (MCP server restart). Updates the session PID to the current process.
func NewEngine(baseDir string, env Environment, kp knowledge.Provider) *Engine {
	e := &Engine{
		stateDir:    baseDir,
		environment: env,
		knowledge:   kp,
	}
	if savedID := loadActiveSession(baseDir); savedID != "" {
		if state, err := LoadSessionByID(baseDir, savedID); err == nil {
			// Only recover sessions from dead processes. If PID matches ours,
			// another Engine in this process owns it (e.g. tests) — don't steal.
			if state.PID != os.Getpid() && !isProcessAlive(state.PID) {
				state.PID = os.Getpid()
				state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				if err := saveSessionState(baseDir, savedID, state); err == nil {
					e.sessionID = savedID
					_ = updateRegistryPID(baseDir, savedID, os.Getpid())
					fmt.Fprintf(os.Stderr, "zcp: auto-recovered session %s from previous process\n", savedID)
				}
			}
		} else {
			// Session file gone (pruned or completed) — clean stale reference.
			clearActiveSession(baseDir)
		}
	}
	return e
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
			recipeDone := existing.Recipe != nil && !existing.Recipe.Active
			if bootstrapDone || deployDone || recipeDone {
				if err := ResetSessionByID(e.stateDir, e.sessionID); err != nil {
					return nil, fmt.Errorf("start auto-reset: %w", err)
				}
				e.sessionID = ""
				clearActiveSession(e.stateDir)
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
	persistActiveSession(e.stateDir, state.SessionID)
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
	clearActiveSession(e.stateDir)
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

		// Fast path: pure adoption plans (all isExisting + all EXISTS deps)
		// skip remaining steps — generate/deploy/close are no-ops for adopted services.
		if state.Bootstrap.Plan != nil && state.Bootstrap.Plan.IsAllExisting() {
			for _, skip := range []string{StepGenerate, StepDeploy, StepClose} {
				if err := state.Bootstrap.SkipStep(skip, "all targets adopted"); err != nil {
					return nil, fmt.Errorf("adoption fast path: %w", err)
				}
			}
		}
	}

	sessionID := e.sessionID // capture before outputs may reference it

	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	var cleanupErr error
	if !state.Bootstrap.Active {
		e.writeBootstrapOutputs(state)
		cleanupErr = ResetSessionByID(e.stateDir, state.SessionID)
		if cleanupErr != nil {
			fmt.Fprintf(os.Stderr, "zcp: cleanup completed session: %v\n", cleanupErr)
		}
		e.completedState = state
		e.sessionID = ""
		clearActiveSession(e.stateDir)
	} else if err := saveSessionState(e.stateDir, sessionID, state); err != nil {
		return nil, fmt.Errorf("bootstrap complete save: %w", err)
	}
	resp := state.Bootstrap.BuildResponse(state.SessionID, state.Intent, state.Iteration, e.environment, e.knowledge)
	resp.CheckResult = checkResult
	if cleanupErr != nil {
		resp.Message += "\n\nWarning: session cleanup failed: " + cleanupErr.Error()
	}
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

	// Per-hostname lock: reject if any target hostname has an incomplete meta
	// from a DIFFERENT session that is still alive. Orphaned metas (dead session)
	// are safe to overwrite — the new bootstrap takes ownership.
	if err := e.checkHostnameLocks(targets); err != nil {
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
// Idempotent: if this engine already owns the session (e.g. auto-recovered at startup),
// returns the current state without error.
func (e *Engine) Resume(sessionID string) (*WorkflowState, error) {
	if e.sessionID == sessionID {
		return LoadSessionByID(e.stateDir, sessionID)
	}
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
	persistActiveSession(e.stateDir, sessionID)
	return state, nil
}

// checkHostnameLocks verifies that no target hostname is locked by another active session.
// A hostname is locked when it has an incomplete ServiceMeta from a different session
// whose process is still alive. Orphaned metas (dead/missing session) are unlocked.
func (e *Engine) checkHostnameLocks(targets []BootstrapTarget) error {
	sessions, _ := ListSessions(e.stateDir)
	sessionPIDs := make(map[string]int, len(sessions))
	for _, s := range sessions {
		sessionPIDs[s.SessionID] = s.PID
	}

	for _, target := range targets {
		hostnames := []string{target.Runtime.DevHostname}
		if stage := target.Runtime.StageHostname(); stage != "" {
			hostnames = append(hostnames, stage)
		}
		for _, hostname := range hostnames {
			meta, err := ReadServiceMeta(e.stateDir, hostname)
			if err != nil || meta == nil {
				continue // no meta = no lock
			}
			if meta.IsComplete() {
				continue // completed = not locked
			}
			if meta.BootstrapSession == e.sessionID {
				continue // our own session = not locked
			}
			// Incomplete meta from another session — check if alive.
			pid, inRegistry := sessionPIDs[meta.BootstrapSession]
			if inRegistry && isProcessAlive(pid) {
				return fmt.Errorf("service %q is being bootstrapped by session %s (PID %d) — finish or reset that session first",
					hostname, meta.BootstrapSession, pid)
			}
			// Dead/missing session = orphaned meta, safe to overwrite.
		}
	}
	return nil
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
