package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// gitSSHCall is one remote command a test's fake ssh was asked to run.
type gitSSHCall struct{ host, command string }

// fakeGitSSH stands in for the ssh seam of `zcp git`: it records each call
// and answers with run (exit 0 when nil).
type fakeGitSSH struct {
	calls []gitSSHCall
	run   func(stdin io.Reader, stdout, stderr io.Writer) (int, error)
}

func (f *fakeGitSSH) ExecSSHStdio(_ context.Context, host, command string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	f.calls = append(f.calls, gitSSHCall{host: host, command: command})
	if f.run == nil {
		return 0, nil
	}
	return f.run(stdin, stdout, stderr)
}

// gitTestDeps is a zcp container named zcp with records in stateDir.
func gitTestDeps(stateDir string, ssh *fakeGitSSH) (gitCmdDeps, *bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	return gitCmdDeps{
		rt:       runtime.Info{InContainer: true, ServiceName: "zcp", ServiceID: "svc", ProjectID: "proj"},
		stateDir: stateDir,
		ssh:      ssh,
		stdin:    strings.NewReader(""),
		stdout:   &stdout,
		stderr:   &stderr,
	}, &stdout, &stderr
}

func writeGitTestMeta(t *testing.T, stateDir string, m *workflow.ServiceMeta) {
	t.Helper()
	if err := workflow.WriteServiceMeta(stateDir, m); err != nil {
		t.Fatalf("write meta %s: %v", m.Hostname, err)
	}
}

// assertGitRefused checks a refusal: exit 2, one stderr line naming why
// (every want in it), nothing on stdout, and no ssh call.
func assertGitRefused(t *testing.T, args []string, code int, stdout, stderr *bytes.Buffer, ssh *fakeGitSSH, want ...string) {
	t.Helper()
	if code != 2 {
		t.Errorf("zcp git %q exit = %d, want 2", args, code)
	}
	if len(ssh.calls) != 0 {
		t.Errorf("zcp git %q ran over ssh: %+v, want refused before any call", args, ssh.calls)
	}
	if stdout.Len() != 0 {
		t.Errorf("zcp git %q stdout = %q, want nothing", args, stdout.String())
	}
	msg := stderr.String()
	if !strings.HasPrefix(msg, "zcp git: ") || strings.Count(msg, "\n") != 1 || !strings.HasSuffix(msg, "\n") {
		t.Errorf("zcp git %q stderr = %q, want one line starting \"zcp git: \"", args, msg)
	}
	for _, w := range want {
		if !strings.Contains(msg, w) {
			t.Errorf("zcp git %q stderr = %q, want it to name %q", args, msg, w)
		}
	}
}

// TestRunGitCmd_LoopbackOrSelfHost_Refused: git runs only in another service
// of the project — a loopback name, the zcp container's own hostname, a name
// that is no service hostname or lease alias, or none at all is refused.
func TestRunGitCmd_LoopbackOrSelfHost_Refused(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{},
		{"localhost", "status"},
		{"localhost4", "status"},
		{"localhost6", "status"},
		{"127.0.0.1", "status"},
		{"::1", "status"},
		{"zcp", "status"},
		{"-oProxyCommand=sh", "status"},
		{"", "status"},
		{"app/dev", "status"},
		{"APPDEV", "status"},
	} {
		ssh := &fakeGitSSH{}
		deps, stdout, stderr := gitTestDeps(t.TempDir(), ssh)
		code := runGitCmd(t.Context(), args, deps)
		assertGitRefused(t, args, code, stdout, stderr, ssh)
	}
}

// TestRunGitCmd_StageHost_RefusedNamingItsDev: the stage half of a pair holds
// no checkout of the pair's repository; the refusal names its dev.
func TestRunGitCmd_StageHost_RefusedNamingItsDev(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeGitTestMeta(t, stateDir, &workflow.ServiceMeta{
		Hostname: "appdev",
		Mode:     topology.PlanModeStandard,
		Stages:   []topology.StageRef{{Hostname: "appstage"}},
	})
	ssh := &fakeGitSSH{}
	deps, stdout, stderr := gitTestDeps(stateDir, ssh)
	args := []string{"appstage", "status"}
	code := runGitCmd(t.Context(), args, deps)
	assertGitRefused(t, args, code, stdout, stderr, ssh, "appstage", "zcp git appdev")
}

