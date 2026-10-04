// Tests for: tools/hq_delivery.go — what a Mate delivering through HQ plans,
// launches and hands over.
package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestAMateDeliveringThroughHQPlansOnlyStandardPairs(t *testing.T) {
	t.Parallel()
	simple := []workflow.BootstrapTarget{{Runtime: workflow.RuntimeTarget{DevHostname: "todoapp", Type: "nodejs@22", BootstrapMode: topology.PlanModeSimple}}}
	devOnly := []workflow.BootstrapTarget{{Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: topology.PlanModeDev}}}
	pair := []workflow.BootstrapTarget{{Runtime: workflow.RuntimeTarget{DevHostname: "appdev", ExplicitStage: "appstage", Type: "nodejs@22", BootstrapMode: topology.PlanModeStandard}}}

	if pe := hqPairPlanError(simple, false); pe != nil {
		t.Fatalf("a container that is not a Mate keeps zcp's own rules, got %q", pe.Message)
	}
	if pe := hqPairPlanError(pair, true); pe != nil {
		t.Fatalf("a standard pair passes, got %q", pe.Message)
	}
	for name, plan := range map[string][]workflow.BootstrapTarget{"simple": simple, "dev-only": devOnly} {
		pe := hqPairPlanError(plan, true)
		if pe == nil {
			t.Fatalf("%s: a Mate delivering through HQ refuses a runtime with no stage half", name)
		}
		if !strings.Contains(pe.Message, "dev/stage pair") || !strings.Contains(pe.Message, plan[0].Runtime.DevHostname) {
			t.Fatalf("%s: the refusal names the rule and the target, got %q", name, pe.Message)
		}
		if !strings.Contains(pe.Suggestion, `bootstrapMode="standard"`) || !strings.Contains(pe.Suggestion, "stageHostname") {
			t.Fatalf("%s: the fix is the standard pair with a stage hostname, got %q", name, pe.Suggestion)
		}
	}
	if got := stageNameFor("appdev"); got != "appstage" {
		t.Fatalf("appdev's stage is appstage, got %q", got)
	}
	if got := stageNameFor("todoapp"); got != "todoappstage" {
		t.Fatalf("todoapp's stage is todoappstage, got %q", got)
	}
}

// enrollAs leaves this test's HQ enrollment missing, unreadable, or kept.
// Callers redirect HOME first.
func enrollAs(t *testing.T, state string) {
	t.Helper()
	switch state {
	case "missing":
	case "unreadable":
		if err := os.MkdirAll(filepath.Dir(hq.EnrollmentPath()), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(hq.EnrollmentPath(), []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
	case "kept":
		if err := hq.SaveEnrollment(hq.EnrollmentPath(), hq.Enrollment{HQ: "https://hq.example", ProjectID: "p1", Credential: "c"}); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown enrollment state %q", state)
	}
}

// TestBootstrapPlan_AMatePlansOnlyPairsWhateverItsEnrollment: being a Mate,
// not holding an enrollment, is what makes every runtime a pair. A Mate whose
// enrollment is still missing or cannot be read plans the same pairs it will
// deliver through once it has one; a container that is not a Mate keeps zcp's
// own plans, an enrollment file on its disk notwithstanding.
//
// Not parallel: the enrollment is read from the process's HOME (t.Setenv).
func TestBootstrapPlan_AMatePlansOnlyPairsWhateverItsEnrollment(t *testing.T) {
	tests := []struct {
		name       string
		mate       bool
		enrollment string
		wantRefuse bool
	}{
		{name: "a Mate not enrolled yet", mate: true, enrollment: "missing", wantRefuse: true},
		{name: "a Mate whose enrollment cannot be read", mate: true, enrollment: "unreadable", wantRefuse: true},
		{name: "an enrolled Mate", mate: true, enrollment: "kept", wantRefuse: true},
		{name: "not a Mate, an enrollment on disk", enrollment: "kept"},
		{name: "not a Mate", enrollment: "missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			enrollAs(t, tt.enrollment)
			dir := t.TempDir()
			engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)
			if _, err := engine.BootstrapStartWithRoute("proj-1", "a todo app", workflow.BootstrapRouteClassic, ""); err != nil {
				t.Fatalf("BootstrapStartWithRoute(classic): %v", err)
			}
			input := WorkflowInput{Step: workflow.StepDiscover, Plan: []workflow.BootstrapTarget{{Runtime: workflow.RuntimeTarget{
				DevHostname: "todoapp", Type: "nodejs@22", BootstrapMode: topology.PlanModeSimple,
			}}}}
			rt := runtime.Info{InContainer: true, ServiceName: "zcp", MateEnabled: tt.mate}
			result, _, err := handleBootstrapComplete(context.Background(), engine, platform.NewMock(), nil, nil, input, nil, "proj-1", dir, nil, nil, rt)
			if err != nil {
				t.Fatalf("handleBootstrapComplete: %v", err)
			}
			text := extractText(result)
			if refused := strings.Contains(text, "dev/stage pair"); refused != tt.wantRefuse {
				t.Fatalf("refused as a Mate's plan = %v, want %v:\n%s", refused, tt.wantRefuse, text)
			}
		})
	}
}

