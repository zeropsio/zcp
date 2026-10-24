package workflow

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
)

// planEnvelope returns a minimal envelope with the requested phase. Helpers
// below layer on services / work session state.
func planEnvelope(phase Phase) StateEnvelope {
	return StateEnvelope{
		Phase:       phase,
		Environment: EnvLocal,
		Generated:   time.Date(2026, 4, 19, 0, 0, 0, 0, time.UTC),
	}
}

func TestBuildPlan_IdleEmptyProject(t *testing.T) {
	t.Parallel()

	plan := BuildPlan(planEnvelope(PhaseIdle))
	if plan.Primary.Tool != "zerops_workflow" {
		t.Errorf("tool = %q, want zerops_workflow", plan.Primary.Tool)
	}
	if plan.Primary.Args["workflow"] != "bootstrap" {
		t.Errorf("workflow arg = %q, want bootstrap", plan.Primary.Args["workflow"])
	}
	if plan.Secondary != nil {
		t.Errorf("secondary = %+v, want nil for empty-idle", plan.Secondary)
	}
}

func TestBuildPlan_IdleBootstrappedOnly(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseIdle)
	env.Services = []ServiceSnapshot{
		{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Mode: topology.ModeDev, CloseDeployMode: topology.CloseModeGitPush},
		{Hostname: "db", RuntimeClass: topology.RuntimeManaged},
	}
	plan := BuildPlan(env)
	if plan.Primary.Args["workflow"] != "develop" {
		t.Errorf("workflow = %q, want develop", plan.Primary.Args["workflow"])
	}
	if len(plan.Alternatives) != 1 {
		t.Errorf("alternatives = %d, want 1 (add-services only; no adoptable)", len(plan.Alternatives))
	}
}

func TestBuildPlan_IdleWithAdoptableAndBootstrapped(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseIdle)
	env.Services = []ServiceSnapshot{
		{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Mode: topology.ModeDev},
		{Hostname: "legacy", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: false},
	}
	plan := BuildPlan(env)
	if plan.Primary.Args["workflow"] != "develop" {
		t.Errorf("primary workflow = %q, want develop", plan.Primary.Args["workflow"])
	}
	if len(plan.Alternatives) != 2 {
		t.Fatalf("alternatives = %d, want [adopt, add-services]", len(plan.Alternatives))
	}
	if plan.Alternatives[0].Args["workflow"] != "bootstrap" || plan.Alternatives[0].Args["route"] != "adopt" {
		t.Errorf("alt[0] = %v, want workflow=bootstrap route=adopt (H2 routing fix)", plan.Alternatives[0].Args)
	}
}

func TestBuildPlan_IdleOnlyUnmanagedRuntimes(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseIdle)
	env.Services = []ServiceSnapshot{
		{Hostname: "legacy", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: false},
	}
	plan := BuildPlan(env)
	if plan.Primary.Args["workflow"] != "bootstrap" || plan.Primary.Args["route"] != "adopt" {
		t.Errorf("primary = %v, want workflow=bootstrap route=adopt (H2 routing fix)", plan.Primary.Args)
	}
}

// TestCountIdleServices_PoolNotAdoptable pins, through the idle plan, that a
// pool or a Box counts as neither bootstrapped nor adoptable
// (docs/spec-workflows.md §8 PS-2): alone they are never adopted — a Box
// alone leaves the project empty for plan purposes, a pool alone is leased
// (TestBuildPlan_IdleDevPool_LeasesAContainer) — and next to a bootstrapped
// runtime they offer no adopt alternative.
func TestCountIdleServices_PoolNotAdoptable(t *testing.T) {
	t.Parallel()

	for _, class := range []topology.RuntimeClass{topology.RuntimePool, topology.RuntimeSandbox} {
		t.Run(string(class)+"_alone", func(t *testing.T) {
			t.Parallel()
			env := planEnvelope(PhaseIdle)
			env.Services = []ServiceSnapshot{{Hostname: "swarm", RuntimeClass: class}}
			plan := BuildPlan(env)
			if plan.Primary.Args["route"] == "adopt" {
				t.Errorf("primary = %v, never route=adopt", plan.Primary.Args)
			}
			if class == topology.RuntimeSandbox && plan.Primary.Args["workflow"] != "bootstrap" {
				t.Errorf("primary = %v, want a plain bootstrap start", plan.Primary.Args)
			}
		})
		t.Run(string(class)+"_beside_bootstrapped", func(t *testing.T) {
			t.Parallel()
			env := planEnvelope(PhaseIdle)
			env.Services = []ServiceSnapshot{
				{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Mode: topology.ModeDev},
				{Hostname: "swarm", RuntimeClass: class},
			}
			plan := BuildPlan(env)
			if plan.Primary.Args["workflow"] != "develop" {
				t.Errorf("primary workflow = %q, want develop", plan.Primary.Args["workflow"])
			}
			for _, alt := range plan.Alternatives {
				if alt.Args["route"] == "adopt" {
					t.Errorf("alternatives offer adopt: %v, want none for a %s", alt.Args, class)
				}
			}
		})
	}
}

