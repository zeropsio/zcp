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
		name        string
		authInfo    auth.Info
		serviceID   string
		workDir     string
		includeGit  bool
		serviceType string
		wantParts   []string
		wantAbsent  []string
	}{
		{
			name:        "basic command with split init/identity-ensure and no auto-commit",
			authInfo:    testAuthInfo(),
			serviceID:   "svc-123",
			workDir:     "/var/www",
			serviceType: "nodejs@22",
			wantParts: []string{
				// gitInit only fires when .git/ is missing (cold path /
				// migration). Identity ensure is top-level and set-if-
				// absent — runs always, but never stomps a value already
				// there — so a pre-existing .git/ from a buildFromGit
				// clone (B13) still gets a canonical identity filled when
				// none exists (P1). Gitignore backfill (guarded by
				// test -e, never overwrites) follows so the HEAD
				// guarantee's marker commit — or the user's own first
				// commit on an already-initialized repo — has it to
				// track. The HEAD guarantee follows, so zcli's archiver
				// always has a commit to diff against (P2) — there is no
				// `git add` / `git commit` in this command at all; the
				// dirty tree ships via zcli's own ephemeral stash-archive.
				"(test -d .git || git init -q -b main)",
				`(test -n "$(git config user.email)" || git config user.email 'agent@zerops.io') && (test -n "$(git config user.name)" || git config user.name 'Zerops Agent')`,
				`test -e .gitignore || printf '%s\n' '.env' '.zcp/' '.DS_Store' '*.log' 'node_modules/' 'dist/' '.next/' '.nuxt/' '.output/' > .gitignore`,
				`(git rev-parse -q --verify HEAD >/dev/null || { if test -e .gitignore; then gi_blob=$(git hash-object -w .gitignore); tree=$(printf '100644 blob %s\t.gitignore\n' "$gi_blob" | git mktree); git update-index --add --cacheinfo 100644,"$gi_blob",.gitignore; else tree=$(git mktree </dev/null); fi; git update-ref HEAD "$(git -c user.email='agent@zerops.io' -c user.name='Zerops Agent' commit-tree "$tree" -m 'zcp init')"; })`,
				"zcli push --service-id svc-123",
			},
			wantAbsent: []string{
				"rm -rf .git",
				"git remote",
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
		{
			name:        "unrecognized service type stays base-only",
			authInfo:    testAuthInfo(),
			serviceID:   "svc-200",
			workDir:     "/var/www",
			serviceType: "some-future-runtime@1",
			wantParts: []string{
				`test -e .gitignore || printf '%s\n' '.env' '.zcp/' '.DS_Store' '*.log' > .gitignore`,
			},
			wantAbsent: []string{
				"node_modules/",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := buildSSHCommand(tt.authInfo, tt.serviceID, tt.workDir, "", tt.includeGit, tt.serviceType)

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

	cmd := buildSSHCommand(testAuthInfo(), "svc-1", "/var/www", "", false, "")

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

	cmd := buildSSHCommand(testAuthInfo(), "svc-1", "/var/www", "", false, "")

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

// TestBuildSSHCommand_PreservesRemote_BackfillsGitignoreIfMissing pins two
// distinct claims that used to be a single "never touches either" test:
// direct deploy still never touches the git remote (that stays git-push-
// setup's job entirely — B6-N1), but it NOW backfills a missing .gitignore
// via the guarded `test -e .gitignore || ...` fragment, never an
// unconditional write that could clobber one the user already has.
func TestBuildSSHCommand_PreservesRemote_BackfillsGitignoreIfMissing(t *testing.T) {
	t.Parallel()

	cmd := buildSSHCommand(testAuthInfo(), "svc-1", "/var/www", "", false, "nodejs@22")

	if contains(cmd, "git remote") {
		t.Errorf("command must NOT touch git remote\ngot: %s", cmd)
	}
	if !contains(cmd, "test -e .gitignore || printf") {
		t.Errorf("command should guard-write .gitignore if missing\ngot: %s", cmd)
	}
}