// TestRunGitCmd_PoolPair_Refused: a pair keyed by its dev pool holds no
// checkout of its own — the pool's hostname reaches a random one of its
// containers — so both of its hostnames are refused, naming the lease route.
func TestRunGitCmd_PoolPair_Refused(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeGitTestMeta(t, stateDir, &workflow.ServiceMeta{
		Hostname: "apppool",
		Mode:     topology.PlanModeStandard,
		Stages:   []topology.StageRef{{Hostname: "appstage"}},
		DevPool:  &workflow.DevPoolRef{ServiceID: "pool-1"},
	})
	for _, host := range []string{"apppool", "appstage"} {
		ssh := &fakeGitSSH{}
		deps, stdout, stderr := gitTestDeps(stateDir, ssh)
		args := []string{host, "status"}
		code := runGitCmd(t.Context(), args, deps)
		assertGitRefused(t, args, code, stdout, stderr, ssh, `zerops_lease action="acquire" pool="apppool"`)
	}
}

// TestRunGitCmd_NotInContainer_Refused: zcp git runs git in a sibling service
// over the project's ssh, which only a zcp container has.
func TestRunGitCmd_NotInContainer_Refused(t *testing.T) {
	t.Parallel()
	ssh := &fakeGitSSH{}
	deps, stdout, stderr := gitTestDeps(t.TempDir(), ssh)
	deps.rt = runtime.Info{}
	args := []string{"appdev", "status"}
	code := runGitCmd(t.Context(), args, deps)
	assertGitRefused(t, args, code, stdout, stderr, ssh)
}

// TestRunGitCmd_PassesStdinStdoutExitCode: the caller's stdin reaches git,
// git's stdout and stderr reach the caller byte for byte, and the exit code is
// git's; ssh's own 255, or an ssh that did not run, is said as an ssh failure.
func TestRunGitCmd_PassesStdinStdoutExitCode(t *testing.T) {
	t.Parallel()
	input := "subject line\n\nbody with 'quotes' and $dollar\x00\r\n"
	for _, tc := range []struct {
		name       string
		exit       int
		err        error
		wantCode   int
		wantSSHMsg bool
	}{
		{name: "git exits 0", exit: 0, wantCode: 0},
		{name: "git exits 1", exit: 1, wantCode: 1},
		{name: "git exits 128", exit: 128, wantCode: 128},
		{name: "ssh exits 255", exit: 255, wantCode: 255, wantSSHMsg: true},
		{name: "ssh did not run", exit: -1, err: errors.New("exec: \"ssh\": executable file not found"), wantCode: 255, wantSSHMsg: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ssh := &fakeGitSSH{run: func(stdin io.Reader, stdout, stderr io.Writer) (int, error) {
				if _, err := io.Copy(stdout, stdin); err != nil {
					return -1, err
				}
				_, _ = io.WriteString(stderr, "git chatter\n")
				return tc.exit, tc.err
			}}
			deps, stdout, stderr := gitTestDeps(t.TempDir(), ssh)
			deps.stdin = strings.NewReader(input)
			code := runGitCmd(t.Context(), []string{"appdev", "commit", "-F", "-"}, deps)
			if code != tc.wantCode {
				t.Errorf("exit = %d, want %d", code, tc.wantCode)
			}
			if stdout.String() != input {
				t.Errorf("stdout = %q, want stdin byte for byte %q", stdout.String(), input)
			}
			if !strings.HasPrefix(stderr.String(), "git chatter\n") {
				t.Errorf("stderr = %q, want git's own stderr first", stderr.String())
			}
			said := strings.TrimPrefix(stderr.String(), "git chatter\n")
			if tc.wantSSHMsg {
				if !strings.HasPrefix(said, "zcp git: ") || !strings.Contains(said, "ssh") {
					t.Errorf("stderr after git's = %q, want a zcp git line naming the ssh failure", said)
				}
			} else if said != "" {
				t.Errorf("stderr after git's = %q, want nothing added", said)
			}
			if len(ssh.calls) != 1 || ssh.calls[0].host != "appdev" {
				t.Fatalf("ssh calls = %+v, want one to appdev", ssh.calls)
			}
			if want := `cd '/var/www' && GIT_TERMINAL_PROMPT=0 exec git 'commit' '-F' '-'`; !strings.HasSuffix(ssh.calls[0].command, want) {
				t.Errorf("remote command = %q, want it to end with %q", ssh.calls[0].command, want)
			}
		})
	}
}

