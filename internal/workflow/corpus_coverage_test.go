// Corpus round-trip coverage and compaction-safety harness. Each fixture
// names a realistic envelope shape and asserts the synthesized output
// contains the load-bearing phrases from atoms that should match. The
// compaction-safety leg asserts Synthesize is byte-identical across repeat
// calls on every fixture.
//
// This is the mechanized form of the Phase 6 audit gate: if an atom's
// load-bearing claim disappears from the corpus, the assertion fails —
// catching silent content regressions that would otherwise only surface
// during a user interaction.
package workflow

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// coverageFixture pairs an envelope with substrings that MUST appear in the
// synthesized output. Phrases are chosen from atom bodies that represent
// load-bearing facts — removing one would regress agent behavior.
type coverageFixture struct {
	Name        string
	Envelope    StateEnvelope
	MustContain []string
}

func coverageFixtures() []coverageFixture {
	boot := bootstrapCoverageFixtures()
	dev := developCoverageFixtures()
	matrix := matrixCoverageFixtures()
	pipeline := pipelineCoverageFixtures()
	out := make([]coverageFixture, 0, len(boot)+len(dev)+len(matrix)+len(pipeline))
	out = append(out, boot...)
	out = append(out, dev...)
	out = append(out, matrix...)
	out = append(out, pipeline...)
	return out
}

func bootstrapCoverageFixtures() []coverageFixture {
	return []coverageFixture{
		{
			Name: "idle_empty_project",
			Envelope: StateEnvelope{
				Phase:        PhaseIdle,
				Environment:  EnvContainer,
				IdleScenario: IdleEmpty,
			},
			MustContain: []string{
				`zerops_workflow action="start" workflow="bootstrap"`,
				"one sentence",
			},
		},
		{
			Name: "idle_bootstrapped_ready_for_develop",
			Envelope: StateEnvelope{
				Phase:        PhaseIdle,
				Environment:  EnvContainer,
				IdleScenario: IdleBootstrapped,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
					Bootstrapped: true, StageHostname: "appstage",
				}},
			},
			MustContain: []string{
				`zerops_workflow action="start" workflow="develop"`,
				"develop-active",
			},
		},
		{
			Name: "idle_adopt_unmanaged_only",
			Envelope: StateEnvelope{
				Phase:        PhaseIdle,
				Environment:  EnvContainer,
				IdleScenario: IdleAdopt,
				Services: []ServiceSnapshot{{
					Hostname: "legacy", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic,
				}},
			},
			MustContain: []string{
				`zerops_workflow action="start" workflow="bootstrap" intent="adopt`,
				"not bootstrapped",
				`route="adopt"`,
			},
		},
		{
			Name: "bootstrap_classic_discover_dynamic",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
					StageHostname: "appstage",
				}},
				Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteClassic, Step: StepDiscover},
			},
			MustContain: []string{
				"dynamic runtime",
				"verification server",
				"dev/stage pairing",
			},
		},
		{
			Name: "bootstrap_recipe_provision",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
				}},
				Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteRecipe, Step: StepProvision},
			},
			MustContain: []string{
				"zerops_import",
				"ACTIVE",
			},
		},
		{
			// F0-DEAD-1 follow-up (2026-04-26): the prior absence of a
			// route=recipe step=close fixture allowed an unescaped
			// `{hostname:value}` placeholder in `bootstrap-recipe-close.md`
			// to ship undetected — Synthesize errors out for the entire
			// envelope when the placeholder leaks through. This fixture
			// pins the post-fix render so the bug class can't recur.
			Name: "bootstrap_recipe_close",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
				}},
				Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteRecipe, Step: StepClose},
			},
			MustContain: []string{
				`zerops_workflow action="complete" step="close"`,
				`strategies={"<hostname>":"<value>"}`,
			},
		},
		{
			Name: "bootstrap_adopt_discover",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
				}},
				Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteAdopt, Step: StepDiscover},
			},
			MustContain: []string{
				"Adopting existing services",
				"bootstrapped: true",
			},
		},
	}
}

