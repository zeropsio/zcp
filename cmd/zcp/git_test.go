package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
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

// remoteScript is the script a remote command hands /bin/sh, after checking
// that a POSIX shell — the login shell sshd runs it in — splits command into
// exactly /bin/sh, -c and that one script (the shell's own split, set --).
func remoteScript(t *testing.T, command string) string {
	t.Helper()
	if !strings.HasPrefix(command, "/bin/sh -c '") || !strings.HasSuffix(command, "'") {
		t.Fatalf("remote command = %q, want /bin/sh -c '<script>'", command)
	}
	out, err := exec.CommandContext(t.Context(), "sh", "-c", "set -- "+command+"; printf '%s\\0' \"$@\"").Output() //nolint:gosec // splits a command this test built, checked above to be one quoted word
	if err != nil {
		t.Fatalf("split %q in sh: %v", command, err)
	}
	words := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(words) != 3 || words[0] != "/bin/sh" || words[1] != "-c" {
		t.Fatalf("remote command %q splits into %q, want exactly /bin/sh -c <script>", command, words)
	}
	return words[2]
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

// TestRunGitCmd_PlainCall_StdinOnlyWhenGitReadsIt: a plain call hands ssh the
// caller's stdin only when the argv makes git read it — the same rule a
// relayed call forwards by — so `while read h; do zcp git "$h" status; done <
// hosts` leaves the loop its input; any other call's remote reads nothing.
func TestRunGitCmd_PlainCall_StdinOnlyWhenGitReadsIt(t *testing.T) {
	t.Parallel()
	const hosts = "appdev\napidev\n"
	for _, tc := range []struct {
		args []string
		read bool
	}{
		{[]string{"appdev", "status"}, false},
		{[]string{"appdev", "log", "-1"}, false},
		{[]string{"appdev", "commit", "-F", "-"}, true},
		{[]string{"appdev", "hash-object", "--stdin"}, true},
	} {
		var got []byte
		ssh := &fakeGitSSH{run: func(stdin io.Reader, _, _ io.Writer) (int, error) {
			if stdin != nil {
				b, err := io.ReadAll(stdin)
				got = b
				return 0, err
			}
			return 0, nil
		}}
		deps, _, stderr := gitTestDeps(t.TempDir(), ssh)
		caller := strings.NewReader(hosts)
		deps.stdin = caller
		if code := runGitCmd(t.Context(), tc.args, deps); code != 0 {
			t.Fatalf("zcp git %q exit %d, stderr %q", tc.args, code, stderr.String())
		}
		if tc.read {
			if string(got) != hosts {
				t.Errorf("zcp git %q: the remote read %q, want the caller's stdin %q", tc.args, got, hosts)
			}
			continue
		}
		if len(got) != 0 || caller.Len() != len(hosts) {
			t.Errorf("zcp git %q: the remote read %q and %d of the caller's %d bytes are left, want nothing read", tc.args, got, caller.Len(), len(hosts))
		}
	}
}

// TestRunGitCmd_PassesStdinStdoutExitCode: the caller's stdin reaches git
// that reads it, git's stdout and stderr reach the caller byte for byte, and
// the exit code is git's; ssh's own 255, or an ssh that did not run, is said
// as an ssh failure.
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
			if want := `cd '/var/www' && GIT_TERMINAL_PROMPT=0 exec git 'commit' '-F' '-'`; !strings.HasSuffix(remoteScript(t, ssh.calls[0].command), want) {
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
			cmd := exec.CommandContext(t.Context(), zcpBinary(t), "git", "appdev", "log", "--all", "-p") //nolint:gosec // Runs this test binary with a fixed zcp command line.
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
		if want := `cd '/var/www/ws' && GIT_TERMINAL_PROMPT=0 exec git 'status'`; !strings.HasSuffix(remoteScript(t, ssh.calls[0].command), want) {
			t.Errorf("remote command for %s = %q, want it to end with %q", host, ssh.calls[0].command, want)
		}
	}
}

