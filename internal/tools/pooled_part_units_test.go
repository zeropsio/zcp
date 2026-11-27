package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// pooledPlanFixture is a Mate whose stand-up records every call a pooled
// plan's birth would make: each request to its HQ and each import on its
// platform. HQ refuses every call, so a birth that starts stops at its first
// HQ call, recorded.
type pooledPlanFixture struct {
	dir     string
	mu      sync.Mutex
	hqCalls int
	client  *importCountingClient
	birth   *pooledBirth
	rt      runtime.Info
}

// importCountingClient is the platform mock counting the imports made on it.
type importCountingClient struct {
	*platform.Mock
	mu      sync.Mutex
	imports int
}

func (c *importCountingClient) ImportServices(ctx context.Context, projectID, yaml string) (*platform.ImportResult, error) {
	c.mu.Lock()
	c.imports++
	c.mu.Unlock()
	return c.Mock.ImportServices(ctx, projectID, yaml)
}

func newPooledPlanFixture(t *testing.T) *pooledPlanFixture {
	t.Helper()
	f := &pooledPlanFixture{dir: t.TempDir(), client: &importCountingClient{Mock: platform.NewMock()}}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		f.hqCalls++
		f.mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"forbidden","reason":"the fake refuses every call"}`))
	}))
	t.Cleanup(srv.Close)
	f.rt = runtime.Info{InContainer: true, MateEnabled: true, ProjectID: "p1", ServiceName: "zcp"}
	f.birth = &pooledBirth{d: standupDeps{
		batch:    batchDeployer{client: f.client, httpClient: srv.Client(), projectID: "p1", rtInfo: f.rt, stateDir: f.dir},
		identity: func() hq.Identity { return hq.Identity{Origin: srv.URL, OrgID: "org", ProjectID: "p1", Key: "key"} },
	}, progress: newStandupProgress(nil)}
	return f
}

// calls is what the stand-up made so far: HQ requests and imports.
func (f *pooledPlanFixture) calls() (hqCalls, imports int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.client.mu.Lock()
	defer f.client.mu.Unlock()
	return f.hqCalls, f.client.imports
}

// send completes discover of engine's bootstrap with plan and repository, as
// zerops_workflow does, and returns the answer's text and whether it is an
// error.
func (f *pooledPlanFixture) send(t *testing.T, engine *workflow.Engine, plan []workflow.BootstrapTarget, repository string) (string, bool) {
	t.Helper()
	input := WorkflowInput{Step: workflow.StepDiscover, Plan: plan, Repository: repository}
	result, _, err := handleBootstrapComplete(context.Background(), engine, f.client, nil, nil, input, nil, "p1", f.dir, nil, nil, f.rt, f.birth)
	if err != nil {
		t.Fatalf("handleBootstrapComplete: %v", err)
	}
	return extractText(result), result.IsError
}

// startedMateEngine is a Mate engine over dir with a classic bootstrap at
// discover.
func startedMateEngine(t *testing.T, dir string) *workflow.Engine {
	t.Helper()
	engine := workflow.NewMateEngine(dir, workflow.EnvContainer, nil)
	if _, err := engine.BootstrapStartWithRoute("p1", "two parts", workflow.BootstrapRouteClassic, ""); err != nil {
		t.Fatalf("start bootstrap: %v", err)
	}
	return engine
}

// pooledParts is a plan of a part per name, each its pool beside its stage.
func pooledParts(names ...string) []workflow.BootstrapTarget {
	plan := make([]workflow.BootstrapTarget, 0, len(names))
	for _, n := range names {
		plan = append(plan, workflow.BootstrapTarget{Runtime: workflow.RuntimeTarget{
			DevHostname: n + "pool", ExplicitStage: n + "stage", Type: "nodejs@22", BootstrapMode: topology.PlanModeStandard,
		}})
	}
	return plan
}

// completedPooledDiscover is the record a pooled plan's discover leaves once
// its parts stand: the plan's parts as existing targets, with the
// repository they share.
func completedPooledDiscover(t *testing.T, engine *workflow.Engine, plan []workflow.BootstrapTarget, repository string) {
	t.Helper()
	if _, err := engine.BootstrapCompletePooledPlan(pooledPlanTargets(plan), repository, nil); err != nil {
		t.Fatalf("complete the pooled discover: %v", err)
	}
}

// interrupted makes engine's session the session of a process that ended.
func interrupted(t *testing.T, engine *workflow.Engine) string {
	t.Helper()
	id := engine.SessionID()
	state, err := workflow.LoadSessionByID(engine.StateDir(), id)
	if err != nil {
		t.Fatal(err)
	}
	state.PID, state.StartTime = 0, ""
	if err := workflow.SaveSessionState(engine.StateDir(), id, state); err != nil {
		t.Fatal(err)
	}
	if err := workflow.UnregisterSession(engine.StateDir(), id); err != nil {
		t.Fatal(err)
	}
	if err := workflow.RegisterSession(engine.StateDir(), workflow.SessionEntry{SessionID: id, Workflow: workflow.WorkflowBootstrap, ProjectID: "p1"}); err != nil {
		t.Fatal(err)
	}
	return id
}

// assertReplayed holds the replay answer: the bootstrap's provision step as
// status gives it, and the stand-up called nothing.
func assertReplayed(t *testing.T, f *pooledPlanFixture, text string, isError bool) {
	t.Helper()
	if isError || !strings.Contains(text, `"name":"provision"`) {
		t.Fatalf("the same plan again is not answered with the provision step:\n%s", text)
	}
	if hqCalls, imports := f.calls(); hqCalls != 0 || imports != 0 {
		t.Fatalf("the replay made %d HQ calls and %d imports, want none", hqCalls, imports)
	}
}

// witness: internal/tools/pooled_part.go | if samePooledPlan(state.Bootstrap.Plan, targets, repository) { → if false {
func TestPooledPlan_PPReplay_SamePlanAnswersTheCurrentStepAndRunsNothing(t *testing.T) {
	t.Parallel()
	f := newPooledPlanFixture(t)
	engine := startedMateEngine(t, f.dir)
	plan := pooledParts("lfa", "lfb")
	completedPooledDiscover(t, engine, plan, "l2fmono")

	text, isError := f.send(t, engine, pooledParts("lfa", "lfb"), "l2fmono")
	assertReplayed(t, f, text, isError)
}

// witness: internal/tools/pooled_part.go | recordedPlanWords(state.Bootstrap.Plan) → "its plan"
func TestPooledPlan_PPOther_DifferentPlanRefusedWithTheRecordedPlan(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		plan       []workflow.BootstrapTarget
		repository string
	}{
		{"another part", pooledParts("lfa", "lfc"), "l2fmono"},
		{"another repository", pooledParts("lfa", "lfb"), "l2fother"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newPooledPlanFixture(t)
			engine := startedMateEngine(t, f.dir)
			completedPooledDiscover(t, engine, pooledParts("lfa", "lfb"), "l2fmono")

			text, isError := f.send(t, engine, tc.plan, tc.repository)
			if !isError {
				t.Fatalf("a different plan past discover is not refused:\n%s", text)
			}
			for _, want := range []string{
				"Discover of this bootstrap is complete with its recorded plan",
				`lfapool \u2192 lfastage (nodejs@22), lfbpool \u2192 lfbstage (nodejs@22) in repository \"l2fmono\"`,
				`zerops_workflow action=\"reset\"`,
			} {
				if !strings.Contains(text, want) && !strings.Contains(text, strings.ReplaceAll(want, `\u2192`, "→")) {
					t.Errorf("the refusal lacks %s:\n%s", want, text)
				}
			}
			if strings.Contains(text, "send the same plan again") {
				t.Errorf("the refusal says to send the same plan again:\n%s", text)
			}
			if hqCalls, imports := f.calls(); hqCalls != 0 || imports != 0 {
				t.Errorf("the refusal made %d HQ calls and %d imports, want none", hqCalls, imports)
			}
		})
	}
}

