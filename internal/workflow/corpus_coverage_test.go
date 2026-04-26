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
			Name: "idle_orphan_only",
			Envelope: StateEnvelope{
				Phase:        PhaseIdle,
				Environment:  EnvContainer,
				IdleScenario: IdleOrphan,
				Services:     nil,
				OrphanMetas: []OrphanMeta{{
					Hostname:         "ghostdev",
					StageHostname:    "ghoststage",
					BootstrapSession: "sess-deadbeef",
					Reason:           OrphanReasonIncompleteLost,
				}},
			},
			MustContain: []string{
				`zerops_workflow action="reset" workflow="bootstrap"`,
				"orphan",
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
				"Read and edit directly on the mount",
				"HTTP diagnostics",
				"zerops_verify",
				"VERDICT",
				"agent-browser",
				// Phase-1 additions — load-bearing awareness + KB pointers.
				"Deploy strategy — current + how to change",
				`action="strategy"`,
				"edit → deploy",
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
				"Closing the task (local)",
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
				"Closing the task (local)",
				`zerops_deploy targetService="appstage"`,
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
				"GIT_TOKEN",
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
// a measured byte count at the time of acknowledgement.
var knownOverflowFixtures = map[string]string{
	"develop_first_deploy_standard_container":          "40228 bytes (2026-04-26): standard-mode first-deploy renders dev+stage atoms × runtime + container atoms. Trim plan: pending.",
	"develop_first_deploy_implicit_webserver_standard": "43447 bytes (2026-04-26): standard-mode first-deploy + implicit-webserver atoms × dev+stage pair. Trim plan: pending.",
}

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
		if _, known := knownOverflowFixtures[fx.Name]; known {
			continue
		}
		t.Run(fx.Name, func(t *testing.T) {
			t.Parallel()
			bodies, err := SynthesizeBodies(fx.Envelope, corpus)
			if err != nil {
				t.Fatalf("Synthesize: %v", err)
			}
			combined := strings.Join(bodies, "\n\n---\n\n")
			if size := len(combined); size > softCapBytes {
				t.Errorf("fixture %q: synthesized output %d bytes exceeds soft cap %d bytes (32 KB MCP cap with 4 KB margin)\nMatched atoms: %d\nFirst 200 chars: %s",
					fx.Name, size, softCapBytes, len(bodies), combined[:min(200, size)])
			}
		})
	}
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
