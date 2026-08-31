package ops

import (
	"strings"
	"testing"
)

// TestGitCommandBuilders_QuoteDynamicInputs pins B3: the SSH git-command
// builders must shell-quote their agent-supplied operands (workingDir,
// branch) and reject invalid hosts, so a value with spaces / quotes / shell
// metacharacters cannot splice the compound command. A crafted host can never
// reach the double-quoted .netrc echo (where $/backtick are live).
func TestGitCommandBuilders_QuoteDynamicInputs(t *testing.T) {
	t.Parallel()

	const evilWorkdir = "/var/www; danger ~"
	const evilBranch = "feat/x; whoami"

	cmd := BuildGitPushCommand(evilWorkdir, "https://github.com/o/r", evilBranch)
	if strings.Contains(cmd, "cd "+evilWorkdir) {
		t.Errorf("workingDir not quoted — injection surface:\n%s", cmd)
	}
	if strings.Contains(cmd, "origin "+evilBranch) {
		t.Errorf("branch not quoted — injection surface:\n%s", cmd)
	}
	if !strings.Contains(cmd, "'"+evilWorkdir+"'") {
		t.Errorf("workingDir should be single-quoted:\n%s", cmd)
	}

	// A crafted host (with $/backtick) must be rejected → default host in the
	// url-scoped credential config key the origin-sync builder emits. (The
	// push builder itself no longer interpolates a host anywhere — auth is
	// the inline helper; the key is also shell-quoted, so this is defense in
	// depth + scoping correctness, not the only barrier.)
	syncEvil := BuildGitOriginSyncCommand("/var/www", "https://ho$(whoami)st/o/r")
	if !strings.Contains(syncEvil, "'credential.https://github.com.helper'") {
		t.Errorf("invalid host should fall back to the default host in the credential config key:\n%s", syncEvil)
	}

	// Origin-sync builder quotes workingDir too.
	sync := BuildGitOriginSyncCommand("/var/www foo", "https://github.com/o/r")
	if !strings.Contains(sync, "'/var/www foo'") {
		t.Errorf("origin-sync workingDir should be single-quoted:\n%s", sync)
	}
}

// TestParseGitHost_RejectsInvalid pins that parseGitHost only accepts
// syntactically-valid hostnames and defaults otherwise.
func TestParseGitHost_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"https://github.com/o/r":   "github.com",
		"https://gitlab.example/o": "gitlab.example",
		"https://ho$(x)st/o/r":     defaultGitHost, // metachar host rejected
		"https://ho`x`st/o/r":      defaultGitHost,
		"":                         defaultGitHost,
	}
	for in, want := range cases {
		if got := parseGitHost(in); got != want {
			t.Errorf("parseGitHost(%q) = %q, want %q", in, got, want)
		}
	}
}
