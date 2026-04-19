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
	return []coverageFixture{
		{
			Name: "idle_empty_project",
			Envelope: StateEnvelope{
				Phase:       PhaseIdle,
				Environment: EnvContainer,
			},
			MustContain: []string{
				`zerops_workflow action="start" workflow="bootstrap"`,
				"one sentence",
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
			Name: "bootstrap_classic_generate_standard",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: RuntimeDynamic, Mode: ModeStandard,
					StageHostname: "appstage",
				}},
				Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteClassic, Step: StepGenerate},
			},
			MustContain: []string{
				"zsc noop --silent",
				"deployFiles: [.]",
				"0.0.0.0",
			},
		},
		{
			Name: "bootstrap_classic_generate_simple",
			Envelope: StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "app", TypeVersion: "nginx@1",
					RuntimeClass: RuntimeDynamic, Mode: ModeSimple,
				}},
				Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteClassic, Step: StepGenerate},
			},
			MustContain: []string{
				"REAL start command",
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
		{
			Name: "develop_push_dev_dev_container",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: RuntimeDynamic, Mode: ModeDev,
					Strategy: "push-dev",
				}},
			},
			MustContain: []string{
				"Push-Dev Deploy Strategy",
				"SSH",
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
					Strategy:      "push-git",
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
					Strategy: "manual",
				}},
			},
			MustContain: []string{
				"zerops_deploy",
			},
		},
		{
			Name: "develop_strategy_unset",
			Envelope: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: RuntimeDynamic, Mode: ModeStandard,
					Strategy: "unset",
				}},
			},
			MustContain: []string{
				"Strategy selection required",
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
					Strategy: "push-dev",
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
