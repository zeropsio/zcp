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
//
// Mode-expansion path: when the plan upgrades an existing runtime's
// bootstrapMode (dev/simple → standard) with IsExisting=true, the existing
// ServiceMeta is merged rather than overwritten — BootstrappedAt,
// DeployStrategy, StrategyConfirmed, FirstDeployedAt are preserved so the
// user's prior choices (and deploy history) survive the mode upgrade.
// See §9.1 of spec-workflows.md.
func (e *Engine) writeBootstrapOutputs(state *WorkflowState) {
	if state.Bootstrap == nil || state.Bootstrap.Plan == nil {
		return
	}

	plan := state.Bootstrap.Plan
	now := time.Now().UTC().Format("2006-01-02")

	// Write service meta for each runtime target (managed deps are API-authoritative).
	for _, target := range plan.Targets {
		mode := target.Runtime.EffectiveMode()
		metaHostname := target.Runtime.DevHostname
		stageHostname := target.Runtime.StageHostname()

		// Adopted services (isExisting=true) get empty BootstrapSession
		// to signal adoption rather than fresh bootstrap.
		bootstrapSession := state.SessionID
		if target.Runtime.IsExisting {
			bootstrapSession = ""
		}

		meta := &ServiceMeta{
			Hostname:         metaHostname,
			Mode:             mode,
			StageHostname:    stageHostname,
			DeployStrategy:   "",
			BootstrapSession: bootstrapSession,
			BootstrappedAt:   now,
		}

		// Expansion merge: gate the disk read behind IsExisting so fresh
		// bootstraps don't pay a ReadServiceMeta for every target.
		if target.Runtime.IsExisting {
			if existing, _ := ReadServiceMeta(e.stateDir, metaHostname); existing != nil && existing.IsComplete() {
				mergeExistingMeta(meta, existing)
			}
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
//
// Expansion path: when an existing complete meta is detected for the
// target hostname, merge in its preserved fields so the intermediate
// (partial) write doesn't lose BootstrappedAt / DeployStrategy. Without
// this, a crash between provision and close would leave the service
// looking like a brand-new bootstrap instead of a mode-upgrade.
func (e *Engine) writeProvisionMetas(state *WorkflowState) {
	if state.Bootstrap == nil || state.Bootstrap.Plan == nil {
		return
	}

	for _, target := range state.Bootstrap.Plan.Targets {
		metaHostname := target.Runtime.DevHostname
		stageHostname := target.Runtime.StageHostname()

		// Adopted services (isExisting=true) get empty BootstrapSession.
		bootstrapSession := state.SessionID
		if target.Runtime.IsExisting {
			bootstrapSession = ""
		}

		meta := &ServiceMeta{
			Hostname:         metaHostname,
			Mode:             target.Runtime.EffectiveMode(),
			StageHostname:    stageHostname,
			BootstrapSession: bootstrapSession,
		}

		if target.Runtime.IsExisting {
			if existing, _ := ReadServiceMeta(e.stateDir, metaHostname); existing != nil && existing.IsComplete() {
				mergeExistingMeta(meta, existing)
			}
		}

		if err := WriteServiceMeta(e.stateDir, meta); err != nil {
			fmt.Fprintf(os.Stderr, "zcp: write service meta %s: %v\n", metaHostname, err)
		}
	}
}

// mergeExistingMeta preserves user-authored fields (BootstrappedAt,
// DeployStrategy, StrategyConfirmed, FirstDeployedAt) on meta during a
// mode-expansion write so a dev→standard upgrade doesn't silently clear
// the user's strategy choice or reset deploy history. Mode and
// StageHostname come from the plan and are left untouched.
func mergeExistingMeta(meta, existing *ServiceMeta) {
	meta.BootstrappedAt = existing.BootstrappedAt
	meta.DeployStrategy = existing.DeployStrategy
	meta.StrategyConfirmed = existing.StrategyConfirmed
	meta.FirstDeployedAt = existing.FirstDeployedAt
}