func developCoverageFixtures() []coverageFixture {
	return []coverageFixture{
		{
			Name: "develop_push_dev_dev_container",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
					Strategy: "push-dev", Bootstrapped: true, Deployed: true,
				}},
			},
			MustContain: []string{
				"Push-Dev Deploy Strategy",
				"SSH",
				// Phase-3 axis-E #5 (commit <pending>) trimmed the
				// "Read and edit directly on the mount" prose in
				// `develop-platform-rules-container` because
				// `claude_container.md:5-6` already delivers the
				// mount basics at session boot. Pin migrated to a
				// post-dedup unique phrase ("Mount caveats" anchors
				// the new bullet that owns the operational cautions
				// not in the boot shim).
				"Mount caveats",
				"HTTP diagnostics",
				"zerops_verify",
				"VERDICT",
				"agent-browser",
				// Phase-1 additions — load-bearing awareness + KB pointers.
				"Deploy strategy — current + how to change",
				`action="strategy"`,
				// Phase-2 dedup #5 (commit <pending>) replaced the
				// blanket "edit → deploy → verify" framing in
				// `develop-change-drives-deploy` with mode-aware
				// guidance to resolve the restart-vs-deploy
				// conflict per Codex round (see
				// `plans/audit-composition/dedup-candidates.md`).
				// Pin migrated post-dedup; followup-Phase-5 dedup
				// F5 drops the inline persistence-boundary cross-link
				// (canonical: develop-platform-rules-common); pin
				// re-migrated to a still-unique post-F5 phrase.
				"Iteration cadence is mode-specific",
				"Knowledge on demand",
				"Infrastructure changes",
				// Phase-4: mode-expansion hint fires for dev services.
				// Note: {hostname} is substituted by the synthesizer to the
				// primary dynamic hostname — for this fixture that's "appdev".
				"Mode expansion — add a stage pair",
				`intent="expand appdev to standard`,
			},
		},
		{
			Name: "develop_push_git_standard_container",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
					StageHostname: "appstage",
					Strategy:      "push-git", Bootstrapped: true, Deployed: true,
				}},
			},
			MustContain: []string{
				"git-push",
				"GIT_TOKEN",
				"push-dev",
			},
		},
		{
			Name: "develop_manual_simple",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "app", TypeVersion: "nginx@1",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeSimple,
					Strategy: "manual", Bootstrapped: true, Deployed: true,
				}},
			},
			MustContain: []string{
				"zerops_deploy",
				"Env var channels",
				"restartedServices",
				"does NOT pick them up",
				"Shadow-loop pitfall",
			},
		},
		{
			// Post-first-deploy with strategy still unset: strategy-review atom
			// asks the agent to confirm an ongoing strategy now that the first
			// deploy has landed. Pre-first-deploy branch owns the
			// strategy-agnostic first-deploy guidance; review only fires once
			// FirstDeployedAt is stamped.
			Name: "develop_deployed_strategy_unset",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
					Strategy: topology.StrategyUnset, Bootstrapped: true, Deployed: true,
				}},
			},
			MustContain: []string{
				"Pick an ongoing deploy strategy",
				`action="strategy"`,
			},
		},
		{
			Name: "develop_local_dynamic",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvLocal,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
					Strategy: "push-dev", Bootstrapped: true, Deployed: true,
				}},
			},
			MustContain: []string{
				"local",
			},
		},
		{
			Name: "develop_closed_auto",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopClosed,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
				}},
			},
			MustContain: []string{
				"develop-closed-auto",
				"auto-complete",
				`action="close"`,
			},
		},
		{
			// First-deploy branch on a standard pair: the promote-stage atom
			// fires so the agent cross-deploys after dev verify and both halves
			// land before auto-close can fire.
			Name: "develop_first_deploy_standard_container",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{
					{
						Hostname: "appdev", TypeVersion: "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
						StageHostname: "appstage",
						Strategy:      topology.StrategyUnset, Bootstrapped: true, Deployed: false,
					},
					{
						Hostname: "appstage", TypeVersion: "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage,
						Strategy: topology.StrategyUnset, Bootstrapped: true, Deployed: false,
					},
				},
			},
			MustContain: []string{
				"first-deploy branch",
				"Promote the first deploy to stage",
				`sourceService="appdev"`,
				`targetService="appstage"`,
				// Pin the per-service execute + verify cmds atoms — their
				// sole purpose is to bind {hostname} per matching service.
				// Asserting the dev-side substitution catches an accidental
				// drop of either cmds atom (which would silently leave the
				// agent without the explicit per-host commands).
				`zerops_deploy targetService="appdev"`,
				`zerops_verify serviceHostname="appdev"`,
			},
		},
		{
			// First-deploy branch on an implicit-webserver pair (php-nginx
			// standard mode): asset-pipeline atom must fire so the agent runs
			// `npm run build` over SSH before verify. Gates on Laravel+Vite
			// / Symfony+Encore flows where dev setups intentionally skip the
			// production asset build.
			Name: "develop_first_deploy_implicit_webserver_standard",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{
					{
						Hostname: "appdev", TypeVersion: "php-nginx@8.4",
						RuntimeClass: topology.RuntimeImplicitWeb, Mode: topology.ModeStandard,
						StageHostname: "appstage",
						Strategy:      topology.StrategyUnset, Bootstrapped: true, Deployed: false,
					},
					{
						Hostname: "appstage", TypeVersion: "php-nginx@8.4",
						RuntimeClass: topology.RuntimeImplicitWeb, Mode: topology.ModeStage,
						Strategy: topology.StrategyUnset, Bootstrapped: true, Deployed: false,
					},
				},
			},
			MustContain: []string{
				"asset pipeline",
				"npm run build",
				"Vite manifest not found",
				"Do NOT add",
				"buildCommands",
			},
		},
		{
			// Local+dev push-dev: close-push-dev-local fills the gap left by
			// close-push-dev-dev's environments=[container] restriction.
			Name: "develop_close_local_dev",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvLocal,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
					Strategy: topology.StrategyPushDev, Bootstrapped: true, Deployed: true,
				}},
			},
			MustContain: []string{
				"Local mode builds from your committed tree",
				`zerops_deploy targetService="appdev"`,
			},
		},
		{
			// Local+standard push-dev: the snapshot's Mode=ModeStage (per
			// resolveEnvelopeMode). close-push-dev-local must still fire.
			Name: "develop_close_local_standard",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvLocal,
				Services: []ServiceSnapshot{{
					Hostname: "appstage", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage,
					Strategy: topology.StrategyPushDev, Bootstrapped: true, Deployed: true,
				}},
			},
			MustContain: []string{
				"Local mode builds from your committed tree",
				`zerops_deploy targetService="appstage"`,
			},
		},
		{
			// Stretch fixture: TWO standard-mode dev/stage runtime pairs in
			// the same first-deploy envelope. Per-service-axis atoms render
			// once per matching service — with four matching services the
			// per-service render duplication scales linearly. This shape is
			// realistic (a project with `app` + `api` runtimes) and forces
			// the trim plan to validate against multi-pair envelopes, not
			// just single-pair (so a fix that closes the single-pair gap
			// silently breaks two-pair shapes can be caught).
			Name: "develop_first_deploy_two_runtime_pairs_standard",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{
					{
						Hostname: "appdev", TypeVersion: "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
						StageHostname: "appstage",
						Strategy:      topology.StrategyUnset, Bootstrapped: true, Deployed: false,
					},
					{
						Hostname: "appstage", TypeVersion: "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage,
						Strategy: topology.StrategyUnset, Bootstrapped: true, Deployed: false,
					},
					{
						Hostname: "apidev", TypeVersion: "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
						StageHostname: "apistage",
						Strategy:      topology.StrategyUnset, Bootstrapped: true, Deployed: false,
					},
					{
						Hostname: "apistage", TypeVersion: "nodejs@22",
						RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage,
						Strategy: topology.StrategyUnset, Bootstrapped: true, Deployed: false,
					},
				},
			},
			MustContain: []string{
				"first-deploy branch",
				"Promote the first deploy to stage",
				`sourceService="appdev" targetService="appstage"`,
				`sourceService="apidev" targetService="apistage"`,
				// Per-service execute + verify cmds atoms must bind to
				// EACH never-deployed service. Pinning both dev hosts in
				// the two-pair shape catches per-host substitution loss
				// in multi-pair envelopes.
				`zerops_deploy targetService="appdev"`,
				`zerops_deploy targetService="apidev"`,
				`zerops_verify serviceHostname="appdev"`,
				`zerops_verify serviceHostname="apidev"`,
			},
		},
	}
}