// witness: internal/tools/pooled_part.go | errors.As(err, &held) && held.Interrupted { → errors.As(err, &held) && !held.Interrupted {
func TestPooledPlan_PPHeld_InterruptedSessionNamesItsResume(t *testing.T) {
	t.Parallel()
	f := newPooledPlanFixture(t)
	first := startedMateEngine(t, f.dir)
	completedPooledDiscover(t, first, pooledParts("lfa", "lfb"), "l2fmono")
	firstID := interrupted(t, first)

	second := startedMateEngine(t, f.dir)
	text, isError := f.send(t, second, pooledParts("lfa", "lfb"), "l2fmono")
	if !isError {
		t.Fatalf("a plan an interrupted bootstrap holds is not refused:\n%s", text)
	}
	for _, want := range []string{firstID, `zerops_workflow action=\"resume\" sessionId=\"` + firstID + `\"`, `zerops_workflow action=\"reset\"`} {
		if !strings.Contains(text, want) {
			t.Errorf("the refusal lacks %s:\n%s", want, text)
		}
	}
	if strings.Contains(strings.ToLower(text), "send the same plan again") {
		t.Errorf("the refusal says to send the same plan again:\n%s", text)
	}
	if hqCalls, imports := f.calls(); hqCalls != 0 || imports != 0 {
		t.Fatalf("the refusal made %d HQ calls and %d imports, want none", hqCalls, imports)
	}

	if _, err := second.Resume(firstID); err != nil {
		t.Fatalf("resume the interrupted bootstrap: %v", err)
	}
	text, isError = f.send(t, second, pooledParts("lfa", "lfb"), "l2fmono")
	assertReplayed(t, f, text, isError)
}

// witness: internal/tools/pooled_part.go | if state.Bootstrap.CurrentStepName() != workflow.StepDiscover { → if state.Bootstrap.CurrentStepName() == workflow.StepDiscover {
func TestPooledPlan_PPRetry_RefusedBirthLeavesDiscoverForTheSamePlan(t *testing.T) {
	t.Parallel()
	f := newPooledPlanFixture(t)
	engine := startedMateEngine(t, f.dir)
	for call := 1; call <= 2; call++ {
		text, isError := f.send(t, engine, pooledParts("lfa", "lfb"), "l2fmono")
		if !isError || !strings.Contains(text, "HQ did not ensure the repository") || !strings.Contains(text, "send the same plan again") {
			t.Fatalf("call %d is not HQ's refusal with the way on:\n%s", call, text)
		}
		if hqCalls, imports := f.calls(); hqCalls != call || imports != 0 {
			t.Fatalf("after call %d the birth made %d HQ calls and %d imports, want %d and none", call, hqCalls, imports, call)
		}
		state, err := engine.GetState()
		if err != nil || state.Bootstrap.CurrentStepName() != workflow.StepDiscover || state.Bootstrap.Plan != nil {
			t.Fatalf("after call %d the bootstrap is not at discover without a plan: %v %+v", call, err, state)
		}
	}
}