// TestRunGitCmd_UnrecordedService_PlainPassthroughInVarWww: a service no
// record names — an unadopted one — runs git in /var/www, where a runtime's
// code lives, with nothing else decided for it.
func TestRunGitCmd_UnrecordedService_PlainPassthroughInVarWww(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeGitTestMeta(t, stateDir, gitPoolPairMeta())
	ssh := &fakeGitSSH{}
	deps, _, stderr := gitTestDeps(stateDir, ssh)
	if code := runGitCmd(t.Context(), []string{"otherdev", "-C", "/tmp/wt-b", "status"}, deps); code != 0 {
		t.Fatalf("zcp git otherdev exit = %d, stderr %q", code, stderr.String())
	}
	if len(ssh.calls) != 1 || ssh.calls[0].host != "otherdev" {
		t.Fatalf("ssh calls = %+v, want one to otherdev", ssh.calls)
	}
	if want := `cd '/var/www' && GIT_TERMINAL_PROMPT=0 exec git '-C' '/tmp/wt-b' 'status'`; !strings.HasSuffix(remoteScript(t, ssh.calls[0].command), want) {
		t.Errorf("remote command = %q, want it to end with %q", ssh.calls[0].command, want)
	}
}

// gitPoolPairMeta is a pair keyed by its dev pool apppool.
func gitPoolPairMeta() *workflow.ServiceMeta {
	return &workflow.ServiceMeta{
		Hostname: "apppool",
		Mode:     topology.PlanModeStandard,
		Stages:   []topology.StageRef{{Hostname: "appstage"}},
		DevPool:  &workflow.DevPoolRef{ServiceID: "svc-pool"},
	}
}