// matrixCoverageFixtures fills in the axis-coverage matrix with bootstrap +
// develop scenarios that didn't get a dedicated fixture above. Grouped so the
// core fixtures stay readable.
func matrixCoverageFixtures() []coverageFixture {
	return []coverageFixture{
		{
			Name: "bootstrap_classic_static",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "webdev", TypeVersion: "static@1",
					RuntimeClass: topology.RuntimeStatic, Mode: topology.ModeDev,
				}},
				Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteClassic, Step: StepDiscover},
			},
			MustContain: []string{
				"Static runtime plan",
				"empty document root",
			},
		},
		{
			Name: "bootstrap_classic_provision",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
					StageHostname: "appstage",
				}},
				Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteClassic, Step: StepProvision},
			},
			MustContain: []string{
				"ACTIVE",
				"zerops_discover",
			},
		},
		{
			Name: "bootstrap_adopt_provision",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
				}},
				Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteAdopt, Step: StepProvision},
			},
			MustContain: []string{
				"Adopt",
				"envVariables",
			},
		},
		{
			Name: "develop_push_dev_simple_container",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "app", TypeVersion: "bun@1",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeSimple,
					Strategy: "push-dev", Bootstrapped: true, Deployed: true,
				}},
			},
			MustContain: []string{
				"healthCheck",
				"zerops_deploy",
				`setup="prod"`,
			},
		},
		{
			// Hygiene-plan §7 Phase 0 step 4 addition (user-test 2026-04-26):
			// single Go simple-mode service that's already deployed and being
			// edited. Mirrors the develop_push_dev_simple_container shape but
			// with a different hostname so pre-/post-hygiene fire-set deltas
			// for THIS specific user-test envelope are greppable separately.
			//
			// MustContain pins were grep-verified UNIQUE to their anchor atoms
			// (Codex round 3 verdict 2026-04-26: UNIQUE-MATCH-CONFIRMED;
			// post-hygiene-followup Phase 3 axis-L migration 2026-04-27):
			//   develop-push-dev-deploy-container ⟶ "The dev container uses SSH push"
			//   develop-push-dev-workflow-simple  ⟶ "auto-starts with its `healthCheck`"
			//   develop-close-push-dev-simple     ⟶ "Simple-mode services auto-start on deploy"
			// None contain placeholders; survive Synthesize substitution.
			// If a later axis-tightening silently dropped any, TestCorpusCoverage_RoundTrip
			// fails on the named target.
			Name: "develop_simple_deployed_container",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "weatherdash", TypeVersion: "go@1.22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeSimple,
					Strategy: "push-dev", Bootstrapped: true, Deployed: true,
				}},
			},
			MustContain: []string{
				"The dev container uses SSH push",
				"auto-starts with its `healthCheck`",
				"Simple-mode services auto-start on deploy",
			},
		},
		{
			Name: "develop_push_dev_standard_container",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
					StageHostname: "appstage",
					Strategy:      "push-dev", Bootstrapped: true, Deployed: true,
				}},
			},
			MustContain: []string{
				"Push-Dev Deploy Strategy",
				"sourceService",
			},
		},
		{
			Name: "develop_local_standard",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvLocal,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
					StageHostname: "appstage",
					Strategy:      "push-dev", Bootstrapped: true, Deployed: true,
				}},
			},
			MustContain: []string{
				"zcli vpn up",
			},
		},
		{
			Name: "develop_local_push_git",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvLocal,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
					StageHostname: "appstage",
					Strategy:      "push-git", Bootstrapped: true, Deployed: true,
				}},
			},
			MustContain: []string{
				"git-push",
				"user's own git credentials",
			},
		},
		{
			Name: "develop_manual_container_dev",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
					Strategy: "manual", Bootstrapped: true, Deployed: true,
				}},
			},
			MustContain: []string{
				"Manual Deploy Strategy",
				"user controls deploy timing",
			},
		},
		{
			Name: "develop_implicit_webserver_php",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "app", TypeVersion: "php-apache@8.3",
					RuntimeClass: topology.RuntimeImplicitWeb, Mode: topology.ModeSimple,
					Strategy: "push-dev",
				}},
			},
			MustContain: []string{
				"documentRoot",
				"Do not SSH",
			},
		},
		{
			Name: "develop_verify_matrix_web_and_managed",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{
					{
						Hostname: "app", TypeVersion: "php-nginx@8.4",
						RuntimeClass: topology.RuntimeImplicitWeb, Mode: topology.ModeSimple,
						Strategy: "push-dev",
					},
					{
						Hostname: "db", TypeVersion: "postgresql@18",
						RuntimeClass: topology.RuntimeManaged,
					},
				},
			},
			MustContain: []string{
				"Per-service verify matrix",
				"web-facing",
				"agent-browser",
				"VERDICT: PASS",
				"VERDICT: FAIL",
				"VERDICT: UNCERTAIN",
			},
		},
		{
			Name: "develop_static_runtime",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "web", TypeVersion: "static@1",
					RuntimeClass: topology.RuntimeStatic, Mode: topology.ModeDev,
					Strategy: "push-dev",
				}},
			},
			MustContain: []string{
				"zerops_deploy",
				"Static runtime — develop workflow",
				"no SSH start",
			},
		},
	}
}

