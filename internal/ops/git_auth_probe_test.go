package ops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildGitWritePushProbeCommand_Shape pins the load-bearing properties of
// the WRITE-auth probe (#2 fix). The proof must MATCH the claim: a passing probe
// authorizes stamping `configured` + writing the secret, so it must prove PUSH
// capability, not mere read reachability (which passes for any token on a public
// repo):
//   - cd into the working dir (push --dry-run needs the local HEAD)
//   - CANDIDATE token read from stdin, given to git alone as GIT_TOKEN
//     (probe-first; never on the command line)
//   - the SAME inline credential helper + reset the push uses
//   - GIT_TERMINAL_PROMPT=0 (no hang on credential prompt)
//   - push --dry-run to the throwaway probe branch (write-class, non-mutating)
//   - unborn-HEAD fallback to ls-remote (can't prove write without a commit)
//   - NO disk writes: the ephemeral-.netrc pattern stays retired
func TestBuildGitWritePushProbeCommand_Shape(t *testing.T) {
	t.Parallel()
	cmd := BuildGitWritePushProbeCommand("/var/www", "https://github.com/example/app.git", "")

	requirements := []struct {
		name, substr string
	}{
		{"cd working dir", "cd '/var/www'"},
		{"candidate token read from stdin", "IFS= read -r zcp_git_token"},
		{"candidate token env-prefix from the variable", `GIT_TOKEN="$zcp_git_token" `},
		{"inline credential helper", "-c credential.helper='!f()"},
		{"helper reset precedes inline", "-c credential.helper= -c credential.helper="},
		{"helper reads env", `password=$GIT_TOKEN`},
		{"prompt disabled", "GIT_TERMINAL_PROMPT=0"},
		{"write-class probe (push --dry-run)", "push --dry-run"},
		{"throwaway probe branch", "HEAD:refs/heads/" + gitWriteAuthProbeBranch},
		{"unborn-HEAD guard", "git rev-parse --verify -q HEAD"},
		{"unborn-HEAD read fallback", "ls-remote"},
	}
	for _, req := range requirements {
		if !strings.Contains(cmd, req.substr) {
			t.Errorf("write-probe command missing %s (substr %q):\n%s", req.name, req.substr, cmd)
		}
	}
	for _, forbidden := range []string{"~/.netrc", "machine ", "trap", "umask", "export "} {
		if strings.Contains(cmd, forbidden) {
			t.Errorf("write-probe command must not carry the retired pattern (%q):\n%s", forbidden, cmd)
		}
	}
}

// TestBuildGitWritePushProbeCommand_UserByRemote: HQ's git reads the user
// `mate` with the Mate credential, so a probe of a repository on the Mate's
// HQ presents that user; any other remote keeps the helper's usual one.
func TestBuildGitWritePushProbeCommand_UserByRemote(t *testing.T) {
	t.Parallel()
	const hq = "https://hq.example"
	for _, tt := range []struct {
		remote, hqURL, user string
	}{
		{hq + "/git/a1/appdev.git", hq, "username=mate"},
		{"https://hq.example:443/git/a1/appdev.git", hq, "username=mate"},
		{"https://github.com/example/app.git", hq, "username=oauth2"},
		{hq + "/git/a1/appdev.git", "", "username=oauth2"},
		{"https://hq.example:8443/git/a1/appdev.git", hq, "username=oauth2"},
	} {
		cmd := BuildGitWritePushProbeCommand("/var/www", tt.remote, tt.hqURL)
		if !strings.Contains(cmd, tt.user) {
			t.Errorf("probe of %s (HQ %q) must present %s:\n%s", tt.remote, tt.hqURL, tt.user, cmd)
		}
		if session := BuildGitSessionAuthProbeCommand(tt.remote, tt.hqURL); !strings.Contains(session, tt.user) {
			t.Errorf("session probe of %s (HQ %q) must present %s:\n%s", tt.remote, tt.hqURL, tt.user, session)
		}
	}
}

