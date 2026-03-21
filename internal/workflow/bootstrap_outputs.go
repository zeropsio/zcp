package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// writeBootstrapOutputs writes final service meta files and appends a reflog entry.
// Sets BootstrappedAt to mark services as fully bootstrapped.
// Both are best-effort — errors are logged to stderr but don't fail bootstrap completion.
func (e *Engine) writeBootstrapOutputs(state *WorkflowState) {
	if state.Bootstrap == nil || state.Bootstrap.Plan == nil {
		return
	}

	plan := state.Bootstrap.Plan
	now := time.Now().UTC().Format("2006-01-02")

	// Write service meta for each runtime target (managed deps are API-authoritative).
	for _, target := range plan.Targets {
		hostname := target.Runtime.DevHostname
		mode := target.Runtime.EffectiveMode()

		// Resolve deploy strategy: explicit > auto-assign for non-standard > none.
		strategy := state.Bootstrap.Strategies[hostname]
		if strategy == "" && (mode == PlanModeDev || mode == PlanModeSimple) {
			strategy = StrategyPushDev
		}

		meta := &ServiceMeta{
			Hostname:         hostname,
			Mode:             mode,
			StageHostname:    target.Runtime.StageHostname(),
			DeployStrategy:   strategy,
			BootstrapSession: state.SessionID,
			BootstrappedAt:   now,
		}
		if err := WriteServiceMeta(e.stateDir, meta); err != nil {
			fmt.Fprintf(os.Stderr, "zcp: write service meta %s: %v\n", hostname, err)
		}
	}

	// Derive project root from stateDir (expected: {projectRoot}/.zcp/state/).
	projectRoot := filepath.Dir(filepath.Dir(e.stateDir))
	claudeMDPath := filepath.Join(projectRoot, "CLAUDE.md")

	if err := AppendReflogEntry(claudeMDPath, state.Intent, plan.Targets, state.SessionID, now); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: append reflog: %v\n", err)
	}
}

// writeProvisionMetas writes partial ServiceMeta files after the provision step.
// These metas have no BootstrappedAt (IsComplete() returns false), signaling
// that bootstrap started but hasn't finished. If bootstrap completes,
// writeBootstrapOutputs overwrites with full metas.
// Only runtime services get metas — managed deps are API-authoritative.
func (e *Engine) writeProvisionMetas(state *WorkflowState) {
	if state.Bootstrap == nil || state.Bootstrap.Plan == nil {
		return
	}

	for _, target := range state.Bootstrap.Plan.Targets {
		meta := &ServiceMeta{
			Hostname:         target.Runtime.DevHostname,
			Mode:             target.Runtime.EffectiveMode(),
			StageHostname:    target.Runtime.StageHostname(),
			BootstrapSession: state.SessionID,
		}
		if err := WriteServiceMeta(e.stateDir, meta); err != nil {
			fmt.Fprintf(os.Stderr, "zcp: write service meta %s: %v\n", target.Runtime.DevHostname, err)
		}
	}
}