// pipelineCoverageFixtures covers strategy-setup and export-active phases.
// strategy-setup has two sub-cases driven by the Trigger axis: the intro
// atom fires pre-trigger-choice, the full setup chain fires once the
// trigger is chosen. Both must render cleanly.
func pipelineCoverageFixtures() []coverageFixture {
	return []coverageFixture{
		{
			Name: "strategy_setup_intro_pre_trigger",
			Envelope: StateEnvelope{
				Phase:       PhaseStrategySetup,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
					StageHostname: "appstage",
					Strategy:      topology.StrategyPushGit,
					Trigger:       topology.TriggerUnset,
				}},
			},
			MustContain: []string{
				"webhook",
				"actions",
				"Confirm the repo URL",
				"GIT_TOKEN", // push atom fires regardless of trigger choice
			},
		},
		{
			Name: "strategy_setup_actions_full_chain",
			Envelope: StateEnvelope{
				Phase:       PhaseStrategySetup,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
					StageHostname: "appstage",
					Strategy:      topology.StrategyPushGit,
					Trigger:       topology.TriggerActions,
				}},
			},
			MustContain: []string{
				"GIT_TOKEN",
				"ZEROPS_TOKEN",
				"zcli push",
			},
		},
		{
			Name: "strategy_setup_webhook_full_chain",
			Envelope: StateEnvelope{
				Phase:       PhaseStrategySetup,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
					StageHostname: "appstage",
					Strategy:      topology.StrategyPushGit,
					Trigger:       topology.TriggerWebhook,
				}},
			},
			MustContain: []string{
				"GIT_TOKEN",
				"dashboard",
				"Trigger automatic builds",
			},
		},
		{
			Name: "export_active",
			Envelope: StateEnvelope{
				Phase:       PhaseExportActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard,
				}},
			},
			MustContain: []string{
				"buildFromGit",
				"zerops_export",
				"import.yaml",
			},
		},
	}
}

