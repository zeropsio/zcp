package ops

import (
	"strings"
	"testing"
)

// TestBuildGitAuthProbeCommand_Shape pins the load-bearing properties
// of the probe command body:
//   - GIT_TOKEN inlined via export (probe runs before container env is set)
//   - .netrc trap-cleaned on exit (no token on disk after probe)
//   - GIT_TERMINAL_PROMPT=0 (no hang on credential prompt — would freeze MCP)
//   - umask 077 + chmod 600 (.netrc world-unreadable)
//   - git ls-remote HEAD only (read-only probe; no mutation)
func TestBuildGitAuthProbeCommand_Shape(t *testing.T) {
	t.Parallel()
	cmd := BuildGitAuthProbeCommand("https://github.com/example/app.git", "ghp_secret")

	requirements := []struct {
		name, substr string
	}{
		{"inline token via export", "export GIT_TOKEN="},
		{"trap cleanup", "trap 'rm -f ~/.netrc' EXIT"},
		{"umask before write", "umask 077"},
		{"chmod 600", "chmod 600 ~/.netrc"},
		{"netrc references env-var (not literal)", "password $GIT_TOKEN"},
		{"prompt disabled", "GIT_TERMINAL_PROMPT=0"},
		{"read-only probe", "git ls-remote"},
		{"head ref only", "HEAD"},
		{"host extracted", "machine github.com"},
	}
	for _, req := range requirements {
		if !strings.Contains(cmd, req.substr) {
			t.Errorf("probe command missing %s (substr %q):\n%s", req.name, req.substr, cmd)
		}
	}
}

// TestBuildGitAuthProbeCommand_TokenShellQuoted ensures shell-metacharacters
// in the token don't break the command. POSIX single-quote escaping rewrites
// embedded apostrophes as `'\”` and wraps the rest in single quotes — the
// result MUST start with `export GIT_TOKEN='` (begin-quote) followed by the
// rewritten content. Verified by separating shellQuote-of-token from the
// rest of the command and matching both halves.
func TestBuildGitAuthProbeCommand_TokenShellQuoted(t *testing.T) {
	t.Parallel()
	maliciousToken := `tok' && rm -rf / && echo 'oops`
	cmd := BuildGitAuthProbeCommand("https://github.com/example/app.git", maliciousToken)

	wantPrefix := "export GIT_TOKEN=" + shellQuote(maliciousToken) + " &&"
	if !strings.HasPrefix(cmd, wantPrefix) {
		t.Errorf("probe command should start with quoted export of token, got prefix:\n%s",
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
func TestBuildGitOriginSyncCommand_Shape(t *testing.T) {
	t.Parallel()
	cmd := BuildGitOriginSyncCommand("/var/www", "https://github.com/example/app.git")

	if !strings.Contains(cmd, "cd /var/www") {
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
}
