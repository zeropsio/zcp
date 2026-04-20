package workflow

import (
	"strings"
	"testing"
	"time"
)

// corpus returns a small in-memory corpus for synthesizer tests. Keeps the
// tests independent of the embedded atoms whose content may drift.
func synthCorpus() []KnowledgeAtom {
	return []KnowledgeAtom{
		{
			ID: "idle-entry", Priority: 1,
			Axes: AxisVector{Phases: []Phase{PhaseIdle}},
			Body: "Start with status.",
		},
		{
			ID: "develop-dynamic-container", Priority: 2,
			Axes: AxisVector{
				Phases:       []Phase{PhaseDevelopActive},
				Runtimes:     []RuntimeClass{RuntimeDynamic},
				Environments: []Environment{EnvContainer},
			},
			Body: "SSH into {hostname} and run {start-command}.",
		},
		{
			ID: "develop-dynamic-local", Priority: 2,
			Axes: AxisVector{
				Phases:       []Phase{PhaseDevelopActive},
				Runtimes:     []RuntimeClass{RuntimeDynamic},
				Environments: []Environment{EnvLocal},
			},
			Body: "From local, SSH into {hostname}.",
		},
		{
			ID: "develop-push-git", Priority: 3,
			Axes: AxisVector{
				Phases:     []Phase{PhaseDevelopActive},
				Strategies: []DeployStrategy{"push-git"},
			},
			Body: "Push to git.",
		},
		{
			ID: "develop-dev-mode", Priority: 4,
			Axes: AxisVector{
				Phases: []Phase{PhaseDevelopActive},
				Modes:  []Mode{ModeDev},
			},
			Body: "Dev mode rules.",
		},
	}
}

func developEnvelope(env Environment, mode Mode, strategy DeployStrategy, runtime RuntimeClass) StateEnvelope {
	return StateEnvelope{
		Phase:       PhaseDevelopActive,
		Environment: env,
		Services: []ServiceSnapshot{{
			Hostname:     "appdev",
			RuntimeClass: runtime,
			Mode:         mode,
			Strategy:     strategy,
		}},
		Generated: time.Date(2026, 4, 19, 0, 0, 0, 0, time.UTC),
	}
}

func TestSynthesize_AxisFiltering(t *testing.T) {
	t.Parallel()

	corpus := synthCorpus()
	cases := []struct {
		name     string
		env      StateEnvelope
		wantIDs  []string
		wantNone []string
	}{
		{
			name:    "idle_only",
			env:     StateEnvelope{Phase: PhaseIdle, Environment: EnvLocal},
			wantIDs: []string{"idle-entry"},
			wantNone: []string{
				"develop-dynamic-container", "develop-dynamic-local",
				"develop-push-git", "develop-dev-mode",
			},
		},
		{
			name:    "develop_container_dynamic_pushdev_dev",
			env:     developEnvelope(EnvContainer, ModeDev, "push-dev", RuntimeDynamic),
			wantIDs: []string{"develop-dynamic-container", "develop-dev-mode"},
			wantNone: []string{
				"idle-entry", "develop-dynamic-local", "develop-push-git",
			},
		},
		{
			name:    "develop_local_dynamic_pushgit_dev",
			env:     developEnvelope(EnvLocal, ModeDev, "push-git", RuntimeDynamic),
			wantIDs: []string{"develop-dynamic-local", "develop-push-git", "develop-dev-mode"},
			wantNone: []string{
				"idle-entry", "develop-dynamic-container",
			},
		},
		{
			name:    "develop_container_static_pushdev_stage",
			env:     developEnvelope(EnvContainer, ModeStage, "push-dev", RuntimeStatic),
			wantIDs: []string{},
			wantNone: []string{
				"idle-entry", "develop-dynamic-container", "develop-dynamic-local",
				"develop-push-git", "develop-dev-mode",
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Synthesize(tt.env, corpus)
			if err != nil {
				t.Fatalf("Synthesize: %v", err)
			}
			joined := strings.Join(got, "\n---\n")
			for _, id := range tt.wantIDs {
				atom := findAtom(corpus, id)
				want := bodyAfterSub(atom, tt.env)
				if !strings.Contains(joined, want) {
					t.Errorf("expected atom %s body %q in output, got: %s", id, want, joined)
				}
			}
			for _, id := range tt.wantNone {
				atom := findAtom(corpus, id)
				want := bodyAfterSub(atom, tt.env)
				if strings.Contains(joined, want) {
					t.Errorf("atom %s should NOT appear, got: %s", id, joined)
				}
			}
		})
	}
}

