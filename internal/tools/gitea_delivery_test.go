package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
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

// TestAStageDeployOfAWiredPairDeliversItself is the owner's run of 2026-09-17:
// "build a todo app" has to end with a pull request without the person saying
// how code travels ("no person is ever going to say this"). The pair's stage
// half running a deploy is the moment the work is shippable, so zcp commits
// the dev half's tree, pushes the Mate's branch and opens the request itself;
// the dev half's own deploys deliver nothing.
func TestAStageDeployOfAWiredPairDeliversItself(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		sshOutput  string
		wantSSH    bool
		wantCreate int
		wantLine   []string
		wantNil    bool
	}{
		{
			name: "the stage half delivers", target: "appstage", sshOutput: "ok",
			wantSSH: true, wantCreate: 1,
			wantLine: []string{"mate/mate-p1", "acme/appdev", "pull request #3", "/acme/appdev/pulls/3"},
		},
		{name: "the dev half delivers nothing", target: "appdev", wantNil: true},
		{
			name: "an unignored node_modules stops it", target: "appstage",
			sshOutput: "ZCP_UNIGNORED: node_modules", wantSSH: true, wantCreate: 0,
			wantLine: []string{"node_modules", ".gitignore", "appstage"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeGitea()
			gitea := fake.start(t)
			stateDir := t.TempDir()
			writeWiredGiteaPairMeta(t, stateDir, gitea.URL+"/acme/appdev")
			t.Setenv("GITEA_URL", gitea.URL)
			t.Setenv("MATE_BROKER_URL", gitea.URL)
			t.Setenv("GITEA_TOKEN", giteaBotToken)

			var hosts []string
			ssh := &hostRecordingSSH{output: tt.sshOutput, hosts: &hosts}
			delivery := deliverGiteaPair(context.Background(), platform.NewMock(), gitea.Client(), ssh,
				runtime.Info{InContainer: true, ProjectID: "proj-1"}, stateDir, tt.target)

			if tt.wantNil {
				if delivery != nil || len(hosts) != 0 {
					t.Fatalf("want no delivery and no SSH, got %+v on %v", delivery, hosts)
				}
				return
			}
			if delivery == nil {
				t.Fatal("want a delivery")
			}
			if tt.wantSSH && (len(hosts) == 0 || hosts[0] != "appdev") {
				t.Fatalf("the push runs in the dev half's checkout, got %v", hosts)
			}
			if fake.pullCreates != tt.wantCreate {
				t.Errorf("pull requests created = %d, want %d", fake.pullCreates, tt.wantCreate)
			}
			for _, want := range tt.wantLine {
				if !strings.Contains(delivery.Line, want) {
					t.Errorf("the line misses %q:\n%s", want, delivery.Line)
				}
			}
			if strings.Contains(delivery.Line, "git-push") || strings.Contains(delivery.Line, "build-integration") {
				t.Errorf("the line must ask the agent for no push and no integration:\n%s", delivery.Line)
			}
		})
	}

	t.Run("a Mate without Gitea delivers nothing", func(t *testing.T) {
		stateDir := t.TempDir()
		writeWiredGiteaPairMeta(t, stateDir, "https://git.example/acme/appdev")
		t.Setenv("GITEA_URL", "")
		t.Setenv("MATE_BROKER_URL", "")
		t.Setenv("GITEA_TOKEN", "")
		var hosts []string
		if d := deliverGiteaPair(context.Background(), platform.NewMock(), nil, &hostRecordingSSH{hosts: &hosts},
			runtime.Info{InContainer: true}, stateDir, "appstage"); d != nil || len(hosts) != 0 {
			t.Fatalf("want nothing, got %+v on %v", d, hosts)
		}
	})
}

// A wired pair's direct deploys are the Mate's own: nothing a Gitea workflow
// runs ever rebuilds a Mate's service, so no integration may turn them into
// push-delivery-required, and no warning may send the agent to push by hand.
func TestAWiredPairDeploysDirectlyAndIsNeverSentToPush(t *testing.T) {
	stateDir := t.TempDir()
	writeWiredGiteaPairMeta(t, stateDir, "https://git.example/acme/appdev")
	if err := workflow.UpsertServiceMeta(stateDir, "appdev", func(m *workflow.ServiceMeta, _ bool) error {
		m.FirstDeployedAt = "2026-09-17T15:00:00Z"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"appdev", "appstage"} {
		if r := repoDeliveryRedirect(stateDir, target, "", false); r != nil {
			t.Errorf("%s: a wired pair's direct deploy must proceed", target)
		}
		if w := repoDeliveryDivergenceWarning(stateDir, target); w != "" {
			t.Errorf("%s: no push-by-hand warning on a wired pair, got %q", target, w)
		}
	}
}

// hostRecordingSSH answers every command with one output and records which
// container each ran in.
type hostRecordingSSH struct {
	output string
	hosts  *[]string
}

func (s *hostRecordingSSH) ExecSSH(_ context.Context, host, _ string) ([]byte, error) {
	*s.hosts = append(*s.hosts, host)
	if strings.Contains(s.output, "ZCP_UNIGNORED:") {
		return []byte(s.output), errors.New("exit status 3")
	}
	return []byte(s.output), nil
}

func (s *hostRecordingSSH) ExecSSHBackground(_ context.Context, host, _ string, _ time.Duration) ([]byte, error) {
	*s.hosts = append(*s.hosts, host)
	return []byte("ok"), nil
}
