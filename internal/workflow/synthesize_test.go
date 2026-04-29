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
				Phases:           []Phase{PhaseDevelopActive},
				CloseDeployModes: []topology.CloseDeployMode{topology.CloseModeGitPush},
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

func developEnvelope(env Environment, mode topology.Mode, closeMode topology.CloseDeployMode, runtime topology.RuntimeClass) StateEnvelope {
	return StateEnvelope{
		Phase:       PhaseDevelopActive,
		Environment: env,
		Services: []ServiceSnapshot{{
			Hostname:        "appdev",
			RuntimeClass:    runtime,
			Mode:            mode,
			CloseDeployMode: closeMode,
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
			env:     developEnvelope(EnvContainer, topology.ModeDev, topology.CloseModeAuto, topology.RuntimeDynamic),
			wantIDs: []string{"develop-dynamic-container", "develop-dev-mode"},
			wantNone: []string{
				"idle-entry", "develop-dynamic-local", "develop-push-git",
			},
		},
		{
			name:    "develop_local_dynamic_pushgit_dev",
			env:     developEnvelope(EnvLocal, topology.ModeDev, topology.CloseModeGitPush, topology.RuntimeDynamic),
			wantIDs: []string{"develop-dynamic-local", "develop-push-git", "develop-dev-mode"},
			wantNone: []string{
				"idle-entry", "develop-dynamic-container",
			},
		},
		{
			name:    "develop_container_static_pushdev_stage",
			env:     developEnvelope(EnvContainer, topology.ModeStage, topology.CloseModeAuto, topology.RuntimeStatic),
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
			got, err := SynthesizeBodies(tt.env, corpus)
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
	got, err := SynthesizeBodies(neverDeployed, corpus)
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
	got, err = SynthesizeBodies(deployed, corpus)
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

// TestSynthesize_RenderTimeBodyDedup pins the post-substitution dedup
// added in Phase 4 of atom-corpus-context-trim. Two renders of the same
// atom that produce byte-identical bodies (e.g. service-scoped axis but
// no per-service placeholder in the body) collapse to one render. Atoms
// whose post-substitution bodies DIFFER per service render N× as before.
func TestSynthesize_RenderTimeBodyDedup(t *testing.T) {
	t.Parallel()

	corpus := []KnowledgeAtom{
		{
			// Service-scoped (runtimes) but body has NO {hostname} —
			// per-service substitution is a no-op, two renders identical.
			ID: "identical-rules", Priority: 1,
			Axes: AxisVector{
				Phases:   []Phase{PhaseDevelopActive},
				Runtimes: []topology.RuntimeClass{topology.RuntimeImplicitWeb},
			},
			Body: "Implicit-webserver guidance with no host placeholder.",
		},
		{
			// Service-scoped + per-service body — two renders MUST stay
			// distinct because each carries its host-specific command.
			ID: "host-specific", Priority: 2,
			Axes: AxisVector{
				Phases:       []Phase{PhaseDevelopActive},
				DeployStates: []DeployState{DeployStateNeverDeployed},
			},
			Body: "Run cmd for {hostname}.",
		},
	}

	twoServices := StateEnvelope{
		Phase: PhaseDevelopActive,
		Services: []ServiceSnapshot{
			{Hostname: "appdev", RuntimeClass: topology.RuntimeImplicitWeb, Bootstrapped: true, Deployed: false},
			{Hostname: "appstage", RuntimeClass: topology.RuntimeImplicitWeb, Bootstrapped: true, Deployed: false},
		},
	}
	matches, err := Synthesize(twoServices, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	var rulesCount, hostCount int
	for _, m := range matches {
		switch m.AtomID {
		case "identical-rules":
			rulesCount++
		case "host-specific":
			hostCount++
		}
	}
	if rulesCount != 1 {
		t.Errorf("identical-body atom rendered %d times, want 1 (dedup should collapse)", rulesCount)
	}
	if hostCount != 2 {
		t.Errorf("host-specific atom rendered %d times, want 2 (different bodies must NOT dedup)", hostCount)
	}
}

// TestSynthesize_EnvelopeDeployStateFilter pins the envelope-scoped twin
// of DeployStates: an atom with envelopeDeployStates: [never-deployed]
// renders ONCE per envelope (not per service) when at least one
// bootstrapped service is in that state. Per-service iteration is the
// trap that motivated this axis (per-service render duplication
// contributed ~10 KB to the standard first-deploy fixture's overflow,
// see plans/atom-corpus-context-trim-2026-04-26.md §4.4 / §17.3).
func TestSynthesize_EnvelopeDeployStateFilter(t *testing.T) {
	t.Parallel()

	corpus := []KnowledgeAtom{
		{
			ID: "rules-once", Priority: 1,
			Axes: AxisVector{
				Phases:               []Phase{PhaseDevelopActive},
				EnvelopeDeployStates: []DeployState{DeployStateNeverDeployed},
			},
			Body: "Envelope rules.",
		},
		{
			ID: "cmds-perservice", Priority: 2,
			Axes: AxisVector{
				Phases:       []Phase{PhaseDevelopActive},
				DeployStates: []DeployState{DeployStateNeverDeployed},
			},
			Body: "Per-service cmds for {hostname}.",
		},
	}

	// Two never-deployed services in the envelope. The rules atom must
	// render exactly once (envelope-scoped); the cmds atom must render
	// twice (per-service). Pre-fix Synthesize had no envelope-deploy-state
	// axis, so a rules atom declaring deployStates would have rendered 2×.
	twoServices := StateEnvelope{
		Phase: PhaseDevelopActive,
		Services: []ServiceSnapshot{
			{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Deployed: false},
			{Hostname: "appstage", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Deployed: false},
		},
	}
	matches, err := Synthesize(twoServices, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	var rulesCount, cmdsCount int
	var cmdsHosts []string
	for _, m := range matches {
		switch m.AtomID {
		case "rules-once":
			rulesCount++
		case "cmds-perservice":
			cmdsCount++
			if m.Service != nil {
				cmdsHosts = append(cmdsHosts, m.Service.Hostname)
			}
		}
	}
	if rulesCount != 1 {
		t.Errorf("envelope-scoped rules atom rendered %d times, want 1", rulesCount)
	}
	if cmdsCount != 2 {
		t.Errorf("service-scoped cmds atom rendered %d times, want 2", cmdsCount)
	}
	if len(cmdsHosts) != 2 || cmdsHosts[0] == cmdsHosts[1] {
		t.Errorf("service-scoped cmds atom should bind to both hostnames once each, got: %v", cmdsHosts)
	}

	// Envelope with only deployed services — rules atom must NOT fire.
	allDeployed := StateEnvelope{
		Phase: PhaseDevelopActive,
		Services: []ServiceSnapshot{
			{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Deployed: true},
		},
	}
	matches, err = Synthesize(allDeployed, corpus)
	if err != nil {
		t.Fatalf("Synthesize all-deployed: %v", err)
	}
	for _, m := range matches {
		if m.AtomID == "rules-once" {
			t.Errorf("envelope-scoped rules atom must not fire when no service is never-deployed")
		}
	}

	// Bootstrapped=false service is skipped (deploy state undefined).
	notBootstrapped := StateEnvelope{
		Phase: PhaseDevelopActive,
		Services: []ServiceSnapshot{
			{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: false},
		},
	}
	matches, err = Synthesize(notBootstrapped, corpus)
	if err != nil {
		t.Fatalf("Synthesize not-bootstrapped: %v", err)
	}
	for _, m := range matches {
		if m.AtomID == "rules-once" {
			t.Errorf("envelope-scoped rules atom must not fire when no service is bootstrapped")
		}
	}
}

// TestParseAtom_DeployStatesAndEnvelopeDeployStatesMutuallyExclusive
// pins the parse-time guard: an atom declaring BOTH service-scoped
// deployStates and envelope-scoped envelopeDeployStates is ambiguous —
// service-scoped wants to render per-host, envelope-scoped wants to
// render once. The parser rejects so the conflict surfaces in the build,
// not as silent double-rendering at runtime.
func TestParseAtom_DeployStatesAndEnvelopeDeployStatesMutuallyExclusive(t *testing.T) {
	t.Parallel()
	src := "---\n" +
		"id: bad-atom\n" +
		"phases: [develop-active]\n" +
		"deployStates: [never-deployed]\n" +
		"envelopeDeployStates: [never-deployed]\n" +
		"---\n" +
		"body\n"
	if _, err := ParseAtom(src); err == nil {
		t.Fatal("ParseAtom: expected error for atom declaring both deployStates and envelopeDeployStates")
	}
}

// TestSynthesize_ServiceScopedAxesRequireSameService pins the conjunction
// invariant: an atom declaring multiple service-scoped axes (modes,
// strategies, runtimes, deployStates) fires only when ONE service satisfies
// all of them. Disjunction across services would fire atoms whose body
// references a service the atom isn't semantically about — e.g. strategy-
// review surfacing because service A is deployed and (different) service B
// has unset strategy.
// TestSynthesize_WorkSessionScopeFilter pins Lever B of audit F9
// (audit-prerelease-internal-testing-2026-04-29). When envelope.WorkSession
// has a non-empty Services list, per-service axis matching narrows to
// in-scope hostnames only — atoms with per-service axes only fire for
// scope services, not the project's full service list. Without the
// filter, status responses with scope=[1 service] in a 4-service project
// rendered atoms 4× when only 1× was relevant.
func TestSynthesize_WorkSessionScopeFilter(t *testing.T) {
	t.Parallel()

	corpus := []KnowledgeAtom{
		{
			ID:   "auto-only",
			Axes: AxisVector{Phases: []Phase{PhaseDevelopActive}, CloseDeployModes: []topology.CloseDeployMode{topology.CloseModeAuto}},
			Body: "auto atom for {hostname}",
		},
	}

	// Project has 3 services on closeMode=auto. Without scope, the atom
	// renders 3× (one per host).
	project := []ServiceSnapshot{
		{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Deployed: true, CloseDeployMode: topology.CloseModeAuto},
		{Hostname: "webdev", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Deployed: true, CloseDeployMode: topology.CloseModeAuto},
		{Hostname: "workerdev", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Deployed: true, CloseDeployMode: topology.CloseModeAuto},
	}

	// WorkSession=nil → full project, atom renders 3×.
	envIdle := StateEnvelope{Phase: PhaseDevelopActive, Services: project}
	gotIdle, err := SynthesizeBodies(envIdle, corpus)
	if err != nil {
		t.Fatalf("idle synthesize: %v", err)
	}
	matchesIdle := 0
	for _, b := range gotIdle {
		if strings.Contains(b, "auto atom for ") {
			matchesIdle++
		}
	}
	if matchesIdle != 3 {
		t.Errorf("idle (no work session): want 3 renders, got %d", matchesIdle)
	}

	// WorkSession scoped to 1 service → atom renders 1× and only for the
	// scope hostname.
	envScoped := StateEnvelope{
		Phase:    PhaseDevelopActive,
		Services: project,
		WorkSession: &WorkSessionSummary{
			Intent:   "fix appdev",
			Services: []string{"appdev"},
		},
	}
	gotScoped, err := SynthesizeBodies(envScoped, corpus)
	if err != nil {
		t.Fatalf("scoped synthesize: %v", err)
	}
	joined := strings.Join(gotScoped, "\n")
	if strings.Count(joined, "auto atom for ") != 1 {
		t.Errorf("scoped (work session 1 host): want 1 render, got %d in %q", strings.Count(joined, "auto atom for "), joined)
	}
	if !strings.Contains(joined, "auto atom for appdev") {
		t.Errorf("scoped: expected render for appdev, got %q", joined)
	}
	if strings.Contains(joined, "auto atom for webdev") || strings.Contains(joined, "auto atom for workerdev") {
		t.Errorf("scoped: must not render for out-of-scope services; got %q", joined)
	}
}

// TestSynthesize_WorkSessionScopeFilter_ServiceAgnosticUnaffected pins
// that atoms WITHOUT per-service axes (service-agnostic) keep firing
// once per envelope regardless of work session scope — they describe
// project-wide concepts, not per-service ones.
func TestSynthesize_WorkSessionScopeFilter_ServiceAgnosticUnaffected(t *testing.T) {
	t.Parallel()

	corpus := []KnowledgeAtom{
		{
			ID:   "project-wide",
			Axes: AxisVector{Phases: []Phase{PhaseDevelopActive}},
			Body: "global atom",
		},
	}
	env := StateEnvelope{
		Phase: PhaseDevelopActive,
		Services: []ServiceSnapshot{
			{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Deployed: true, CloseDeployMode: topology.CloseModeAuto},
		},
		WorkSession: &WorkSessionSummary{Intent: "x", Services: []string{"appdev"}},
	}
	got, err := SynthesizeBodies(env, corpus)
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if strings.Count(strings.Join(got, "\n"), "global atom") != 1 {
		t.Errorf("service-agnostic atom: want 1 render, got %d", strings.Count(strings.Join(got, "\n"), "global atom"))
	}
}

func TestSynthesize_ServiceScopedAxesRequireSameService(t *testing.T) {
	t.Parallel()

	corpus := []KnowledgeAtom{
		{
			ID:   "two-axes",
			Axes: AxisVector{Phases: []Phase{PhaseDevelopActive}, DeployStates: []DeployState{DeployStateDeployed}, CloseDeployModes: []topology.CloseDeployMode{topology.CloseModeUnset}},
			Body: "Two-axis atom.",
		},
	}

	// Mixed envelope: A is deployed with push-dev, B is never-deployed with
	// unset. No single service satisfies deployed+unset → atom must NOT fire.
	mixed := StateEnvelope{
		Phase: PhaseDevelopActive,
		Services: []ServiceSnapshot{
			{Hostname: "a", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Deployed: true, CloseDeployMode: topology.CloseModeAuto},
			{Hostname: "b", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Deployed: false, CloseDeployMode: topology.CloseModeUnset},
		},
	}
	got, err := SynthesizeBodies(mixed, corpus)
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
			{Hostname: "a", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Deployed: true, CloseDeployMode: topology.CloseModeUnset},
		},
	}
	got, err = SynthesizeBodies(match, corpus)
	if err != nil {
		t.Fatalf("Synthesize match: %v", err)
	}
	if !strings.Contains(strings.Join(got, "\n"), "Two-axis atom.") {
		t.Error("match envelope: two-axis atom must fire when one service satisfies both axes")
	}
}

func TestSynthesize_PrioritySort(t *testing.T) {
	t.Parallel()

	env := developEnvelope(EnvContainer, topology.ModeDev, topology.CloseModeGitPush, topology.RuntimeDynamic)
	got, err := SynthesizeBodies(env, synthCorpus())
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

	env := developEnvelope(EnvContainer, topology.ModeDev, topology.CloseModeGitPush, topology.RuntimeDynamic)
	corpus := synthCorpus()
	first, err := SynthesizeBodies(env, corpus)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	for i := range 10 {
		got, err := SynthesizeBodies(env, corpus)
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
	got, err := SynthesizeBodies(env, corpus)
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
			got, err := SynthesizeBodies(env, corpus)
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
				got, err := SynthesizeBodies(env, corpus)
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
	got, err := SynthesizeBodies(env, corpus)
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
// non-empty atom output for the git-push capability setup case.
func TestSynthesizeStrategySetup_LocalEnv(t *testing.T) {
	t.Parallel()
	snaps := []ServiceSnapshot{{
		Hostname:         "appdev",
		Bootstrapped:     true,
		Mode:             topology.PlanModeDev,
		CloseDeployMode:  topology.CloseModeGitPush,
		GitPushState:     topology.GitPushUnconfigured,
		BuildIntegration: topology.BuildIntegrationNone,
	}}
	got, err := SynthesizeStrategySetup(runtime.Info{InContainer: false}, snaps)
	if err != nil {
		t.Fatalf("SynthesizeStrategySetup: %v", err)
	}
	if got == "" {
		t.Fatal("expected non-empty guidance for git-push setup on local env")
	}
}

// TestSynthesizeStrategySetup_ContainerEnv same wrapper, container env —
// a different set of atoms fires (container-specific push setup) so the
// output must differ from the local case. If both come back identical,
// the Environment axis isn't reaching the synthesizer.
func TestSynthesizeStrategySetup_ContainerEnv(t *testing.T) {
	t.Parallel()
	snaps := []ServiceSnapshot{{
		Hostname:         "appdev",
		Bootstrapped:     true,
		Mode:             topology.PlanModeDev,
		CloseDeployMode:  topology.CloseModeGitPush,
		GitPushState:     topology.GitPushUnconfigured,
		BuildIntegration: topology.BuildIntegrationNone,
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

// TestExpandServicesListDirectives pins the engine ticket E1 placeholder
// expander: `{services-list:TEMPLATE}` directives are replaced with one
// rendering of TEMPLATE per service, joined with newlines, where TEMPLATE
// may itself contain `{hostname}` and `{stage-hostname}` tokens that the
// expander substitutes per service. Directives are top-level brace-matched
// (placeholder tokens like `{hostname}` increment depth and stop being a
// terminator) so TEMPLATE can carry arbitrary placeholder substitutions
// without escaping. Body fragments outside any directive pass through
// unchanged. Empty service list = empty expansion (no error).
func TestExpandServicesListDirectives(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		body     string
		services []ServiceSnapshot
		want     string
		wantErr  bool
	}{
		{
			name: "single_directive_two_services",
			body: "Run for each:\n{services-list:- `zerops_deploy targetService=\"{hostname}\"`}\nDone.",
			services: []ServiceSnapshot{
				{Hostname: "appdev"},
				{Hostname: "apidev"},
			},
			want: "Run for each:\n- `zerops_deploy targetService=\"appdev\"`\n- `zerops_deploy targetService=\"apidev\"`\nDone.",
		},
		{
			name: "stage_hostname_substituted",
			body: "{services-list:- `zerops_deploy sourceService=\"{hostname}\" targetService=\"{stage-hostname}\"`}",
			services: []ServiceSnapshot{
				{Hostname: "appdev", StageHostname: "appstage"},
				{Hostname: "apidev", StageHostname: "apistage"},
			},
			want: "- `zerops_deploy sourceService=\"appdev\" targetService=\"appstage\"`\n- `zerops_deploy sourceService=\"apidev\" targetService=\"apistage\"`",
		},
		{
			name:     "no_directive_passes_through",
			body:     "Plain prose with no directive.",
			services: []ServiceSnapshot{{Hostname: "appdev"}},
			want:     "Plain prose with no directive.",
		},
		{
			name:     "empty_services_yields_empty_expansion",
			body:     "before\n{services-list:- {hostname}}\nafter",
			services: nil,
			want:     "before\n\nafter",
		},
		{
			name: "two_directives_in_body",
			body: "deploy:\n{services-list:- {hostname}}\nverify:\n{services-list:- {hostname}!}",
			services: []ServiceSnapshot{
				{Hostname: "appdev"},
				{Hostname: "apidev"},
			},
			want: "deploy:\n- appdev\n- apidev\nverify:\n- appdev!\n- apidev!",
		},
		{
			name:    "unterminated_directive_errors",
			body:    "before {services-list:- {hostname} after",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := expandServicesListDirectives(tc.body, tc.services)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil; output=%q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("expansion mismatch:\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// TestSynthesize_MultiServiceAggregateRendersOnce pins engine ticket E1
// behavior: an atom declaring `multiService: aggregate` with multiple
// matching services produces a SINGLE MatchedRender (Service nil) whose
// body contains `{services-list:TEMPLATE}` expansions enumerating every
// matching service. The same axes-without-aggregate atom would render
// once per matching service (legacy behavior; pinned by
// TestSynthesize_MultiMatchRendersOncePerService). The two-pair fixture
// inherits this behavior — atoms migrated to aggregate render 1× instead
// of 2× per dev/stage pair.
func TestSynthesize_MultiServiceAggregateRendersOnce(t *testing.T) {
	t.Parallel()

	atom := KnowledgeAtom{
		ID:       "test-aggregate-promote",
		Priority: 5,
		Axes: AxisVector{
			Phases:       []Phase{PhaseDevelopActive},
			Modes:        []topology.Mode{topology.ModeStandard},
			DeployStates: []DeployState{DeployStateNeverDeployed},
			MultiService: MultiServiceAggregate,
		},
		Body: "Promote each pair:\n\n{services-list:- `zerops_deploy sourceService=\"{hostname}\" targetService=\"{stage-hostname}\"`}",
	}

	twoPairEnv := StateEnvelope{
		Phase: PhaseDevelopActive,
		Services: []ServiceSnapshot{
			{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, StageHostname: "appstage", Bootstrapped: true, Deployed: false},
			{Hostname: "appstage", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage, Bootstrapped: true, Deployed: false},
			{Hostname: "apidev", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, StageHostname: "apistage", Bootstrapped: true, Deployed: false},
			{Hostname: "apistage", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage, Bootstrapped: true, Deployed: false},
		},
	}

	matches, err := Synthesize(twoPairEnv, []KnowledgeAtom{atom})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("aggregate atom rendered %d times, want 1", len(matches))
	}
	if matches[0].Service != nil {
		t.Errorf("aggregate render should have nil Service, got %+v", matches[0].Service)
	}
	body := matches[0].Body
	for _, want := range []string{
		`sourceService="appdev" targetService="appstage"`,
		`sourceService="apidev" targetService="apistage"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("aggregate body missing %q, got:\n%s", want, body)
		}
	}
	// Stage-mode services don't satisfy modes:[standard], so they must
	// NOT appear as the "from" hostname (only dev hosts are listed).
	for _, unwanted := range []string{
		`sourceService="appstage"`,
		`sourceService="apistage"`,
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("aggregate body wrongly includes stage host as source: %q", unwanted)
		}
	}
}

// TestSynthesize_PerServicePlaceholderBinding pins Phase 2 (F3/C2) of
// the pipeline-repair plan: an atom with service-scoped axes binds
// `{hostname}` substitution to the service that satisfied the axes, not
// to the global primaryHostnames picker. Pre-fix multi-service projects
// could see commands targeted at the wrong service (atom matched via
// service B, rendered with service A's hostname).
//
// Alphabet-rotation: same scenario in two hostname orderings — `apidev`
// or `appdev` first by sort order. The atom must consistently render
// the matched service's hostname regardless of which sorts first.
func TestSynthesize_PerServicePlaceholderBinding(t *testing.T) {
	t.Parallel()

	atom := KnowledgeAtom{
		ID:       "test-first-deploy-write-app",
		Priority: 5,
		Axes: AxisVector{
			Phases:       []Phase{PhaseDevelopActive},
			Modes:        []topology.Mode{topology.ModeDev},
			DeployStates: []DeployState{DeployStateNeverDeployed},
		},
		Body: "Write code on {hostname} and deploy.",
	}

	cases := []struct {
		name          string
		services      []ServiceSnapshot
		wantHost      string
		wantNotInBody string
	}{
		{
			name: "apidev_never_deployed_appdev_deployed",
			services: []ServiceSnapshot{
				{Hostname: "apidev", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Deployed: false, Mode: topology.ModeDev},
				{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Deployed: true, Mode: topology.ModeDev},
			},
			wantHost:      "apidev",
			wantNotInBody: "appdev",
		},
		{
			name: "appdev_never_deployed_apidev_deployed",
			services: []ServiceSnapshot{
				{Hostname: "apidev", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Deployed: true, Mode: topology.ModeDev},
				{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Deployed: false, Mode: topology.ModeDev},
			},
			wantHost:      "appdev",
			wantNotInBody: "apidev",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := StateEnvelope{Phase: PhaseDevelopActive, Services: tc.services}
			matches, err := Synthesize(env, []KnowledgeAtom{atom})
			if err != nil {
				t.Fatalf("Synthesize: %v", err)
			}
			if len(matches) != 1 {
				t.Fatalf("expected 1 match (only never-deployed satisfies), got %d", len(matches))
			}
			body := matches[0].Body
			if !strings.Contains(body, tc.wantHost) {
				t.Errorf("body should mention %q, got: %s", tc.wantHost, body)
			}
			if strings.Contains(body, tc.wantNotInBody) {
				t.Errorf("body should NOT mention %q (would indicate global picker bug), got: %s", tc.wantNotInBody, body)
			}
			if matches[0].Service == nil {
				t.Errorf("MatchedRender.Service should be populated for service-scoped atom")
			} else if matches[0].Service.Hostname != tc.wantHost {
				t.Errorf("MatchedRender.Service.Hostname = %q, want %q", matches[0].Service.Hostname, tc.wantHost)
			}
		})
	}
}

// TestSynthesize_MultiMatchRendersOncePerService pins the multi-match
// policy: when multiple services satisfy an atom's service-scoped axes,
// the atom renders once per matching service. Each rendering binds
// `{hostname}` to that service.
func TestSynthesize_MultiMatchRendersOncePerService(t *testing.T) {
	t.Parallel()

	atom := KnowledgeAtom{
		ID:       "test-strategy-iter",
		Priority: 5,
		Axes: AxisVector{
			Phases:           []Phase{PhaseDevelopActive},
			CloseDeployModes: []topology.CloseDeployMode{topology.CloseModeAuto},
		},
		Body: "Push-dev on {hostname}.",
	}
	env := StateEnvelope{
		Phase: PhaseDevelopActive,
		Services: []ServiceSnapshot{
			{Hostname: "apidev", Bootstrapped: true, CloseDeployMode: topology.CloseModeAuto},
			{Hostname: "appdev", Bootstrapped: true, CloseDeployMode: topology.CloseModeAuto},
		},
	}
	matches, err := Synthesize(env, []KnowledgeAtom{atom})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches (one per service), got %d", len(matches))
	}
	seen := map[string]bool{}
	for _, m := range matches {
		if m.Service == nil {
			t.Fatalf("MatchedRender.Service nil for service-scoped atom")
		}
		if !strings.Contains(m.Body, m.Service.Hostname) {
			t.Errorf("body for %s missing hostname: %s", m.Service.Hostname, m.Body)
		}
		seen[m.Service.Hostname] = true
	}
	for _, host := range []string{"apidev", "appdev"} {
		if !seen[host] {
			t.Errorf("expected match for %s, none found", host)
		}
	}
}