// bodyAfterSub returns the atom body after the same placeholder substitution
// that Synthesize applies. Lets axis-filter assertions compare against what
// actually lands in the synthesized output.
func bodyAfterSub(atom KnowledgeAtom, env StateEnvelope) string {
	hostname, stageHostname := primaryHostnames(env.Services)
	replacer := strings.NewReplacer(
		"{hostname}", hostname,
		"{stage-hostname}", stageHostname,
		"{project-name}", env.Project.Name,
	)
	return firstLine(replacer.Replace(atom.Body))
}

func TestSynthesize_PrioritySort(t *testing.T) {
	t.Parallel()

	env := developEnvelope(EnvContainer, ModeDev, "push-git", RuntimeDynamic)
	got, err := Synthesize(env, synthCorpus())
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	expectedOrder := []string{
		"SSH into appdev", // priority 2, container
		"Push to git.",    // priority 3
		"Dev mode rules.", // priority 4
	}
	if len(got) != len(expectedOrder) {
		t.Fatalf("want %d atoms, got %d: %v", len(expectedOrder), len(got), got)
	}
	for i, want := range expectedOrder {
		if !strings.Contains(got[i], want) {
			t.Errorf("position %d: expected %q, got %q", i, want, got[i])
		}
	}
}

func TestSynthesize_CompactionSafe(t *testing.T) {
	t.Parallel()

	env := developEnvelope(EnvContainer, ModeDev, "push-git", RuntimeDynamic)
	corpus := synthCorpus()
	first, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	for i := range 10 {
		got, err := Synthesize(env, corpus)
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if strings.Join(got, "|") != strings.Join(first, "|") {
			t.Fatalf("iter %d output differs; non-deterministic synthesize", i)
		}
	}
}

func TestSynthesize_PlaceholderSubstitution(t *testing.T) {
	t.Parallel()

	corpus := []KnowledgeAtom{
		{
			ID: "hostname-atom", Priority: 1,
			Axes: AxisVector{
				Phases:   []Phase{PhaseDevelopActive},
				Runtimes: []RuntimeClass{RuntimeDynamic},
			},
			Body: "Connect to {hostname}. Stage pair is {stage-hostname}.",
		},
	}
	env := StateEnvelope{
		Phase:       PhaseDevelopActive,
		Environment: EnvLocal,
		Services: []ServiceSnapshot{{
			Hostname: "appdev", RuntimeClass: RuntimeDynamic, StageHostname: "appstage", Mode: ModeDev,
		}},
	}
	got, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 atom, got %d", len(got))
	}
	if !strings.Contains(got[0], "Connect to appdev") {
		t.Errorf("hostname not substituted: %s", got[0])
	}
	if !strings.Contains(got[0], "Stage pair is appstage") {
		t.Errorf("stage hostname not substituted: %s", got[0])
	}
}

