// Tests for: tools/hq_pair.go — what a Mate keeping its code in HQ plans,
// launches and hands over.
package tools

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq/hqtest"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// linkAs leaves this test's container without a key for HQ ("missing") or
// holding one ("kept"). Callers redirect HOME first, and the test is not
// parallel: the identity is the process environment.
func linkAs(t *testing.T, state string) {
	t.Helper()
	switch state {
	case "missing":
		hqtest.Link(t, "", "", "")
	case "kept":
		hqtest.Link(t, "https://hq.example", "p1", "c")
	default:
		t.Fatalf("unknown link state %q", state)
	}
}

// TestLaunchProduction_AMateKeepingItsCodeInHQRefusesAndSaysWhatIsTrue: an
// application's production is a project the person adds from the projects
// page, so a Mate keeping its code in HQ refuses launch-production outright,
// and its next step says what is TRUE of its own pairs — a change still open
// by number, Put it live once the work is on main, or to push and merge first
// — read fresh, so a change merged since the last pass is never named as
// open.
func TestLaunchProduction_AMateKeepingItsCodeInHQRefusesAndSaysWhatIsTrue(t *testing.T) {
	t.Parallel()
	if refusal := hqLaunchProductionRefusal(nil, runtime.Info{}, t.TempDir(), false); refusal != nil {
		t.Fatalf("a container that is not a Mate keeps zcp's own launch-production, got %q", resultText(t, refusal))
	}
	tests := []struct {
		name string
		// settle is what happens to change #1 before the launch is asked for;
		// nil opens no change at all.
		settle   func(lab *hqLab)
		want     []string
		wantNone []string
	}{
		{name: "no change at all", want: []string{"wired_mate_production_is_the_groups", "branch of your own"}},
		{name: "a change still open is named", settle: func(*hqLab) {}, want: []string{"#1", "appdev", "merge"}},
		{name: "HQ's open change is named after its record is lost", settle: func(lab *hqLab) { lab.forget(1) },
			want: []string{"#1", "appdev", "merge"}},
		{name: "a change merged since the last pass is not named as open", settle: func(lab *hqLab) { lab.hq.merge() },
			want: []string{"Put it live"}, wantNone: []string{"#1", "merge ", "branch of your own"}},
		{name: "a change closed without merging means push and merge first", settle: func(lab *hqLab) { lab.hq.close(1) },
			want: []string{"branch of your own"}, wantNone: []string{"#1", "Tell the person: production is added and released"}},
		{name: "a change that landed with another is on main, as a merged one", settle: func(lab *hqLab) { lab.hq.landWith(1, 2) },
			want: []string{"Put it live"}, wantNone: []string{"#1", "merge ", "branch of your own"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lab := newHQLab(t)
			lab.wire()
			if tt.settle != nil {
				lab.write(map[string]string{"index.js": "the app\n"})
				lab.openChange()
				tt.settle(lab)
				lab.snapshotMate(t.Context())
			}
			text := resultText(t, hqLaunchProductionRefusal(lab.hq.srv.Client(), lab.rt, lab.stateDir, true))
			for _, want := range tt.want {
				if !strings.Contains(text, want) {
					t.Errorf("the refusal misses %q: %s", want, text)
				}
			}
			for _, never := range tt.wantNone {
				if strings.Contains(text, never) {
					t.Errorf("the refusal must not say %q: %s", never, text)
				}
			}
		})
	}
}

// TestSessionAnnotations_HandoffOnlyWithAnOpenChange: a session that deployed
// the stage half of a pair with an open change in its repository closes by
// handing the person the change; a stand-up that left the stage out has
// nothing to hand over. The ledger's record of a change in the pair's
// repository is the whole condition: the container holds no enrollment here,
// and a change on record is still the person's to review.
//
// Not parallel: HOME is redirected so no enrollment is found (t.Setenv).
func TestSessionAnnotations_HandoffOnlyWithAnOpenChange(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tests := []struct {
		name        string
		roles       map[string]string
		deployed    []string
		change      int
		wantHandoff bool
	}{
		{name: "a stand-up: the stage left out, no change", roles: map[string]string{"appstage": workflow.RoleOutOfScope}, deployed: []string{"appdev"}},
		{name: "a task with an open change, through the stage", deployed: []string{"appdev", "appstage"}, change: 1, wantHandoff: true},
		{name: "a task with no change open", deployed: []string{"appdev", "appstage"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			now := time.Now().UTC().Format(time.RFC3339)
			if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
				Hostname: "appdev", Stages: []topology.StageRef{{Hostname: "appstage"}}, Mode: topology.PlanModeStandard,
				CloseDeployMode: topology.CloseModeAuto, BootstrappedAt: now,
				HQ: &workflow.HQRepoRef{AppID: labApp, Repo: "appdev"},
			}); err != nil {
				t.Fatalf("WriteServiceMeta: %v", err)
			}
			if tt.change != 0 {
				if err := workflow.RecordHQChange(dir, workflow.HQChange{AppID: labApp, Repo: "appdev", Number: tt.change, Branch: "fix-login"}); err != nil {
					t.Fatal(err)
				}
			}
			ws := workflow.NewWorkSession("proj-1", string(workflow.EnvContainer), "Stand up development of the project.", []string{"appdev", "appstage"})
			ws.Roles = tt.roles
			ws.Deploys = map[string][]workflow.DeployAttempt{}
			ws.Verifies = map[string][]workflow.VerifyAttempt{}
			for _, h := range tt.deployed {
				ws.Deploys[h] = []workflow.DeployAttempt{{AttemptedAt: now, SucceededAt: now}}
				ws.Verifies[h] = []workflow.VerifyAttempt{{AttemptedAt: now, PassedAt: now, Passed: true}}
			}
			ws.ClosedAt = now
			ws.CloseReason = workflow.CloseReasonAutoComplete
			if err := workflow.SaveWorkSession(dir, ws); err != nil {
				t.Fatalf("SaveWorkSession: %v", err)
			}
			t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })

			got := sessionAnnotations(dir)
			if got == nil || got.Status != "auto-closed" {
				t.Fatalf("annotations = %+v, want auto-closed", got)
			}
			if gotHandoff := strings.Contains(got.Note, hqHandoffNote(false)); gotHandoff != tt.wantHandoff {
				t.Errorf("handoff in the closing note = %v, want %v: %q", gotHandoff, tt.wantHandoff, got.Note)
			}
		})
	}
}