// TestBuildPlan_IdleDevPool_LeasesAContainer pins the idle plan of a project
// whose only runtimes are Swarm pools (docs/spec-workflows.md §8 DP-29): its
// work is a lease, so the primary step acquires one — `pool` for one pool,
// `pools` (rendered as a list) for several — with adding services the
// alternative. A pool beside an app runtime may be infrastructure that app
// uses, so there no plan step names a lease at all.
func TestBuildPlan_IdleDevPool_LeasesAContainer(t *testing.T) {
	t.Parallel()

	pool := func(h string) ServiceSnapshot { return ServiceSnapshot{Hostname: h, RuntimeClass: topology.RuntimePool} }
	tests := []struct {
		name     string
		services []ServiceSnapshot
		wantArgs map[string]string // nil: no plan step names zerops_lease
	}{
		{"one pool", []ServiceSnapshot{pool("webpool"), {Hostname: "db", RuntimeClass: topology.RuntimeManaged}},
			map[string]string{"action": "acquire", "pool": "webpool"}},
		{"two pools, sorted", []ServiceSnapshot{pool("webpool"), pool("hqpool")},
			map[string]string{"action": "acquire", "pools": `["hqpool","webpool"]`}},
		{"a pool beside an unmanaged runtime", []ServiceSnapshot{{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic}, pool("webpool")}, nil},
		{"a pool beside a bootstrapped runtime", []ServiceSnapshot{{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Mode: topology.ModeDev}, pool("webpool")}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := planEnvelope(PhaseIdle)
			env.Services = tt.services
			plan := BuildPlan(env)
			steps := append([]NextAction{plan.Primary}, plan.Alternatives...)
			if plan.Secondary != nil {
				steps = append(steps, *plan.Secondary)
			}
			if tt.wantArgs == nil {
				for _, s := range steps {
					if s.Tool == "zerops_lease" {
						t.Errorf("plan step %+v leases a pool beside an app runtime", s)
					}
				}
				return
			}
			if plan.Primary.Tool != "zerops_lease" || !reflect.DeepEqual(plan.Primary.Args, tt.wantArgs) {
				t.Errorf("primary = %s %v, want zerops_lease %v", plan.Primary.Tool, plan.Primary.Args, tt.wantArgs)
			}
			if len(plan.Alternatives) != 1 || plan.Alternatives[0].Args["workflow"] != "bootstrap" || plan.Alternatives[0].Args["route"] != "" {
				t.Errorf("alternatives = %+v, want adding services only", plan.Alternatives)
			}
		})
	}
}

// TestBuildPlan_IdleOrphanCollapsesToBootstrap pins the post-E3 routing:
// after bootstrap-start absorbed orphan cleanup as a transparent
// side-effect, an orphan-only project (stale ServiceMetas, no live
// counterparts) reaches the standard `start bootstrap` branch — there is
// no longer a dedicated reset primary on the idle plan.
func TestBuildPlan_IdleOrphanCollapsesToBootstrap(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseIdle)
	plan := BuildPlan(env)
	if plan.Primary.Args["action"] != "start" {
		t.Errorf("primary action = %q, want start", plan.Primary.Args["action"])
	}
	if plan.Primary.Args["workflow"] != "bootstrap" {
		t.Errorf("primary workflow = %q, want bootstrap", plan.Primary.Args["workflow"])
	}
	if len(plan.Alternatives) != 0 {
		t.Errorf("alternatives = %d, want 0 (no reset alternative after E3)", len(plan.Alternatives))
	}
}

