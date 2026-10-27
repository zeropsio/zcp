package workflow

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// Develop's start answered 38–55 KB in three live Mate sessions on 2026-10-27
// — one past the client's display limit, read back from a file — mostly with
// guidance written for a first deploy of a greenfield app: served to a
// repository that already carried its zerops.yaml, and to stages whose dev
// halves (or dev pools) already ran the repository's code. These pin what such
// a start no longer carries, and that the rendered status keeps within the
// composer's budget with its per-service lines counted.

// firstDeployAtoms are the atoms written for a runtime that has never run the
// app's code: scaffold it, write it, first-deploy it.
var firstDeployAtoms = []string{
	"develop-first-deploy-intro",
	"develop-first-deploy-scaffold-yaml",
	"develop-first-deploy-write-app",
	"develop-first-deploy-env-vars",
	"develop-nodejs-greenfield-buildhint",
	"develop-platform-rules-common",
	"develop-env-var-model",
}

// greenfieldAtoms are the atoms that scaffold a repository: a zerops.yaml to
// write, an app to write, a lockfile not yet committed.
var greenfieldAtoms = []string{
	"develop-first-deploy-scaffold-yaml",
	"develop-first-deploy-write-app",
	"develop-nodejs-greenfield-buildhint",
}

// poolPartPair is a pair keyed by its part's dev pool: the pool and a stage
// imported without code, the way a Mate's pool migration leaves each part.
func poolPartPair(part, stageType, setup string) []ServiceSnapshot {
	pool, stage := part+"pool", part+"stage"
	return []ServiceSnapshot{
		{
			Hostname: pool, TypeVersion: "swarm@1", RuntimeClass: topology.RuntimePool, Status: StatusActive,
			DevIsPool: true, Bootstrapped: true, Deployed: true, StageHostname: stage,
			Stages:          []topology.StageRef{{Hostname: stage, SetupName: setup}},
			CloseDeployMode: topology.CloseModeUnset, GitPushState: topology.GitPushConfigured, HQRepo: "noola",
		},
		{
			Hostname: stage, TypeVersion: stageType, RuntimeClass: topology.RuntimeClassFor(stageType), Status: "READY_TO_DEPLOY",
			Mode: topology.ModeStage, Bootstrapped: true, SetupName: setup,
			CloseDeployMode: topology.CloseModeUnset, GitPushState: topology.GitPushConfigured,
			RemoteURL: "https://mate-hq.zerops.io/git/m1VrZPJlSnAmnYAvfuEZEg/14031c2a-152a-41c7-b637-67a627692e48/noola.git",
		},
	}
}

// multiPartPoolProject is the Noola project as its fourth develop start saw
// it: six parts, each keyed by its dev pool with a held lease and a stage
// imported without code, beside the managed services they use; the session
// puts main on the six stages.
func multiPartPoolProject() StateEnvelope {
	parts := []struct{ part, stageType, setup string }{
		{"api", "ubuntu/nodejs@24", "apiprod"},
		{"edge", "alpine/elixir@1.16.3", "edgeprod"},
		{"embedder", "ubuntu/nodejs@24", "embedderprod"},
		{"geo", "ubuntu/nodejs@24", "geoprod"},
		{"site", "alpine/static@1.0", "siteprod"},
		{"web", "alpine/static@1.0", "webprod"},
	}
	services := make([]ServiceSnapshot, 0, 2*len(parts)+6)
	leases := make([]LeaseSnapshot, 0, len(parts))
	stages := make([]string, 0, len(parts))
	for _, p := range parts {
		services = append(services, poolPartPair(p.part, p.stageType, p.setup)...)
		leases = append(leases, LeaseSnapshot{
			Alias: p.part + "pool-1", Pool: p.part + "pool", Branch: "dev-pools", State: "held",
			ContainerHostname: "node-id-1.runtime." + p.part + "pool.zerops",
		})
		stages = append(stages, p.part+"stage")
	}
	services = append(services,
		fixSnapManaged("broker", "nats:single@2.12"),
		fixSnapManaged("cache", "valkey:single@7.2"),
		fixSnapManaged("db", "postgresql:single@18"),
		fixSnapManaged("qdrant", "qdrant:single@1.12"),
		fixSnapManaged("search", "typesense:single@30.2"),
		fixSnapManaged("storage", "object-storage"),
	)
	slices.SortFunc(services, func(a, b ServiceSnapshot) int { return strings.Compare(a.Hostname, b.Hostname) })
	session := fixSession(stages...)
	session.Intent = "Deploy HQ main (merged change #1) to the previews apistage, edgestage, embedderstage, geostage, sitestage, webstage with their *prod setups, wired like the recipe's stage tier; verify each"
	return StateEnvelope{
		Phase: PhaseDevelopActive, Environment: EnvContainer,
		Project:  ProjectSummary{ID: "proj-noola", Name: "noola"},
		Services: services, Leases: &leases, WorkSession: session,
	}
}

func atomFired(matches []MatchedRender, id string) bool {
	return slices.ContainsFunc(matches, func(m MatchedRender) bool { return m.AtomID == id })
}

