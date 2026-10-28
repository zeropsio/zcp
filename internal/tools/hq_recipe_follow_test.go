// Tests for: tools/hq_recipe_follow.go — a tool call that changes what the
// group recipe reads runs the recipe pass again, after the call answered and
// off its path, one pass for a burst of calls (docs/spec-mate.md §10.10).
package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// followedPasses is a follower whose passes are counted instead of run: each
// pass answers the next of lines (the last one repeats, "" with none), waits
// for hold to close when hold is set, and records whether its context was
// still alive.
type followedPasses struct {
	t      *testing.T
	wg     sync.WaitGroup
	mu     sync.Mutex
	passes int
	lines  []string
	hold   chan struct{}
	dead   []bool
	said   []string
}

func newFollowedPasses(t *testing.T, ctx context.Context, stateDir string, quiet time.Duration) (*RecipeFollower, *followedPasses) {
	t.Helper()
	p := &followedPasses{t: t}
	f := newRecipeFollower(ctx, stateDir, p.pass, p.wg.Go, p.say, quiet, time.Minute)
	return f, p
}

func (p *followedPasses) pass(ctx context.Context) string {
	p.mu.Lock()
	n := p.passes
	p.passes++
	hold := p.hold
	line := ""
	if len(p.lines) > 0 {
		line = p.lines[min(n, len(p.lines)-1)]
	}
	p.mu.Unlock()
	if hold != nil {
		<-hold
	}
	p.mu.Lock()
	p.dead = append(p.dead, ctx.Err() != nil)
	p.mu.Unlock()
	return line
}

func (p *followedPasses) say(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.said = append(p.said, line)
}

// settle waits for every pass asked for to have run, and answers how many
// did.
func (p *followedPasses) settle() int {
	p.t.Helper()
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		p.t.Fatal("the follower's passes did not end")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.passes
}

// call is one tool call the follower watches: its arguments as JSON, during
// what it does to zcp's records while it runs, and whether it answered with
// an error.
func call(t *testing.T, f *RecipeFollower, tool string, args map[string]any, during func(), failed bool) {
	t.Helper()
	var raw json.RawMessage
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		raw = b
	}
	answered := f.Watch(tool, raw)
	if during != nil {
		during()
	}
	answered(failed)
}

// pairStateDir is a state directory holding one finished dev/stage pair,
// appdev and appstage, each half's setup recorded.
func pairStateDir(t *testing.T) string {
	t.Helper()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}},
		PrimarySetupName: "dev", BootstrapSession: "test", BootstrappedAt: "2026-10-02",
	}); err != nil {
		t.Fatal(err)
	}
	return stateDir
}

// recordSetup is a call's write of setup as host's recorded setup — what a
// deploy writes back before it builds, and set-default-setup.
func recordSetup(t *testing.T, stateDir, host, setup string) func() {
	return func() {
		t.Helper()
		if err := recordResolvedDeploySetupMeta(stateDir, host, setup, true); err != nil {
			t.Fatal(err)
		}
	}
}

