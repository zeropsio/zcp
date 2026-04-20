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
					RuntimeClass: RuntimeDynamic, Mode: ModeStandard,
					Bootstrapped: true, StageHostname: "appstage",
				}},
			},
			MustContain: []string{
				`zerops_workflow action="start" workflow="develop"`,
				"auto-closes",
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
					RuntimeClass: RuntimeDynamic,
				}},
			},
			MustContain: []string{
				`zerops_workflow action="start" workflow="bootstrap" intent="adopt`,
				"adopt route",
			},
		},
		{
			Name: "bootstrap_classic_discover_dynamic",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: RuntimeDynamic, Mode: ModeStandard,
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
					RuntimeClass: RuntimeDynamic, Mode: ModeStandard,
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
					RuntimeClass: RuntimeDynamic, Mode: ModeStandard,
				}},
				Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteAdopt, Step: StepDiscover},
			},
			MustContain: []string{
				"Adopting existing services",
				"ServiceMeta",
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
					RuntimeClass: RuntimeDynamic, Mode: ModeDev,
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
					RuntimeClass: RuntimeDynamic, Mode: ModeStandard,
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
					RuntimeClass: RuntimeDynamic, Mode: ModeSimple,
					Strategy: "manual", Bootstrapped: true, Deployed: true,
				}},
			},
			MustContain: []string{
				"zerops_deploy",
				"Env var channels",
				"auto-restarts** the affected service",
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
					RuntimeClass: RuntimeDynamic, Mode: ModeStandard,
					Strategy: StrategyUnset, Bootstrapped: true, Deployed: true,
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
					RuntimeClass: RuntimeDynamic, Mode: ModeDev,
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
					RuntimeClass: RuntimeDynamic, Mode: ModeDev,
				}},
			},
			MustContain: []string{
				"auto-closed",
				`action="close"`,
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
					RuntimeClass: RuntimeStatic, Mode: ModeDev,
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
					RuntimeClass: RuntimeDynamic, Mode: ModeStandard,
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
					RuntimeClass: RuntimeDynamic, Mode: ModeStandard,
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
					RuntimeClass: RuntimeDynamic, Mode: ModeSimple,
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
					RuntimeClass: RuntimeDynamic, Mode: ModeStandard,
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
					RuntimeClass: RuntimeDynamic, Mode: ModeStandard,
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
					RuntimeClass: RuntimeDynamic, Mode: ModeStandard,
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
					RuntimeClass: RuntimeDynamic, Mode: ModeDev,
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
					RuntimeClass: RuntimeImplicitWeb, Mode: ModeSimple,
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
						RuntimeClass: RuntimeImplicitWeb, Mode: ModeSimple,
						Strategy: "push-dev",
					},
					{
						Hostname: "db", TypeVersion: "postgresql@18",
						RuntimeClass: RuntimeManaged,
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
					RuntimeClass: RuntimeStatic, Mode: ModeDev,
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

// pipelineCoverageFixtures covers cicd-active and export-active phases.
// Both are phase-only axes — the atoms filter purely on phase, not on
// services/modes/strategies, so a single envelope per phase is enough.
func pipelineCoverageFixtures() []coverageFixture {
	return []coverageFixture{
		{
			Name: "cicd_active",
			Envelope: StateEnvelope{
				Phase:       PhaseCICDActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: RuntimeDynamic, Mode: ModeStandard,
					StageHostname: "appstage",
					Strategy:      "push-git",
				}},
			},
			MustContain: []string{
				"GIT_TOKEN",
				"ZEROPS_TOKEN",
				"zcli push",
				".netrc",
			},
		},
		{
			Name: "export_active",
			Envelope: StateEnvelope{
				Phase:       PhaseExportActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: RuntimeDynamic, Mode: ModeStandard,
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
			bodies, err := Synthesize(fx.Envelope, corpus)
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

func TestCorpusCoverage_CompactionSafe(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	for _, fx := range coverageFixtures() {
		t.Run(fx.Name, func(t *testing.T) {
			t.Parallel()
			first, err := Synthesize(fx.Envelope, corpus)
			if err != nil {
				t.Fatalf("Synthesize first: %v", err)
			}
			firstJoined := strings.Join(first, "||")
			for i := range 10 {
				got, err := Synthesize(fx.Envelope, corpus)
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