// TestScenario_StageOfARunningDevHalf_GetsNoFirstDeployGuidance pins that a
// stage imported without code is no first deploy of the app when its pair's
// dev half already runs the repository's code — a deployed dev service, or a
// dev pool, whose leases run it: the stage's deploy is a cross-deploy of code
// that exists. A pair whose dev half never deployed keeps the first-deploy
// branch.
func TestScenario_StageOfARunningDevHalf_GetsNoFirstDeployGuidance(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	standardPair := func(devDeployed bool) []ServiceSnapshot {
		return []ServiceSnapshot{
			{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, StageHostname: "appstage",
				CloseDeployMode: topology.CloseModeAuto, GitPushState: topology.GitPushUnconfigured, Bootstrapped: true, Deployed: devDeployed},
			{Hostname: "appstage", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage,
				CloseDeployMode: topology.CloseModeAuto, GitPushState: topology.GitPushUnconfigured, Bootstrapped: true},
		}
	}
	tests := []struct {
		name       string
		env        StateEnvelope
		firstDepl  bool
		wantAtomID string
	}{
		{
			name: "a deployed dev service's stage",
			env: StateEnvelope{Phase: PhaseDevelopActive, Environment: EnvContainer, Services: standardPair(true),
				WorkSession: fixSession("appdev", "appstage")},
			wantAtomID: "develop-close-mode-auto-standard",
		},
		{
			name:       "stages keyed by dev pools",
			env:        multiPartPoolProject(),
			wantAtomID: "develop-dev-pool",
		},
		{
			name: "a pair that never deployed",
			env: StateEnvelope{Phase: PhaseDevelopActive, Environment: EnvContainer, Services: standardPair(false),
				WorkSession: fixSession("appdev", "appstage")},
			firstDepl:  true,
			wantAtomID: "develop-first-deploy-intro",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, err := Synthesize(tt.env, corpus)
			if err != nil {
				t.Fatalf("Synthesize: %v", err)
			}
			requireAtomIDsContain(t, tt.name, out, tt.wantAtomID)
			if tt.firstDepl {
				return
			}
			for _, id := range firstDeployAtoms {
				if atomFired(out, id) {
					t.Errorf("%s: %s fired for a stage whose dev half already runs the code", tt.name, id)
				}
			}
		})
	}
}

// TestScenario_RepoWithItsZeropsYaml_GetsNoGreenfieldAtoms pins that a first
// deploy of a repository that already carries its zerops.yaml — read on the
// dev half (ServiceSnapshot.HasZeropsYAML) — gets the existing-repository
// guidance and none that scaffolds a zerops.yaml or writes the app; a repo
// with none, or one zcp did not read, keeps them.
func TestScenario_RepoWithItsZeropsYaml_GetsNoGreenfieldAtoms(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	for _, hasYAML := range []bool{true, false} {
		t.Run(fmt.Sprintf("zerops.yaml present %v", hasYAML), func(t *testing.T) {
			t.Parallel()
			pair := []ServiceSnapshot{
				{Hostname: "appdev", TypeVersion: "ubuntu/nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, StageHostname: "appstage",
					CloseDeployMode: topology.CloseModeUnset, GitPushState: topology.GitPushUnconfigured, Bootstrapped: true, HasZeropsYAML: hasYAML},
				{Hostname: "appstage", TypeVersion: "ubuntu/nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage,
					CloseDeployMode: topology.CloseModeUnset, GitPushState: topology.GitPushUnconfigured, Bootstrapped: true, HasZeropsYAML: hasYAML},
			}
			out, err := Synthesize(StateEnvelope{Phase: PhaseDevelopActive, Environment: EnvContainer, Services: pair, WorkSession: fixSession("appdev", "appstage")}, corpus)
			if err != nil {
				t.Fatalf("Synthesize: %v", err)
			}
			requireAtomIDsContain(t, "first deploy", out, "develop-first-deploy-intro", "develop-first-deploy-existing-repo")
			for _, id := range greenfieldAtoms {
				if atomFired(out, id) == hasYAML {
					t.Errorf("%s fired = %v for a repo whose zerops.yaml present = %v", id, hasYAML, hasYAML)
				}
			}
		})
	}
}

