package ops

import (
	"net/url"
	"os"
	"os/exec"
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
// remote-ref read shared by the launch push-proof (container) — the
// consolidation the 2026-05-28 audit ordered (one primitive, no inline
// duplicates in tools/). GF-7: the ref is the caller-supplied tracked ref,
// never a hardcoded "HEAD".
func TestBuildGitAuthedLsRemoteCommand_Shape(t *testing.T) {
	t.Parallel()

	cmd := BuildGitAuthedLsRemoteCommand("https://github.com/example/app", "main")
	for _, req := range []struct{ name, substr string }{
		{"prompt disabled", "GIT_TERMINAL_PROMPT=0"},
		{"inline helper", "-c credential.helper='!f()"},
		{"read-only probe", "git"},
		{"ls-remote against the tracked ref", "ls-remote 'https://github.com/example/app' 'main'"},
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

// TestBuildGitAuthedLsRemoteCommand_RefIsParameterized pins GF-7: passing a
// non-default ref produces a command targeting THAT ref, never a hardcoded
// "HEAD" — the launch gate compares against the recorded tracked ref, not
// whatever the remote's symbolic default happens to be.
func TestBuildGitAuthedLsRemoteCommand_RefIsParameterized(t *testing.T) {
	t.Parallel()

	cmd := BuildGitAuthedLsRemoteCommand("https://github.com/example/app", "release")
	if !strings.Contains(cmd, "ls-remote 'https://github.com/example/app' 'release'") {
		t.Errorf("command must target the supplied ref 'release':\n%s", cmd)
	}
	if strings.Contains(cmd, "' HEAD") {
		t.Errorf("command must not fall back to the symbolic HEAD ref when a tracked ref is supplied:\n%s", cmd)
	}
}

// TestGitCredentialHelperConfigFragment_URLScoped pins the persistent
// helper written by origin sync: url-scoped (credential.<url>.helper —
// a global helper would answer for ANY https host, parity with the old
// host-scoped `machine` line), plus the one-time stray-.netrc cleanup
// (migration off the ephemeral-.netrc era; the fail-open residue class).
func TestGitCredentialHelperConfigFragment_URLScoped(t *testing.T) {
	t.Parallel()

	frag := gitCredentialHelperConfigFragment("https://gitlab.com/team/app.git", "")
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

// TestBuildGitReconstructCommand_Shape pins F1 site 3: reconstruction of a
// missing .git inits + fills identity via the SAME set-if-absent shape
// every other self-heal site uses (single-owner gitIdentityEnsureFragment)
// — a fresh init has no identity yet, so ensure-vs-unconditional-write is
// behaviorally identical here, but the shape stays consistent so F3's
// derived-identity fill can key off one owner everywhere.
//
// Ordering is asserted, not mere containment (Codex diff-review finding
// 3a): the identity fragment must sit INSIDE the `if test ! -d .git;
// then ... fi` body — after the guard opens, before origin is added, and
// before the guard closes. A fragment that merely appears SOMEWHERE in
// the command string (e.g. accidentally spliced outside the `if`, or
// after the fetch/reset) would pass a plain substring-contains check
// without actually running as part of the missing-.git recovery.
func TestBuildGitReconstructCommand_Shape(t *testing.T) {
	t.Parallel()
	cmd := BuildGitReconstructCommand("/var/www", "https://github.com/example/app.git", "", DeployGitIdentity)

	ifIdx := strings.Index(cmd, "if test ! -d .git; then git init -q -b main")
	if ifIdx < 0 {
		t.Fatalf("reconstruction must be guarded by a missing-.git check: %s", cmd)
	}
	identityFrag := `(test -n "$(git config user.email)" || git config user.email 'agent@zerops.io') && (test -n "$(git config user.name)" || git config user.name 'Zerops Agent')`
	identityIdx := strings.Index(cmd, identityFrag)
	if identityIdx < 0 {
		t.Fatalf("reconstruction must fill identity via the single-owner ensure fragment: %s", cmd)
	}
	originIdx := strings.Index(cmd, "git remote add origin")
	if originIdx < 0 {
		t.Fatalf("reconstruction must add the remote origin: %s", cmd)
	}
	fiIdx := strings.LastIndex(cmd, "; fi")
	if fiIdx < 0 {
		t.Fatalf("reconstruction must close its guard with fi: %s", cmd)
	}
	if ifIdx >= identityIdx || identityIdx >= originIdx || originIdx >= fiIdx {
		t.Errorf("identity fragment must sit inside the `then` body, before origin is added, before the guard closes: if=%d identity=%d origin=%d fi=%d\n%s",
			ifIdx, identityIdx, originIdx, fiIdx, cmd)
	}

	if strings.Contains(cmd, "git config user.email 'agent@zerops.io' && git config user.name") {
		t.Errorf("reconstruction must NOT write identity unconditionally (bare assignment, not ensure): %s", cmd)
	}
	if !strings.Contains(cmd, "fetch -q origin HEAD") || !strings.Contains(cmd, "git reset -q FETCH_HEAD") {
		t.Errorf("reconstruction must fetch + mixed-reset onto the remote HEAD: %s", cmd)
	}
}

// TestBuildGitReconstructCommand_UsesSuppliedIdentity is the F3 pin: a
// reconstruction with a GitHub-derived identity available must fill THAT
// identity on init, not the hardcoded robot default — a rebuilt repo
// should land human-attributed from the first commit, never
// robot-then-migrate.
func TestBuildGitReconstructCommand_UsesSuppliedIdentity(t *testing.T) {
	t.Parallel()
	derived := GitIdentity{Name: "octocat", Email: "octocat@users.noreply.github.com"}
	cmd := BuildGitReconstructCommand("/var/www", "https://github.com/example/app.git", "", derived)

	if !strings.Contains(cmd, `git config user.email 'octocat@users.noreply.github.com'`) {
		t.Errorf("reconstruction must fill the SUPPLIED derived email, not the robot default: %s", cmd)
	}
	if !strings.Contains(cmd, `git config user.name 'octocat'`) {
		t.Errorf("reconstruction must fill the SUPPLIED derived name, not the robot default: %s", cmd)
	}
	if strings.Contains(cmd, "agent@zerops.io") || strings.Contains(cmd, "Zerops Agent") {
		t.Errorf("reconstruction with a derived identity must not reference the robot identity at all: %s", cmd)
	}
}

// TestBuildGitTagPushCommand_NoInlineIdentity is the F3 "consequence for
// free" pin: the release-tag command carries NO identity override at all
// (no `-c user.email=`/`user.name=`) — `git tag -a` always reads the
// repo's AMBIENT config for the tagger. Once F3 seeds a human identity
// into that ambient config, every release tag inherits it automatically;
// this command needs no code change to pick that up, which this test
// exists to keep true.
func TestBuildGitTagPushCommand_NoInlineIdentity(t *testing.T) {
	t.Parallel()
	cmd := BuildGitTagPushCommand("/var/www", "v1.2.3")

	if !strings.Contains(cmd, "git tag -a") {
		t.Fatalf("expected an annotated tag command: %s", cmd)
	}
	for _, forbidden := range []string{"-c user.email", "-c user.name", "agent@zerops.io", "Zerops Agent"} {
		if strings.Contains(cmd, forbidden) {
			t.Errorf("release tag command must carry NO inline identity override (reads ambient config) — found %q: %s", forbidden, cmd)
		}
	}
}

// The persisted helper answers two shells. The dev service's own sessions
// carry GIT_TOKEN, the service secret git-push-setup writes. The Mate's shell
// runs git on the same repository through the mount, and carries the bot's
// token as GITEA_TOKEN only — so on a remote on the Mate's Gitea the helper
// falls back to it. Every other host answers GIT_TOKEN alone: the bot's token
// is never sent anywhere but the Mate's own Gitea.
const (
	helperGiteaURL   = "https://gitea.example.invalid"
	helperBotToken   = "the-mates-bot-token"
	helperGitToken   = "the-service-secret"
	helperGiteaRepo  = helperGiteaURL + "/acme/appdev.git"
	helperGitHubRepo = "https://github.com/acme/appdev.git"
)

// gitShell runs a shell command in dir as a session carrying only env — a
// HOME of its own and no system config, so nothing of the machine running the
// test answers a credential.
func gitShell(t *testing.T, dir, home string, env map[string]string, stdin, script string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sh", "-c", script)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0"}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// answeredPassword asks the repository's configured helpers for a credential
// to remoteURL, the way a `git fetch` in that shell would, and returns the
// password they answered ("" for none).
func answeredPassword(t *testing.T, repo, home string, env map[string]string, remoteURL string) string {
	t.Helper()
	u, err := url.Parse(remoteURL)
	if err != nil {
		t.Fatalf("parse %q: %v", remoteURL, err)
	}
	out, _ := gitShell(t, repo, home, env,
		"protocol="+u.Scheme+"\nhost="+u.Host+"\npath="+strings.TrimPrefix(u.Path, "/")+"\n\n",
		"git credential fill")
	for line := range strings.SplitSeq(out, "\n") {
		if v, ok := strings.CutPrefix(line, "password="); ok {
			return v
		}
	}
	return ""
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

// TestPersistedCredentialHelper_TokenByShellAndHost runs origin sync on a real
// repository and asks the helper it persisted for a credential, from the dev
// service's session and from the Mate's shell.
func TestPersistedCredentialHelper_TokenByShellAndHost(t *testing.T) {
	t.Parallel()
	requireGit(t)

	devSession := map[string]string{"GIT_TOKEN": helperGitToken}
	mateShell := map[string]string{"GITEA_TOKEN": helperBotToken}
	both := map[string]string{"GIT_TOKEN": helperGitToken, "GITEA_TOKEN": helperBotToken}

	tests := []struct {
		name     string
		remote   string
		giteaURL string
		env      map[string]string
		want     string
	}{
		{"the Mate's shell on its Gitea answers the bot token", helperGiteaRepo, helperGiteaURL, mateShell, helperBotToken},
		{"the dev service on the Gitea answers its service secret", helperGiteaRepo, helperGiteaURL, devSession, helperGitToken},
		{"the service secret wins where both are set", helperGiteaRepo, helperGiteaURL, both, helperGitToken},
		{"github.com never gets the bot token", helperGitHubRepo, helperGiteaURL, mateShell, ""},
		{"github.com answers the service secret", helperGitHubRepo, helperGiteaURL, both, helperGitToken},
		{"another host never gets the bot token", "https://code.example.invalid/acme/appdev.git", helperGiteaURL, mateShell, ""},
		{"a Mate with no Gitea wiring falls back to nothing", helperGiteaRepo, "", mateShell, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repo, home := t.TempDir(), t.TempDir()
			if out, err := gitShell(t, repo, home, nil, "", BuildGitOriginSyncCommand(repo, tt.remote, tt.giteaURL)); err != nil {
				t.Fatalf("origin sync: %v\n%s", err, out)
			}
			if got := answeredPassword(t, repo, home, tt.env, tt.remote); got != tt.want {
				t.Errorf("the helper answered %q, want %q", got, tt.want)
			}
		})
	}
}
