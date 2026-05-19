package workflow

import "github.com/zeropsio/zcp/internal/topology"

// DeployDelivery is the high-level delivery model for one resolved deploy.
//
//   - direct: zcli push from agent (synchronous, runs the build now)
//   - git-push: commit + push to remote, build runs async on a CI integration
//   - manual: ZCP does not initiate the deploy; agent yields to the user
//
// Distinct from `deploy_strategy_gate.go`'s wire-level `strategy=` param of
// `zerops_deploy` (only `""` / `"git-push"` are valid there) — Delivery is
// the workflow-side projection that also covers `manual` and the
// first-deploy-bypass override.
type DeployDelivery string

const (
	DeployDeliveryDirect  DeployDelivery = "direct"
	DeployDeliveryGitPush DeployDelivery = "git-push"
	DeployDeliveryManual  DeployDelivery = "manual"
)

// DeployIntent is the resolved projection of (ServiceSnapshot, target) onto
// the concrete deploy command shape. Centralizes the (mode, deploy-class,
// source, target, setup) inference that was previously scattered across
// build_plan.go::deployActionFor, workflow_build_integration handler
// responses, and various verify/record-deploy call sites.
//
// Constructed via Resolve(target, services). Pure function — no I/O, no
// platform calls; reads only ServiceSnapshot fields plus the surrounding
// services list (for paired-mode dev-half lookup when target is a stage).
//
// Wired through build_plan.go in this phase. Later phases route auto-close
// gate, build-integration targeting, verify, and record-deploy through the
// resolved fields. See plan
// `plans/git-push-deploy-flow-redesign-2026-05-19.md`.
type DeployIntent struct {
	// Delivery is the high-level delivery model.
	Delivery DeployDelivery

	// PushSource is the hostname that originates the push: the dev half of a
	// standard pair, a single-service mode (simple/dev/local-*), or the dev
	// half found by reverse-lookup when target is a stage half. Empty when
	// the target is a stage half whose paired dev is missing from services.
	PushSource string

	// BuildTarget is the hostname whose runtime actually receives the build:
	// stage half for a standard pair (cross-deploy), or the same as
	// PushSource for self-deploy modes. Empty for local-only with no linked
	// Zerops runtime.
	BuildTarget string

	// PushSetup is the zerops.yaml setup name used during the local push
	// pre-flight. Empty when the source/target share a setup (self-deploy)
	// — the deploy tool defaults to the target's hostname.
	PushSetup string

	// BuildSetup is the zerops.yaml setup name the builder consumes when
	// building BuildTarget. "prod" for standard-pair cross-deploy
	// (matches the develop-close-mode-auto-standard cadence); empty for
	// self-deploy (builder defaults to the hostname).
	BuildSetup string

	// DeployTool is the MCP tool name. Always "zerops_deploy" for direct /
	// git-push deliveries; empty for manual.
	DeployTool string

	// DeployArgs is the pre-rendered argument map matching DeployTool's
	// schema. Nil for manual delivery.
	DeployArgs map[string]string

	// EventsService is the hostname to pass to zerops_events for build /
	// runtime observation. Always BuildTarget when non-empty.
	EventsService string

	// RecordDeployTarget is the hostname to pass to action="record-deploy"
	// after an async build lands. Populated when RequiresAsyncAck=true;
	// empty for synchronous deliveries (no record-deploy required).
	RecordDeployTarget string

	// VerifyTarget is the hostname to pass to zerops_verify after deploy.
	// Always BuildTarget when non-empty.
	VerifyTarget string

	// RequiresAsyncAck is true when this delivery returns before the build
	// has landed and the agent must observe zerops_events + call
	// action="record-deploy" to advance the work-session. True for
	// git-push delivery; false for direct.
	RequiresAsyncAck bool

	// FirstDeployBypass is true when target.Deployed=false. Signals callers
	// that the very first deploy must use direct delivery regardless of
	// CloseDeployMode — git-push needs committed code + remote credentials
	// that are typically set up after the first deploy lands.
	FirstDeployBypass bool
}

