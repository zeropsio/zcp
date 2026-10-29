// Tests for: tools/hq_recipe.go — one group recipe pass at a time on a Mate,
// across every zcp process it runs, against a fake HQ that serves real git
// (hq_lab_test.go).
package tools

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// waitAtMost waits for done, or for d to pass.
func waitAtMost(done <-chan struct{}, d time.Duration) {
	select {
	case <-done:
	case <-time.After(d):
	}
}

// TestGroupRecipePass_TwoAtOnceOntoUnbornMain_OpensOneChange: two recipe
// passes at once — zcp's start and the agent's ask in one process, or the
// starts of two conversations' zcp — against a recipe repository without
// main open one change between them. The second runs once the first is done
// and reads HQ afresh: main carrying the tiers Core landed, or the first's
// change. Both used to read the Mate's state and main before either change
// existed: Core landed the first, and the second pushed a root commit of its
// own onto change #2, unrelated to main, which can never land (Mate Web,
// 2026-10-27).
func TestGroupRecipePass_TwoAtOnceOntoUnbornMain_OpensOneChange(t *testing.T) {
	t.Parallel()
	lab := newRecipeLab(t)
	lab.hq.unbornRecipe()
	// HQ lets the passes overlap as far as they can: the first change opens
	// once a second pass asks to open one too — or after a second, when none
	// does — and Core lands it, adding only, on its push; the second open is
	// answered once that landed.
	var opens atomic.Int32
	secondOpen, landed := make(chan struct{}), make(chan struct{})
	var landOnce sync.Once
	recipePush := "/git/" + labOrg + "/" + labApp + "/" + hq.RecipeRepo + ".git/git-receive-pack"
	lab.hq.around(func(r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/mate/changes" {
			return
		}
		switch opens.Add(1) {
		case 1:
			waitAtMost(secondOpen, time.Second)
		case 2:
			close(secondOpen)
			waitAtMost(landed, 10*time.Second)
		}
	}, func(r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == recipePush {
			landOnce.Do(func() {
				lab.hq.mergeChange(hq.RecipeRepo, 1)
				close(landed)
			})
		}
	})

	outcomes := make([]recipeOutcome, 2)
	var wg sync.WaitGroup
	for i := range outcomes {
		wg.Go(func() {
			outcomes[i] = groupRecipeOutcome(t.Context(), lab.mock, lab.hq.srv.Client(), lab.rt, lab.stateDir, lab.schemas)
		})
	}
	wg.Wait()

	if c := lab.hq.changeIn(hq.RecipeRepo, 2); c != nil {
		t.Fatalf("two passes at once opened change #2 (%s, on %s) beside #1; outcomes %+v and %+v", c.State, c.Branch, outcomes[0], outcomes[1])
	}
	first, second := outcomes[0], outcomes[1]
	if !first.Created {
		first, second = second, first
	}
	if !first.Created || first.Change != 1 {
		t.Fatalf("no pass opened change #1; outcomes %+v and %+v", outcomes[0], outcomes[1])
	}
	if second.Created || !second.OnMain && second.Change != 1 {
		t.Errorf("the second pass = %+v, want it to find main carrying the tiers or change #1", second)
	}
}

// TestGroupRecipePass_AnotherPassRunning_ChangesNothing: a pass that cannot
// start before its call ends — another pass on the Mate holds the recipe all
// that time — changes nothing, asks HQ nothing, and says another pass is
// running; once that one is done, the next pass proposes.
func TestGroupRecipePass_AnotherPassRunning_ChangesNothing(t *testing.T) {
	t.Parallel()
	lab := newRecipeLab(t)
	lab.hq.unbornRecipe()
	// Another zcp process's pass, holding the recipe.
	dir := filepath.Join(lab.stateDir, hqStateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(dir, recipePassLockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if held, err := tryLockFile(lock); err != nil || !held {
		t.Fatalf("hold the recipe: held %v, %v", held, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	asked := func() int { return lab.hq.callCount("POST /api/mate/repos") + lab.hq.callCount("GET /api/mate/self") }
	askedBefore := asked()

	outcome := groupRecipeOutcome(ctx, lab.mock, lab.hq.srv.Client(), lab.rt, lab.stateDir, lab.schemas)

	if lab.hq.changeIn(hq.RecipeRepo, 1) != nil || outcome.Created || outcome.Committed {
		t.Fatalf("a pass while another runs = %+v, change %+v; want nothing changed", outcome, lab.hq.changeIn(hq.RecipeRepo, 1))
	}
	if calls := asked() - askedBefore; calls != 0 {
		t.Errorf("a pass while another runs asked HQ %d times, want none", calls)
	}
	if !strings.Contains(outcome.Line, "another group recipe pass") {
		t.Errorf("line = %q, want it to say another group recipe pass is running", outcome.Line)
	}

	if err := unlockFile(lock); err != nil {
		t.Fatal(err)
	}
	if next := groupRecipeOutcome(t.Context(), lab.mock, lab.hq.srv.Client(), lab.rt, lab.stateDir, lab.schemas); !next.Created || next.Change != 1 {
		t.Errorf("the pass after the other is done = %+v, want it to propose change #1", next)
	}
}

// TestGroupRecipePass_NothingToDo_WritesNothing: a pass that stops before it
// would ask HQ anything — the Mate not linked yet, nothing bootstrapped, no
// pair with its repository in HQ — takes no turn and writes nothing to the
// state directory: every zcp start in a Mate runs one.
func TestGroupRecipePass_NothingToDo_WritesNothing(t *testing.T) {
	t.Parallel()
	linked := &runtime.HQLink{Origin: "https://hq.invalid", ProjectID: labMate, Key: labKey}
	tests := []struct {
		name  string
		link  *runtime.HQLink
		metas []*workflow.ServiceMeta
	}{
		{name: "not linked to its HQ yet", link: &runtime.HQLink{}},
		{name: "nothing bootstrapped yet", link: linked},
		{
			name: "no pair has its repository in HQ", link: linked,
			metas: []*workflow.ServiceMeta{{Hostname: "appdev", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "appstage"}},
				BootstrapSession: "test", BootstrappedAt: "2026-10-02"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			for _, m := range tt.metas {
				if err := workflow.WriteServiceMeta(stateDir, m); err != nil {
					t.Fatal(err)
				}
			}
			rt := runtime.Info{InContainer: true, MateEnabled: true, ProjectID: labMate, OrgID: labOrg, HQ: tt.link}

			outcome := groupRecipeOutcome(t.Context(), platform.NewMock(), http.DefaultClient, rt, stateDir, nil)

			if outcome.Blocked == "" {
				t.Fatalf("outcome = %+v, want the pass blocked", outcome)
			}
			if _, err := os.Stat(filepath.Join(stateDir, hqStateDir)); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("a pass with nothing to do wrote %s (%v)", filepath.Join(stateDir, hqStateDir), err)
			}
		})
	}
}
