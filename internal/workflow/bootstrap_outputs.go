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
		devHostname := target.Runtime.DevHostname
		mode := target.Runtime.EffectiveMode()

		// Strategy lookup ALWAYS by DevHostname — agent stores strategies under DevHostname.
		strategy := state.Bootstrap.Strategies[devHostname]

		// Meta hostname: local mode writes appstage (dev doesn't exist), container writes appdev.
		metaHostname := devHostname
		stageHostname := target.Runtime.StageHostname()
		if e.environment == EnvLocal && stageHostname != "" {
			metaHostname = stageHostname
			stageHostname = ""
		}

		meta := &ServiceMeta{
			Hostname:         metaHostname,
			Mode:             mode,
			StageHostname:    stageHostname,
			DeployStrategy:   strategy,
			Environment:      string(e.environment),
			BootstrapSession: state.SessionID,
			BootstrappedAt:   now,
		}
		if err := WriteServiceMeta(e.stateDir, meta); err != nil {
			fmt.Fprintf(os.Stderr, "zcp: write service meta %s: %v\n", metaHostname, err)
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
		metaHostname := target.Runtime.DevHostname
		stageHostname := target.Runtime.StageHostname()
		if e.environment == EnvLocal && stageHostname != "" {
			metaHostname = stageHostname
			stageHostname = ""
		}

		meta := &ServiceMeta{
			Hostname:         metaHostname,
			Mode:             target.Runtime.EffectiveMode(),
			StageHostname:    stageHostname,
			Environment:      string(e.environment),
			BootstrapSession: state.SessionID,
		}
		if err := WriteServiceMeta(e.stateDir, meta); err != nil {
			fmt.Fprintf(os.Stderr, "zcp: write service meta %s: %v\n", metaHostname, err)
		}
	}
}
