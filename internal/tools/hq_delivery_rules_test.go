// Tests for: tools/hq_delivery.go — what a Mate delivering through HQ plans,
// launches and hands over.
package tools

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestAMateDeliveringThroughHQPlansOnlyStandardPairs(t *testing.T) {
	t.Parallel()
	simple := []workflow.BootstrapTarget{{Runtime: workflow.RuntimeTarget{DevHostname: "todoapp", Type: "nodejs@22", BootstrapMode: topology.PlanModeSimple}}}
	devOnly := []workflow.BootstrapTarget{{Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: topology.PlanModeDev}}}
	pair := []workflow.BootstrapTarget{{Runtime: workflow.RuntimeTarget{DevHostname: "appdev", ExplicitStage: "appstage", Type: "nodejs@22", BootstrapMode: topology.PlanModeStandard}}}

	if pe := hqPairPlanError(simple, false); pe != nil {
		t.Fatalf("a Mate not delivering through HQ keeps zcp's own rules, got %q", pe.Message)
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

// TestLaunchProduction_AMateDeliveringThroughHQRefusesAndSaysWhatIsTrue: an
// application's production is a project the person adds from the projects
// page, so a Mate delivering through HQ refuses launch-production outright,
// and its next step says what is TRUE of its own pairs — a change still open
// by number, the projects page once the work is on main, or to deliver first
// — read fresh, so a change merged since the last pass is never named as
// open.
func TestLaunchProduction_AMateDeliveringThroughHQRefusesAndSaysWhatIsTrue(t *testing.T) {
	if refusal := hqLaunchProductionRefusal(context.Background(), nil, t.TempDir(), false); refusal != nil {
		t.Fatalf("a Mate not delivering through HQ keeps zcp's own launch-production, got %q", resultText(t, refusal))
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
// that left the stage out delivered nothing to hand over.
func TestSessionAnnotations_HandoffOnlyAfterADelivery(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := hq.SaveEnrollment(hq.EnrollmentPath(), hq.Enrollment{HQ: "https://hq.example", ProjectID: "p1", Credential: "c"}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		roles       map[string]string
		deployed    []string
		wantHandoff bool
	}{
		{name: "a stand-up: the stage left out, nothing delivered", roles: map[string]string{"appstage": workflow.RoleOutOfScope}, deployed: []string{"appdev"}},
		{name: "a task delivered through the stage", deployed: []string{"appdev", "appstage"}, wantHandoff: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			now := time.Now().UTC().Format(time.RFC3339)
			if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
				Hostname: "appdev", StageHostname: "appstage", Mode: topology.PlanModeStandard,
				CloseDeployMode: topology.CloseModeAuto, BootstrappedAt: now,
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
