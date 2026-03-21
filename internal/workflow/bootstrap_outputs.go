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

	// Write service meta for each target and its dependencies.
	for _, target := range plan.Targets {
		depHostnames := make([]string, 0, len(target.Dependencies))
		for _, dep := range target.Dependencies {
			depHostnames = append(depHostnames, dep.Hostname)

			// Skip meta overwrite for pre-existing or shared dependencies.
			if dep.Resolution == ResolutionExists || dep.Resolution == ResolutionShared {
				continue
			}

			depMeta := &ServiceMeta{
				Hostname:         dep.Hostname,
				Mode:             dep.Mode,
				BootstrapSession: state.SessionID,
				BootstrappedAt:   now,
			}
			if err := WriteServiceMeta(e.stateDir, depMeta); err != nil {
				fmt.Fprintf(os.Stderr, "zcp: write service meta %s: %v\n", dep.Hostname, err)
			}
		}

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
			Dependencies:     depHostnames,
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
// EXISTS/SHARED dependencies are skipped to avoid overwriting pre-existing metas.
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

		for _, dep := range target.Dependencies {
			if dep.Resolution == ResolutionExists || dep.Resolution == ResolutionShared {
				continue
			}
			depMeta := &ServiceMeta{
				Hostname:         dep.Hostname,
				Mode:             dep.Mode,
				BootstrapSession: state.SessionID,
			}
			if err := WriteServiceMeta(e.stateDir, depMeta); err != nil {
				fmt.Fprintf(os.Stderr, "zcp: write service meta %s: %v\n", dep.Hostname, err)
			}
		}
	}
}