// writeGitLeaseRecord writes the record zerops_lease keeps of apppool-3, a
// lease of the pool apppool, parked or not.
func writeGitLeaseRecord(t *testing.T, stateDir string, parked bool) {
	t.Helper()
	writeInitLeaseRecord(t, stateDir)
	if !parked {
		return
	}
	path := filepath.Join(stateDir, "leases", "apppool-3.json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body = bytes.Replace(body, []byte(`{`), []byte(`{"parked":true,"parkedAt":"2026-10-23T11:00:00Z",`), 1)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

// gitLeaseOrch stands in for the pool's orchestrator a lease check asks: a
// renew of apppool-3's lease answers renew, a read of its container by id
// answers byID. Any other call is none a check makes.
type gitLeaseOrch struct {
	ops.SwarmAPI

	renew func() (platform.SwarmContainer, error)
	byID  platform.SwarmContainer

	mu     sync.Mutex
	renews int
}

func (o *gitLeaseOrch) Renew(context.Context, string, string) (platform.SwarmContainer, error) {
	o.mu.Lock()
	o.renews++
	o.mu.Unlock()
	return o.renew()
}

func (o *gitLeaseOrch) Container(context.Context, string) (platform.SwarmContainer, error) {
	return o.byID, nil
}

func (o *gitLeaseOrch) renewed() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.renews
}

// ownedLease is the renew answer of a lease whose container is its own and
// running; stoppedLease of one whose container is its own and stopped.
func ownedLease() (platform.SwarmContainer, error) {
	return platform.SwarmContainer{ID: "c-3", LockState: "reserved", Status: platform.SwarmContainerActive}, nil
}

func stoppedLease() (platform.SwarmContainer, error) {
	return platform.SwarmContainer{ID: "c-3", LockState: "reserved", Status: platform.SwarmContainerStopped}, nil
}

// gitLeaseDeps builds what the lease check asks with — the project holding
// the pool apppool, its orchestrator orch, the bearer in the live env — and
// counts each build.
func gitLeaseDeps(orch *gitLeaseOrch, built *int) func(context.Context) (ops.LeaseDeps, error) {
	return func(context.Context) (ops.LeaseDeps, error) {
		*built++
		return ops.LeaseDeps{
			Client: platform.NewMock().WithServices([]platform.ServiceStack{{
				ID: "svc-pool", Name: "apppool", ProjectID: "proj", Status: platform.ServiceStatusActive,
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"},
			}}),
			ProjectID: "proj",
			Swarm:     func(string, string) ops.SwarmAPI { return orch },
			Env: func(key string) (string, bool) {
				if key == "apppool_API_TOKEN" {
					return "orch-bearer-FAKE", true
				}
				return "", false
			},
		}, nil
	}
}

// TestRunGitCmd_LeaseAlias_NoRecord_Refused: only zcp's own lease records
// make an alias reachable (docs/spec-workflows.md §8 DP-2): an alias zcp
// holds no lease of is refused before any ssh call, and nobody is asked.
func TestRunGitCmd_LeaseAlias_NoRecord_Refused(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeGitTestMeta(t, stateDir, gitPoolPairMeta())
	ssh := &fakeGitSSH{}
	deps, stdout, stderr := gitTestDeps(stateDir, ssh)
	built := 0
	deps.leaseDeps = gitLeaseDeps(&gitLeaseOrch{renew: ownedLease}, &built)
	args := []string{"apppool-3", "status"}
	code := runGitCmd(t.Context(), args, deps)
	assertGitRefused(t, args, code, stdout, stderr, ssh, "apppool-3", "zerops_lease action=status")
	if built != 0 {
		t.Errorf("the lease check's client was built %d times for an alias with no record, want never", built)
	}
}

// TestRunGitCmd_LeaseAlias_NotVouched_RefusedBeforeSSH: a lease's container
// is asked about before anything reaches it (DP-2). One that is no longer
// the lease's — another holder's, gone — is refused naming the release that
// drops its record; one the orchestrator does not answer about, or that this
// zcp cannot ask (no orchestrator, no API client), naming why and the status
// that asks again. Each is refused with exit 2 and no ssh call.
func TestRunGitCmd_LeaseAlias_NotVouched_RefusedBeforeSSH(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		orch  *gitLeaseOrch
		deps  func(ops.LeaseDeps) (ops.LeaseDeps, error)
		want  []string
		asked bool
	}{
		{
			name: "another holder's",
			orch: &gitLeaseOrch{
				renew: func() (platform.SwarmContainer, error) {
					return platform.SwarmContainer{}, &platform.SwarmError{Status: 400, Reason: platform.SwarmReasonUnknownLease}
				},
				byID: platform.SwarmContainer{ID: "c-3", LockState: "reserved", Status: platform.SwarmContainerActive},
			},
			want:  []string{"apppool-3", "zerops_lease action=release lease=apppool-3"},
			asked: true,
		},
		{
			name: "gone",
			orch: &gitLeaseOrch{renew: func() (platform.SwarmContainer, error) {
				return platform.SwarmContainer{}, &platform.SwarmError{Status: 400, Reason: platform.SwarmReasonContainerNotFound}
			}},
			want:  []string{"apppool-3", "zerops_lease action=release lease=apppool-3"},
			asked: true,
		},
		{
			name: "not answered",
			orch: &gitLeaseOrch{renew: func() (platform.SwarmContainer, error) {
				return platform.SwarmContainer{}, errors.New("dial tcp: connection refused")
			}},
			want:  []string{"apppool-3", "zerops_lease action=status"},
			asked: true,
		},
		{
			name: "no orchestrator",
			orch: &gitLeaseOrch{renew: ownedLease},
			deps: func(d ops.LeaseDeps) (ops.LeaseDeps, error) { d.Swarm = nil; return d, nil },
			want: []string{"apppool-3", "zerops_lease action=status"},
		},
		{
			name: "no API client",
			orch: &gitLeaseOrch{renew: ownedLease},
			deps: func(ops.LeaseDeps) (ops.LeaseDeps, error) {
				return ops.LeaseDeps{}, errors.New("auth: No authentication found: set ZCP_API_KEY or log in with zcli")
			},
			want: []string{"apppool-3", "API client", "set ZCP_API_KEY"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			writeGitTestMeta(t, stateDir, gitPoolPairMeta())
			writeGitLeaseRecord(t, stateDir, false)
			ssh := &fakeGitSSH{}
			deps, stdout, stderr := gitTestDeps(stateDir, ssh)
			built := 0
			base := gitLeaseDeps(tc.orch, &built)
			deps.leaseDeps = func(ctx context.Context) (ops.LeaseDeps, error) {
				d, err := base(ctx)
				if tc.deps != nil {
					return tc.deps(d)
				}
				return d, err
			}
			args := []string{"apppool-3", "status"}
			code := runGitCmd(t.Context(), args, deps)
			assertGitRefused(t, args, code, stdout, stderr, ssh, tc.want...)
			if asked := tc.orch.renewed() > 0; asked != tc.asked {
				t.Errorf("the orchestrator was asked %d times, want asked = %v", tc.orch.renewed(), tc.asked)
			}
		})
	}
}