// TestRunGitCmd_SignalCancelsTheCall: Ctrl-C or SIGTERM sent to zcp git stops
// the ssh call at once — no deadline of its own decides — and zcp git says the
// remote git may still be finishing, never to delete a lock.
func TestRunGitCmd_SignalCancelsTheCall(t *testing.T) {
	t.Parallel()
	if goruntime.GOOS == "windows" {
		t.Skip("signals and the shell-script ssh stand-in are POSIX")
	}
	bin := t.TempDir()
	installFakeExecutable(t, filepath.Join(bin, "ssh"), "#!/bin/sh\n: > \"$ZCP_TEST_SSH_STARTED\"\nexec sleep 30\n")
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			t.Parallel()
			started := filepath.Join(t.TempDir(), "started")
			cmd := exec.CommandContext(t.Context(), zcpBinary(t), "git", "appdev", "log", "--all", "-p")
			cmd.Env = append(os.Environ(),
				"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"ZCP_TEST_SSH_STARTED="+started,
				"HOME="+t.TempDir(),
				"serviceId=svc", "hostname=zcp", "projectId=proj",
			)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			cmd.WaitDelay = 5 * time.Second
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(20 * time.Second)
			for {
				if _, err := os.Stat(started); err == nil {
					break
				}
				if time.Now().After(deadline) {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					t.Fatalf("ssh never started; stderr: %s", stderr.String())
				}
				time.Sleep(20 * time.Millisecond)
			}
			signalled := time.Now()
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			err := cmd.Wait()
			if elapsed := time.Since(signalled); elapsed > 10*time.Second {
				t.Errorf("zcp git took %s to stop after %s, want promptly", elapsed, sig)
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 130 {
				t.Errorf("zcp git stopped by %s = %v, want exit 130; stderr: %s", sig, err, stderr.String())
			}
			msg := stderr.String()
			if !strings.HasPrefix(msg, "zcp git: ") || !strings.Contains(msg, "may still be") {
				t.Errorf("stderr = %q, want a zcp git line saying the remote git may still be finishing", msg)
			}
			if regexp.MustCompile(`(?i)(delete|remove|rm )`).MatchString(msg) {
				t.Errorf("stderr = %q, must never tell anyone to delete a lock", msg)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want nothing", stdout.String())
			}
		})
	}
}

// TestRunGitCmd_FromAnotherWorkingDir_FindsTheRecordAndCodeVolumeDir: the
// records come from the fixed state dir, never the working directory's, and a
// variant-B pair's dev services run git in the code volume's mount.
//
// non-parallel: t.Chdir changes the process's working directory.
func TestRunGitCmd_FromAnotherWorkingDir_FindsTheRecordAndCodeVolumeDir(t *testing.T) {
	if got := brokeredGitStateDir(); got != "/var/www/.zcp/state" {
		t.Errorf("records dir = %q, want the fixed /var/www/.zcp/state", got)
	}
	stateDir := t.TempDir()
	writeGitTestMeta(t, stateDir, &workflow.ServiceMeta{
		Hostname: "appdev",
		Mode:     topology.PlanModeStandard,
		Stages:   []topology.StageRef{{Hostname: "appstage"}},
		CodeVolume: &topology.CodeVolume{
			Hostname:    "code",
			MountPath:   "/var/www/ws",
			DevServices: []topology.DevServiceRef{{Hostname: "apidev"}},
		},
	})
	// A decoy in the working directory's own .zcp/state: read, it would refuse
	// appdev as a pool and run apidev in /var/www.
	cwd := t.TempDir()
	writeGitTestMeta(t, filepath.Join(cwd, ".zcp", "state"), &workflow.ServiceMeta{
		Hostname: "appdev",
		DevPool:  &workflow.DevPoolRef{ServiceID: "decoy"},
	})
	t.Chdir(cwd)
	for _, host := range []string{"appdev", "apidev"} {
		ssh := &fakeGitSSH{}
		deps, _, stderr := gitTestDeps(stateDir, ssh)
		if code := runGitCmd(t.Context(), []string{host, "status"}, deps); code != 0 {
			t.Fatalf("zcp git %s status exit = %d, stderr %q", host, code, stderr.String())
		}
		if len(ssh.calls) != 1 || ssh.calls[0].host != host {
			t.Fatalf("ssh calls = %+v, want one to %s", ssh.calls, host)
		}
		if want := `cd '/var/www/ws' && GIT_TERMINAL_PROMPT=0 exec git 'status'`; !strings.HasSuffix(ssh.calls[0].command, want) {
			t.Errorf("remote command for %s = %q, want it to end with %q", host, ssh.calls[0].command, want)
		}
	}
}

