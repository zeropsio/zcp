package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// writeBootstrapOutputs writes service meta files and appends a reflog entry.
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
				Type:             dep.Type,
				Mode:             dep.Mode,
				Status:           MetaStatusBootstrapped,
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
			Type:             target.Runtime.Type,
			Mode:             mode,
			Status:           MetaStatusBootstrapped,
			StageHostname:    target.Runtime.StageHostname(),
			Dependencies:     depHostnames,
			BootstrapSession: state.SessionID,
			BootstrappedAt:   now,
		}
		if strategy != "" {
			meta.Decisions = map[string]string{DecisionDeployStrategy: strategy}
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

// writeServiceMetas writes ServiceMeta files for all plan targets with the given status.
// EXISTS/SHARED dependencies are skipped to avoid overwriting pre-existing metas.
// Errors are logged but do not fail the operation.
func (e *Engine) writeServiceMetas(state *WorkflowState, status string) {
	if state.Bootstrap == nil || state.Bootstrap.Plan == nil {
		return
	}

	for _, target := range state.Bootstrap.Plan.Targets {
		meta := &ServiceMeta{
			Hostname:         target.Runtime.DevHostname,
			Type:             target.Runtime.Type,
			Mode:             target.Runtime.EffectiveMode(),
			Status:           status,
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
				Type:             dep.Type,
				Mode:             dep.Mode,
				Status:           status,
				BootstrapSession: state.SessionID,
			}
			if err := WriteServiceMeta(e.stateDir, depMeta); err != nil {
				fmt.Fprintf(os.Stderr, "zcp: write service meta %s: %v\n", dep.Hostname, err)
			}
		}
	}
}
