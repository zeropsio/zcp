package workflow

import (
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
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
				Runtimes:     []topology.RuntimeClass{topology.RuntimeDynamic},
				Environments: []Environment{EnvContainer},
			},
			Body: "SSH into {hostname} and run {start-command}.",
		},
		{
			ID: "develop-dynamic-local", Priority: 2,
			Axes: AxisVector{
				Phases:       []Phase{PhaseDevelopActive},
				Runtimes:     []topology.RuntimeClass{topology.RuntimeDynamic},
				Environments: []Environment{EnvLocal},
			},
			Body: "From local, SSH into {hostname}.",
		},
		{
			ID: "develop-push-git", Priority: 3,
			Axes: AxisVector{
				Phases:     []Phase{PhaseDevelopActive},
				Strategies: []topology.DeployStrategy{"push-git"},
			},
			Body: "Push to git.",
		},
		{
			ID: "develop-dev-mode", Priority: 4,
			Axes: AxisVector{
				Phases: []Phase{PhaseDevelopActive},
				Modes:  []topology.Mode{topology.ModeDev},
			},
			Body: "Dev mode rules.",
		},
	}
}

func developEnvelope(env Environment, mode topology.Mode, strategy topology.DeployStrategy, runtime topology.RuntimeClass) StateEnvelope {
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
			env:     developEnvelope(EnvContainer, topology.ModeDev, "push-dev", topology.RuntimeDynamic),
			wantIDs: []string{"develop-dynamic-container", "develop-dev-mode"},
			wantNone: []string{
				"idle-entry", "develop-dynamic-local", "develop-push-git",
			},
		},
		{
			name:    "develop_local_dynamic_pushgit_dev",
			env:     developEnvelope(EnvLocal, topology.ModeDev, "push-git", topology.RuntimeDynamic),
			wantIDs: []string{"develop-dynamic-local", "develop-push-git", "develop-dev-mode"},
			wantNone: []string{
				"idle-entry", "develop-dynamic-container",
			},
		},
		{
			name:    "develop_container_static_pushdev_stage",
			env:     developEnvelope(EnvContainer, topology.ModeStage, "push-dev", topology.RuntimeStatic),
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

// TestSynthesize_DeployStateFilter covers the never-deployed / deployed axis
// used by the develop first-deploy branch. An atom that declares
// deployStates: [never-deployed] must only fire when at least one bootstrapped
// service has not yet been deployed (no FirstDeployedAt). Deployed-only atoms
// mirror the inverse for the edit-loop branch.
func TestSynthesize_DeployStateFilter(t *testing.T) {
	t.Parallel()

	corpus := []KnowledgeAtom{
		{
			ID: "first-deploy-scaffold", Priority: 2,
			Axes: AxisVector{
				Phases:       []Phase{PhaseDevelopActive},
				DeployStates: []DeployState{DeployStateNeverDeployed},
			},
			Body: "Scaffold and first deploy.",
		},
		{
			ID: "edit-loop", Priority: 2,
			Axes: AxisVector{
				Phases:       []Phase{PhaseDevelopActive},
				DeployStates: []DeployState{DeployStateDeployed},
			},
			Body: "Edit-deploy-verify loop.",
		},
	}

	neverDeployed := StateEnvelope{
		Phase: PhaseDevelopActive,
		Services: []ServiceSnapshot{{
			Hostname:     "appdev",
			RuntimeClass: topology.RuntimeDynamic,
			Bootstrapped: true,
			Deployed:     false,
		}},
	}
	got, err := Synthesize(neverDeployed, corpus)
	if err != nil {
		t.Fatalf("Synthesize never-deployed: %v", err)
	}
	joined := strings.Join(got, "\n---\n")
	if !strings.Contains(joined, "Scaffold and first deploy.") {
		t.Errorf("never-deployed envelope: expected first-deploy atom, got: %s", joined)
	}
	if strings.Contains(joined, "Edit-deploy-verify loop.") {
		t.Errorf("never-deployed envelope: edit-loop atom should NOT appear, got: %s", joined)
	}

	deployed := StateEnvelope{
		Phase: PhaseDevelopActive,
		Services: []ServiceSnapshot{{
			Hostname:     "appdev",
			RuntimeClass: topology.RuntimeDynamic,
			Bootstrapped: true,
			Deployed:     true,
		}},
	}
	got, err = Synthesize(deployed, corpus)
	if err != nil {
		t.Fatalf("Synthesize deployed: %v", err)
	}
	joined = strings.Join(got, "\n---\n")
	if !strings.Contains(joined, "Edit-deploy-verify loop.") {
		t.Errorf("deployed envelope: expected edit-loop atom, got: %s", joined)
	}
	if strings.Contains(joined, "Scaffold and first deploy.") {
		t.Errorf("deployed envelope: first-deploy atom should NOT appear, got: %s", joined)
	}
}

// TestSynthesize_ServiceScopedAxesRequireSameService pins the conjunction
// invariant: an atom declaring multiple service-scoped axes (modes,
// strategies, runtimes, deployStates) fires only when ONE service satisfies
// all of them. Disjunction across services would fire atoms whose body
// references a service the atom isn't semantically about — e.g. strategy-
// review surfacing because service A is deployed and (different) service B
// has unset strategy.
func TestSynthesize_ServiceScopedAxesRequireSameService(t *testing.T) {
	t.Parallel()

	corpus := []KnowledgeAtom{
		{
			ID:   "two-axes",
			Axes: AxisVector{Phases: []Phase{PhaseDevelopActive}, DeployStates: []DeployState{DeployStateDeployed}, Strategies: []topology.DeployStrategy{topology.StrategyUnset}},
			Body: "Two-axis atom.",
		},
	}

	// Mixed envelope: A is deployed with push-dev, B is never-deployed with
	// unset. No single service satisfies deployed+unset → atom must NOT fire.
	mixed := StateEnvelope{
		Phase: PhaseDevelopActive,
		Services: []ServiceSnapshot{
			{Hostname: "a", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Deployed: true, Strategy: topology.StrategyPushDev},
			{Hostname: "b", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Deployed: false, Strategy: topology.StrategyUnset},
		},
	}
	got, err := Synthesize(mixed, corpus)
	if err != nil {
		t.Fatalf("Synthesize mixed: %v", err)
	}
	if strings.Contains(strings.Join(got, "\n"), "Two-axis atom.") {
		t.Error("mixed envelope: two-axis atom must not fire when no single service satisfies both axes")
	}

	// Single service satisfying both axes → atom fires.
	match := StateEnvelope{
		Phase: PhaseDevelopActive,
		Services: []ServiceSnapshot{
			{Hostname: "a", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Deployed: true, Strategy: topology.StrategyUnset},
		},
	}
	got, err = Synthesize(match, corpus)
	if err != nil {
		t.Fatalf("Synthesize match: %v", err)
	}
	if !strings.Contains(strings.Join(got, "\n"), "Two-axis atom.") {
		t.Error("match envelope: two-axis atom must fire when one service satisfies both axes")
	}
}

func TestSynthesize_PrioritySort(t *testing.T) {
	t.Parallel()

	env := developEnvelope(EnvContainer, topology.ModeDev, "push-git", topology.RuntimeDynamic)
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

	env := developEnvelope(EnvContainer, topology.ModeDev, "push-git", topology.RuntimeDynamic)
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
				Runtimes: []topology.RuntimeClass{topology.RuntimeDynamic},
			},
			Body: "Connect to {hostname}. Stage pair is {stage-hostname}.",
		},
	}
	env := StateEnvelope{
		Phase:       PhaseDevelopActive,
		Environment: EnvLocal,
		Services: []ServiceSnapshot{{
			Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic, StageHostname: "appstage", Mode: topology.ModeDev,
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

// TestSynthesize_LocalModeAtomsFireForAllRoutes is the regression guard for
// the Option A refactor that removed `routes: [classic]` from the local-mode
// atoms (bootstrap-discover-local, bootstrap-provision-local). Before the
// refactor, recipe-route and adopt-route bootstraps on a local environment
// missed the local-specific guidance entirely — the atom filter excluded
// them. After the refactor, any local-environment bootstrap gets the local
// topology rules regardless of route.
//
// This test fails if someone adds `routes:` back to either atom.
func TestSynthesize_LocalModeAtomsFireForAllRoutes(t *testing.T) {
	t.Parallel()

	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	// The local atoms under test. Discover step for bootstrap-discover-local,
	// provision step for bootstrap-provision-local.
	targets := []struct {
		atomID string
		step   string
	}{
		{"bootstrap-discover-local", StepDiscover},
		{"bootstrap-provision-local", StepProvision},
	}

	for _, target := range targets {
		atom := findAtom(corpus, target.atomID)
		if atom.ID == "" {
			t.Fatalf("atom %s missing from corpus", target.atomID)
		}
		// Sanity: atom should declare environments: [local] and no route filter.
		if len(atom.Axes.Routes) > 0 {
			t.Errorf("%s has routes filter %v — local-mode atoms must be route-agnostic",
				target.atomID, atom.Axes.Routes)
		}

		routes := []BootstrapRoute{
			BootstrapRouteClassic,
			BootstrapRouteRecipe,
			BootstrapRouteAdopt,
		}
		for _, route := range routes {
			t.Run(target.atomID+"_"+string(route), func(t *testing.T) {
				t.Parallel()
				env := StateEnvelope{
					Phase:       PhaseBootstrapActive,
					Environment: EnvLocal,
					Bootstrap: &BootstrapSessionSummary{
						Route: route,
						Step:  target.step,
					},
					Services: []ServiceSnapshot{{
						Hostname:     "appdev",
						RuntimeClass: topology.RuntimeDynamic,
						Mode:         topology.ModeDev,
					}},
				}
				got, err := Synthesize(env, corpus)
				if err != nil {
					t.Fatalf("Synthesize: %v", err)
				}
				joined := strings.Join(got, "\n")
				if !strings.Contains(joined, atom.Body[:min(60, len(atom.Body))]) {
					t.Errorf("%s not in synthesis output for local+%s — did someone re-add routes: filter?",
						target.atomID, route)
				}
			})
		}
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
				Runtimes: []topology.RuntimeClass{topology.RuntimeDynamic},
			},
			Body: "Run `{start-command}` on {hostname}.",
		},
	}
	env := StateEnvelope{
		Phase:       PhaseDevelopActive,
		Environment: EnvLocal,
		Services:    []ServiceSnapshot{{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic}},
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

// TestSynthesizeStrategySetup_LocalEnv pins the wrapper's behavior:
// hand it a runtime + snapshots, get back guidance built from a
// StateEnvelope{Phase=PhaseStrategySetup, Environment=local, Services}.
// The wrapper exists so tools/ don't construct envelopes inline; this
// test pins both halves of that contract — the envelope shape AND the
// non-empty atom output for the standard push-git setup case.
func TestSynthesizeStrategySetup_LocalEnv(t *testing.T) {
	t.Parallel()
	snaps := []ServiceSnapshot{{
		Hostname:     "appdev",
		Bootstrapped: true,
		Mode:         topology.PlanModeDev,
		Strategy:     topology.StrategyPushGit,
		Trigger:      topology.TriggerWebhook,
	}}
	got, err := SynthesizeStrategySetup(runtime.Info{InContainer: false}, snaps)
	if err != nil {
		t.Fatalf("SynthesizeStrategySetup: %v", err)
	}
	if got == "" {
		t.Fatal("expected non-empty guidance for push-git/webhook setup")
	}
}

// TestSynthesizeStrategySetup_ContainerEnv same wrapper, container env —
// a different set of atoms fires (container-specific push setup) so the
// output must differ from the local case. If both come back identical,
// the Environment axis isn't reaching the synthesizer.
func TestSynthesizeStrategySetup_ContainerEnv(t *testing.T) {
	t.Parallel()
	snaps := []ServiceSnapshot{{
		Hostname:     "appdev",
		Bootstrapped: true,
		Mode:         topology.PlanModeDev,
		Strategy:     topology.StrategyPushGit,
		Trigger:      topology.TriggerWebhook,
	}}
	local, err := SynthesizeStrategySetup(runtime.Info{InContainer: false}, snaps)
	if err != nil {
		t.Fatalf("local: %v", err)
	}
	container, err := SynthesizeStrategySetup(runtime.Info{InContainer: true}, snaps)
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	if local == container {
		t.Errorf("environment axis dropped: local and container guidance are identical")
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
