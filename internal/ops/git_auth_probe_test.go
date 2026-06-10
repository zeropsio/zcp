package ops

import (
	"strings"
	"testing"
)

// TestBuildGitAuthProbeCommand_Shape pins the load-bearing properties
// of the probe command body (spec-git-delivery-target §4):
//   - CANDIDATE token via env-assignment prefix (probe-first: the probe
//     runs before the token is written anywhere, so it cannot rely on the
//     session env the configured flows use)
//   - auth via the SAME inline credential helper the push uses — probe
//     and push share identical auth semantics
//   - GIT_TERMINAL_PROMPT=0 (no hang on credential prompt — would freeze MCP)
//   - git ls-remote HEAD only (read-only probe; no mutation)
//   - NO disk writes: the ephemeral-.netrc pattern is retired
func TestBuildGitAuthProbeCommand_Shape(t *testing.T) {
	t.Parallel()
	cmd := BuildGitAuthProbeCommand("https://github.com/example/app.git", "ghp_secret")

	requirements := []struct {
		name, substr string
	}{
		{"candidate token env-prefix", "GIT_TOKEN='ghp_secret' "},
		{"inline credential helper", "-c credential.helper='!f()"},
		{"helper reset precedes inline", "-c credential.helper= -c credential.helper="},
		{"helper reads env", `password=$GIT_TOKEN`},
		{"prompt disabled", "GIT_TERMINAL_PROMPT=0"},
		{"read-only probe", "ls-remote"},
		{"head ref only", "HEAD"},
	}
	for _, req := range requirements {
		if !strings.Contains(cmd, req.substr) {
			t.Errorf("probe command missing %s (substr %q):\n%s", req.name, req.substr, cmd)
		}
	}
	for _, forbidden := range []string{"~/.netrc", "machine ", "trap", "umask", "export "} {
		if strings.Contains(cmd, forbidden) {
			t.Errorf("probe command must not carry the retired pattern (%q):\n%s", forbidden, cmd)
		}
	}
}

// TestBuildGitAuthProbeCommand_TokenShellQuoted ensures shell-metacharacters
// in the token don't break the command. POSIX single-quote escaping rewrites
// embedded apostrophes and wraps the rest in single quotes — the result MUST
// start with the quoted env-assignment prefix `GIT_TOKEN='…'` followed by
// the rest of the command.
func TestBuildGitAuthProbeCommand_TokenShellQuoted(t *testing.T) {
	t.Parallel()
	maliciousToken := `tok' && rm -rf / && echo 'oops`
	cmd := BuildGitAuthProbeCommand("https://github.com/example/app.git", maliciousToken)

	wantPrefix := "GIT_TOKEN=" + shellQuote(maliciousToken) + " GIT_TERMINAL_PROMPT=0"
	if !strings.HasPrefix(cmd, wantPrefix) {
		t.Errorf("probe command should start with quoted env-assignment of token, got prefix:\n%s",
			cmd[:min(len(cmd), 200)])
	}
	// shellQuote always begins + ends with single quotes when escaping is
	// required — never leaves shell metacharacters bare.
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
}