func TestBuildPlan_DevelopClosedAutoRecommendsCloseAndStartNext(t *testing.T) {
	t.Parallel()

	plan := BuildPlan(planEnvelope(PhaseDevelopClosed))
	if plan.Primary.Args["action"] != "close" {
		t.Errorf("primary action = %q, want close", plan.Primary.Args["action"])
	}
	if plan.Secondary == nil || plan.Secondary.Args["action"] != "start" {
		t.Errorf("secondary = %+v, want start", plan.Secondary)
	}
}

func TestBuildPlan_DevelopActiveDeployPending(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseDevelopActive)
	env.WorkSession = &WorkSessionSummary{
		Intent:   "fix login",
		Services: []string{"appdev"},
	}
	plan := BuildPlan(env)
	if plan.Primary.Tool != "zerops_deploy" {
		t.Errorf("tool = %q, want zerops_deploy", plan.Primary.Tool)
	}
	if plan.Primary.Args["targetService"] != "appdev" {
		t.Errorf("targetService = %q, want appdev", plan.Primary.Args["targetService"])
	}
	if plan.Primary.Args["sourceService"] != "" {
		t.Errorf("sourceService = %q, want empty (self-deploy)", plan.Primary.Args["sourceService"])
	}
}

// TestBuildPlan_DevelopActiveDeployPending_StageHalf_CrossDeploy pins H1
// (audit-prerelease-internal-testing-2026-04-29). Iterating
// WorkSession.Services with a stage half pending used to emit
// `zerops_deploy targetService=<stagehost>` — a self-deploy of stage,
// which DM-2 then rejects because stage's deployFiles is post-build-tree
// (cross-deploy from dev), not the full source. Now planDevelopActive
// looks up the dev half via env.Services and emits the cross-deploy
// shape: sourceService=<devhost>, targetService=<stagehost>, setup="prod".
// Mirrors the cadence pinned by develop-close-mode-auto-standard.
func TestBuildPlan_DevelopActiveDeployPending_StageHalf_CrossDeploy(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseDevelopActive)
	env.Services = []ServiceSnapshot{
		{Hostname: "appdev", Mode: topology.ModeStandard, StageHostname: "appstage", Bootstrapped: true, Deployed: true},
		{Hostname: "appstage", Mode: topology.ModeStage, Bootstrapped: true, Deployed: false},
	}
	env.WorkSession = &WorkSessionSummary{
		Intent:   "promote first deploy",
		Services: []string{"appstage"},
		Deploys:  map[string][]AttemptInfo{},
	}
	plan := BuildPlan(env)

	if plan.Primary.Tool != "zerops_deploy" {
		t.Fatalf("tool = %q, want zerops_deploy", plan.Primary.Tool)
	}
	if got := plan.Primary.Args["targetService"]; got != "appstage" {
		t.Errorf("targetService = %q, want appstage", got)
	}
	if got := plan.Primary.Args["sourceService"]; got != "appdev" {
		t.Errorf("sourceService = %q, want appdev (cross-deploy from dev half)", got)
	}
	if got := plan.Primary.Args["setup"]; got != "prod" {
		t.Errorf("setup = %q, want prod (matches develop-close-mode-auto-standard cadence)", got)
	}
}