// TestRunGitCmd_LeaseAlias_Parked_RefusedNamingTheUnpark: nothing answers on
// a parked lease (DP-28) — parked by zcp's record, or its container stopped
// outside zcp — so the call is refused with exit 2 naming the unpark, before
// any ssh call.
func TestRunGitCmd_LeaseAlias_Parked_RefusedNamingTheUnpark(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		parked bool
		renew  func() (platform.SwarmContainer, error)
	}{
		{"parked by zcp", true, stoppedLease},
		{"stopped outside zcp", false, stoppedLease},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			writeGitTestMeta(t, stateDir, gitPoolPairMeta())
			writeGitLeaseRecord(t, stateDir, tc.parked)
			ssh := &fakeGitSSH{}
			deps, stdout, stderr := gitTestDeps(stateDir, ssh)
			built := 0
			deps.leaseDeps = gitLeaseDeps(&gitLeaseOrch{renew: tc.renew}, &built)
			args := []string{"apppool-3", "status"}
			code := runGitCmd(t.Context(), args, deps)
			assertGitRefused(t, args, code, stdout, stderr, ssh, "apppool-3", "parked", "zerops_lease action=unpark lease=apppool-3")
		})
	}
}

// TestRunGitCmd_LeaseAlias_Owned_RunsInTheLeaseCheckout: a lease the
// orchestrator gives zcp — asked once, before the ssh call — runs git in the
// lease's checkout, /var/www of its container, reached by the alias; in a
// Mate container a lease of a pool pair wired to HQ is relayed, answered
// from the pool pair's record, and one of a pair not wired stays the plain
// passthrough.
func TestRunGitCmd_LeaseAlias_Owned_RunsInTheLeaseCheckout(t *testing.T) {
	t.Parallel()
	wired := gitPoolPairMeta()
	wired.HQ = &workflow.HQRepoRef{AppID: "app-1", Repo: "appdev"}
	for _, tc := range []struct {
		name    string
		meta    *workflow.ServiceMeta
		mate    bool
		relayed bool
	}{
		{"plain, no Mate", wired, false, false},
		{"plain, pair not wired", gitPoolPairMeta(), true, false},
		{"relayed, pair wired to HQ", wired, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			writeGitTestMeta(t, stateDir, tc.meta)
			writeGitLeaseRecord(t, stateDir, false)
			ssh := &fakeGitSSH{}
			deps, stdout, stderr := gitTestDeps(stateDir, ssh)
			deps.rt.MateEnabled = tc.mate
			orch := &gitLeaseOrch{renew: ownedLease}
			built := 0
			deps.leaseDeps = gitLeaseDeps(orch, &built)
			reads := 0
			deps.hqIdentity = func() hq.Identity { reads++; return hq.Identity{Origin: "https://hq.example"} }
			if code := runGitCmd(t.Context(), []string{"apppool-3", "-C", "/tmp/wt-b", "status"}, deps); code != 0 {
				t.Fatalf("exit %d, stderr %q", code, stderr.String())
			}
			if orch.renewed() != 1 {
				t.Errorf("the orchestrator was asked %d times, want once before the ssh call", orch.renewed())
			}
			if len(ssh.calls) != 1 || ssh.calls[0].host != "apppool-3" {
				t.Fatalf("ssh calls = %+v, want one to apppool-3", ssh.calls)
			}
			script := remoteScript(t, ssh.calls[0].command)
			if !strings.Contains(script, "cd '/var/www'") {
				t.Errorf("script = %q, want git run in the lease's checkout /var/www", script)
			}
			if tc.relayed {
				for _, mark := range relayedScriptMarks {
					if !strings.Contains(script, mark) {
						t.Errorf("script lacks %q:\n%s", mark, script)
					}
				}
				if reads != 1 {
					t.Errorf("the HQ identity was read %d times, want once for a relayed call", reads)
				}
			} else {
				if want := `cd '/var/www' && GIT_TERMINAL_PROMPT=0 exec git '-C' '/tmp/wt-b' 'status'`; !strings.HasSuffix(script, want) {
					t.Errorf("script = %q, want the plain passthrough ending %q", script, want)
				}
				if reads != 0 {
					t.Errorf("the HQ identity was read %d times for a plain call", reads)
				}
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q", stdout.String())
			}
		})
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
	cmd := exec.CommandContext(t.Context(), zcpBinary(t), "git", "appdev") //nolint:gosec // Runs this test binary with a fixed zcp command line.
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

// hqWiredMeta is a pair wired to HQ's repository appdev in app-1 — wired
// through or not (no credential fingerprint: no GIT_TOKEN in its dev).
func hqWiredMeta() *workflow.ServiceMeta {
	return &workflow.ServiceMeta{
		Hostname: "appdev",
		Mode:     topology.PlanModeStandard,
		Stages:   []topology.StageRef{{Hostname: "appstage"}},
		HQ:       &workflow.HQRepoRef{AppID: "app-1", Repo: "appdev"},
	}
}

// relayedScriptMarks are what only a relayed call's script holds: answers
// and frames on their own descriptors, persisted helpers cleared for zcp's
// own, the protocol pin, and git's stdin taken off the session.
var relayedScriptMarks = []string{
	"exec 3<&0 4>&2 8<&0 9>&2",
	"'-c' 'credential.helper=' '-c' 'credential.helper=!f() {",
	"GIT_ALLOW_PROTOCOL=",
	"'status' < /dev/null",
	"ZCPDONE-",
}

// TestRunGitCmd_NotWiredThrough_StillRelayed: in a Mate container every
// subcommand of an HQ-wired pair is relayed — a partial clone's lazy fetches
// need a credential on any of them — whether the pair was wired through (its
// dev holds GIT_TOKEN) or not.
func TestRunGitCmd_NotWiredThrough_StillRelayed(t *testing.T) {
	t.Parallel()
	for _, fingerprint := range []string{"", "fp-1"} {
		stateDir := t.TempDir()
		meta := hqWiredMeta()
		meta.HQ.CredentialFingerprint = fingerprint
		writeGitTestMeta(t, stateDir, meta)
		ssh := &fakeGitSSH{}
		deps, stdout, stderr := gitTestDeps(stateDir, ssh)
		deps.rt.MateEnabled = true
		if code := runGitCmd(t.Context(), []string{"appdev", "status"}, deps); code != 0 {
			t.Fatalf("fingerprint %q: exit %d, stderr %q", fingerprint, code, stderr.String())
		}
		if len(ssh.calls) != 1 || ssh.calls[0].host != "appdev" {
			t.Fatalf("fingerprint %q: ssh calls = %+v, want one to appdev", fingerprint, ssh.calls)
		}
		script := remoteScript(t, ssh.calls[0].command)
		for _, mark := range relayedScriptMarks {
			if !strings.Contains(script, mark) {
				t.Errorf("fingerprint %q: script lacks %q:\n%s", fingerprint, mark, script)
			}
		}
		if stdout.Len() != 0 {
			t.Errorf("fingerprint %q: stdout = %q", fingerprint, stdout.String())
		}
	}
}

// TestRunGitCmd_NotWired_PassthroughOnly: a pair not wired to HQ, a host no
// record names, and any host outside a Mate container stay the plain
// passthrough — no helper, no relay.
func TestRunGitCmd_NotWired_PassthroughOnly(t *testing.T) {
	t.Parallel()
	notWired := hqWiredMeta()
	notWired.HQ = nil
	noRepo := hqWiredMeta()
	noRepo.HQ.Repo = ""
	for _, tc := range []struct {
		name string
		meta *workflow.ServiceMeta
		host string
		mate bool
	}{
		{"not wired", notWired, "appdev", true},
		{"wired with no repository", noRepo, "appdev", true},
		{"no record", nil, "otherdev", true},
		{"wired, no Mate", hqWiredMeta(), "appdev", false},
	} {
		stateDir := t.TempDir()
		if tc.meta != nil {
			writeGitTestMeta(t, stateDir, tc.meta)
		}
		ssh := &fakeGitSSH{}
		deps, _, stderr := gitTestDeps(stateDir, ssh)
		deps.rt.MateEnabled = tc.mate
		if code := runGitCmd(t.Context(), []string{tc.host, "status"}, deps); code != 0 {
			t.Fatalf("%s: exit %d, stderr %q", tc.name, code, stderr.String())
		}
		if len(ssh.calls) != 1 {
			t.Fatalf("%s: ssh calls = %+v", tc.name, ssh.calls)
		}
		script := remoteScript(t, ssh.calls[0].command)
		if !strings.HasSuffix(script, "GIT_TERMINAL_PROMPT=0 exec git 'status'") || strings.Contains(script, "credential.helper") {
			t.Errorf("%s: script = %q, want the plain passthrough", tc.name, script)
		}
	}
}

// TestBrokeredGit_Allowlist_FromRecordsAndArgvURLsOnly: what a relayed call
// answers GitHub for is the record's HQ origin — written by zcp from HQ's own
// state — and the URLs its argv writes literally; never the record's
// RemoteURL, written from the checkout's live origin, and never a remote
// name. Addresses compare by repository: case, the default port and .git
// aside.
func TestBrokeredGit_Allowlist_FromRecordsAndArgvURLsOnly(t *testing.T) {
	t.Parallel()
	id := hq.Identity{Origin: "https://hq.example", Key: "key-FAKE", ProjectID: "p1"}
	args, err := ops.ParseBrokeredGitArgs([]string{"fetch", "upstream", "https://github.com/acme/lib", "main"})
	if err != nil {
		t.Fatal(err)
	}

	meta := hqWiredMeta()
	meta.RemoteURL = "https://github.com/acme/evil.git"
	meta.HQ.Origin = &workflow.HQOrigin{Mode: hq.OriginOnGitHub, URL: "https://GitHub.com/Acme/Shop.git"}
	relay := brokeredGitRelayFor(meta, id, nil, args)
	if want := []string{"https://GitHub.com/Acme/Shop.git", "https://github.com/acme/lib"}; !slices.Equal(relay.Repos, want) {
		t.Errorf("relay repositories = %q, want the record's origin and the argv URL %q", relay.Repos, want)
	}
	if relay.LiveOrigin != nil {
		t.Errorf("relay reads HQ's live origin %+v though the record names one", relay.LiveOrigin)
	}
	if relay.Identity != id {
		t.Errorf("relay identity = %+v, want the call's", relay.Identity)
	}
	for _, same := range []string{"https://github.com:443/acme/shop", "https://github.com/acme/shop.git/", "https://GITHUB.COM/ACME/SHOP"} {
		if !relay.AllowsRepo(same) {
			t.Errorf("AllowsRepo(%q) = false, want the record's repository", same)
		}
	}
	for _, other := range []string{meta.RemoteURL, "upstream", "https://github.com/acme/shopx", "https://github.com:8443/acme/shop", "http://github.com/acme/shop", ""} {
		if relay.AllowsRepo(other) {
			t.Errorf("AllowsRepo(%q) = true, want declined", other)
		}
	}
	if strings.Contains(fmt.Sprintf("%+v", relay), "evil") {
		t.Errorf("the relay carries the record's RemoteURL: %+v", relay)
	}

	meta.HQ.Origin = &workflow.HQOrigin{Mode: workflow.HQOriginNone}
	relay = brokeredGitRelayFor(meta, id, nil, args)
	if want := []string{"https://github.com/acme/lib"}; !slices.Equal(relay.Repos, want) {
		t.Errorf("relay repositories without a recorded origin = %q, want the argv URL alone", relay.Repos)
	}
	if relay.LiveOrigin == nil || *relay.LiveOrigin != (ops.BrokeredGitHQRepo{AppID: "app-1", Repo: "appdev"}) {
		t.Errorf("relay live origin = %+v, want HQ's for app-1/appdev", relay.LiveOrigin)
	}
}

// TestRunGitCmd_HQIdentityReadOnlyForRelayedCalls: the Mate's HQ identity —
// the live env store, the installed Mate's package — is read only for a
// call that is relayed, once.
func TestRunGitCmd_HQIdentityReadOnlyForRelayedCalls(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		meta  *workflow.ServiceMeta
		mate  bool
		reads int
	}{
		{"relayed", hqWiredMeta(), true, 1},
		{"wired, no Mate", hqWiredMeta(), false, 0},
		{"no record", nil, true, 0},
	} {
		stateDir := t.TempDir()
		if tc.meta != nil {
			writeGitTestMeta(t, stateDir, tc.meta)
		}
		ssh := &fakeGitSSH{}
		deps, _, stderr := gitTestDeps(stateDir, ssh)
		deps.rt.MateEnabled = tc.mate
		reads := 0
		deps.hqIdentity = func() hq.Identity { reads++; return hq.Identity{Origin: "https://hq.example"} }
		if code := runGitCmd(t.Context(), []string{"appdev", "status"}, deps); code != 0 {
			t.Fatalf("%s: exit %d, stderr %q", tc.name, code, stderr.String())
		}
		if reads != tc.reads {
			t.Errorf("%s: the HQ identity was read %d times, want %d", tc.name, reads, tc.reads)
		}
	}
}