// TestLaunchProduction_AMateWithoutItsEnrollmentSaysSo: whether this Mate's
// work reached main is HQ's to say. With no enrollment to ask it with — none
// yet, or one that cannot be read — the refusal says that, never that
// nothing was delivered.
//
// Not parallel: the enrollment is read from the process's HOME (t.Setenv).
func TestLaunchProduction_AMateWithoutItsEnrollmentSaysSo(t *testing.T) {
	tests := []struct {
		enrollment string
		want       []string
	}{
		{enrollment: "missing", want: []string{"no HQ enrollment yet", "Ask again"}},
		{enrollment: "unreadable", want: []string{"HQ enrollment cannot be read", "Ask again"}},
	}
	for _, tt := range tests {
		t.Run(tt.enrollment, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			enrollAs(t, tt.enrollment)
			text := resultText(t, hqLaunchProductionRefusal(context.Background(), nil, t.TempDir(), true))
			for _, want := range append(tt.want, "wired_mate_production_is_the_groups") {
				if !strings.Contains(text, want) {
					t.Errorf("the refusal misses %q: %s", want, text)
				}
			}
			if strings.Contains(text, "none of this Mate's work") {
				t.Errorf("a missing enrollment is not evidence that nothing was delivered: %s", text)
			}
		})
	}
}