// TestBuildPlan_DevelopActiveDeployPending_DevHalf_SelfDeploy pins the
// other half: when the dev hostname is pending, the typed Plan emits a
// SELF-deploy (no sourceService, no setup) — the dev runtime's
// deployFiles is `[.]`, full source, no cross-deploy needed.
func TestBuildPlan_DevelopActiveDeployPending_DevHalf_SelfDeploy(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseDevelopActive)
	env.Services = []ServiceSnapshot{
		{Hostname: "appdev", Mode: topology.ModeStandard, StageHostname: "appstage", Bootstrapped: true, Deployed: false},
		{Hostname: "appstage", Mode: topology.ModeStage, Bootstrapped: true, Deployed: false},
	}
	env.WorkSession = &WorkSessionSummary{
		Intent:   "first deploy",
		Services: []string{"appdev"},
		Deploys:  map[string][]AttemptInfo{},
	}
	plan := BuildPlan(env)

	if got := plan.Primary.Args["targetService"]; got != "appdev" {
		t.Errorf("targetService = %q, want appdev", got)
	}
	if got := plan.Primary.Args["sourceService"]; got != "" {
		t.Errorf("sourceService = %q, want empty (dev half is self-deploy)", got)
	}
	if got := plan.Primary.Args["setup"]; got != "" {
		t.Errorf("setup = %q, want empty (dev half uses default setup)", got)
	}
}

// TestBuildPlan_DevelopActive_OutOfScopeStageHalf pins RC-B in the typed
// Plan: a service the session left out of scope ("leave the stage as it
// is") never becomes a deploy or verify step — not the Primary while the dev
// half is pending, not after it is green, and not in Per service. The session
// closes on the required services alone, so the plan says close.
func TestBuildPlan_DevelopActive_OutOfScopeStageHalf(t *testing.T) {
	t.Parallel()

	green := map[string][]AttemptInfo{"appdev": {{Success: true}}}
	tests := []struct {
		name       string
		deploys    map[string][]AttemptInfo
		verifies   map[string][]AttemptInfo
		wantTool   string
		wantTarget string
		wantAction string
	}{
		{name: "dev half pending", deploys: map[string][]AttemptInfo{}, wantTool: "zerops_deploy", wantTarget: "appdev"},
		{name: "dev half deployed, verify pending", deploys: green, wantTool: "zerops_verify"},
		{name: "dev half green", deploys: green, verifies: green, wantTool: "zerops_workflow", wantAction: "close"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := planEnvelope(PhaseDevelopActive)
			env.Services = []ServiceSnapshot{
				{Hostname: "appdev", Mode: topology.ModeStandard, StageHostname: "appstage", Bootstrapped: true},
				{Hostname: "appstage", Mode: topology.ModeStage, Bootstrapped: true},
			}
			env.WorkSession = &WorkSessionSummary{
				Intent:   "stand up development",
				Services: []string{"appdev", "appstage"},
				Roles:    map[string]string{"appstage": RoleOutOfScope},
				Deploys:  tt.deploys,
				Verifies: tt.verifies,
			}
			plan := BuildPlan(env)

			if plan.Primary.Tool != tt.wantTool {
				t.Fatalf("Primary = %+v, want tool %s", plan.Primary, tt.wantTool)
			}
			if tt.wantTarget != "" && plan.Primary.Args["targetService"] != tt.wantTarget {
				t.Errorf("Primary target = %q, want %q", plan.Primary.Args["targetService"], tt.wantTarget)
			}
			if tt.wantAction != "" && plan.Primary.Args["action"] != tt.wantAction {
				t.Errorf("Primary action = %q, want %q", plan.Primary.Args["action"], tt.wantAction)
			}
			for _, arg := range []string{"targetService", "serviceHostname"} {
				if plan.Primary.Args[arg] == "appstage" {
					t.Errorf("the out-of-scope stage half is the Primary: %+v", plan.Primary)
				}
			}
			if _, listed := plan.PerService["appstage"]; listed {
				t.Errorf("the out-of-scope stage half is listed per service: %+v", plan.PerService["appstage"])
			}
		})
	}
}

func TestBuildPlan_DevelopActiveVerifyPending(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseDevelopActive)
	env.WorkSession = &WorkSessionSummary{
		Intent:   "fix login",
		Services: []string{"appdev"},
		Deploys: map[string][]AttemptInfo{
			"appdev": {{Success: true, Iteration: 1}},
		},
	}
	plan := BuildPlan(env)
	if plan.Primary.Tool != "zerops_verify" {
		t.Errorf("tool = %q, want zerops_verify", plan.Primary.Tool)
	}
}

