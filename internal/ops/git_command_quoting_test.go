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

	// A crafted host (with $/backtick) must be rejected: the origin-sync
	// builder persists no helper for it at all — never one under the default
	// host, which would answer github.com with this remote's token. (The key
	// is also shell-quoted, so this is scoping correctness plus defense in
	// depth, not the only barrier.)
	syncEvil := BuildGitOriginSyncCommand("/var/www", "https://ho$(whoami)st/o/r", "")
	if strings.Contains(syncEvil, "credential.https://") {
		t.Errorf("an invalid host must get no persisted helper:\n%s", syncEvil)
	}

	// Origin-sync builder quotes workingDir too.
	sync := BuildGitOriginSyncCommand("/var/www foo", "https://github.com/o/r", "")
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
