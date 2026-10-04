// Tests for: workflow.go / hq_wiring.go (hqRepositoryActions) — in a
// Mate the repository, its credential and what builds from it are HQ's, so the
// public actions that would choose them refuse, while zcp's own wiring of a
// pair to HQ goes on.
package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestMateRepositoryActions_RefuseAndPointAtHQ: a Mate's agent asked for a
// repository URL, a token or CI is told the pair's repository is HQ's, and
// nothing is probed, written or stamped — origin, GIT_TOKEN and the record
// stay as they were. Outside a Mate the same calls keep zcp's own setup.
func TestMateRepositoryActions_RefuseAndPointAtHQ(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		action string
		input  WorkflowInput
	}{
		{name: "git-push-setup walkthrough", action: "git-push-setup", input: WorkflowInput{Service: "appdev"}},
		{name: "git-push-setup with a remote and a token", action: "git-push-setup", input: WorkflowInput{
			Service: "appdev", RemoteURL: "https://github.com/example/app.git", GitToken: "ghp_example",
		}},
		{name: "build-integration walkthrough", action: "build-integration", input: WorkflowInput{Service: "appdev"}},
		{name: "build-integration choosing CI", action: "build-integration", input: WorkflowInput{Service: "appdev", Integration: "actions"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, mate := range []bool{true, false} {
				stateDir := t.TempDir()
				writePairMetaForGitPushSetup(t, stateDir)
				before, _ := workflow.ReadServiceMeta(stateDir, "appdev")
				ssh := &containerSSHStub{}
				rt := runtime.Info{InContainer: true, MateEnabled: mate}

				input := tt.input
				input.Action = tt.action
				engine := workflow.NewEngine(stateDir, workflow.EnvContainer, nil)
				result, _, _ := handleWorkflowAction(context.Background(), "test-project", engine, platform.NewMock(), nil, nil, nil, input, stateDir, "zcp", nil, ssh, rt, "")
				text := extractText(result)

				refused := result.IsError && strings.Contains(text, "HQ")
				if refused != mate {
					t.Fatalf("mate=%v: refused for HQ = %v, want %v: %s", mate, refused, mate, text)
				}
				if !mate {
					continue
				}
				for _, want := range []string{"INVALID_USAGE", "repository in HQ", "no token is to be asked for"} {
					if !strings.Contains(text, want) {
						t.Errorf("the refusal misses %q: %s", want, text)
					}
				}
				if len(ssh.commands) != 0 {
					t.Errorf("a refusal runs nothing on the pair: %v", ssh.commands)
				}
				after, _ := workflow.ReadServiceMeta(stateDir, "appdev")
				if after.GitPushState != before.GitPushState || after.RemoteURL != before.RemoteURL || after.BuildIntegration != before.BuildIntegration {
					t.Errorf("a refusal leaves the record as it was: before %+v, after %+v", before, after)
				}
			}
		})
	}
}

// TestMateRepositoryActions_ZcpStillWiresThePair: the refusal is of the
// public actions only. zcp's own pass hands a Mate's pair its repository in
// HQ and HQ's credential, the confirmation those actions share included.
// Non-parallel: the lab redirects HOME and writes the enrollment there.
func TestMateRepositoryActions_ZcpStillWiresThePair(t *testing.T) {
	lab := newHQLab(t)
	lab.rt.MateEnabled = true
	lab.wire()
	meta := lab.meta()
	if !hqPairWired(meta) || meta.GitPushState != topology.GitPushConfigured {
		t.Fatalf("want the pair wired to its repository in HQ, got %+v", meta)
	}
}