// TestRecipeFollower_AfterACall_FollowsWhatTheRecipeReads: a call that
// changed what the group recipe reads asks for one pass, once it answered
// without an error — an import or a delete (the services a tier creates), a
// variable set or deleted (what a tier carries, and which services a
// runtime reads), the retire of a pool migration (a pair keyed by its pool),
// a deploy to a stage (the type it runs as); a call that moved what zcp's
// records hold of the pairs (a setup) asks for one whatever it answered,
// since the record moved. A call the recipe reads nothing of — a scale and
// public access, which change no tier's shape, a read, a lease — asks for
// none.
func TestRecipeFollower_AfterACall_FollowsWhatTheRecipeReads(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		tool   string
		args   map[string]any
		during func(t *testing.T, stateDir string) func()
		failed bool
		want   int
	}{
		{name: "an import", tool: "zerops_import", args: map[string]any{"content": "services: []"}, want: 1},
		{name: "an import that failed", tool: "zerops_import", args: map[string]any{"content": "services: []"}, failed: true},
		{name: "a delete", tool: "zerops_delete", args: map[string]any{"serviceHostname": "cache", "consent": "cache"}, want: 1},
		{name: "a delete refused", tool: "zerops_delete", args: map[string]any{"serviceHostname": "cache"}, failed: true},
		{name: "a variable set", tool: "zerops_env", args: map[string]any{"action": "set", "serviceHostname": "appstage", "variables": []string{"A=b"}}, want: 1},
		{name: "a variable deleted", tool: "zerops_env", args: map[string]any{"action": "delete", "project": true, "variables": []string{"A"}}, want: 1},
		{name: "the organization's AI key", tool: "zerops_env", args: map[string]any{"action": "ai-key"}, want: 1},
		{name: "a variable read", tool: "zerops_env", args: map[string]any{"action": "get", "serviceHostname": "appstage"}},
		{name: "a variable requested of the person", tool: "zerops_env", args: map[string]any{"action": "request", "key": "STRIPE_KEY"}},
		{name: "a pool migration's retire", tool: "zerops_workflow", args: map[string]any{"workflow": "pool-migrate", "action": "retire", "service": "appdev"}, want: 1},
		{name: "a status", tool: "zerops_workflow", args: map[string]any{"action": "status"}},
		{name: "set-default-setup recording another stage setup", tool: "zerops_workflow", args: map[string]any{"action": "set-default-setup", "targetService": "appstage"},
			during: func(t *testing.T, stateDir string) func() { return recordSetup(t, stateDir, "appstage", "stage") }, want: 1},
		{name: "a deploy to the stage", tool: "zerops_deploy", args: map[string]any{"sourceService": "appdev", "targetService": "appstage"}, want: 1},
		{name: "a deploy to the stage that failed", tool: "zerops_deploy", args: map[string]any{"sourceService": "appdev", "targetService": "appstage"}, failed: true},
		{name: "a deploy to the dev keeping its setup", tool: "zerops_deploy", args: map[string]any{"targetService": "appdev"},
			during: func(t *testing.T, stateDir string) func() { return recordSetup(t, stateDir, "appdev", "dev") }},
		{name: "a deploy to the dev that recorded another setup and failed", tool: "zerops_deploy", args: map[string]any{"targetService": "appdev", "setup": "api"},
			during: func(t *testing.T, stateDir string) func() { return recordSetup(t, stateDir, "appdev", "api") }, failed: true, want: 1},
		{name: "a batch with the stage among its targets", tool: "zerops_deploy_batch",
			args: map[string]any{"targets": []map[string]string{{"targetService": "appdev"}, {"sourceService": "appdev", "targetService": "appstage"}}}, want: 1},
		{name: "a batch to the dev alone", tool: "zerops_deploy_batch", args: map[string]any{"targets": []map[string]string{{"targetService": "appdev"}}}},
		{name: "a scale", tool: "zerops_scale", args: map[string]any{"serviceHostname": "appstage", "maxCpu": 4}},
		{name: "public access", tool: "zerops_subdomain", args: map[string]any{"serviceHostname": "appstage", "action": "enable"}},
		{name: "a restart", tool: "zerops_manage", args: map[string]any{"serviceHostname": "appstage", "action": "restart"}},
		{name: "a discover", tool: "zerops_discover"},
		{name: "a lease", tool: "zerops_lease", args: map[string]any{"action": "acquire", "pool": "apppool"}},
		{name: "arguments that do not parse", tool: "zerops_env", args: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := pairStateDir(t)
			f, passes := newFollowedPasses(t, t.Context(), stateDir, time.Millisecond)
			var during func()
			if tt.during != nil {
				during = tt.during(t, stateDir)
			}

			call(t, f, tt.tool, tt.args, during, tt.failed)

			if got := passes.settle(); got != tt.want {
				t.Errorf("%s asked for %d passes, want %d", tt.name, got, tt.want)
			}
		})
	}
}

// TestRecipeFollower_ABurst_IsOnePass: several calls in a row — an import,
// its variables, another import — are one pass, run once the calls stopped
// coming, under the follower's own context, which the calls' ending leaves
// alive.
func TestRecipeFollower_ABurst_IsOnePass(t *testing.T) {
	t.Parallel()
	stateDir := pairStateDir(t)
	f, passes := newFollowedPasses(t, t.Context(), stateDir, 50*time.Millisecond)

	for _, tool := range []string{"zerops_import", "zerops_import", "zerops_delete", "zerops_import"} {
		call(t, f, tool, nil, nil, false)
	}
	call(t, f, "zerops_env", map[string]any{"action": "set", "project": true, "variables": []string{"A=b"}}, nil, false)

	if got := passes.settle(); got != 1 {
		t.Errorf("a burst of five calls ran %d passes, want 1", got)
	}
	if slices.Contains(passes.dead, true) {
		t.Error("a pass ran with its context already ended")
	}
}

