package ops

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// A cwd in a folder of host's mount names that folder of the service; the
// mount's root, another service's mount, a mount whose name only starts with
// the host's, and a path that climbs out of the mount name the checkout.
// witness: internal/ops/service_run.go | strings.CutPrefix(path.Clean(cwd), mount+"/") → strings.CutPrefix(path.Clean(cwd), mount)
func TestServiceRunDir_FolderOfTheMount_IsThatFolderOfTheService(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, host, checkout, cwd, want string
	}{
		{"a folder of the mount", "appdev", "/var/www", "/var/www/appdev/apps/web", "/var/www/apps/web"},
		{"a folder of a lease's mount", "webpool-7", "/var/www", "/var/www/webpool-7/packages/shared", "/var/www/packages/shared"},
		{"the mount's root", "appdev", "/var/www", "/var/www/appdev", "/var/www"},
		{"outside every mount", "appdev", "/var/www", "/tmp", "/var/www"},
		{"outside, the code on a volume", "appdev", "/var/www/code", "/tmp", "/var/www/code"},
		{"another service's mount", "appdev", "/var/www", "/var/www/apidev/src", "/var/www"},
		{"a mount whose name starts with the host's", "appdev", "/var/www", "/var/www/appdev2/src", "/var/www"},
		{"a path that climbs out of the mount", "appdev", "/var/www", "/var/www/appdev/../../etc", "/var/www"},
		{"no folder read", "appdev", "/var/www", "", "/var/www"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ServiceRunDir(tc.host, tc.checkout, tc.cwd); got != tc.want {
				t.Errorf("ServiceRunDir(%q, %q, %q) = %q, want %q", tc.host, tc.checkout, tc.cwd, got, tc.want)
			}
		})
	}
}

// Every argument reaches the command as data: a space, a quote, `;`, `$` and
// `*` are bytes of one argument, never the shell's.
// witness: internal/ops/service_run.go | b.WriteString(ShellQuote(a)) → b.WriteString(a)
func TestBuildServiceRunCommand_Arguments_ReachTheCommandAsData(t *testing.T) {
	t.Parallel()
	// The script the service's /bin/sh runs, POSIX single-quoted by hand …
	script := `cd '/var/www/apps/web' && 'printf' '%s' 'a b' 'it'\''s' ';ls' '$HOME' '*'; exit $?`
	// … after the step that moves its shell into a scope of its own, and that
	// script as the one -c argument of the login shell's command.
	want := "/bin/sh -c '" + strings.ReplaceAll(ownScopeStep(ownScopeRun)+script, `'`, `'\''`) + "'"
	got := BuildServiceRunCommand("/var/www/apps/web", []string{"printf", "%s", "a b", "it's", ";ls", "$HOME", "*"})
	if got != want {
		t.Errorf("remote command\n got: %s\nwant: %s", got, want)
	}
}

// runScoped runs a remote command one of zcp's builders made in a local
// /bin/sh, as sshd runs it in the service, with bin alone on PATH, stdin
// given and env added; it returns the command's stdout, stderr and exit code.
func runScoped(t *testing.T, command, bin, stdin string, env ...string) (string, string, int) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", command)
	cmd.Env = append([]string{"PATH=" + bin}, env...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("run %q: %v", command, err)
	}
	return stdout.String(), stderr.String(), cmd.ProcessState.ExitCode()
}

// writeFakeSudo puts a sudo on bin that writes its arguments, one per line,
// to argsFile and ends with exit, saying something on stdout and stderr.
func writeFakeSudo(t *testing.T, bin, argsFile string, exit int) {
	t.Helper()
	body := "#!/bin/sh\nfor a; do printf '%s\\n' \"$a\"; done > " + ShellQuote(argsFile) +
		"\necho 'sudo: a password is required'\necho 'sudo: a password is required' >&2\nexit " + strconv.Itoa(exit) + "\n"
	if err := os.WriteFile(filepath.Join(bin, "sudo"), []byte(body), 0o755); err != nil { //nolint:gosec // the test's own executable fake
		t.Fatal(err)
	}
}

