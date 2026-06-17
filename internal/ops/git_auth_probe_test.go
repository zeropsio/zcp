package ops

import (
	"strings"
	"testing"
)

// TestBuildGitWritePushProbeCommand_Shape pins the load-bearing properties of
// the WRITE-auth probe (#2 fix). The proof must MATCH the claim: a passing probe
// authorizes stamping `configured` + writing the secret, so it must prove PUSH
// capability, not mere read reachability (which passes for any token on a public
// repo):
//   - cd into the working dir (push --dry-run needs the local HEAD)
//   - CANDIDATE token via env-assignment prefix (probe-first)
//   - the SAME inline credential helper + reset the push uses
//   - GIT_TERMINAL_PROMPT=0 (no hang on credential prompt)
//   - push --dry-run to the throwaway probe branch (write-class, non-mutating)
//   - unborn-HEAD fallback to ls-remote (can't prove write without a commit)
//   - NO disk writes: the ephemeral-.netrc pattern stays retired
func TestBuildGitWritePushProbeCommand_Shape(t *testing.T) {
	t.Parallel()
	cmd := BuildGitWritePushProbeCommand("/var/www", "https://github.com/example/app.git", "ghp_secret")

	requirements := []struct {
		name, substr string
	}{
		{"cd working dir", "cd '/var/www'"},
		{"candidate token env-prefix", "GIT_TOKEN='ghp_secret' "},
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

// TestBuildGitWritePushProbeCommand_TokenShellQuoted ensures shell-metacharacters
// in the token can't break out of the command (POSIX single-quote escaping).
func TestBuildGitWritePushProbeCommand_TokenShellQuoted(t *testing.T) {
	t.Parallel()
	maliciousToken := `tok' && rm -rf / && echo 'oops`
	cmd := BuildGitWritePushProbeCommand("/var/www", "https://github.com/example/app.git", maliciousToken)

	// The quoted token must appear (both branches carry it) and the raw
	// metacharacter sequence must never appear unquoted.
	if !strings.Contains(cmd, "GIT_TOKEN="+shellQuote(maliciousToken)+" ") {
		t.Errorf("probe command should carry the quoted env-assignment of the token:\n%s", cmd)
	}
	quoted := shellQuote(maliciousToken)
	if !strings.HasPrefix(quoted, "'") || !strings.HasSuffix(quoted, "'") {
		t.Errorf("shellQuote should wrap content in single quotes; got %q", quoted)
	}
}

// TestBuildGitOriginSyncCommand_Shape pins the idempotent remote-set
// pattern: `add 2>/dev/null || set-url` works whether origin exists or not.
// Origin sync is also the single assertion owner for the persistent
// url-scoped credential helper + the one-way stray-.netrc cleanup.
func TestBuildGitOriginSyncCommand_Shape(t *testing.T) {
	t.Parallel()
	cmd := BuildGitOriginSyncCommand("/var/www", "https://github.com/example/app.git")

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

// TestBuildGitShallowFixCommand_Shape pins the F1b shallow-clone guard: detect
// .git/shallow, attempt `git fetch --unshallow` from the CURRENT origin, and
// echo a dispatch token the handler keys on. Must run before origin is
// rewritten (the unshallow source is the original origin).
func TestBuildGitShallowFixCommand_Shape(t *testing.T) {
	t.Parallel()
	cmd := BuildGitShallowFixCommand("/var/www", "ghp_secret")

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
	// Token must be shell-quoted, never bare.
	if strings.Contains(cmd, "GIT_TOKEN=ghp_secret ") {
		t.Errorf("token must be shell-quoted, not bare: %s", cmd)
	}
}