// TestRecipeFollower_ACallDuringAPass_IsOneMorePass: a call that lands while
// a pass runs is one more pass after it — however many such calls — never a
// second pass beside it.
func TestRecipeFollower_ACallDuringAPass_IsOneMorePass(t *testing.T) {
	t.Parallel()
	stateDir := pairStateDir(t)
	f, passes := newFollowedPasses(t, t.Context(), stateDir, time.Millisecond)
	passes.hold = make(chan struct{})

	call(t, f, "zerops_import", nil, nil, false)
	deadline := time.Now().Add(10 * time.Second)
	for {
		passes.mu.Lock()
		started := passes.passes
		passes.mu.Unlock()
		if started == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the first pass never started")
		}
		time.Sleep(time.Millisecond)
	}
	for range 3 {
		call(t, f, "zerops_import", nil, nil, false)
	}
	close(passes.hold)

	if got := passes.settle(); got != 2 {
		t.Errorf("three calls during a pass made %d passes in all, want 2", got)
	}
}

// TestRecipeFollower_ServerStopped_RunsNoPass: a pass still waiting for the
// calls to stop is dropped when the server stops — zcp's next start is a
// pass — and a call after that asks for none.
func TestRecipeFollower_ServerStopped_RunsNoPass(t *testing.T) {
	t.Parallel()
	stateDir := pairStateDir(t)
	ctx, stop := context.WithCancel(t.Context())
	f, passes := newFollowedPasses(t, ctx, stateDir, time.Hour)

	call(t, f, "zerops_import", nil, nil, false)
	stop()
	call(t, f, "zerops_import", nil, nil, false)

	if got := passes.settle(); got != 0 {
		t.Errorf("a stopped server ran %d passes, want none", got)
	}
}

// TestRecipeFollower_AnOutcome_SaidOnce: what a pass says goes to the log
// once: a pass that says what the last one said — a pair the recipe still
// waits for, a tier still refused — says nothing, nor does one with nothing
// to say.
func TestRecipeFollower_AnOutcome_SaidOnce(t *testing.T) {
	t.Parallel()
	stateDir := pairStateDir(t)
	f, passes := newFollowedPasses(t, t.Context(), stateDir, time.Millisecond)
	passes.lines = []string{"the recipe waits for appdev", "the recipe waits for appdev", "", "the recipe waits for appdev", "change #1 carries the update"}

	for range passes.lines {
		call(t, f, "zerops_import", nil, nil, false)
		passes.settle()
	}

	if want := []string{"the recipe waits for appdev", "change #1 carries the update"}; !slices.Equal(passes.said, want) {
		t.Errorf("said %q, want %q", passes.said, want)
	}
}

// TestNewRecipeFollower_OutsideAMate_FollowsNothing: the follower is a
// Mate's (spec-mate §2.0): anywhere else there is none, and a call watched
// by none is answered as ever.
func TestNewRecipeFollower_OutsideAMate_FollowsNothing(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	spawn := func(func()) { t.Error("a follower outside a Mate started work") }
	tests := []struct {
		name string
		rt   runtime.Info
	}{
		{name: "a container that is no Mate", rt: runtime.Info{InContainer: true}},
		{name: "a Mate's environment off a container", rt: runtime.Info{MateEnabled: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := NewRecipeFollower(t.Context(), mock, &http.Client{}, tt.rt, t.TempDir(), spawn, nil)
			if f != nil {
				t.Fatal("a follower outside a Mate")
			}
			f.Watch("zerops_import", nil)(false)
		})
	}
}

// followRecipe is one tool call of tool over the lab, watched by a Mate's
// follower whose pass composes from client: it answers what the pass said
// once it ran.
func (l *hqLab) followRecipe(client platform.Client, tool string) []string {
	l.t.Helper()
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		said []string
	)
	f := newRecipeFollower(l.t.Context(), l.stateDir, func(ctx context.Context) string {
		return groupRecipeUnasked(ctx, client, l.hq.srv.Client(), l.rt, l.stateDir)
	}, wg.Go, func(line string) {
		mu.Lock()
		defer mu.Unlock()
		said = append(said, line)
	}, time.Millisecond, time.Second)
	f.Watch(tool, nil)(false)
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	return said
}

