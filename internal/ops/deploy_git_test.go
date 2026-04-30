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
			name:      "basic command with split init/config and always-commit",
			authInfo:  testAuthInfo(),
			serviceID: "svc-123",
			workDir:   "/var/www",
			wantParts: []string{
				// gitInit only fires when .git/ is missing (cold path /
				// migration). gitConfig is top-level — runs always — so a
				// pre-existing .git/ from buildFromGit clone (B13) still
				// gets the canonical Zerops Agent identity written before
				// the downstream `git add -A && git commit`.
				"(test -d .git || git init -q -b main)",
				"git config user.email 'agent@zerops.io' && git config user.name 'Zerops Agent'",
				"git add -A",
				"git diff-index --quiet HEAD 2>/dev/null || git commit -q -m 'deploy'",
				"zcli push --service-id svc-123",
			},
			wantAbsent: []string{
				"rm -rf .git",
				"git remote",
				".gitignore",
				// gitConfig nested inside the gitInit OR branch reverts
				// the B13 fix — the buildFromGit case (.git/ exists from
				// upstream clone, but no identity) would short-circuit and
				// fail with `fatal: unable to auto-detect email address`.
				"(git init -q -b main && git config user.email",
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

// Identity no longer varies by caller — it's always DeployGitIdentity
// read inside the package constant. The old table exercised alternate
// identities that would have been injected via the (now-removed) `id
// GitIdentity` parameter; the useful invariant from that table — the
// diff-index-guarded commit — is what stays here.
func TestBuildSSHCommand_AlwaysCommits(t *testing.T) {
	t.Parallel()

	cmd := buildSSHCommand(testAuthInfo(), "svc-1", "/var/www", "", false)
	if !contains(cmd, "git add -A && (git diff-index") {
		t.Errorf("command must always stage and commit\ngot: %s", cmd)
	}
}

func TestBuildSSHCommand_NoChanges_SkipsCommit(t *testing.T) {
	t.Parallel()

	cmd := buildSSHCommand(testAuthInfo(), "svc-1", "/var/www", "", false)

	if !contains(cmd, "git diff-index --quiet HEAD 2>/dev/null || git commit -q -m 'deploy'") {
		t.Errorf("must use diff-index to skip commit when nothing changed\ngot: %s", cmd)
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
