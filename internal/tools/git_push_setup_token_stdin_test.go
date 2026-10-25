// Tests for: the candidate GIT_TOKEN git-push-setup proves before it writes
// the secret never rides an ssh command line — the write probe and the
// shallow-clone fix read it from the session's stdin — since a command line
// is visible to every process on both containers for the call's length
// (review R4, pre-existing: ops/git_auth_probe.go).
package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// stdinRecordingSSH is containerSSHStub that also keeps, per command, what
// the call wrote to the session's stdin ("" for a call that wrote none).
type stdinRecordingSSH struct {
	containerSSHStub
	inputs []string
}

func (s *stdinRecordingSSH) ExecSSH(ctx context.Context, hostname, cmd string) ([]byte, error) {
	s.inputs = append(s.inputs, "")
	return s.containerSSHStub.ExecSSH(ctx, hostname, cmd)
}

func (s *stdinRecordingSSH) ExecSSHStdin(ctx context.Context, hostname, cmd string, stdin []byte) ([]byte, error) {
	s.inputs = append(s.inputs, string(stdin))
	return s.containerSSHStub.ExecSSH(ctx, hostname, cmd)
}

// TestGitPushSetupContainer_CandidateTokenNeverOnACommandLine: a first
// configuration runs the write probe and the shallow-clone fix with the
// candidate token. Neither command carries it; each call that needs it writes
// it to the session's stdin, and no other call does.
func TestGitPushSetupContainer_CandidateTokenNeverOnACommandLine(t *testing.T) {
	t.Parallel()
	const token = "ghp_candidate_token_0001"
	stateDir := t.TempDir()
	writePairMetaForGitPushSetup(t, stateDir)
	ssh := &stdinRecordingSSH{}
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	result, _, _ := handleGitPushSetup(
		context.Background(), client, nil, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git", GitToken: token},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("git-push-setup failed: %s", extractText(result))
	}
	var probe, shallow bool
	for i, cmd := range ssh.commands {
		if strings.Contains(cmd, token) {
			t.Errorf("ssh command %d carries the candidate token:\n%s", i, cmd)
		}
		needsToken := strings.Contains(cmd, "push --dry-run") || strings.Contains(cmd, "fetch --unshallow")
		probe = probe || strings.Contains(cmd, "push --dry-run")
		shallow = shallow || strings.Contains(cmd, "fetch --unshallow")
		switch {
		case needsToken && ssh.inputs[i] != token+"\n":
			t.Errorf("ssh command %d needs the token, but its stdin is %q:\n%s", i, ssh.inputs[i], cmd)
		case !needsToken && strings.Contains(ssh.inputs[i], token):
			t.Errorf("ssh command %d does not need the token, but its stdin carries it:\n%s", i, cmd)
		}
	}
	if !probe || !shallow {
		t.Errorf("want the write probe (%v) and the shallow-clone fix (%v) among the calls: %v", probe, shallow, ssh.commands)
	}
}