// TestGitAuthProbeCommands_TokenFromStdinOnly: the write probe and the
// shallow-clone fix never carry the candidate token — a command line is
// visible to every process on both containers — and read it from the
// session's stdin (GitTokenInput), so git gets it byte for byte, shell
// metacharacters and all, with nothing evaluated.
func TestGitAuthProbeCommands_TokenFromStdinOnly(t *testing.T) {
	t.Parallel()
	const token = `tok' && touch pwned && echo 'oops $(touch pwned2) "q" \x`
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "work", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "work", ".git", "shallow"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// git as the commands call it: rev-parse finds HEAD, remote get-url names
	// an origin, and the calls that authenticate print the GIT_TOKEN they got.
	stub := filepath.Join(dir, "bin")
	if err := os.MkdirAll(stub, 0o755); err != nil {
		t.Fatal(err)
	}
	gitStub := "#!/bin/sh\ncase \" $* \" in *' push '*|*' fetch '*) printf 'GOT[%s]\\n' \"$GIT_TOKEN\" >> ../got;; esac\nexit 0\n"
	if err := os.WriteFile(filepath.Join(stub, "git"), []byte(gitStub), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, cmd := range map[string]string{
		"write probe":       BuildGitWritePushProbeCommand(filepath.Join(dir, "work"), "https://github.com/example/app.git", ""),
		"shallow-clone fix": BuildGitShallowFixCommand(filepath.Join(dir, "work")),
	} {
		if strings.Contains(cmd, "tok'") || strings.Contains(cmd, "pwned") {
			t.Errorf("%s carries the token:\n%s", name, cmd)
		}
		_ = os.Remove(filepath.Join(dir, "got"))
		run := exec.CommandContext(context.Background(), "sh", "-c", cmd)
		run.Env = []string{"PATH=" + stub + string(os.PathListSeparator) + os.Getenv("PATH")}
		run.Stdin = strings.NewReader(string(GitTokenInput(token)))
		if out, err := run.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
		got, err := os.ReadFile(filepath.Join(dir, "got"))
		if err != nil {
			t.Fatalf("%s: git was never asked to authenticate: %v", name, err)
		}
		if want := "GOT[" + token + "]\n"; string(got) != want {
			t.Errorf("%s: git got GIT_TOKEN %q, want the token as given %q", name, got, want)
		}
		for _, mark := range []string{"pwned", "pwned2"} {
			if _, err := os.Stat(filepath.Join(dir, "work", mark)); err == nil {
				t.Errorf("%s evaluated the token (%s exists)", name, mark)
			}
		}
	}
}

// TestBuildGitOriginSyncCommand_Shape pins the idempotent remote-set
// pattern: `add 2>/dev/null || set-url` works whether origin exists or not.
// Origin sync is also the single assertion owner for the persistent
// url-scoped credential helper + the one-way stray-.netrc cleanup.
func TestBuildGitOriginSyncCommand_Shape(t *testing.T) {
	t.Parallel()
	cmd := BuildGitOriginSyncCommand("/var/www", "https://github.com/example/app.git", "")

	if !strings.Contains(cmd, "cd '/var/www'") {
		t.Errorf("origin sync should cd to workingDir: %s", cmd)
	}
	if !strings.Contains(cmd, "git remote add origin") {
		t.Errorf("origin sync should attempt add: %s", cmd)
	}
	if !strings.Contains(cmd, "git remote set-url origin") {
		t.Errorf("origin sync should fall back to set-url: %s", cmd)
	}
	if !strings.Contains(cmd, "2>/dev/null") {
		t.Errorf("origin sync should suppress add's stderr (idempotent): %s", cmd)
	}
	// GAP4-1: must self-heal a missing .git the same way the deploy path
	// does — git-push-setup runs before any deploy, on a /var/www that may
	// not yet be a git repo.
	if !strings.Contains(cmd, "test -d .git || git init -q -b main") {
		t.Errorf("origin sync must guard .git init (GAP4-1): %s", cmd)
	}
	// Credential-helper assertion: url-scoped persistent helper for git
	// invocations outside ZCP's own commands (manual git on the container),
	// plus stray legacy-.netrc cleanup.
	if !strings.Contains(cmd, "git config 'credential.https://github.com.helper'") {
		t.Errorf("origin sync must assert the url-scoped credential helper: %s", cmd)
	}
	if !strings.Contains(cmd, "rm -f ~/.netrc") {
		t.Errorf("origin sync must clean up stray legacy .netrc: %s", cmd)
	}
	// F1b non-destructive: a pre-existing origin (e.g. a recipe source remote)
	// is preserved under zerops-original-origin BEFORE origin is overwritten,
	// so the only `--unshallow` source is never silently discarded.
	if !strings.Contains(cmd, "git remote add zerops-original-origin") {
		t.Errorf("origin sync must back up a pre-existing origin (F1b): %s", cmd)
	}
}

// TestBuildGitOriginSyncCommand_SetsIdentityIfAbsent pins F1 site 4: a
// git-push-setup flow that inits a repo via the GAP4-1 guard (no prior
// deploy ever ran) must also fill identity, or the flow's first commit
// (the user's, over SSH) fails with "unable to auto-detect email address".
// Must be the SAME set-if-absent shape every other site uses (single
// owner — gitIdentityEnsureFragment), placed right after the init guard
// and before origin is touched.
func TestBuildGitOriginSyncCommand_SetsIdentityIfAbsent(t *testing.T) {
	t.Parallel()
	cmd := BuildGitOriginSyncCommand("/var/www", "https://github.com/example/app.git", "")

	if !strings.Contains(cmd, `(test -n "$(git config user.email)" || git config user.email 'agent@zerops.io') && (test -n "$(git config user.name)" || git config user.name 'Zerops Agent')`) {
		t.Errorf("origin sync must fill identity if absent (single-owner ensure fragment): %s", cmd)
	}
	initIdx := strings.Index(cmd, "(test -d .git || git init -q -b main)")
	identityIdx := strings.Index(cmd, `test -n "$(git config user.email)"`)
	originIdx := strings.Index(cmd, "git remote add origin")
	if initIdx < 0 || identityIdx < 0 || originIdx < 0 {
		t.Fatalf("command missing init/identity/origin pieces: %s", cmd)
	}
	if initIdx >= identityIdx || identityIdx >= originIdx {
		t.Errorf("identity ensure must run after the init guard and before origin sync: init=%d identity=%d origin=%d\n%s",
			initIdx, identityIdx, originIdx, cmd)
	}
}

// TestBuildGitShallowFixCommand_Shape pins the F1b shallow-clone guard: detect
// .git/shallow, attempt `git fetch --unshallow` from the CURRENT origin, and
// echo a dispatch token the handler keys on. Must run before origin is
// rewritten (the unshallow source is the original origin).
func TestBuildGitShallowFixCommand_Shape(t *testing.T) {
	t.Parallel()
	cmd := BuildGitShallowFixCommand("/var/www")

	for _, want := range []string{
		"cd '/var/www'",
		"-f .git/shallow",
		"fetch --unshallow origin",
		"ZCP_UNSHALLOW_OK",
		"ZCP_UNSHALLOW_FAIL",
		"ZCP_NOT_SHALLOW",
		"GIT_TERMINAL_PROMPT=0",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("shallow-fix command missing %q: %s", want, cmd)
		}
	}
	// The token comes over stdin, never in the command.
	if !strings.Contains(cmd, "IFS= read -r zcp_git_token") || !strings.Contains(cmd, `GIT_TOKEN="$zcp_git_token" `) {
		t.Errorf("the token must be read from stdin and given to git alone: %s", cmd)
	}
}