// TestGroupRecipeFollow_ReopensNothingAPersonDeclined: the pass a tool call
// asks for is nobody's ask, as zcp's start is not: a recipe proposal a person
// closed unmerged is not opened again, while one never proposed is.
func TestGroupRecipeFollow_ReopensNothingAPersonDeclined(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		settle   func(*fakeHQ)
		wantOpen int
	}{
		{name: "never proposed", wantOpen: 1},
		{name: "a person closed it unmerged", settle: func(f *fakeHQ) { f.closeChange(hq.RecipeRepo, 1) }},
		{name: "Core landed it", settle: func(f *fakeHQ) { f.mergeChange(hq.RecipeRepo, 1) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lab := newRecipeLab(t)
			lab.hq.unbornRecipe()
			if tt.settle != nil {
				lab.mustPropose(lab.mock)
				tt.settle(lab.hq)
			}

			said := lab.followRecipe(lab.mock, "zerops_import")

			open := 0
			for number := 1; number <= 2; number++ {
				if c := lab.hq.changeIn(hq.RecipeRepo, number); c != nil && c.State == hq.ChangeOpen {
					open = number
				}
			}
			if open != tt.wantOpen {
				t.Errorf("the pass left change #%d open, want #%d (said %q)", open, tt.wantOpen, said)
			}
			if tt.wantOpen == 0 && len(said) > 0 {
				t.Errorf("a pass that changed nothing said %q", said)
			}
		})
	}
}

// TestGroupRecipeFollow_TheMatesOneChange: the pass a tool call asks for
// updates the Mate's one open recipe change and never opens a second; a pass
// over a project that did not change since pushes nothing, so Core judges
// the change no more often than the project changes.
func TestGroupRecipeFollow_TheMatesOneChange(t *testing.T) {
	t.Parallel()
	lab := newRecipeLab(t)
	lab.followRecipe(lab.mock, "zerops_import")
	first := lab.hq.recipeHead(recipeBranch(1))
	cache := recipeReconcileClient(platform.ServiceStack{ID: "svc-cache", Name: "cache", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "valkey:single@7", ServiceStackTypeCategoryName: "USER"}})

	said := lab.followRecipe(cache, "zerops_import")

	if lab.hq.changeIn(hq.RecipeRepo, 2) != nil || lab.hq.openCount(hq.RecipeRepo) != 1 {
		t.Fatalf("the pass after an import opened a second recipe change (said %q)", said)
	}
	updated := lab.hq.recipeHead(recipeBranch(1))
	if updated == first || !strings.Contains(lab.hq.recipeFiles(recipeBranch(1))[recipeAgentTier], "hostname: cache") {
		t.Errorf("change #1 does not carry the imported service (said %q)", said)
	}
	for range 2 {
		lab.followRecipe(cache, "zerops_env")
	}
	if lab.hq.recipeHead(recipeBranch(1)) != updated {
		t.Error("a pass over an unchanged project pushed change #1 again")
	}
}

// TestGroupRecipeFollow_MainCarriesTheRecipe_OpensAndPushesNothing: once main
// of the recipe repository carries what the project composes, the passes tool
// calls ask for open no change and push nothing.
func TestGroupRecipeFollow_MainCarriesTheRecipe_OpensAndPushesNothing(t *testing.T) {
	t.Parallel()
	lab := newRecipeLab(t)
	lab.mustPropose(lab.mock)
	main := lab.hq.mergeChange(hq.RecipeRepo, 1)

	for _, tool := range []string{"zerops_import", "zerops_env", "zerops_delete"} {
		if said := lab.followRecipe(lab.mock, tool); len(said) > 0 {
			t.Errorf("a pass after %s said %q over a main that carries the recipe", tool, said)
		}
	}

	if lab.hq.changeIn(hq.RecipeRepo, 2) != nil {
		t.Error("a pass opened a change over a main that carries the recipe")
	}
	if got := lab.hq.recipeHead("main"); got != main {
		t.Errorf("main moved from %s to %s", main, got)
	}
}
