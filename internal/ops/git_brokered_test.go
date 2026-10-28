package ops

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeGitBody is a stand-in for git on PATH: it prints the directory it runs
// in, then each argument it was given between markers, then its environment
// — what the remote shell handed git.
const fakeGitBody = "#!/bin/sh\nprintf 'pwd=%s\\n' \"$(pwd -P)\"\nfor a; do printf '<%s>\\n' \"$a\"; done\necho '--env--'\nenv\n"

// runBuiltCommand runs a command BuildBrokeredGitCommand built in a local sh,
// as sshd runs it in the dev service, with the fake git first on PATH and env
// added to the environment; it returns what the fake git printed.
func runBuiltCommand(t *testing.T, command string, env ...string) string {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(fakeGitBody), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "sh", "-c", command)
	cmd.Env = append(append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH")), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sh -c %q: %v\n%s", command, err, out)
	}
	return string(out)
}

// TestBuildBrokeredGitCommand_QuotesEveryArgument_RunsInCheckout: the remote
// command changes into the checkout's directory and hands git every argument
// exactly as given — spaces, quotes, `$`, `;`, newlines and backslashes
// reach git as data, never as shell syntax — each through ShellQuote.
func TestBuildBrokeredGitCommand_QuotesEveryArgument_RunsInCheckout(t *testing.T) {
	t.Parallel()
	t.Run("literal shape", func(t *testing.T) {
		t.Parallel()
		got := BuildBrokeredGitCommand("/var/www", BrokeredGitArgs{
			Subcommand: "commit",
			Rest:       []string{"-m", "it's $HOME; rm -rf /"},
		})
		want := `cd '/var/www' && GIT_TERMINAL_PROMPT=0 exec git 'commit' '-m' 'it'\''s $HOME; rm -rf /'`
		if !strings.HasSuffix(got, want) {
			t.Errorf("command = %q\nwant it to end with %q", got, want)
		}
		withOptions := BuildBrokeredGitCommand("/var/www/ws", BrokeredGitArgs{
			Config:     []string{"user.name=A B"},
			Dir:        "/tmp/wt-b",
			Subcommand: "log",
			Rest:       []string{"-1"},
		})
		wantOptions := `cd '/var/www/ws' && GIT_TERMINAL_PROMPT=0 exec git '-C' '/tmp/wt-b' '-c' 'user.name=A B' 'log' '-1'`
		if !strings.HasSuffix(withOptions, wantOptions) {
			t.Errorf("command = %q\nwant it to end with %q", withOptions, wantOptions)
		}
	})

	t.Run("argv round trip through a shell", func(t *testing.T) {
		t.Parallel()
		checkout := t.TempDir()
		rest := []string{
			"-m", "it's $HOME; echo pwned",
			"a b",
			`back\slash "double"`,
			"line one\nline two",
			"$(touch /tmp/zcp-git-pwned)",
			"`id`",
			"-",
		}
		out := runBuiltCommand(t, BuildBrokeredGitCommand(checkout, BrokeredGitArgs{
			Config:     []string{"core.pager=less -R"},
			Subcommand: "commit",
			Rest:       rest,
		}))
		realCheckout, err := filepath.EvalSymlinks(checkout)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "pwd="+realCheckout+"\n") {
			t.Errorf("git ran in the wrong directory, want %s:\n%s", realCheckout, out)
		}
		var got []string
		for _, line := range strings.Split(strings.SplitN(out, "--env--", 2)[0], "<") {
			if i := strings.LastIndex(line, ">\n"); i >= 0 {
				got = append(got, line[:i])
			}
		}
		want := append([]string{"-c", "core.pager=less -R", "commit"}, rest...)
		if !slices.Equal(got, want) {
			t.Errorf("git argv = %q\nwant      %q", got, want)
		}
	})
}

