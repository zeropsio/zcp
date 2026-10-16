package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
		stages := target.Runtime.StageRefs()
		stageHostname := firstStageHostname(stages)
		primarySetup := target.Runtime.PrimarySetupName

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
					// Key the meta on the DEV hostname (the local/CWD identity,
					// not a Zerops service) with Stages = the deployed
					// stage — matching adopt-local's local-stage shape. ModeFor
					// then projects the dev side to "" and the stage to
					// local-stage. (Keying on the stage would collide
					// Hostname==StageHostname and short-circuit ModeFor(stage)→"".)
					mode = topology.PlanModeLocalStage
					primarySetup = ""
				}
			}
			if metaHostname == "" && stageHostname != "" {
				mode = topology.PlanModeLocalStage
				metaHostname = stageHostname
			}
		}

		meta := targetMeta(target, metaHostname, mode, stages, state.SessionID, now)

		// Gate R — recipe-bootstrap setup names come from the recipe shape's
		// LITERAL zeropsSetup (carried on the target by DeriveRecipePlan): a
		// worker's is "worker" (not the mode→convention "prod"), and a recipe
		// using "develop"/"staging" keeps that literal — one owner, no drift.
		// Fresh (!IsExisting) recipe targets only; adopted services run Gate A.
		if state.Bootstrap.Route == BootstrapRouteRecipe && !target.Runtime.IsExisting {
			meta.PrimarySetupName = primarySetup
			meta.Stages = slices.Clone(stages)
			// B4/F11: a buildFromGit recipe runtime is deployed by the platform
			// at import — mark it so DeriveDeployed reads deployed=true at ACTIVE.
			meta.ProvisionedFromGit = target.Runtime.BuildFromGit != ""
		}

		if err := writeTargetMeta(e.stateDir, target, meta); err != nil {
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
		stages := target.Runtime.StageRefs()
		stageHostname := firstStageHostname(stages)
		mode := target.Runtime.EffectiveMode()
		primarySetup := target.Runtime.PrimarySetupName

		// Local-mode projection — see writeBootstrapOutputs for the rationale.
		if e.environment == EnvLocal {
			if state.Bootstrap.Route == BootstrapRouteRecipe {
				if mode == topology.PlanModeDev {
					continue
				}
				if mode == topology.PlanModeStandard && stageHostname != "" {
					mode = topology.PlanModeLocalStage
					primarySetup = ""
				}
			}
			if metaHostname == "" && stageHostname != "" {
				mode = topology.PlanModeLocalStage
				metaHostname = stageHostname
			}
		}

		// Partial: no BootstrappedAt, so the meta reads as provisioned, not
		// bootstrapped, until writeBootstrapOutputs completes it.
		meta := targetMeta(target, metaHostname, mode, stages, state.SessionID, "")

		// Gate R — partial-write counterpart of writeBootstrapOutputs (setup
		// names from the target's literal zeropsSetup). So a crash between
		// provision and close leaves the canonical setup names already recorded.
		if state.Bootstrap.Route == BootstrapRouteRecipe && !target.Runtime.IsExisting {
			meta.PrimarySetupName = primarySetup
			meta.Stages = slices.Clone(stages)
			meta.ProvisionedFromGit = target.Runtime.BuildFromGit != ""
		}

		if err := writeTargetMeta(e.stateDir, target, meta); err != nil {
			fmt.Fprintf(os.Stderr, "zcp: write service meta %s: %v\n", metaHostname, err)
		}
	}
}

// targetMeta is the meta a plan target is recorded as: the three deploy
// dimensions at their zero values, the plan's stages by hostname (their setups
// are recorded only where the plan's setup is authoritative — Gate R), the
// plan's public-access intent on every half, and — for a service adopted
// rather than created (IsExisting) — no bootstrap session, which is what makes
// it read as adopted (IsAdopted). bootstrappedAt is empty for provision's
// partial write.
func targetMeta(target BootstrapTarget, hostname string, mode topology.Mode, stages []topology.StageRef, sessionID, bootstrappedAt string) *ServiceMeta {
	bootstrapSession := sessionID
	if target.Runtime.IsExisting {
		bootstrapSession = ""
	}
	meta := &ServiceMeta{
		Hostname:         hostname,
		Mode:             mode,
		Stages:           planStages(stages),
		CloseDeployMode:  topology.CloseModeUnset,
		GitPushState:     topology.GitPushUnconfigured,
		BuildIntegration: topology.BuildIntegrationNone,
		BootstrapSession: bootstrapSession,
		BootstrappedAt:   bootstrappedAt,
		CodeVolume:       cloneCodeVolume(target.Runtime.CodeVolume),
	}
	if serves := target.Runtime.ServesHTTP; serves != nil {
		meta.SetServesHTTP(hostname, *serves)
	}
	setPlanPublicAccess(meta, target.Runtime.PublicAccess, hostname, meta.StageHostnames())
	return meta
}

