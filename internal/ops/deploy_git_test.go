// Tests for: ops/deploy.go — Git/command helper tests.
package ops

import (
	"testing"

	"github.com/zeropsio/zcp/internal/auth"
)

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && contains(s, sub))
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestBuildSSHCommand_GitGuard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		authInfo   auth.Info
		serviceID  string
		workDir    string
		includeGit bool
		wantParts  []string
		wantAbsent []string
	}{
		{
			name:      "basic command with split init/identity-ensure and no auto-commit",
			authInfo:  testAuthInfo(),
			serviceID: "svc-123",
			workDir:   "/var/www",
			wantParts: []string{
				// gitInit only fires when .git/ is missing (cold path /
				// migration). Identity ensure is top-level and set-if-
				// absent — runs always, but never stomps a value already
				// there — so a pre-existing .git/ from a buildFromGit
				// clone (B13) still gets a canonical identity filled when
				// none exists (P1). The HEAD guarantee follows, so zcli's
				// archiver always has a commit to diff against (P2) —
				// there is no `git add` / `git commit` in this command at
				// all; the dirty tree ships via zcli's own ephemeral
				// stash-archive.
				"(test -d .git || git init -q -b main)",
				`(test -n "$(git config user.email)" || git config user.email 'agent@zerops.io') && (test -n "$(git config user.name)" || git config user.name 'Zerops Agent')`,
				`(git rev-parse -q --verify HEAD >/dev/null || git update-ref HEAD "$(git -c user.email='agent@zerops.io' -c user.name='Zerops Agent' commit-tree "$(git mktree </dev/null)" -m 'zcp init')")`,
				"zcli push --service-id svc-123",
			},
			wantAbsent: []string{
				"rm -rf .git",
				"git remote",
				".gitignore",
				// Identity ensure nested inside the gitInit OR branch
				// reverts the B13 fix — the buildFromGit case (.git/
				// exists from upstream clone, but no identity) would
				// short-circuit and fail with `fatal: unable to
				// auto-detect email address`.
				"(git init -q -b main && git config user.email",
				// P2: never stage or commit on a direct deploy.
				"git add -A",
				"git commit -q -m 'deploy'",
			},
		},
		{
			name: "with different region",
			authInfo: auth.Info{
				Token:   "my-token",
				APIHost: "api.app-fra1.zerops.io",
				Region:  "fra1",
			},
			serviceID: "svc-789",
			workDir:   "/var/www",
			wantParts: []string{
				"zcli login -- 'my-token'",
				"(test -d .git || git init -q -b main)",
				"git config user.email 'agent@zerops.io'",
				"zcli push --service-id svc-789",
			},
		},
		{
			name:       "with includeGit flag",
			authInfo:   testAuthInfo(),
			serviceID:  "svc-123",
			workDir:    "/var/www",
			includeGit: true,
			wantParts: []string{
				"zcli push --service-id svc-123 -g",
			},
		},
		{
			name: "uses hardcoded deploy identity regardless of auth info",
			authInfo: auth.Info{
				Token:    "my-token",
				APIHost:  "api.app-prg1.zerops.io",
				Region:   "prg1",
				Email:    "deploy@company.io",
				FullName: "Deploy Bot",
			},
			serviceID: "svc-100",
			workDir:   "/var/www",
			wantParts: []string{
				"git config user.email 'agent@zerops.io'",
				"git config user.name 'Zerops Agent'",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := buildSSHCommand(tt.authInfo, tt.serviceID, tt.workDir, "", tt.includeGit)

			for _, part := range tt.wantParts {
				if !contains(cmd, part) {
					t.Errorf("command missing %q\ngot: %s", part, cmd)
				}
			}
			for _, absent := range tt.wantAbsent {
				if contains(cmd, absent) {
					t.Errorf("command should NOT contain %q\ngot: %s", absent, cmd)
				}
			}
		})
	}
}

func TestBuildSSHCommand_FreshInit_BranchMain(t *testing.T) {
	t.Parallel()

	cmd := buildSSHCommand(testAuthInfo(), "svc-1", "/var/www", "", false)

	if !contains(cmd, "git init -q -b main") {
		t.Errorf("fresh init must use -b main\ngot: %s", cmd)
	}
}

// TestBuildSSHCommand_NoAutoCommit_HeadGuardPresent is the P2 shape pin:
// no `git add` / `git commit` anywhere in the emitted command (a direct
// deploy is an artifact operation — it must never mint a user-visible
// commit), the HEAD-guard fragment is present so zcli's archiver always
// has a commit to diff against, and the guard's marker commit carries the
// robot identity inline via `-c` (never persisted config, regardless of
// what the identity-ensure fragment did or didn't write).
func TestBuildSSHCommand_NoAutoCommit_HeadGuardPresent(t *testing.T) {
	t.Parallel()

	cmd := buildSSHCommand(testAuthInfo(), "svc-1", "/var/www", "", false)

	for _, forbidden := range []string{"git add -A", "git add ", "-m 'deploy'", "git commit"} {
		if contains(cmd, forbidden) {
			t.Errorf("command must NOT contain %q — direct deploy never commits\ngot: %s", forbidden, cmd)
		}
	}
	for _, want := range []string{
		"git rev-parse -q --verify HEAD",
		"git update-ref HEAD",
		"commit-tree",
		"git mktree </dev/null",
		"-c user.email='agent@zerops.io' -c user.name='Zerops Agent'",
		"-m 'zcp init'",
	} {
		if !contains(cmd, want) {
			t.Errorf("command missing HEAD-guard fragment piece %q\ngot: %s", want, cmd)
		}
	}
}

func TestBuildSSHCommand_PreservesRemoteAndGitignore(t *testing.T) {
	t.Parallel()

	cmd := buildSSHCommand(testAuthInfo(), "svc-1", "/var/www", "", false)

	unwanted := []string{"git remote", ".gitignore"}
	for _, s := range unwanted {
		if contains(cmd, s) {
			t.Errorf("command must NOT contain %q\ngot: %s", s, cmd)
		}
	}
}