func TestBuildPlan_DevelopActiveAllGreenSuggestsClose(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseDevelopActive)
	env.WorkSession = &WorkSessionSummary{
		Intent:   "fix login",
		Services: []string{"appdev"},
		Deploys: map[string][]AttemptInfo{
			"appdev": {{Success: true}},
		},
		Verifies: map[string][]AttemptInfo{
			"appdev": {{Success: true}},
		},
	}
	plan := BuildPlan(env)
	if plan.Primary.Args["action"] != "close" {
		t.Errorf("all-green session primary = %q, want close", plan.Primary.Args["action"])
	}
}

func TestBuildPlan_DevelopActiveFailedAttemptRoutesToLogs(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseDevelopActive)
	env.WorkSession = &WorkSessionSummary{
		Intent:   "ship release",
		Services: []string{"appdev"},
		Deploys: map[string][]AttemptInfo{
			"appdev": {{Success: true}, {Success: false, Iteration: 2}},
		},
		Verifies: map[string][]AttemptInfo{
			"appdev": {{Success: true}},
		},
	}
	plan := BuildPlan(env)
	if plan.Primary.Tool != "zerops_deploy" {
		t.Errorf("with last deploy failed, expected deploy retry, got %q", plan.Primary.Tool)
	}
	if plan.Primary.Args["targetService"] != "appdev" {
		t.Errorf("targetService = %q, want appdev", plan.Primary.Args["targetService"])
	}
}

func TestBuildPlan_BootstrapActive(t *testing.T) {
	t.Parallel()

	plan := BuildPlan(planEnvelope(PhaseBootstrapActive))
	if plan.Primary.Args["workflow"] != "bootstrap" || plan.Primary.Args["action"] != "iterate" {
		t.Errorf("primary args = %+v, want bootstrap/iterate", plan.Primary.Args)
	}
}

func TestBuildPlan_UnknownPhaseReturnsZero(t *testing.T) {
	t.Parallel()

	plan := BuildPlan(StateEnvelope{Phase: "ghost"})
	if !plan.Primary.IsZero() {
		t.Errorf("unknown phase should yield empty plan, got %+v", plan.Primary)
	}
}

func TestBuildPlan_DeterministicByHostnameOrder(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseDevelopActive)
	// Two services, both needing deploy. The plan should pick the first by
	// the work session's declared ordering (not by hash iteration).
	env.WorkSession = &WorkSessionSummary{
		Intent:   "migrate",
		Services: []string{"alpha", "beta"},
	}
	first := BuildPlan(env)
	for i := range 5 {
		got := BuildPlan(env)
		if got.Primary.Args["targetService"] != first.Primary.Args["targetService"] {
			t.Fatalf("iteration %d picked %q, original picked %q", i, got.Primary.Args["targetService"], first.Primary.Args["targetService"])
		}
	}
}

// TestBuildPlan_PerServicePopulatedMultiScope pins the per-service breakdown
// that the render layer shows under `Per service:`. All services have pending
// work → the map carries one entry per hostname.
func TestBuildPlan_PerServicePopulatedMultiScope(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseDevelopActive)
	env.WorkSession = &WorkSessionSummary{
		Intent:   "ship multi-service",
		Services: []string{"apidev", "webdev"},
		Deploys: map[string][]AttemptInfo{
			"apidev": {{Success: true, Iteration: 1}},
		},
		// webdev has no deploy yet → deploy action
		// apidev has deploy ok, no verify → verify action
	}
	plan := BuildPlan(env)
	if len(plan.PerService) != 2 {
		t.Fatalf("PerService = %d entries, want 2: %+v", len(plan.PerService), plan.PerService)
	}
	if plan.PerService["apidev"].Tool != "zerops_verify" {
		t.Errorf("apidev PerService tool = %q, want zerops_verify", plan.PerService["apidev"].Tool)
	}
	if plan.PerService["webdev"].Tool != "zerops_deploy" {
		t.Errorf("webdev PerService tool = %q, want zerops_deploy", plan.PerService["webdev"].Tool)
	}
}

