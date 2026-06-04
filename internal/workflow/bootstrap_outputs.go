package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
)

// writeBootstrapOutputs writes final service meta files and appends a reflog entry.
// Sets BootstrappedAt to mark services as fully bootstrapped.
// Both are best-effort — errors are logged to stderr but don't fail bootstrap completion.
//
// Mode-expansion path: when the plan upgrades an existing runtime's
// bootstrapMode (dev/simple → standard) with IsExisting=true, the existing
// ServiceMeta is merged rather than overwritten — BootstrappedAt, the
// per-pair dimensions (CloseDeployMode + CloseDeployModeConfirmed +
// GitPushState + RemoteURL + BuildIntegration), and FirstDeployedAt are
// preserved so the user's prior choices and deploy history survive the
// mode upgrade. See §9.1 of spec-workflows.md.
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
		primarySetup := target.Runtime.PrimarySetupName
		stageSetup := target.Runtime.StageSetupName

		// Local-mode projection: the dev runtime is the user's CWD, never
		// provisioned on Zerops (LocalizeRecipeImportYAML drops
		// zeropsSetup:dev). For a recipe standard pair the stage is the only
		// Zerops runtime → key the meta on the stage with local-stage mode
		// and drop the local-dev setup name; a lone-dev recipe has no Zerops
		// runtime → no meta. The DevHostname=="" branch is the legacy
		// classic-local fallback (kept for classic route compatibility).
		if e.environment == EnvLocal {
			if state.Bootstrap.Route == BootstrapRouteRecipe {
				if mode == topology.PlanModeDev {
					continue
				}
				if mode == topology.PlanModeStandard && stageHostname != "" {
					mode = topology.PlanModeLocalStage
					metaHostname = stageHostname
					primarySetup = ""
				}
			}
			if metaHostname == "" && stageHostname != "" {
				mode = topology.PlanModeLocalStage
				metaHostname = stageHostname
			}
		}

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
			ServesHTTP:       target.Runtime.ServesHTTP,
			CloseDeployMode:  topology.CloseModeUnset,
			GitPushState:     topology.GitPushUnconfigured,
			BuildIntegration: topology.BuildIntegrationNone,
			BootstrapSession: bootstrapSession,
			BootstrappedAt:   now,
		}

		// Gate R — recipe-bootstrap setup names come from the recipe shape's
		// LITERAL zeropsSetup (carried on the target by DeriveRecipePlan): a
		// worker's is "worker" (not the mode→convention "prod"), and a recipe
		// using "develop"/"staging" keeps that literal — one owner, no drift.
		// Fresh (!IsExisting) recipe targets only; adopted services run Gate A.
		if state.Bootstrap.Route == BootstrapRouteRecipe && !target.Runtime.IsExisting {
			meta.PrimarySetupName = primarySetup
			meta.StageSetupName = stageSetup
		}

		// Constructive write, atomic under .services.lock. For an existing-service
		// expansion (IsExisting) the read+merge+write must be one critical section
		// so a concurrent dimension write isn't clobbered; preserve the user's
		// authored fields by merging the on-disk meta onto the constructed one.
		if err := UpsertServiceMeta(e.stateDir, metaHostname, func(m *ServiceMeta, existed bool) error {
			if target.Runtime.IsExisting && existed && m.IsComplete() {
				mergeExistingMeta(meta, m)
			}
			*m = *meta
			return nil
		}); err != nil {
			fmt.Fprintf(os.Stderr, "zcp: write service meta %s: %v\n", metaHostname, err)
		}
	}

	// Derive project root from stateDir (expected: {projectRoot}/.zcp/state/).
	// REFLOG lives in AGENTS.md post-multi-agent migration — Codex,
	// Cursor, Gemini, and Antigravity all read AGENTS.md natively;
	// Claude pulls it via the @AGENTS.md include in CLAUDE.md.
	projectRoot := filepath.Dir(filepath.Dir(e.stateDir))
	agentsMDPath := filepath.Join(projectRoot, "AGENTS.md")

	if err := AppendReflogEntry(agentsMDPath, state.Intent, plan.Targets, state.SessionID, now); err != nil {
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
// (partial) write doesn't lose BootstrappedAt / CloseDeployMode /
// GitPushState / BuildIntegration. Without this, a crash between
// provision and close would leave the service looking like a brand-new
// bootstrap instead of a mode-upgrade.
func (e *Engine) writeProvisionMetas(state *WorkflowState) {
	if state.Bootstrap == nil || state.Bootstrap.Plan == nil {
		return
	}

	for _, target := range state.Bootstrap.Plan.Targets {
		metaHostname := target.Runtime.DevHostname
		stageHostname := target.Runtime.StageHostname()
		mode := target.Runtime.EffectiveMode()
		primarySetup := target.Runtime.PrimarySetupName
		stageSetup := target.Runtime.StageSetupName

		// Local-mode projection — see writeBootstrapOutputs for the rationale.
		if e.environment == EnvLocal {
			if state.Bootstrap.Route == BootstrapRouteRecipe {
				if mode == topology.PlanModeDev {
					continue
				}
				if mode == topology.PlanModeStandard && stageHostname != "" {
					mode = topology.PlanModeLocalStage
					metaHostname = stageHostname
					primarySetup = ""
				}
			}
			if metaHostname == "" && stageHostname != "" {
				mode = topology.PlanModeLocalStage
				metaHostname = stageHostname
			}
		}

		// Adopted services (isExisting=true) get empty BootstrapSession.
		bootstrapSession := state.SessionID
		if target.Runtime.IsExisting {
			bootstrapSession = ""
		}

		meta := &ServiceMeta{
			Hostname:         metaHostname,
			Mode:             mode,
			StageHostname:    stageHostname,
			ServesHTTP:       target.Runtime.ServesHTTP,
			CloseDeployMode:  topology.CloseModeUnset,
			GitPushState:     topology.GitPushUnconfigured,
			BuildIntegration: topology.BuildIntegrationNone,
			BootstrapSession: bootstrapSession,
		}

		// Gate R — partial-write counterpart of writeBootstrapOutputs (setup
		// names from the target's literal zeropsSetup). So a crash between
		// provision and close leaves the canonical setup names already recorded.
		if state.Bootstrap.Route == BootstrapRouteRecipe && !target.Runtime.IsExisting {
			meta.PrimarySetupName = primarySetup
			meta.StageSetupName = stageSetup
		}

		if err := UpsertServiceMeta(e.stateDir, metaHostname, func(m *ServiceMeta, existed bool) error {
			if target.Runtime.IsExisting && existed && m.IsComplete() {
				mergeExistingMeta(meta, m)
			}
			*m = *meta
			return nil
		}); err != nil {
			fmt.Fprintf(os.Stderr, "zcp: write service meta %s: %v\n", metaHostname, err)
		}
	}
}

// mergeExistingMeta preserves user-authored fields on meta during a
// mode-expansion write so a dev→standard upgrade doesn't silently clear
// the user's deploy choices or reset deploy history. Mode and
// StageHostname come from the plan and are left untouched.
//
// PrimarySetupName / StageSetupName follow migrate-forward-empty
// semantics: a non-empty existing value wins (a previously-discovered
// setup name is the canonical record); an empty existing value lets the
// fresh meta's value through unchanged (covers the dev→standard
// expansion where the new stage half's "prod" comes from the fresh meta
// while the existing "dev" survives on PrimarySetupName).
func mergeExistingMeta(meta, existing *ServiceMeta) {
	meta.BootstrappedAt = existing.BootstrappedAt
	meta.FirstDeployedAt = existing.FirstDeployedAt

	meta.CloseDeployMode = existing.CloseDeployMode
	meta.CloseDeployModeConfirmed = existing.CloseDeployModeConfirmed
	meta.GitPushState = existing.GitPushState
	meta.RemoteURL = existing.RemoteURL
	meta.BuildIntegration = existing.BuildIntegration

	if existing.PrimarySetupName != "" {
		meta.PrimarySetupName = existing.PrimarySetupName
	}
	if existing.StageSetupName != "" {
		meta.StageSetupName = existing.StageSetupName
	}
}
