// Tests for: with.go — `zcp with`: one command gets the named values of the
// container's live env store, a missing key and a hidden name are refused
// before anything runs, and no line of zcp's carries a value
// (docs/spec-mate.md §5.8a, MF-19).

package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The values Fen's container holds: a test sign-in and a key for its tools
// the person saved, beside the key zcp acts with.
const (
	fenTestAccounts = "fen@example.test:pw-of-the-test-account"
	fenToolKey      = "tk-of-the-linter"
	fenZcpKey       = "zk-of-zcp-itself"
)

// fenStoreValues are every value of fenStore: none may reach a line zcp
// prints.
var fenStoreValues = []string{fenTestAccounts, fenToolKey, fenZcpKey}

// fenStore writes the live env store of Fen's container and returns its path.
func fenStore(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "env.json")
	body := `{"TEST_ACCOUNTS":"` + fenTestAccounts + `","TOOL_KEY":"` + fenToolKey + `","ZCP_API_KEY":"` + fenZcpKey + `","PATH":"/store/bin"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write the store: %v", err)
	}
	return path
}

// withRun is what `zcp with` handed over: the command it became, or nothing.
type withRun struct {
	ran  bool
	path string
	argv []string
	env  []string
}

// withTestDeps is `zcp with` on a store at storePath, called from a shell
// whose environment still holds an older TEST_ACCOUNTS; the command it
// becomes exits with exit.
func withTestDeps(storePath string, exit int) (withDeps, *withRun, *strings.Builder) {
	var stderr strings.Builder
	run := &withRun{}
	return withDeps{
		storePath: storePath,
		environ:   []string{"HOME=/home/zerops", "TEST_ACCOUNTS=the-env-this-shell-started-with", "PATH=/usr/bin"},
		lookPath:  func(command string) (string, error) { return "/usr/bin/" + command, nil },
		replace: func(path string, argv, env []string) (int, error) {
			*run = withRun{ran: true, path: path, argv: argv, env: env}
			return exit, nil
		},
		stderr: &stderr,
	}, run, &stderr
}

// The command gets the named values as the store holds them now — over what
// the caller's own environment held under the name — keeps the rest of the
// caller's environment, gets no key that was not named, and its exit code is
// the call's.
// witness: cmd/zcp/with.go | return append(env, key+"="+value) → return env
func TestWith_RunsTheCommandWithTheNamedValues(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"TEST_ACCOUNTS,TOOL_KEY", "--", "sign-in", "--as", "first"},
		{"TEST_ACCOUNTS,TOOL_KEY", "sign-in", "--as", "first"},
	} {
		deps, run, stderr := withTestDeps(fenStore(t), 7)
		code := runWithCmd(args, deps)
		if code != 7 || !run.ran {
			t.Fatalf("zcp with %v: exit %d, ran %v; want the command's own 7\n%s", args, code, run.ran, stderr)
		}
		if run.path != "/usr/bin/sign-in" || !slices.Equal(run.argv, []string{"sign-in", "--as", "first"}) {
			t.Errorf("zcp with %v ran %s %v", args, run.path, run.argv)
		}
		want := []string{"HOME=/home/zerops", "PATH=/usr/bin", "TEST_ACCOUNTS=" + fenTestAccounts, "TOOL_KEY=" + fenToolKey}
		if got := slices.Sorted(slices.Values(run.env)); !slices.Equal(got, want) {
			t.Errorf("zcp with %v: the command's environment is %v, want %v", args, got, want)
		}
		if stderr.Len() != 0 {
			t.Errorf("zcp with %v printed %q", args, stderr)
		}
	}
}

// A key the store does not hold stops the call before the command runs, with
// zcp's own exit code and words that say what to do: wait for the person, or
// ask with mate=true.
// witness: cmd/zcp/with.go | if !ok { → if !ok && key == "" {
func TestWith_AMissingKey_RunsNothingAndSaysWhatToDo(t *testing.T) {
	t.Parallel()

	for _, keys := range []string{"SIGNING_KEY", "TEST_ACCOUNTS,SIGNING_KEY"} {
		deps, run, stderr := withTestDeps(fenStore(t), 0)
		code := runWithCmd([]string{keys, "--", "sign-in"}, deps)
		if code != gitExitRefused || run.ran {
			t.Errorf("zcp with %s: exit %d, ran %v; want refused before the command", keys, code, run.ran)
		}
		for _, want := range []string{"SIGNING_KEY", `zerops_env action="request" mate=true`} {
			if !strings.Contains(stderr.String(), want) {
				t.Errorf("zcp with %s: the refusal lacks %q: %s", keys, want, stderr)
			}
		}
		assertNoStoreValue(t, stderr.String())
	}
}

// A name Mate and zcp keep for themselves is never handed to a command,
// though the store holds it.
// witness: cmd/zcp/with.go | if ops.IsVaultHidden(key) { → if false {
func TestWith_AHiddenName_IsHandedToNoCommand(t *testing.T) {
	t.Parallel()

	for _, keys := range []string{"ZCP_API_KEY", "TEST_ACCOUNTS,ZCP_API_KEY", "MATE_SETUP", "GITEA_TOKEN", "GIT_TOKEN"} {
		deps, run, stderr := withTestDeps(fenStore(t), 0)
		code := runWithCmd([]string{keys, "--", "env"}, deps)
		if code != gitExitRefused || run.ran {
			t.Errorf("zcp with %s: exit %d, ran %v; want refused before the command", keys, code, run.ran)
		}
		assertNoStoreValue(t, stderr.String())
	}
}

// No line zcp prints carries a value: not one typed where the key names
// belong, not the content of a store that does not parse, not on a command
// that is not found or cannot be started.
// witness: cmd/zcp/with.go | if !ops.IsEnvKeyName(key) { → if false {
func TestWith_NoRefusal_PrintsAValue(t *testing.T) {
	t.Parallel()

	const typed = "typed-where-the-name-belongs"
	broken := filepath.Join(t.TempDir(), "env.json")
	if err := os.WriteFile(broken, []byte(`{"TEST_ACCOUNTS":"`+fenTestAccounts+`",`+fenToolKey), 0o600); err != nil {
		t.Fatalf("write the store: %v", err)
	}
	tests := []struct {
		name     string
		args     []string
		store    string
		lookPath func(string) (string, error)
		replace  func(string, []string, []string) (int, error)
		wantCode int
	}{
		{name: "a KEY=value where the names belong", args: []string{"TEST_ACCOUNTS=" + typed, "sign-in"}, wantCode: gitExitRefused},
		{name: "an empty name", args: []string{"TEST_ACCOUNTS,", "sign-in"}, wantCode: gitExitRefused},
		{name: "no key", args: nil, wantCode: gitExitRefused},
		{name: "no command", args: []string{"TEST_ACCOUNTS"}, wantCode: gitExitRefused},
		{name: "no command after the dashes", args: []string{"TEST_ACCOUNTS", "--"}, wantCode: gitExitRefused},
		{name: "no store", args: []string{"TEST_ACCOUNTS", "sign-in"}, store: filepath.Join(t.TempDir(), "absent.json"), wantCode: gitExitRefused},
		{name: "a store that does not parse", args: []string{"TEST_ACCOUNTS", "sign-in"}, store: broken, wantCode: gitExitRefused},
		{name: "a command that is not found", args: []string{"TEST_ACCOUNTS", "sign-in"},
			lookPath: func(string) (string, error) { return "", errors.New("not in PATH") }, wantCode: withExitNotFound},
		{name: "a command that cannot be started", args: []string{"TEST_ACCOUNTS", "sign-in"},
			replace:  func(string, []string, []string) (int, error) { return 0, errors.New("permission denied") },
			wantCode: withExitNotStarted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := tt.store
			if store == "" {
				store = fenStore(t)
			}
			deps, run, stderr := withTestDeps(store, 0)
			if tt.lookPath != nil {
				deps.lookPath = tt.lookPath
			}
			if tt.replace != nil {
				deps.replace = tt.replace
			}
			code := runWithCmd(tt.args, deps)
			if code != tt.wantCode || run.ran {
				t.Errorf("exit %d, ran %v; want %d and nothing run", code, run.ran, tt.wantCode)
			}
			if stderr.Len() == 0 {
				t.Error("the call ended without a line saying why")
			}
			if strings.Contains(stderr.String(), typed) {
				t.Errorf("the line carries what was typed where the names belong: %s", stderr)
			}
			assertNoStoreValue(t, stderr.String())
		})
	}
}

// assertNoStoreValue fails when text carries a value of fenStore.
func assertNoStoreValue(t *testing.T, text string) {
	t.Helper()
	for _, value := range fenStoreValues {
		if strings.Contains(text, value) {
			t.Errorf("the line carries a value of the store: %s", text)
		}
	}
}