// planStages is the stages a plan records on the meta — nil when there are
// none, so a stageless meta reads back unchanged. A pair of several stages
// keeps each stage's setup: it is what tells the parts apart, and the plan
// step checked it against the repo's zerops.yml (docs/spec-mate.md
// §2.8a.4). A one-stage pair records only its hostname; its setup is
// resolved at its first deploy.
func planStages(stages []topology.StageRef) []topology.StageRef {
	if len(stages) == 0 {
		return nil
	}
	if len(stages) > 1 {
		return slices.Clone(stages)
	}
	out := make([]topology.StageRef, 0, len(stages))
	for _, s := range stages {
		out = append(out, topology.StageRef{Hostname: s.Hostname})
	}
	return out
}

// cloneCodeVolume copies a plan's code volume into a meta, so the plan and the
// meta never share its dev-service list.
func cloneCodeVolume(v *topology.CodeVolume) *topology.CodeVolume {
	if v == nil {
		return nil
	}
	c := *v
	c.DevServices = slices.Clone(v.DevServices)
	return &c
}

// firstStageHostname is the hostname of a plan target's first stage, "" when
// it has none — what the local-mode projection keys on.
func firstStageHostname(stages []topology.StageRef) string {
	if len(stages) == 0 {
		return ""
	}
	return stages[0].Hostname
}

// writeTargetMeta is the constructive write of a target's meta, atomic under
// .services.lock. For an existing service already recorded complete (a mode
// expansion, or a second adoption) the read+merge+write is one critical
// section so a concurrent dimension write isn't clobbered, and the user's
// authored fields survive by merging the on-disk meta onto the constructed one.
func writeTargetMeta(stateDir string, target BootstrapTarget, meta *ServiceMeta) error {
	return UpsertServiceMeta(stateDir, meta.Hostname, func(m *ServiceMeta, existed bool) error {
		if target.Runtime.IsExisting && existed && m.IsComplete() {
			mergeExistingMeta(meta, m)
		}
		*m = *meta
		return nil
	})
}

// mergeExistingMeta preserves user-authored fields on meta during a
// mode-expansion write so a dev→standard upgrade doesn't silently clear
// the user's deploy choices or reset deploy history. Mode and the stage
// hostnames come from the plan and are left untouched.
//
// PrimarySetupName and each stage's Setup follow migrate-forward-empty
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
	// The pair's repository record (HQ's repository, the Mate's branch, its
	// change) travels with the push state it belongs to: without it an
	// expanded pair reads as never wired, and the group recipe is never
	// re-proposed (Dara's todoapp, 2026-09-17).
	if meta.HQ == nil {
		meta.HQ = existing.HQ
	}

	if existing.PrimarySetupName != "" {
		meta.PrimarySetupName = existing.PrimarySetupName
	}
	if meta.CodeVolume == nil {
		meta.CodeVolume = existing.CodeVolume
	}
	for _, s := range existing.Stages {
		if s.SetupName != "" {
			meta.SetStageSetup(s.Hostname, s.SetupName)
		}
	}
	// Sticky once set either side: the recipe-buildFromGit provenance can't
	// regress on a later partial write or pair-expansion merge (B4).
	meta.ProvisionedFromGit = existing.ProvisionedFromGit || meta.ProvisionedFromGit

	// Per-hostname public-access intent (§8 E9): an existing recorded entry
	// (the user's prior choice, or a stamp from a past auto-enable) survives
	// a mode-expansion re-plan. A hostname the existing meta never recorded
	// (e.g. the new stage half of a dev→standard expansion) keeps the fresh
	// meta's plan-derived value already set by setPlanPublicAccess.
	for hostname, rec := range existing.PublicAccess {
		meta.SetPublicAccess(hostname, rec)
	}
}

// setPlanPublicAccess stamps meta.PublicAccess for every half of a target
// from the plan's RuntimeTarget.PublicAccess (§8 O3 PA-6). Empty/invalid
// defaults to auto at this I/O boundary — PublicAccessIntent.IsValid()
// deliberately rejects "" so this default lives at the single call site,
// not duplicated across every reader. No stage hostnames is a no-op (dev/
// simple/local-only targets have no stage half to stamp).
func setPlanPublicAccess(meta *ServiceMeta, planned, hostname string, stageHostnames []string) {
	intent, ok := topology.ParsePublicAccessIntent(planned)
	if !ok {
		intent = topology.PublicAccessAuto
	}
	meta.SetPublicAccess(hostname, topology.PublicAccessRecord{Intent: intent})
	for _, stageHostname := range stageHostnames {
		meta.SetPublicAccess(stageHostname, topology.PublicAccessRecord{Intent: intent})
	}
}
