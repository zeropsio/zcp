// Tests for: ops/deploy_ssh.go — the Zerops key reaches `zcli push` over the
// SSH session's stdin, never on a command line.
package ops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
)

// pushKey is a key no other test uses, so a process list never matches
// another test's push.
const pushKey = "zcp-key-only-on-stdin"

// exportBuiltin matches the shell's export builtin as a command.
var exportBuiltin = regexp.MustCompile(`(^|[\s;&|({])export\s`)

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

// TestPushCommands_KeyInAPrivateName_OnlyZcliSeesIt runs each push variant in
// real bash, with the key on stdin and the container's own ZEROPS_TOKEN
// exported, as a dev container may: zcli gets the key as ZEROPS_TOKEN and
// under no other name, while the crew save and the push wrapper's own child
// keep the container's ZEROPS_TOKEN and never see the key; the remote shell
// exports nothing.
func TestPushCommands_KeyInAPrivateName_OnlyZcliSeesIt(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	const containerToken = "container-own-token"
	realMv, err := exec.LookPath("mv")
	if err != nil {
		t.Fatalf("mv: %v", err)
	}
	tests := []struct {
		name    string
		command func(r *pushRig) string
		tree    bool
	}{
		{
			name:    "working tree with .git",
			command: func(r *pushRig) string { return buildSSHCommand("svc-1", r.dir, "prod", true, "main abc", r.pushDir) },
			tree:    true,
		},
		{
			name:    "working tree without .git",
			command: func(r *pushRig) string { return buildSSHCommand("svc-1", r.dir, "", false, "", r.pushDir) },
			tree:    true,
		},
		{
			name:    "commit",
			command: func(r *pushRig) string { return buildSSHCommandSHA("svc-1", r.dir, "prod", "commit abc") },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			seen := t.TempDir()
			r := newPushRig(t, "env > \"$SEEN/zcli\"\necho 'Deploying service'\n")
			bin, _, _ := strings.Cut(strings.TrimPrefix(r.env[len(r.env)-1], "PATH="), string(os.PathListSeparator))
			if err := os.WriteFile(filepath.Join(bin, "mv"), []byte("#!/bin/sh\nenv > \"$SEEN/mv\"\nexec "+shellQuote(realMv)+" \"$@\"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(r.dir, ".git", "t3-crew-save"), []byte("#!/bin/sh\nenv > \"$SEEN/save\"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			r.env = append(r.env, "SEEN="+seen, "ZEROPS_TOKEN="+containerToken)
			r.stdin = pushKey + "\n"
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			command := tt.command(r)
			// The export builtin, not a word that merely starts with it (the
			// upload measurement asks git for the export-ignore attribute).
			if strings.Contains(command, pushKey) || exportBuiltin.MatchString(command) {
				t.Errorf("push command carries the key or exports a name:\n%s", command)
			}
			if out, code := r.run(ctx, command); code != 0 {
				t.Fatalf("push = exit %d: %s", code, out)
			}
			if tt.tree {
				if out, code := r.run(ctx, buildWaitCommand(r.pushDir)); code != 0 {
					t.Fatalf("build wait = exit %d: %s", code, out)
				}
			}
			assertEnvKey(t, filepath.Join(seen, "zcli"), pushKey)
			if tt.tree {
				assertEnvKey(t, filepath.Join(seen, "save"), containerToken)
				assertEnvKey(t, filepath.Join(seen, "mv"), containerToken)
			}
		})
	}
}

// assertEnvKey reads the `env` a process recorded at file: its ZEROPS_TOKEN
// is want, and the push key appears in no other variable.
func assertEnvKey(t *testing.T, file, want string) {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("%s never ran: %v", filepath.Base(file), err)
	}
	var token string
	for line := range strings.SplitSeq(string(raw), "\n") {
		if v, ok := strings.CutPrefix(line, "ZEROPS_TOKEN="); ok {
			token = v
			continue
		}
		if strings.Contains(line, pushKey) {
			t.Errorf("%s sees the key as %q", filepath.Base(file), line)
		}
	}
	if token != want {
		t.Errorf("%s ZEROPS_TOKEN = %q, want %q", filepath.Base(file), token, want)
	}
}