// PhaseRecipeActive is intentionally not covered here. Recipe authoring runs
// through its own section-parser pipeline (recipe_guidance.go, recipe_topic_
// registry.go) reading from workflows/recipe.md — the atom corpus never owns
// recipe guidance, so a recipe-active fixture would be meaningless here.

func TestCorpusCoverage_RoundTrip(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	if len(corpus) == 0 {
		t.Fatal("corpus is empty — atoms directory not embedded?")
	}

	for _, fx := range coverageFixtures() {
		t.Run(fx.Name, func(t *testing.T) {
			t.Parallel()
			bodies, err := SynthesizeBodies(fx.Envelope, corpus)
			if err != nil {
				t.Fatalf("Synthesize: %v", err)
			}
			if len(bodies) == 0 {
				t.Fatalf("no atoms matched for fixture %q; expected at least one", fx.Name)
			}
			combined := strings.Join(bodies, "\n")
			for _, phrase := range fx.MustContain {
				if !strings.Contains(combined, phrase) {
					t.Errorf("fixture %q: synthesized output missing load-bearing phrase %q\n--- output ---\n%s",
						fx.Name, phrase, combined)
				}
			}
		})
	}
}

// knownOverflowFixtures are coverage fixtures whose Synthesize output
// is ALREADY over the soft 28 KB cap on the current corpus. Each entry
// is a documented-defect audit target — these envelopes overflow the
// 32 KB MCP tool-response cap at runtime today, so an LLM hitting one
// of these shapes gets a truncated or failed response.
//
// The fix is a corpus-trim plan (separate scope: split fat atoms,
// move long-form prose to zerops_knowledge topics, prune duplicate
// rationale across atoms). When that lands and a fixture drops under
// the cap, removing it from this map IS the verification — the
// `_KnownOverflows_StillOverflow` test below fails the moment the
// trim takes effect, forcing a clean removal rather than silent
// drift the other way.
//
// Adding a new entry requires the same justification: a rationale and
// a measured byte count at the time of acknowledgement. Each entry
// records BOTH metrics so the trim trajectory is visible: the body-join
// number is what `KnownOverflows_StillOverflow` asserts on; the
// wire-frame number is the actual MCP-cap-relevant measurement (~3 KB
// larger than body-join — see plans/atom-corpus-context-trim-2026-04-26.md
// §17.1 and the wire-frame info Logf in `_OutputUnderMCPCap`).
var knownOverflowFixtures = map[string]string{}