// Resolve computes a DeployIntent for the target snapshot, using services as
// the lookup table for paired-mode info (the dev half of a stage target).
//
// Pure function. Caller passes the snapshot from envelope; resolver does no
// I/O.
//
// Behavior matrix (closeMode × gitPushState × deployed):
//
//	closeMode=auto                      → Delivery=direct (today's behavior)
//	closeMode=manual                    → Delivery=manual
//	closeMode=git-push + !deployed      → Delivery=direct (first-deploy bypass)
//	closeMode=git-push + configured     → Delivery=git-push
//	closeMode=git-push + !configured    → Delivery=direct (capability gap;
//	                                       develop-strategy-review surfaces
//	                                       the git-push-setup pointer)
//	closeMode=unset (any)               → Delivery=direct (pre-strategy-review)
//
// Push-vs-build mapping (by Mode):
//
//	ModeStandard (dev half) → PushSource=self, BuildTarget=StageHostname
//	                          (cross-deploy under direct; remote build
//	                          under git-push — both target the stage runtime)
//	ModeStage (build target) → PushSource=findDevHalfForStage, BuildTarget=self,
//	                           BuildSetup="prod"
//	ModeSimple / ModeDev    → PushSource=BuildTarget=self
//	ModeLocalStage          → PushSource=self (project name; local CWD),
//	                          BuildTarget=StageHostname
//	ModeLocalOnly           → PushSource=self, BuildTarget="" (no Zerops target)
//	unset/empty             → PushSource=BuildTarget=self (defensive default)
//
// Phase 1 wires this through build_plan only and preserves today's
// deployActionFor output 1:1; later phases activate the git-push delivery
// branch and the paired BuildTarget for standard dev halves.
func Resolve(target ServiceSnapshot, services []ServiceSnapshot) DeployIntent {
	intent := DeployIntent{
		DeployTool:        "zerops_deploy",
		FirstDeployBypass: !target.Deployed,
	}

	intent.Delivery = resolveDelivery(target, intent.FirstDeployBypass)

	// Push-source / build-target computation. BuildTarget depends on
	// (Mode, Delivery): under direct delivery a standard-pair dev half
	// deploys to itself (the stage half receives its own cross-deploy as
	// a separate step), but under git-push delivery the dev half's push
	// causes the stage half to rebuild from the remote — no local
	// cross-deploy from dev. The stage half always builds on itself.
	switch target.Mode {
	case topology.ModeStage:
		intent.PushSource = findDevHalfForStage(target.Hostname, services)
		intent.BuildTarget = target.Hostname
		intent.BuildSetup = RecipeSetupProd
	case topology.ModeStandard, topology.ModeLocalStage:
		intent.PushSource = target.Hostname
		// PushSetup names the SOURCE-side setup block used by the local
		// push pre-flight (zerops.yaml validation before transmit).
		// Recipe-derived yamls use `dev` on the source half; legacy
		// single-runtime adoptions use the hostname. The deploy tool
		// defaults to hostname-as-setup when omitted, which fails on
		// recipe-style yamls — so always emit explicitly.
		intent.PushSetup = RecipeSetupDev
		if intent.Delivery == DeployDeliveryGitPush && target.StageHostname != "" {
			// Remote push → stage rebuilds from git. Stage runtime is the
			// build target; build setup is the stage entry ("prod" by
			// recipe convention).
			intent.BuildTarget = target.StageHostname
			intent.BuildSetup = RecipeSetupProd
		} else {
			// Direct delivery (or git-push without a paired stage):
			// dev half deploys to itself.
			intent.BuildTarget = target.Hostname
		}
	case topology.ModeLocalOnly:
		intent.PushSource = target.Hostname
		// No Zerops runtime linked — BuildTarget stays empty so callers
		// can branch on it (build-integration must refuse until
		// adopt-local links a stage).
	case topology.ModeDev, topology.ModeSimple:
		// Single-service shapes: dev container (legacy ModeDev) or simple
		// single-runtime; both self-deploy.
		intent.PushSource = target.Hostname
		intent.BuildTarget = target.Hostname
	default:
		// Empty Mode or any future variant: single-service fallback.
		intent.PushSource = target.Hostname
		intent.BuildTarget = target.Hostname
	}

	switch intent.Delivery {
	case DeployDeliveryDirect:
		// Phase 1 parity: emit the same args today's deployActionFor
		// emits. Cross-deploy when PushSource != BuildTarget AND the
		// target is the stage half (PushSource was reverse-looked-up);
		// self-deploy otherwise.
		if target.Mode == topology.ModeStage && intent.PushSource != "" {
			intent.DeployArgs = map[string]string{
				"sourceService": intent.PushSource,
				"targetService": intent.BuildTarget,
				"setup":         intent.BuildSetup,
			}
		} else {
			intent.DeployArgs = map[string]string{
				"targetService": target.Hostname,
			}
		}
		intent.EventsService = target.Hostname
		intent.VerifyTarget = target.Hostname
	case DeployDeliveryGitPush:
		// Emit setup=<PushSetup> so the local push pre-flight validates
		// against the source-side setup block (e.g. "dev" for recipe-
		// style standard pairs). Without this the deploy tool defaults
		// setup to the target hostname, which fails on recipe yamls
		// (setup blocks are `dev`/`prod`, not the hostname).
		intent.DeployArgs = map[string]string{
			"targetService": intent.PushSource,
			"strategy":      "git-push",
		}
		if intent.PushSetup != "" {
			intent.DeployArgs["setup"] = intent.PushSetup
		}
		intent.EventsService = intent.BuildTarget
		intent.RecordDeployTarget = intent.BuildTarget
		intent.VerifyTarget = intent.BuildTarget
		intent.RequiresAsyncAck = true
	case DeployDeliveryManual:
		intent.DeployTool = ""
		intent.DeployArgs = nil
	}

	return intent
}

// resolveDelivery picks the delivery model from CloseDeployMode + GitPushState,
// with first-deploy bypass overriding closeMode=git-push (git-push needs
// committed code + remote credentials that don't exist before first deploy).
func resolveDelivery(target ServiceSnapshot, firstDeployBypass bool) DeployDelivery {
	switch target.CloseDeployMode {
	case topology.CloseModeManual:
		return DeployDeliveryManual
	case topology.CloseModeGitPush:
		if firstDeployBypass {
			return DeployDeliveryDirect
		}
		if target.GitPushState == topology.GitPushConfigured {
			return DeployDeliveryGitPush
		}
		return DeployDeliveryDirect
	case topology.CloseModeAuto, topology.CloseModeUnset:
		// Auto and pre-strategy-review unset both resolve to direct.
		return DeployDeliveryDirect
	}
	// Empty or future CloseDeployMode variants default to direct.
	return DeployDeliveryDirect
}