// TestLaunchProduction_AMateDeliveringThroughHQRefusesAndSaysWhatIsTrue: an
// application's production is a project the person adds from the projects
// page, so a Mate delivering through HQ refuses launch-production outright,
// and its next step says what is TRUE of its own pairs — a change still open
// by number, the projects page once the work is on main, or to deliver first
// — read fresh, so a change merged since the last pass is never named as
// open.
func TestLaunchProduction_AMateDeliveringThroughHQRefusesAndSaysWhatIsTrue(t *testing.T) {
	if refusal := hqLaunchProductionRefusal(context.Background(), nil, t.TempDir(), false); refusal != nil {
		t.Fatalf("a container that is not a Mate keeps zcp's own launch-production, got %q", resultText(t, refusal))
	}
	tests := []struct {
		name string
		// settle is what happens to change #1 before the launch is asked for;
		// nil delivers nothing at all.
		settle   func(lab *hqLab)
		want     []string
		wantNone []string
	}{
		{name: "nothing delivered", want: []string{"wired_mate_production_is_the_groups", "stage half first"}},
		{name: "a change still open is named", settle: func(*hqLab) {}, want: []string{"#1", "appdev", "merge"}},
		{name: "HQ's open change is named after its local number is lost", settle: func(lab *hqLab) {
			if err := workflow.UpsertServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta, _ bool) error {
				m.HQ.Change = 0
				return nil
			}); err != nil {
				lab.t.Fatal(err)
			}
		}, want: []string{"#1", "appdev", "merge"}},
		{name: "a change merged since the last pass is not named as open", settle: func(lab *hqLab) { lab.hq.merge() },
			want: []string{"projects page"}, wantNone: []string{"#1", "merge ", "stage half first"}},
		{name: "a change closed without merging means deliver first", settle: func(lab *hqLab) { lab.hq.close(1) },
			want: []string{"stage half first"}, wantNone: []string{"#1", "Tell the person: production is added and released"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			if tt.settle != nil {
				lab.write(map[string]string{"index.js": "the app\n"})
				lab.deliver()
				tt.settle(lab)
			}
			text := resultText(t, hqLaunchProductionRefusal(context.Background(), lab.hq.srv.Client(), lab.stateDir, true))
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

// TestSessionAnnotations_HandoffOnlyAfterADelivery: a session that delivered
// through the stage half closes by handing the person the change; a stand-up
// that left the stage out delivered nothing to hand over. The pair's record
// of its change is the whole condition: the container holds no enrollment
// here, and a change on record is still the person's to review.
//
// Not parallel: HOME is redirected so no enrollment is found (t.Setenv).
func TestSessionAnnotations_HandoffOnlyAfterADelivery(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tests := []struct {
		name        string
		roles       map[string]string
		deployed    []string
		change      int
		wantHandoff bool
	}{
		{name: "a stand-up: the stage left out, nothing delivered", roles: map[string]string{"appstage": workflow.RoleOutOfScope}, deployed: []string{"appdev"}},
		{name: "a task delivered through the stage", deployed: []string{"appdev", "appstage"}, change: 1, wantHandoff: true},
		{name: "a delivery with nothing beyond main", deployed: []string{"appdev", "appstage"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			now := time.Now().UTC().Format(time.RFC3339)
			if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
				Hostname: "appdev", StageHostname: "appstage", Mode: topology.PlanModeStandard,
				CloseDeployMode: topology.CloseModeAuto, BootstrappedAt: now,
				HQ: &workflow.HQRepoRef{AppID: labApp, Repo: "appdev", Change: tt.change},
			}); err != nil {
				t.Fatalf("WriteServiceMeta: %v", err)
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
			if gotHandoff := strings.Contains(got.Note, hqHandoffNote); gotHandoff != tt.wantHandoff {
				t.Errorf("handoff in the closing note = %v, want %v: %q", gotHandoff, tt.wantHandoff, got.Note)
			}
		})
	}
}

// HQ owns the merged history after the checkout has absorbed its landing.
// Non-parallel: the lab redirects HOME and writes the enrollment there.
func TestProductionKnowsMergedWorkAfterTheMergeIsAbsorbed(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.write(map[string]string{"index.js": "the app\n"})
	lab.deliver()
	lab.hq.merge()
	if d := lab.deliver(); d == nil || d.Change != nil || lab.meta().HQ.Landed != nil {
		t.Fatalf("want an absorbed landing and nothing new to propose, got %+v", d)
	}
	text := hqLaunchProductionNextStep(t.Context(), lab.hq.srv.Client(), lab.stateDir)
	if !strings.Contains(text, "projects page") || strings.Contains(text, "stage half first") || strings.Contains(text, "merge ") {
		t.Fatalf("the guidance forgot the work HQ has merged: %s", text)
	}
}

// An unavailable authority is unknown, never evidence that no work landed.
// Non-parallel: the lab redirects HOME and writes the enrollment there.
func TestProductionGuidance_HQUnavailableIsUnknown(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.hq.setDown(true)
	text := hqLaunchProductionNextStep(t.Context(), lab.hq.srv.Client(), lab.stateDir)
	if !strings.Contains(text, "could not read") || !strings.Contains(text, "Ask again") || strings.Contains(text, "none of this Mate's work") {
		t.Fatalf("HQ's failure must be visible, with a manual next step: %s", text)
	}
}