// TestBuildPlan_PerServiceSkipsGreen pins the "green services are excluded"
// rule: a fully deployed+verified service does not appear in PerService.
// The remaining pending service still surfaces.
func TestBuildPlan_PerServiceSkipsGreen(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseDevelopActive)
	env.WorkSession = &WorkSessionSummary{
		Intent:   "mixed",
		Services: []string{"apidev", "webdev"},
		Deploys: map[string][]AttemptInfo{
			"apidev": {{Success: true, Iteration: 1}},
		},
		Verifies: map[string][]AttemptInfo{
			"apidev": {{Success: true, Iteration: 1}},
		},
	}
	plan := BuildPlan(env)
	if _, ok := plan.PerService["apidev"]; ok {
		t.Error("green service apidev must be excluded from PerService")
	}
	if plan.PerService["webdev"].Tool != "zerops_deploy" {
		t.Errorf("webdev tool = %q, want zerops_deploy", plan.PerService["webdev"].Tool)
	}
}

// TestBuildPlan_PerServiceSingleServiceStillPopulated asserts the map is
// populated even for a single-service scope — the render layer decides
// whether to emit the section based on len > 1. Keeping BuildPlan uniform
// lets renderers or JSON consumers inspect per-service state directly.
func TestBuildPlan_PerServiceSingleServiceStillPopulated(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseDevelopActive)
	env.WorkSession = &WorkSessionSummary{
		Intent:   "solo",
		Services: []string{"appdev"},
	}
	plan := BuildPlan(env)
	if len(plan.PerService) != 1 {
		t.Fatalf("PerService = %d entries, want 1: %+v", len(plan.PerService), plan.PerService)
	}
	if plan.PerService["appdev"].Tool != "zerops_deploy" {
		t.Errorf("appdev PerService tool = %q, want zerops_deploy", plan.PerService["appdev"].Tool)
	}
}

// TestBuildPlan_PerServiceNilWhenAllGreen pins the close-suggest branch:
// once every scope service is green, BuildPlan recommends close and
// PerService is nil (no pending work).
func TestBuildPlan_PerServiceNilWhenAllGreen(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseDevelopActive)
	env.WorkSession = &WorkSessionSummary{
		Intent:   "done",
		Services: []string{"appdev"},
		Deploys: map[string][]AttemptInfo{
			"appdev": {{Success: true}},
		},
		Verifies: map[string][]AttemptInfo{
			"appdev": {{Success: true}},
		},
	}
	plan := BuildPlan(env)
	if plan.PerService != nil {
		t.Errorf("PerService should be nil when all services are green, got %+v", plan.PerService)
	}
	if plan.Primary.Args["action"] != "close" {
		t.Errorf("primary action = %q, want close", plan.Primary.Args["action"])
	}
}

// TestBuildPlan_FailureTargetedRationale pins Phase 1 (C1) of the
// pipeline-repair plan: when the last deploy attempt carries a Reason +
// FailureClass, BuildPlan's Primary action surfaces the failure shape in
// its Rationale string. Without this, the Plan reads "No successful
// deploy recorded" even when the actual issue is a build timeout — losing
// the actionable signal post-compaction.
func TestBuildPlan_FailureTargetedRationale(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		failureClass   topology.FailureClass
		reason         string
		wantInRational []string
	}{
		{
			name:           "build_failure",
			failureClass:   topology.FailureClassBuild,
			reason:         "build timeout after 15min",
			wantInRational: []string{"build", "timeout"},
		},
		{
			name:           "start_failure",
			failureClass:   topology.FailureClassStart,
			reason:         "container didn't start",
			wantInRational: []string{"start", "didn't start"},
		},
		{
			name:           "config_failure",
			failureClass:   topology.FailureClassConfig,
			reason:         "zerops.yaml: deployFiles required",
			wantInRational: []string{"config", "deployFiles"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvLocal,
				WorkSession: &WorkSessionSummary{
					Intent:   "fix",
					Services: []string{"apidev"},
					Deploys: map[string][]AttemptInfo{
						"apidev": {{
							At:           time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC),
							Success:      false,
							Reason:       tc.reason,
							FailureClass: tc.failureClass,
						}},
					},
				},
			}

			plan := BuildPlan(env)

			if plan.Primary.Tool != "zerops_deploy" {
				t.Errorf("Primary.Tool = %q, want zerops_deploy", plan.Primary.Tool)
			}
			if plan.Primary.Args["targetService"] != "apidev" {
				t.Errorf("Primary.targetService = %q, want apidev", plan.Primary.Args["targetService"])
			}
			for _, want := range tc.wantInRational {
				if !strings.Contains(plan.Primary.Rationale, want) {
					t.Errorf("Rationale missing %q\nGot: %s", want, plan.Primary.Rationale)
				}
			}
		})
	}
}