// TestRunGitCmd_RelayedStdinRead_StoppedExit130: Ctrl-C while zcp git reads
// a stdin that never ends — am with no file — stops it with exit 130 and one
// line saying nothing ran, before any ssh call.
func TestRunGitCmd_RelayedStdinRead_StoppedExit130(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeGitTestMeta(t, stateDir, hqWiredMeta())
	ssh := &fakeGitSSH{}
	deps, stdout, stderr := gitTestDeps(stateDir, ssh)
	deps.rt.MateEnabled = true
	stdin, never := io.Pipe()
	t.Cleanup(func() { _ = never.Close() })
	deps.stdin = stdin
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(200*time.Millisecond, cancel)
	code := make(chan int, 1)
	go func() { code <- runGitCmd(ctx, []string{"appdev", "am"}, deps) }()
	select {
	case c := <-code:
		if c != 130 {
			t.Errorf("exit = %d, want 130", c)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("zcp git kept reading stdin after Ctrl-C")
	}
	if len(ssh.calls) != 0 {
		t.Errorf("ssh calls = %+v, want none", ssh.calls)
	}
	if msg := stderr.String(); !strings.HasPrefix(msg, "zcp git: ") || strings.Count(msg, "\n") != 1 || !strings.Contains(msg, "nothing ran") {
		t.Errorf("stderr = %q, want one zcp git line saying nothing ran", msg)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q", stdout.String())
	}
}

// TestRunGitCmd_RelayedCall_RefusesBeforeSSH: on a relayed call, git's own
// credential subcommands, a user -c that decides credentials or transport,
// and stdin over the 1 MiB zcp forwards are refused with exit 2, before any
// ssh call.
func TestRunGitCmd_RelayedCall_RefusesBeforeSSH(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeGitTestMeta(t, stateDir, hqWiredMeta())
	for _, tc := range []struct {
		args  []string
		stdin string
		want  string
	}{
		{[]string{"appdev", "credential", "fill"}, "", "credential"},
		{[]string{"appdev", "credential-store", "get"}, "", "credential-store"},
		{[]string{"appdev", "-c", "credential.helper=!x", "fetch"}, "", "credential.helper"},
		{[]string{"appdev", "-c", "url.https://evil.example/.insteadOf=https://github.com/", "fetch"}, "", "url."},
		{[]string{"appdev", "commit", "-F", "-"}, strings.Repeat("m", 1<<20+1), "1 MiB"},
	} {
		ssh := &fakeGitSSH{}
		deps, stdout, stderr := gitTestDeps(stateDir, ssh)
		deps.rt.MateEnabled = true
		deps.stdin = strings.NewReader(tc.stdin)
		code := runGitCmd(t.Context(), tc.args, deps)
		assertGitRefused(t, tc.args, code, stdout, stderr, ssh, tc.want)
	}
	ssh := &fakeGitSSH{}
	deps, _, stderr := gitTestDeps(stateDir, ssh)
	deps.rt.MateEnabled = true
	deps.stdin = strings.NewReader(strings.Repeat("m", 1<<20))
	if code := runGitCmd(t.Context(), []string{"appdev", "commit", "-F", "-"}, deps); code != 0 || len(ssh.calls) != 1 {
		t.Errorf("a 1 MiB message: exit %d, ssh calls %d, stderr %q; want it forwarded", code, len(ssh.calls), stderr.String())
	}
}