// TestRunGitCmd_LeaseAlias_PlainPassthroughInVarWww: a host no record names —
// a lease alias of a pool, an unadopted service — runs git in /var/www, where
// a runtime's code lives, with nothing else decided for it.
func TestRunGitCmd_LeaseAlias_PlainPassthroughInVarWww(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeGitTestMeta(t, stateDir, &workflow.ServiceMeta{
		Hostname: "apppool",
		Mode:     topology.PlanModeStandard,
		Stages:   []topology.StageRef{{Hostname: "appstage"}},
		DevPool:  &workflow.DevPoolRef{ServiceID: "pool-1"},
	})
	for _, host := range []string{"apppool-3", "otherdev"} {
		ssh := &fakeGitSSH{}
		deps, _, stderr := gitTestDeps(stateDir, ssh)
		if code := runGitCmd(t.Context(), []string{host, "-C", "/tmp/wt-b", "status"}, deps); code != 0 {
			t.Fatalf("zcp git %s exit = %d, stderr %q", host, code, stderr.String())
		}
		if len(ssh.calls) != 1 || ssh.calls[0].host != host {
			t.Fatalf("ssh calls = %+v, want one to %s", ssh.calls, host)
		}
		if want := `cd '/var/www' && GIT_TERMINAL_PROMPT=0 exec git '-C' '/tmp/wt-b' 'status'`; !strings.HasSuffix(ssh.calls[0].command, want) {
			t.Errorf("remote command for %s = %q, want it to end with %q", host, ssh.calls[0].command, want)
		}
	}
}

// TestRun_GitVerb_SkipsTelemetryAndServe: `zcp git` is dispatched before CLI
// telemetry — which would print its disclosure into git's stdout and stamp
// its install file — and never falls through to the MCP server.
func TestRun_GitVerb_SkipsTelemetryAndServe(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		args []string
		want bool
	}{
		{[]string{"git"}, true},
		{[]string{"git", "appdev", "status"}, true},
		{[]string{"hq", "git"}, false},
		{[]string{"github"}, false},
		{nil, false},
	} {
		if got := isGit(tt.args); got != tt.want {
			t.Errorf("isGit(%q) = %v, want %v", tt.args, got, tt.want)
		}
	}

	home := t.TempDir()
	cmd := exec.CommandContext(t.Context(), zcpBinary(t), "git", "appdev")
	cmd.Env = append(os.Environ(), "HOME="+home, "ZCP_TELEMETRY=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = 5 * time.Second
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatal("zcp git did not return: it fell through to the MCP server")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
		t.Errorf("zcp git appdev = %v, want exit 2 (no subcommand); stderr %q", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing: telemetry's disclosure must not reach git's stdout", stdout.String())
	}
	if !strings.HasPrefix(stderr.String(), "zcp git: ") {
		t.Errorf("stderr = %q, want the zcp git refusal", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".zcp", "telemetry", "install.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("telemetry install file stat = %v, want absent: CLI telemetry ran", err)
	}
}