// TestBuildPlan_PerServiceNilOutsideDevelopActive pins the scope: only the
// develop-active branch populates PerService. Other phases (idle, closed-auto,
// bootstrap) leave the field nil.
func TestBuildPlan_PerServiceNilOutsideDevelopActive(t *testing.T) {
	t.Parallel()

	phases := []Phase{PhaseIdle, PhaseDevelopClosed, PhaseBootstrapActive}
	for _, p := range phases {
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()
			plan := BuildPlan(planEnvelope(p))
			if plan.PerService != nil {
				t.Errorf("phase %s: PerService = %+v, want nil", p, plan.PerService)
			}
		})
	}
}

// TestBuildPlan_PoolDevHalf_NoDeployStep pins the plan of a pair whose dev
// half is a pool: the pool half is never a deploy or verify step, not even
// when a session names it, and the stage's deploy runs from a held lease of
// the pool — or, with none held, the step is acquiring one.
func TestBuildPlan_PoolDevHalf_NoDeployStep(t *testing.T) {
	t.Parallel()
	held := LeaseSnapshot{Alias: "apppool-4", Pool: "apppool", Branch: "feat-x", State: "held"}
	cases := []struct {
		name     string
		leases   *[]LeaseSnapshot
		wantTool string
		wantArgs map[string]string
	}{
		{"a held lease: deploy the stage from it", &[]LeaseSnapshot{held}, "zerops_deploy",
			map[string]string{"sourceService": "apppool-4", "targetService": "appstage", "setup": "web"}},
		{"no lease held: acquire one first", &[]LeaseSnapshot{}, "zerops_lease",
			map[string]string{"action": "acquire", "pool": "apppool"}},
		{"leases not read: acquire one first", nil, "zerops_lease",
			map[string]string{"action": "acquire", "pool": "apppool"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := planEnvelope(PhaseDevelopActive)
			env.Services = poolDevPairServices(topology.CloseModeAuto, topology.GitPushUnconfigured)
			env.Leases = tc.leases
			env.WorkSession = &WorkSessionSummary{
				Intent:   "ship the change",
				Services: []string{"apppool", "appstage"},
				Deploys:  map[string][]AttemptInfo{},
			}
			plan := BuildPlan(env)
			if plan.Primary.Tool != tc.wantTool || !reflect.DeepEqual(plan.Primary.Args, tc.wantArgs) {
				t.Errorf("primary = %s %v, want %s %v", plan.Primary.Tool, plan.Primary.Args, tc.wantTool, tc.wantArgs)
			}
			if _, ok := plan.PerService["apppool"]; ok {
				t.Errorf("the pool half has a step: %+v", plan.PerService["apppool"])
			}
			if !strings.Contains(plan.Primary.Rationale, "apppool") {
				t.Errorf("rationale %q does not name the pool", plan.Primary.Rationale)
			}
		})
	}

	// Once the stage is deployed and verified, nothing is left: the pool
	// half never holds the session open.
	env := planEnvelope(PhaseDevelopActive)
	env.Services = poolDevPairServices(topology.CloseModeAuto, topology.GitPushUnconfigured)
	at := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	env.WorkSession = &WorkSessionSummary{
		Intent:   "ship the change",
		Services: []string{"apppool", "appstage"},
		Deploys:  map[string][]AttemptInfo{"appstage": {{At: at, Success: true}}},
		Verifies: map[string][]AttemptInfo{"appstage": {{At: at, Success: true}}},
	}
	if plan := BuildPlan(env); plan.Primary.Args["action"] != "close" || plan.PerService != nil {
		t.Errorf("plan = %+v, want close with nothing per service", plan)
	}
}
