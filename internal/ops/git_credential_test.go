package ops

import (
	"strings"
	"testing"
)

// TestGitCredentialHelper_Shape pins the inline credential helper that
// replaced the ephemeral-.netrc pattern (spec-git-delivery-target §4):
//   - answers only the credential protocol's "get" action
//   - emits username=oauth2 + password from $GIT_TOKEN of the INVOKING
//     session env (live per fresh SSH session — rotation needs no restart)
//   - never writes a file, never traps, never embeds a token literal
func TestGitCredentialHelper_Shape(t *testing.T) {
	t.Parallel()

	for _, req := range []struct{ name, substr string }{
		{"shell-command helper form", "!f()"},
		{"get action guarded", `test "$1" = get`},
		{"oauth2 username", "echo username=oauth2"},
		{"password from session env", `password=$GIT_TOKEN`},
	} {
		if !strings.Contains(gitCredentialHelperShell, req.substr) {
			t.Errorf("helper missing %s (substr %q):\n%s", req.name, req.substr, gitCredentialHelperShell)
		}
	}
	for _, forbidden := range []string{"netrc", "trap", "umask"} {
		if strings.Contains(gitCredentialHelperShell, forbidden) {
			t.Errorf("helper must not reference %q:\n%s", forbidden, gitCredentialHelperShell)
		}
	}
}

// TestGitCredentialHelperArgs_ResetsConfiguredHelpers pins the `-c
// credential.helper=` reset before the inline helper: a stale or foreign
// configured helper must never answer ahead of the one this invocation
// carries.
func TestGitCredentialHelperArgs_ResetsConfiguredHelpers(t *testing.T) {
	t.Parallel()

	args := gitCredentialHelperArgs()
	resetIdx := strings.Index(args, "-c credential.helper= ")
	helperIdx := strings.Index(args, "-c credential.helper='")
	if resetIdx == -1 {
		t.Fatalf("helper args missing empty reset: %s", args)
	}
	if helperIdx == -1 || helperIdx < resetIdx {
		t.Fatalf("inline helper must follow the reset: %s", args)
	}
}

// TestBuildGitAuthedLsRemoteCommand_Shape pins the single authenticated
// remote-HEAD read shared by the launch push-proof (container) — the
// consolidation the 2026-05-28 audit ordered (one primitive, no inline
// duplicates in tools/).
func TestBuildGitAuthedLsRemoteCommand_Shape(t *testing.T) {
	t.Parallel()

	cmd := BuildGitAuthedLsRemoteCommand("https://github.com/example/app")
	for _, req := range []struct{ name, substr string }{
		{"prompt disabled", "GIT_TERMINAL_PROMPT=0"},
		{"inline helper", "-c credential.helper='!f()"},
		{"read-only probe", "git"},
		{"ls-remote HEAD", "ls-remote 'https://github.com/example/app' HEAD"},
		{"first SHA column", "head -1 | cut -f1"},
		{"state-not-transport tolerance", "|| true"},
	} {
		if !strings.Contains(cmd, req.substr) {
			t.Errorf("authed ls-remote missing %s (substr %q):\n%s", req.name, req.substr, cmd)
		}
	}
	for _, forbidden := range []string{"netrc", "machine ", "trap"} {
		if strings.Contains(cmd, forbidden) {
			t.Errorf("authed ls-remote must not use the retired .netrc pattern (%q):\n%s", forbidden, cmd)
		}
	}
}

// TestGitCredentialHelperConfigFragment_URLScoped pins the persistent
// helper written by origin sync: url-scoped (credential.<url>.helper —
// a global helper would answer for ANY https host, parity with the old
// host-scoped `machine` line), plus the one-time stray-.netrc cleanup
// (migration off the ephemeral-.netrc era; the fail-open residue class).
func TestGitCredentialHelperConfigFragment_URLScoped(t *testing.T) {
	t.Parallel()

	frag := gitCredentialHelperConfigFragment("https://gitlab.com/team/app.git")
	if !strings.Contains(frag, "git config 'credential.https://gitlab.com.helper'") {
		t.Errorf("config fragment must be url-scoped to the remote's host:\n%s", frag)
	}
	if !strings.Contains(frag, "rm -f ~/.netrc") {
		t.Errorf("config fragment must clean up stray legacy .netrc:\n%s", frag)
	}
	if !strings.Contains(frag, `password=$GIT_TOKEN`) {
		t.Errorf("config fragment must carry the session-env helper:\n%s", frag)
	}
}