// Over ssh the command would run in the platform agent's service, under its
// task limit of 300; its shell first asks systemd, as root, for a transient
// scope holding its own pid — named for zcp run and that pid, with no task
// limit of its own and collected once it ends — and only then runs the
// command, which is its child.
// witness: internal/ops/own_scope.go | TasksMax t 18446744073709551615 CollectMode s inactive-or-failed 0 → CollectMode s inactive-or-failed 0
func TestBuildServiceRunCommand_MovesItsShellIntoAScopeOfItsOwn(t *testing.T) {
	t.Parallel()
	bin, args := t.TempDir(), filepath.Join(t.TempDir(), "sudo-args")
	writeFakeSudo(t, bin, args, 0)
	stdout, _, code := runScoped(t, BuildServiceRunCommand(t.TempDir(), []string{"/bin/sh", "-c", "echo $PPID"}), bin, "")
	shell := strings.TrimSpace(stdout)
	if code != 0 || shell == "" {
		t.Fatalf("exit %d, stdout %q; want the command's parent pid", code, stdout)
	}
	asked, err := os.ReadFile(args)
	if err != nil {
		t.Fatalf("the shell never asked sudo for a scope: %v", err)
	}
	want := []string{
		"-n", "busctl", "call", "org.freedesktop.systemd1", "/org/freedesktop/systemd1", "org.freedesktop.systemd1.Manager",
		"StartTransientUnit", "ssa(sv)a(sa(sv))", "zcp-run-" + shell + ".scope", "fail",
		"3", "PIDs", "au", "1", shell, "TasksMax", "t", "18446744073709551615", "CollectMode", "s", "inactive-or-failed",
		"0",
	}
	if got := strings.Split(strings.TrimSuffix(string(asked), "\n"), "\n"); !slices.Equal(got, want) {
		t.Errorf("sudo asked with\n got: %q\nwant: %q", got, want)
	}
}

// Where no scope can be had — sudo wants a password or is not there at all —
// the command runs as before: its stdin, stdout, stderr, folder, environment
// and exit code are its own, and nothing of the attempt shows.
// witness: internal/ops/own_scope.go | </dev/null >/dev/null 2>&1; then → </dev/null; then
func TestBuildServiceRunCommand_NoScopeToBeHad_RunsAsBefore(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		sudo bool
	}{
		{"sudo wants a password", true},
		{"no sudo", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bin, dir := t.TempDir(), t.TempDir()
			if tc.sudo {
				writeFakeSudo(t, bin, filepath.Join(t.TempDir(), "sudo-args"), 1)
			}
			line := `read -r l; printf '%s|%s|%s' "$l" "$(pwd -P)" "$ZCP_PROBE"; echo a warning >&2; exit 7`
			stdout, stderr, code := runScoped(t, BuildServiceRunCommand(dir, []string{"/bin/sh", "-c", line}), bin, "hi\n", "ZCP_PROBE=kept")
			real, err := filepath.EvalSymlinks(dir)
			if err != nil {
				t.Fatal(err)
			}
			if want := "hi|" + real + "|kept"; code != 7 || stdout != want || stderr != "a warning\n" {
				t.Errorf("exit %d, stdout %q, stderr %q; want 7, %q, %q", code, stdout, stderr, want, "a warning\n")
			}
		})
	}
}

// Every script zcp runs over ssh for the repository's own work starts in a
// scope of its own, named for what it runs: a command, git plain and relayed
// (a hook runs the checks), a lease's prepare and the supervisor with its
// dev servers.
// witness: internal/ops/lease_supervisor.go | return ownScopeStep(ownScopeDevsup) + strings.Join( → return strings.Join(
func TestOwnScope_EveryScriptForTheRepositorysWork_StartsInItsOwnScope(t *testing.T) {
	t.Parallel()
	git := BrokeredGitArgs{Subcommand: "commit", Rest: []string{"-m", "a change"}}
	for _, tc := range []struct {
		name, scope, script string
	}{
		{"zcp run", ownScopeRun, binShScript(t, BuildServiceRunCommand("/var/www", []string{"npm", "test"}))},
		{"zcp git", ownScopeGit, binShScript(t, BuildBrokeredGitCommand("/var/www", git))},
		{"zcp git, relayed", ownScopeGit, binShScript(t, buildRelayedGitCommand("/var/www", git, "n0nce", false, "https"))},
		{"a lease's prepare", ownScopePrepare, leasePrepareCommand(LeaseWorkDir, "npm install")},
		{"the lease's supervisor", ownScopeDevsup, leaseSupervisorCommand(LeaseWorkDir, 10)},
	} {
		if !strings.HasPrefix(tc.script, ownScopeStep(tc.scope)) {
			t.Errorf("%s: script does not start in a %s scope:\n%s", tc.name, tc.scope, tc.script)
		}
	}
}