// TestCorpusCoverage_OutputUnderMCPCap pins G13: every representative
// envelope shape's synthesized output stays under a soft 28 KB ceiling
// — 4 KB below the documented MCP tool-response cap (~32 KB, see
// internal/workflow/dispatch_brief_envelope.go). The margin reserves
// room for the surrounding Response{Envelope, Plan} JSON serialization
// at the wire and any per-handler prefix the renderer adds.
//
// Pre-fix runtime atoms had no size gate. Live observation of the
// develop-active first-deploy briefing on a single simple-mode service
// returned ~29 KB; the standard-mode multi-service variants exceeded
// 40 KB. The fix below skips known overflows so the gate is honest
// about the green tree while the trim plan runs separately. The
// companion test asserts known overflows STILL overflow — the moment
// the corpus trim lands, that test fails and the entry must be removed.
//
// On failure for a NEW fixture, the right move is usually:
//
//   - Trim verbose atom prose (look at the largest atoms in the matched
//     set with `wc -c internal/content/atoms/*.md | sort -n`).
//   - Split a fat atom into a smaller envelope-axis-scoped one + an
//     opt-in dispatch-brief atom retrieved on demand.
//   - Move long-form prose to a `zerops_knowledge` topic the agent
//     fetches when needed.
//
// Raising the ceiling without first attempting the trim is an anti-
// pattern (T2 in audit-workflow-llm-information-flow.md).
func TestCorpusCoverage_OutputUnderMCPCap(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	const softCapBytes = 28 * 1024 // 28 KB; 4 KB margin under the 32 KB MCP cap.

	for _, fx := range coverageFixtures() {
		t.Run(fx.Name, func(t *testing.T) {
			t.Parallel()
			bodies, err := SynthesizeBodies(fx.Envelope, corpus)
			if err != nil {
				t.Fatalf("Synthesize: %v", err)
			}
			combined := strings.Join(bodies, "\n\n---\n\n")
			rendered := RenderStatus(Response{Envelope: fx.Envelope, Guidance: bodies})
			wireFrame := mcpWireFrameSize(rendered)

			// Info-only Logf (no assertion) so each phase's trim trajectory
			// is visible in test output. The wire-frame number is the
			// metric Claude Code's stdio cap actually applies to;
			// body-join is what this test asserts on for historical
			// reasons (the synthesizer-only metric pre-dated this gate).
			t.Logf("fixture %q: body-join %d B, wire-frame %d B (atoms=%d, render=%d B)",
				fx.Name, len(combined), wireFrame, len(bodies), len(rendered))

			if _, known := knownOverflowFixtures[fx.Name]; known {
				return
			}
			if size := len(combined); size > softCapBytes {
				t.Errorf("fixture %q: synthesized output %d bytes exceeds soft cap %d bytes (32 KB MCP cap with 4 KB margin)\nMatched atoms: %d\nFirst 200 chars: %s",
					fx.Name, size, softCapBytes, len(bodies), combined[:min(200, size)])
			}
		})
	}
}

