package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestAWiredMatePlansOnlyStandardPairs(t *testing.T) {
	t.Parallel()
	simple := []workflow.BootstrapTarget{{Runtime: workflow.RuntimeTarget{DevHostname: "todoapp", Type: "nodejs@22", BootstrapMode: topology.PlanModeSimple}}}
	devOnly := []workflow.BootstrapTarget{{Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: topology.PlanModeDev}}}
	pair := []workflow.BootstrapTarget{{Runtime: workflow.RuntimeTarget{DevHostname: "appdev", ExplicitStage: "appstage", Type: "nodejs@22", BootstrapMode: topology.PlanModeStandard}}}

	if pe := giteaPairPlanError(simple, false); pe != nil {
		t.Fatalf("a Mate without Gitea keeps zcp's own rules, got %q", pe.Message)
	}
	if pe := giteaPairPlanError(pair, true); pe != nil {
		t.Fatalf("a standard pair passes, got %q", pe.Message)
	}
	for name, plan := range map[string][]workflow.BootstrapTarget{"simple": simple, "dev-only": devOnly} {
		pe := giteaPairPlanError(plan, true)
		if pe == nil {
			t.Fatalf("%s: a wired Mate refuses a runtime with no stage half", name)
		}
		if !strings.Contains(pe.Message, "dev/stage pair") || !strings.Contains(pe.Message, plan[0].Runtime.DevHostname) {
			t.Fatalf("%s: the refusal names the rule and the target, got %q", name, pe.Message)
		}
		if !strings.Contains(pe.Suggestion, `bootstrapMode="standard"`) || !strings.Contains(pe.Suggestion, "stageHostname") {
			t.Fatalf("%s: the fix is the standard pair with a stage hostname, got %q", name, pe.Suggestion)
		}
	}
	if got := giteaStageNameFor("appdev"); got != "appstage" {
		t.Fatalf("appdev's stage is appstage, got %q", got)
	}
	if got := giteaStageNameFor("todoapp"); got != "todoappstage" {
		t.Fatalf("todoapp's stage is todoappstage, got %q", got)
	}
}

func TestADirectDeployOfAWiredPairNamesThePushThatLandsIt(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:       "appdev",
		StageHostname:  "appstage",
		Mode:           topology.PlanModeStandard,
		BootstrappedAt: now,
		GitPushState:   topology.GitPushConfigured,
		Gitea:          &workflow.GiteaRepoRef{FullName: "acme/app", Branch: "mate/mate-x", DefaultBranch: "main"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:       "worker",
		Mode:           topology.PlanModeSimple,
		BootstrappedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	// The stage half resolves to the pair's meta, and the push is the dev half's.
	got := giteaDeliveryNextAction(stateDir, "appstage", true)
	for _, want := range []string{`targetService="appdev"`, `strategy="git-push"`, "acme/app", "pull request"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the line names the push, the repository and the request; missing %q in %q", want, got)
		}
	}
	if got := giteaDeliveryNextAction(stateDir, "appdev", false); got != "" {
		t.Fatalf("a Mate without Gitea says nothing, got %q", got)
	}
	if got := giteaDeliveryNextAction(stateDir, "worker", true); got != "" {
		t.Fatalf("a pair the broker has not wired says nothing, got %q", got)
	}
	if got := giteaDeliveryNextAction(stateDir, "nothere", true); got != "" {
		t.Fatalf("a service with no meta says nothing, got %q", got)
	}
}