// TestBuildBrokeredGitCommand_ScrubsTraceAndAskpassEnv: before git runs, the
// remote command unsets every variable that would trace git's traffic, turn
// TLS checks off, prompt through an askpass program, point git at another
// repository, executable, ssh or proxy command, or load config from elsewhere,
// and runs git with terminal prompts off.
func TestBuildBrokeredGitCommand_ScrubsTraceAndAskpassEnv(t *testing.T) {
	t.Parallel()
	scrubbed := []string{
		"GIT_TRACE", "GIT_TRACE_PACKET", "GIT_TRACE_PACK_ACCESS", "GIT_TRACE_PERFORMANCE",
		"GIT_TRACE_SETUP", "GIT_TRACE_CURL", "GIT_TRACE_CURL_NO_DATA", "GIT_TRACE2",
		"GIT_TRACE2_EVENT", "GIT_TRACE2_PERF", "GIT_CURL_VERBOSE", "GIT_TRACE_REDACT",
		"GIT_SSL_NO_VERIFY", "GIT_SSL_CAINFO", "GIT_SSL_CAPATH",
		"GIT_ASKPASS", "SSH_ASKPASS",
		"GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT",
		"GIT_EXEC_PATH", "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE",
		"GIT_SSH", "GIT_SSH_COMMAND", "GIT_PROXY_COMMAND",
		"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM",
		"http_proxy", "https_proxy", "HTTPS_PROXY", "ALL_PROXY", "all_proxy",
	}
	env := make([]string, 0, len(scrubbed)+1)
	for _, name := range scrubbed {
		env = append(env, name+"=planted-"+name)
	}
	env = append(env, "GIT_TERMINAL_PROMPT=1")
	command := BuildBrokeredGitCommand(t.TempDir(), BrokeredGitArgs{Subcommand: "status"})
	if !strings.HasPrefix(command, "unset ") {
		t.Errorf("command = %q, want it to start by unsetting the scrubbed variables", command)
	}
	out := runBuiltCommand(t, command, env...)
	gitEnv := strings.SplitN(out, "--env--", 2)[1]
	for _, name := range scrubbed {
		if strings.Contains(gitEnv, "\n"+name+"=") {
			t.Errorf("git saw %s; want it unset before git runs", name)
		}
	}
	if !strings.Contains(gitEnv, "\nGIT_TERMINAL_PROMPT=0\n") {
		t.Errorf("git env lacks GIT_TERMINAL_PROMPT=0:\n%s", gitEnv)
	}
}

// TestParseBrokeredGitArgs_RefusesWorktreeOverrides: before the subcommand,
// only `-c <key=value>` and `-C <dir>` with the cleaned dir an absolute path
// strictly below /tmp/ are accepted; any other leading option, a -C anywhere
// else, and a call with no subcommand are refused. Everything after the
// subcommand is the subcommand's, passed as given.
func TestParseBrokeredGitArgs_RefusesWorktreeOverrides(t *testing.T) {
	t.Parallel()
	refused := [][]string{
		{"-C", "/", "status"},
		{"-C", "/var/www", "status"},
		{"-C", "/tmp", "status"},
		{"-C", "/tmp/", "status"},
		{"-C", "/tmp/../var/www", "status"},
		{"-C", "/tmp/wt-b/../../var/www", "status"},
		{"-C", "tmp/wt-b", "status"},
		{"-C", "wt-b", "status"},
		{"-C", "", "status"},
		{"-C"},
		{"-c"},
		{"-c", "", "status"},
		{"--git-dir=/var/www/.git", "status"},
		{"--git-dir", "/var/www/.git", "status"},
		{"--work-tree=/var/www", "status"},
		{"--work-tree", "/var/www", "status"},
		{"--exec-path=/tmp/x", "status"},
		{"--namespace=x", "status"},
		{"-p", "log"},
		{"--version"},
		{"--config-env=core.sshCommand=X", "fetch"},
		{"-C/tmp/wt-b", "status"},
		{},
		{""},
		{"-C", "/tmp/wt-b"},
		{"-c", "user.name=x"},
	}
	for _, args := range refused {
		if got, err := ParseBrokeredGitArgs(args); err == nil {
			t.Errorf("ParseBrokeredGitArgs(%q) = %+v, want refused", args, got)
		}
	}

	accepted := []struct {
		args []string
		want BrokeredGitArgs
	}{
		{[]string{"status"}, BrokeredGitArgs{Subcommand: "status"}},
		{[]string{"-C", "/tmp/wt-b", "log", "-1"}, BrokeredGitArgs{Dir: "/tmp/wt-b", Subcommand: "log", Rest: []string{"-1"}}},
		{[]string{"-C", "/tmp/wt-b/", "status"}, BrokeredGitArgs{Dir: "/tmp/wt-b", Subcommand: "status"}},
		{[]string{"-c", "user.name=x", "commit", "-F", "-"}, BrokeredGitArgs{Config: []string{"user.name=x"}, Subcommand: "commit", Rest: []string{"-F", "-"}}},
		{[]string{"worktree", "add", "--detach", "/tmp/wt-tracer", "HEAD"}, BrokeredGitArgs{Subcommand: "worktree", Rest: []string{"add", "--detach", "/tmp/wt-tracer", "HEAD"}}},
		{[]string{"log", "-C", "/", "--git-dir=x"}, BrokeredGitArgs{Subcommand: "log", Rest: []string{"-C", "/", "--git-dir=x"}}},
	}
	for _, tt := range accepted {
		got, err := ParseBrokeredGitArgs(tt.args)
		if err != nil {
			t.Errorf("ParseBrokeredGitArgs(%q) refused: %v", tt.args, err)
			continue
		}
		if got.Dir != tt.want.Dir || got.Subcommand != tt.want.Subcommand ||
			!slices.Equal(got.Config, tt.want.Config) || !slices.Equal(got.Rest, tt.want.Rest) {
			t.Errorf("ParseBrokeredGitArgs(%q) = %+v, want %+v", tt.args, got, tt.want)
		}
	}
}