// mcpWireFrameSize returns the byte count of the JSON-RPC frame Claude
// Code's stdio reader sees for a tool response carrying `text` as its
// single TextContent. Reproduces the wire shape from
// github.com/modelcontextprotocol/go-sdk@v1.5.0:
//
//	{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"<text>"}]}}\n
//
// Computed locally so workflow/ doesn't take a hard dep on the MCP SDK.
// The encoding mirrors the SDK precisely: json.Encoder + SetEscapeHTML(false)
// + one trailing '\n'. See cmd/atomsize_probe/main.go::wireFrameBytes for
// the SDK-using equivalent and the Codex round #1 verification of the
// shape (plans/atom-corpus-context-trim-2026-04-26.md §17.1).
func mcpWireFrameSize(text string) int {
	type wireText struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type wireResult struct {
		Content []wireText `json:"content"`
	}
	resultJSON, err := json.Marshal(wireResult{Content: []wireText{{Type: "text", Text: text}}})
	if err != nil {
		return -1
	}
	wire := struct {
		Jsonrpc string          `json:"jsonrpc"`
		ID      any             `json:"id,omitempty"`
		Result  json.RawMessage `json:"result,omitempty"`
	}{Jsonrpc: "2.0", ID: 1, Result: resultJSON}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(wire); err != nil {
		return -1
	}
	return buf.Len()
}

// TestCorpusCoverage_KnownOverflows_StillOverflow guards the audit
// allowlist: every fixture in knownOverflowFixtures must STILL exceed
// the soft cap. The moment a corpus trim brings one under the ceiling,
// this test fails and forces removal from the allowlist (one-way
// ratchet — the allowlist can only shrink). Without this companion
// the allowlist would silently grow into a permanent escape hatch.
func TestCorpusCoverage_KnownOverflows_StillOverflow(t *testing.T) {
	t.Parallel()
	if len(knownOverflowFixtures) == 0 {
		t.Skip("no known overflow fixtures to verify")
	}
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	const softCapBytes = 28 * 1024

	fixturesByName := make(map[string]coverageFixture, len(coverageFixtures()))
	for _, fx := range coverageFixtures() {
		fixturesByName[fx.Name] = fx
	}

	for name, rationale := range knownOverflowFixtures {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fx, ok := fixturesByName[name]
			if !ok {
				t.Fatalf("knownOverflowFixtures lists %q but no such coverage fixture exists — remove the stale entry", name)
			}
			bodies, err := SynthesizeBodies(fx.Envelope, corpus)
			if err != nil {
				t.Fatalf("Synthesize: %v", err)
			}
			combined := strings.Join(bodies, "\n\n---\n\n")
			if size := len(combined); size <= softCapBytes {
				t.Errorf("fixture %q now fits under %d bytes (current: %d). Remove it from knownOverflowFixtures — the soft cap test will then enforce it forward.\nRationale at acknowledgement: %s",
					name, softCapBytes, size, rationale)
			}
		})
	}
}

func TestCorpusCoverage_CompactionSafe(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	for _, fx := range coverageFixtures() {
		t.Run(fx.Name, func(t *testing.T) {
			t.Parallel()
			first, err := SynthesizeBodies(fx.Envelope, corpus)
			if err != nil {
				t.Fatalf("Synthesize first: %v", err)
			}
			firstJoined := strings.Join(first, "||")
			for i := range 10 {
				got, err := SynthesizeBodies(fx.Envelope, corpus)
				if err != nil {
					t.Fatalf("Synthesize iter %d: %v", i, err)
				}
				if strings.Join(got, "||") != firstJoined {
					t.Fatalf("fixture %q iter %d: non-deterministic output", fx.Name, i)
				}
			}
		})
	}
}