// TestScenario_DevPoolAtom_OnlyWithAPoolOrLeaseInScope pins that the dev-pool
// loop renders only when the work reaches a pool: the session names it, it
// keys a pair whose stage the session names, or this zcp holds a lease of it.
// A pool elsewhere in the project is not this session's (15 KB of guidance
// for work that never leases).
func TestScenario_DevPoolAtom_OnlyWithAPoolOrLeaseInScope(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	const atom = "develop-dev-pool"
	apiPair := []ServiceSnapshot{
		{Hostname: "apidev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, StageHostname: "apistage",
			CloseDeployMode: topology.CloseModeAuto, GitPushState: topology.GitPushUnconfigured, Bootstrapped: true, Deployed: true},
		{Hostname: "apistage", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage,
			CloseDeployMode: topology.CloseModeAuto, GitPushState: topology.GitPushUnconfigured, Bootstrapped: true, Deployed: true},
	}
	webPair := poolPartPair("web", "alpine/static@1.0", "webprod")
	partdev := ServiceSnapshot{Hostname: "partdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev, Bootstrapped: true, Deployed: true}
	held := []LeaseSnapshot{{Alias: "devpool-1", Pool: "devpool", Branch: "feat", State: "held"}}
	tests := []struct {
		name     string
		services []ServiceSnapshot
		scope    []string
		leases   *[]LeaseSnapshot
		want     bool
	}{
		{name: "another pair's pool", services: append(slices.Clone(apiPair), webPair...), scope: []string{"apidev", "apistage"}},
		{name: "the pool keys the stage in scope", services: append(slices.Clone(apiPair), webPair...), scope: []string{"webstage"}, want: true},
		{name: "a pool nothing leases", services: []ServiceSnapshot{partdev, fixSnapPool("devpool", "swarm@1")}, scope: []string{"partdev"}},
		{name: "a pool this zcp holds a lease of", services: []ServiceSnapshot{partdev, fixSnapPool("devpool", "swarm@1")}, scope: []string{"partdev"}, leases: &held, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := StateEnvelope{Phase: PhaseDevelopActive, Environment: EnvContainer, Services: tt.services, Leases: tt.leases, WorkSession: fixSession(tt.scope...)}
			out, err := Synthesize(env, corpus)
			if err != nil {
				t.Fatalf("Synthesize: %v", err)
			}
			if got := atomFired(out, atom); got != tt.want {
				t.Errorf("%s fired = %v, want %v", atom, got, tt.want)
			}
		})
	}
}

// TestGuidanceBudget_CountsTheServiceLines pins that the budget the composer
// keeps the guidance under is ComposeBodyBudget less what the project's
// services cost the rest of the answer: the per-service and lease lines the
// status renders above the guidance, and the envelope block's bytes beyond a
// small project's. A project of many services leaves its guidance less room,
// and the answer as a whole stays within the cap.
func TestGuidanceBudget_CountsTheServiceLines(t *testing.T) {
	t.Parallel()
	env := multiPartPoolProject()
	var lines strings.Builder
	renderServices(&lines, env)
	renderLeases(&lines, env)
	envelope := len(AppendEnvelope("", env))
	if got, want := GuidanceBudget(env), ComposeBodyBudget-lines.Len()-(envelope-envelopeBlockBaseline); got != want {
		t.Errorf("GuidanceBudget = %d, want ComposeBodyBudget less %d bytes of service and lease lines and %d of envelope beyond the baseline = %d",
			got, lines.Len(), envelope-envelopeBlockBaseline, want)
	}
	if small := GuidanceBudget(StateEnvelope{Phase: PhaseIdle}); small >= ComposeBodyBudget {
		t.Errorf("GuidanceBudget of an empty project = %d, want the services line counted under %d", small, ComposeBodyBudget)
	}
}

// TestRenderServices_BasePrefixRuleSaidOnce pins that the base-prefix rule's
// sentence is written once under the services, not on every runtime's line:
// each line keeps only its own prefix.
func TestRenderServices_BasePrefixRuleSaidOnce(t *testing.T) {
	t.Parallel()
	env := multiPartPoolProject()
	var b strings.Builder
	renderServices(&b, env)
	out := b.String()
	if n := strings.Count(out, "on BOTH build.base and run.base"); n != 1 {
		t.Errorf("the base-prefix rule is said %d times, want once:\n%s", n, out)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "  - apistage ") && !strings.Contains(line, "base prefix ubuntu/") {
			t.Errorf("apistage's line no longer names its prefix: %s", line)
		}
		if strings.HasPrefix(line, "  - edgestage ") && !strings.Contains(line, "base prefix alpine/") {
			t.Errorf("edgestage's line no longer names its prefix: %s", line)
		}
	}
}

// TestDevelopBriefing_MultiPartPoolProject_UnderTheCap pins the develop start
// of the multi-part pool project within the 32 KB MCP tool-response cap — the
// JSON-RPC frame the client reads, envelope block included — with the dev-pool
// loop, the guidance its stages need, whole; and logs its size.
func TestDevelopBriefing_MultiPartPoolProject_UnderTheCap(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	env := multiPartPoolProject()
	matches, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	matches = ComposeUnderBudget(matches, corpus, GuidanceBudget(env))
	plan := BuildPlan(env)
	text := AppendEnvelope(RenderStatus(Response{Envelope: env, Guidance: BodiesOf(matches), Plan: &plan}), env)
	wire := mcpWireFrameSize(text)
	t.Logf("develop start of a six-part pool project: %d B, %d B on the wire (atoms: %v)", len(text), wire, atomIDsOf(matches))
	if wire >= 32*1024 {
		t.Errorf("develop start is %d B on the wire, over the 32 KB MCP cap", wire)
	}
	for _, m := range matches {
		if m.AtomID == "develop-dev-pool" && !strings.Contains(m.Body, "**Stage check.**") {
			t.Errorf("the dev-pool loop was cut to a head:\n%s", m.Body)
		}
	}
}
