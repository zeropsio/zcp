// Tests for: ops/deploy_ssh.go — the sha parameter (docs/spec-workflows.md
// §4.5): DeploySSH resolves an explicit sha, extracts its tree into a fresh
// temp dir inside the source container (over SSH, never the SSHFS mount),
// and pushes from there with --version-name, --no-git, and no
// GitEnsureRepoHeadCommand step.
package ops

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestDeploySSH_WithSHA_ResolvesExtractsAndPushesFromExtractedDir(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	ssh := &mockSSHDeployer{results: []sshResult{
		{output: []byte("fullsha1234567\n")},     // resolve
		{output: []byte("/tmp/zcp-extract-1\n")}, // mktemp -d
		{output: []byte("")},                     // extract (archive | tar -x)
		{output: []byte("ok")},                   // login+push
		{output: []byte("")},                     // cleanup rm -rf
	}}
	authInfo := testAuthInfo()

	result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", "", "abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SHA != "fullsha1234567" {
		t.Errorf("result.SHA = %q, want fullsha1234567", result.SHA)
	}
	if len(ssh.calls) != 5 {
		t.Fatalf("ssh calls = %d, want 5 (resolve, mktemp, extract, push, cleanup): %+v", len(ssh.calls), ssh.calls)
	}
	for _, c := range ssh.calls {
		if c.hostname != "builder" {
			t.Errorf("call hostname = %q, want builder (the SOURCE container): %+v", c.hostname, c)
		}
	}
	if !strings.Contains(ssh.calls[0].command, "rev-parse --verify") {
		t.Errorf("call[0] = %q, want a rev-parse --verify", ssh.calls[0].command)
	}
	if !strings.Contains(ssh.calls[1].command, "mktemp -d") {
		t.Errorf("call[1] = %q, want mktemp -d", ssh.calls[1].command)
	}
	if !strings.Contains(ssh.calls[2].command, "git archive --format=tar") || !strings.Contains(ssh.calls[2].command, "| tar -x -C") {
		t.Errorf("call[2] = %q, want the archive|tar pipe", ssh.calls[2].command)
	}
	pushCmd := ssh.calls[3].command
	if !strings.Contains(pushCmd, "--no-git") {
		t.Errorf("push command = %q, want --no-git", pushCmd)
	}
	if !strings.Contains(pushCmd, "--version-name 'fullsha1234567'") {
		t.Errorf("push command = %q, want --version-name 'fullsha1234567'", pushCmd)
	}
	if !strings.Contains(pushCmd, "cd '/tmp/zcp-extract-1'") {
		t.Errorf("push command = %q, want to cd into the extracted dir", pushCmd)
	}
	if strings.Contains(pushCmd, " -g") {
		t.Errorf("push command = %q, must not include -g (no .git in an extracted tree)", pushCmd)
	}
	if !strings.Contains(ssh.calls[4].command, "rm -rf '/tmp/zcp-extract-1'") {
		t.Errorf("call[4] = %q, want cleanup of the extracted dir", ssh.calls[4].command)
	}
}

func TestDeploySSH_WithUnresolvableSHA_ReturnsInvalidParameterError(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	ssh := &mockSSHDeployer{err: errTestSHA}
	authInfo := testAuthInfo()

	_, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", "", "nope")
	if err == nil {
		t.Fatal("expected error for an unresolvable sha")
	}
	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidParameter {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrInvalidParameter)
	}
	if len(ssh.calls) != 1 {
		t.Errorf("only the resolve call should run when sha does not resolve, got %d: %+v", len(ssh.calls), ssh.calls)
	}
}

func TestDeploySSH_NoSHA_UnaffectedByShaMachinery(t *testing.T) {
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	ssh := &mockSSHDeployer{output: []byte("ok")}
	authInfo := testAuthInfo()

	result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SHA != "" {
		t.Errorf("result.SHA = %q, want empty (no sha requested)", result.SHA)
	}
	if len(ssh.calls) != 1 {
		t.Fatalf("ssh calls = %d, want 1 — sha machinery must not run at all when sha is empty", len(ssh.calls))
	}
}

type shaTestError struct{ msg string }

func (e *shaTestError) Error() string { return e.msg }

var errTestSHA = &shaTestError{"exit 128: bad revision"}
