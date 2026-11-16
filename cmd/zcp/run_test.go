package main

import (
	"io"
	"strings"
	"syscall"
	"testing"
)

// runTestDeps is gitTestDeps for `zcp run`.
func runTestDeps(t *testing.T, ssh *fakeGitSSH) (gitCmdDeps, *strings.Builder, *strings.Builder) {
	t.Helper()
	deps, _, _ := gitTestDeps(t.TempDir(), ssh)
	var stdout, stderr strings.Builder
	deps.verb, deps.stdout, deps.stderr = runHostVerb(), &stdout, &stderr
	return deps, &stdout, &stderr
}

// The command's stdin, stdout, stderr and exit code are its own: a heredoc
// or a pipe feeds it, and its answer is the call's.
// witness: cmd/zcp/run.go | argv, d.stdin, stdout, stderr) → argv, nil, stdout, stderr)
func TestRunRunCmd_Command_StdinStdoutStderrAndExitCodeAreItsOwn(t *testing.T) {
	t.Parallel()
	ssh := &fakeGitSSH{run: func(stdin io.Reader, stdout, stderr io.Writer) (int, error) {
		fed := "nothing"
		if stdin != nil {
			read, err := io.ReadAll(stdin)
			if err != nil {
				return -1, err
			}
			fed = string(read)
		}
		_, _ = io.WriteString(stdout, "read "+fed)
		_, _ = io.WriteString(stderr, "a warning")
		return 7, nil
	}}
	deps, stdout, stderr := runTestDeps(t, ssh)
	deps.stdin = strings.NewReader("a heredoc")
	code := runRunCmd(t.Context(), []string{"appdev", "wc", "-c"}, "/tmp", deps)
	if code != 7 || stdout.String() != "read a heredoc" || stderr.String() != "a warning" {
		t.Errorf("exit %d, stdout %q, stderr %q; want 7, %q, %q", code, stdout.String(), stderr.String(), "read a heredoc", "a warning")
	}
	if len(ssh.calls) != 1 || ssh.calls[0].host != "appdev" {
		t.Errorf("ssh calls = %+v, want one to appdev", ssh.calls)
	}
}

// A call from a folder of the service's mount runs in that folder of the
// service.
// witness: cmd/zcp/run.go | ops.ServiceRunDir(host, target.dir, cwd) → ops.ServiceRunDir(host, target.dir, "")
func TestRunRunCmd_FromAFolderOfTheMount_RunsInThatFolderOfTheService(t *testing.T) {
	t.Parallel()
	ssh := &fakeGitSSH{}
	deps, _, stderr := runTestDeps(t, ssh)
	if code := runRunCmd(t.Context(), []string{"appdev", "npm", "test"}, "/var/www/appdev/apps/web", deps); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr.String())
	}
	if len(ssh.calls) != 1 || !strings.Contains(ssh.calls[0].command, "/var/www/apps/web") {
		t.Errorf("ssh calls = %+v, want one that changes into /var/www/apps/web", ssh.calls)
	}
}

// goneReader is a stdout whose reader went away: every write fails as on a closed pipe.
type goneReader struct{}

func (goneReader) Write([]byte) (int, error) { return 0, syscall.EPIPE }

// A reader of the output that goes away (`zcp run … | head -1`) ends the call
// as it ends any command writing into a pipe: 141, and no line saying ssh
// failed, though ssh ends with its own 255 on the same closed pipe.
// witness: cmd/zcp/run.go | r.gone.Store(true) → r.gone.Store(false)
func TestRunRunCmd_ReaderOfTheOutputGone_EndsQuietlyAsAPipeDoes(t *testing.T) {
	t.Parallel()
	ssh := &fakeGitSSH{run: func(_ io.Reader, stdout, _ io.Writer) (int, error) {
		if _, err := io.WriteString(stdout, "a line\n"); err == nil {
			return 0, nil
		}
		return 255, nil
	}}
	deps, _, stderr := runTestDeps(t, ssh)
	deps.stdout = goneReader{}
	code := runRunCmd(t.Context(), []string{"appdev", "seq", "1", "9"}, "/tmp", deps)
	if code != 141 || stderr.Len() != 0 {
		t.Errorf("exit %d, stderr %q; want 141 and no line", code, stderr.String())
	}
}

// A call that names no command is refused before anything reaches the
// service.
// witness: cmd/zcp/run.go | if len(argv) == 0 { → if len(argv) < 0 {
func TestRunRunCmd_NoCommand_RefusedBeforeSSH(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"appdev"}, {"appdev", "--"}} {
		ssh := &fakeGitSSH{}
		deps, _, _ := runTestDeps(t, ssh)
		if code := runRunCmd(t.Context(), args, "/tmp", deps); code != 2 || len(ssh.calls) != 0 {
			t.Errorf("zcp run %v: exit %d, ssh calls %d; want 2 and none", args, code, len(ssh.calls))
		}
	}
}
