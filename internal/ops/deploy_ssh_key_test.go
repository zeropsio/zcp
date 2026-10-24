// Tests for: ops/deploy_ssh.go — the Zerops key reaches `zcli push` over the
// SSH session's stdin, never on a command line.
package ops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
)

// pushKey is a key no other test uses, so a process list never matches
// another test's push.
const pushKey = "zcp-key-only-on-stdin"

// TestDeploySSH_EveryPush_KeyOnStdinNeverOnACommandLine: no command a deploy
// hands to the SSH executor carries the key, and the push's own call writes
// it, one line, to the session's stdin — a working-tree push and a commit
// push alike.
func TestDeploySSH_EveryPush_KeyOnStdinNeverOnACommandLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ssh  *mockSSHDeployer
		sha  string
	}{
		{name: "working tree", ssh: &mockSSHDeployer{output: []byte("ok")}},
		{
			name: "commit",
			sha:  "abc123",
			ssh: &mockSSHDeployer{results: []sshResult{
				{output: []byte("f0115ba1234567\n")},
				{output: []byte(validCommitZeropsYaml)},
				{output: []byte("/tmp/zcp-extract-1\n")},
				{output: []byte("")},
				{output: []byte("0123456789abcdef\nZCP:BRANCH:main\n")},
				{output: []byte("ok")},
				{output: []byte("")},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().WithServices([]platform.ServiceStack{
				{ID: "svc-1", Name: "builder"},
				{ID: "svc-2", Name: "app"},
			})
			if _, err := DeploySSH(context.Background(), mock, "proj-1", tt.ssh, auth.Info{Token: pushKey},
				"builder", "app", "", "", tt.sha); err != nil {
				t.Fatalf("deploy: %v", err)
			}
			var pushes int
			for _, c := range tt.ssh.calls {
				if strings.Contains(c.command, pushKey) {
					t.Errorf("command carries the key:\n%s", c.command)
				}
				if c.stdin == "" {
					continue
				}
				pushes++
				if c.stdin != pushKey+"\n" || !strings.Contains(c.command, "zcli push") {
					t.Errorf("call with stdin %q, command %q; want the key line on the push's call", c.stdin, c.command)
				}
			}
			if pushes != 1 {
				t.Errorf("calls writing stdin = %d, want 1 (the push): %+v", pushes, tt.ssh.calls)
			}
		})
	}
}

// TestPushCommands_KeyFromStdin_ReachesZcliOnlyThroughItsEnvironment runs
// each push command in real bash with the key on stdin, as an SSH session
// does: zcli reads the key from ZEROPS_TOKEN, and while it runs no process's
// command line carries the key.
func TestPushCommands_KeyFromStdin_ReachesZcliOnlyThroughItsEnvironment(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	tests := []struct {
		name    string
		command func(r *pushRig) string
		wait    bool
	}{
		{
			name: "working tree",
			command: func(r *pushRig) string {
				return buildSSHCommand("svc-1", r.dir, "prod", true, "main abc", r.pushDir)
			},
			wait: true,
		},
		{
			name: "commit",
			command: func(r *pushRig) string {
				return buildSSHCommandSHA("svc-1", r.dir, "prod", "commit abc")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			seen := filepath.Join(t.TempDir(), "seen")
			r := newPushRig(t, `{ printf 'env %s\n' "$ZEROPS_TOKEN"; printf 'args %s\n' "$*"; ps -A -o args=; } > "$ZCLI_SEEN"
echo 'Deploying service'
`)
			r.env = append(r.env, "ZCLI_SEEN="+seen)
			r.stdin = pushKey + "\n"
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			command := tt.command(r)
			if strings.Contains(command, pushKey) {
				t.Errorf("push command carries the key:\n%s", command)
			}
			if out, code := r.run(ctx, command); code != 0 {
				t.Fatalf("push = exit %d: %s", code, out)
			}
			if tt.wait {
				if out, code := r.run(ctx, buildWaitCommand(r.pushDir)); code != 0 {
					t.Fatalf("build wait = exit %d: %s", code, out)
				}
			}
			raw, err := os.ReadFile(seen)
			if err != nil {
				t.Fatalf("zcli never ran: %v", err)
			}
			lines := strings.Split(string(raw), "\n")
			if len(lines) < 2 || lines[0] != "env "+pushKey {
				t.Fatalf("zcli environment ZEROPS_TOKEN line = %q, want %q", lines[0], "env "+pushKey)
			}
			for _, line := range lines[1:] {
				if strings.Contains(line, pushKey) {
					t.Errorf("a command line carries the key while zcli runs: %q", line)
				}
			}
		})
	}
}