func TestSynthesize_RouteAxisFiltering(t *testing.T) {
	t.Parallel()

	corpus := []KnowledgeAtom{
		{
			ID: "recipe-only", Priority: 1,
			Axes: AxisVector{
				Phases: []Phase{PhaseBootstrapActive},
				Routes: []BootstrapRoute{BootstrapRouteRecipe},
			},
			Body: "Recipe-only guidance.",
		},
		{
			ID: "classic-or-adopt", Priority: 2,
			Axes: AxisVector{
				Phases: []Phase{PhaseBootstrapActive},
				Routes: []BootstrapRoute{BootstrapRouteClassic, BootstrapRouteAdopt},
			},
			Body: "Classic/adopt guidance.",
		},
		{
			ID: "route-wildcard", Priority: 3,
			Axes: AxisVector{Phases: []Phase{PhaseBootstrapActive}},
			Body: "Fires on any route.",
		},
	}

	cases := []struct {
		name       string
		bootstrap  *BootstrapSessionSummary
		wantIDs    []string
		wantNoneID []string
	}{
		{
			name:       "recipe_route_fires_recipe_and_wildcard",
			bootstrap:  &BootstrapSessionSummary{Route: BootstrapRouteRecipe},
			wantIDs:    []string{"recipe-only", "route-wildcard"},
			wantNoneID: []string{"classic-or-adopt"},
		},
		{
			name:       "classic_route_fires_classic_and_wildcard",
			bootstrap:  &BootstrapSessionSummary{Route: BootstrapRouteClassic},
			wantIDs:    []string{"classic-or-adopt", "route-wildcard"},
			wantNoneID: []string{"recipe-only"},
		},
		{
			name:       "adopt_route_fires_adopt_and_wildcard",
			bootstrap:  &BootstrapSessionSummary{Route: BootstrapRouteAdopt},
			wantIDs:    []string{"classic-or-adopt", "route-wildcard"},
			wantNoneID: []string{"recipe-only"},
		},
		{
			name:       "no_bootstrap_only_fires_wildcard",
			bootstrap:  nil,
			wantIDs:    []string{"route-wildcard"},
			wantNoneID: []string{"recipe-only", "classic-or-adopt"},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := StateEnvelope{
				Phase:       PhaseBootstrapActive,
				Environment: EnvLocal,
				Bootstrap:   tt.bootstrap,
			}
			got, err := Synthesize(env, corpus)
			if err != nil {
				t.Fatalf("Synthesize: %v", err)
			}
			joined := strings.Join(got, "\n")
			for _, id := range tt.wantIDs {
				atom := findAtom(corpus, id)
				if !strings.Contains(joined, atom.Body) {
					t.Errorf("expected atom %s in output, got: %s", id, joined)
				}
			}
			for _, id := range tt.wantNoneID {
				atom := findAtom(corpus, id)
				if strings.Contains(joined, atom.Body) {
					t.Errorf("atom %s should NOT appear, got: %s", id, joined)
				}
			}
		})
	}
}

func TestSynthesize_UnknownPlaceholderErrors(t *testing.T) {
	t.Parallel()

	corpus := []KnowledgeAtom{
		{
			ID: "bad", Priority: 1,
			Axes: AxisVector{Phases: []Phase{PhaseIdle}},
			Body: "Unknown {weird-token} here.",
		},
	}
	env := StateEnvelope{Phase: PhaseIdle, Environment: EnvLocal}
	_, err := Synthesize(env, corpus)
	if err == nil {
		t.Fatal("expected error on unknown placeholder")
	}
	if !strings.Contains(err.Error(), "weird-token") {
		t.Errorf("error should name the bad token, got: %v", err)
	}
}

func TestSynthesize_AllowsStartCommandPlaceholder(t *testing.T) {
	t.Parallel()

	corpus := []KnowledgeAtom{
		{
			ID: "run", Priority: 1,
			Axes: AxisVector{
				Phases:   []Phase{PhaseDevelopActive},
				Runtimes: []RuntimeClass{RuntimeDynamic},
			},
			Body: "Run `{start-command}` on {hostname}.",
		},
	}
	env := StateEnvelope{
		Phase:       PhaseDevelopActive,
		Environment: EnvLocal,
		Services:    []ServiceSnapshot{{Hostname: "appdev", RuntimeClass: RuntimeDynamic}},
	}
	got, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if !strings.Contains(got[0], "{start-command}") {
		t.Errorf("start-command should survive untouched: %s", got[0])
	}
	if !strings.Contains(got[0], "appdev") {
		t.Errorf("hostname still expected substituted: %s", got[0])
	}
}

func TestLoadAtomCorpus_EmbeddedParses(t *testing.T) {
	t.Parallel()

	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	if len(corpus) == 0 {
		t.Fatal("expected at least one atom")
	}
	seen := make(map[string]bool, len(corpus))
	for _, atom := range corpus {
		if seen[atom.ID] {
			t.Errorf("duplicate atom id: %s", atom.ID)
		}
		seen[atom.ID] = true
		if len(atom.Axes.Phases) == 0 {
			t.Errorf("atom %s missing phases axis", atom.ID)
		}
	}
}

func findAtom(corpus []KnowledgeAtom, id string) KnowledgeAtom {
	for _, a := range corpus {
		if a.ID == id {
			return a
		}
	}
	return KnowledgeAtom{}
}

func firstLine(body string) string {
	if i := strings.IndexByte(body, '\n'); i > 0 {
		return body[:i]
	}
	return body
}