// HQ owns the merged history: a merged change the pair holds no record of
// still reads as work on main.
func TestProductionKnowsMergedWorkThePairHoldsNoRecordOf(t *testing.T) {
	t.Parallel()
	lab := newHQLab(t)
	lab.wire()
	lab.write(map[string]string{"index.js": "the app\n"})
	lab.openChange()
	lab.hq.merge()
	lab.snapshotMate(t.Context())
	if kept := lab.ledger(); len(kept) != 0 {
		t.Fatalf("the ledger keeps %+v; want no record", kept)
	}
	text := hqLaunchProductionNextStep(lab.hq.srv.Client(), lab.rt, lab.stateDir)
	if !strings.Contains(text, "Put it live") || strings.Contains(text, "branch of your own") || strings.Contains(text, "merge ") {
		t.Fatalf("the guidance forgot the work HQ has merged: %s", text)
	}
	if !strings.Contains(text, "Put it live on the project's card") || strings.Contains(text, "both offered in this conversation") {
		t.Fatalf("production setup belongs to the project, not the change review: %s", text)
	}
}

// An unavailable authority is unknown, never evidence that no work landed:
// the Mate's server holds no state from HQ.
func TestProductionGuidance_HQUnavailableIsUnknown(t *testing.T) {
	t.Parallel()
	lab := newHQLab(t)
	lab.wire()
	if err := os.Remove(lab.rt.HQ.StatePath); err != nil {
		t.Fatal(err)
	}
	text := hqLaunchProductionNextStep(lab.hq.srv.Client(), lab.rt, lab.stateDir)
	if !strings.Contains(text, "could not read") || !strings.Contains(text, "Ask again") || strings.Contains(text, "none of this Mate's work") {
		t.Fatalf("HQ's failure must be visible, with a manual next step: %s", text)
	}
}

// The handoff points to the change's Review, so its text needs no HQ read.
// Not parallel: HOME and the identity are redirected (t.Setenv).
func TestSessionAnnotations_HandoffUsesReviewWithoutReadingHQ(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var reads atomic.Int64
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		_, _ = w.Write([]byte(`{"projectId":"P_MATE","name":"Ada","face":"f","standupRequestedBy":null,"appId":"` + labApp + `","changes":[]}`))
	}))
	t.Cleanup(fake.Close)
	hqtest.Link(t, fake.URL, "P_MATE", "good")
	dir := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname: "appdev", Stages: []topology.StageRef{{Hostname: "appstage"}}, Mode: topology.PlanModeStandard,
		CloseDeployMode: topology.CloseModeAuto, BootstrappedAt: now,
		HQ: &workflow.HQRepoRef{AppID: labApp, Repo: "appdev", Change: 1},
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	ws := workflow.NewWorkSession("proj-1", string(workflow.EnvContainer), "Add a login page.", []string{"appdev", "appstage"})
	ws.Deploys = map[string][]workflow.DeployAttempt{}
	ws.Verifies = map[string][]workflow.VerifyAttempt{}
	for _, h := range []string{"appdev", "appstage"} {
		ws.Deploys[h] = []workflow.DeployAttempt{{AttemptedAt: now, SucceededAt: now}}
		ws.Verifies[h] = []workflow.VerifyAttempt{{AttemptedAt: now, PassedAt: now, Passed: true}}
	}
	ws.ClosedAt = now
	ws.CloseReason = workflow.CloseReasonAutoComplete
	if err := workflow.SaveWorkSession(dir, ws); err != nil {
		t.Fatalf("SaveWorkSession: %v", err)
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })
	got := sessionAnnotations(dir)
	if got == nil || !strings.Contains(got.Note, "press Review") {
		t.Fatalf("want the change's Review in the closing note, got %+v", got)
	}
	if strings.Contains(got.Note, "Put it live") {
		t.Errorf("a change handoff offers Review, not production setup: %s", got.Note)
	}
	if got := reads.Load(); got != 0 {
		t.Errorf("handoff text read HQ %d times; its Review needs no production fact", got)
	}
}

// The closing message points at what Mate shows under it, never at a path
// through other pages the person would have to find.
func TestHQHandoffNote_NamesWhatMateOffersUnderTheMessage(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"Review"} {
		if !strings.Contains(hqHandoffNote(false), want) {
			t.Errorf("handoff note lacks %q: %q", want, hqHandoffNote(false))
		}
	}
	for _, never := range []string{"Put it live", "projects page", "stage"} {
		if strings.Contains(hqHandoffNote(false), never) {
			t.Errorf("handoff note still says %q: %q", never, hqHandoffNote(false))
		}
	}
}

// Once production runs a release, the closing message names production's
// address as the app and the Mate's own as the person's preview.
func TestHQHandoffNote_NamesProductionOnceItRunsARelease(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"production runs a release", "that address is the app", "preview"} {
		if !strings.Contains(hqHandoffNote(false), want) {
			t.Errorf("handoff note lacks %q: %q", want, hqHandoffNote(false))
		}
	}
}
