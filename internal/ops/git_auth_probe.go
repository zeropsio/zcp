package ops

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// BuildGitAuthProbeCommand builds an SSH command body that probes git
// remote auth using a one-shot inline GIT_TOKEN. Uses the SAME credential
// helper as BuildGitPushCommand and BuildGitAuthedLsRemoteCommand so probe
// and real push share identical auth semantics — a passing probe is the
// strongest possible pre-stamp guarantee that the next push will
// authenticate.
//
// The probe is read-only (`git ls-remote HEAD`) — it does not mutate
// remote refs, does not push, and touches NO container disk (the
// ephemeral-.netrc era is over; spec-git-delivery-target §4).
//
// Safety flags:
//   - `GIT_TERMINAL_PROMPT=0` — never prompt for credentials. Without
//     this, a missing/wrong token can hang the SSH session waiting for
//     stdin, freezing the MCP call.
//
// The CANDIDATE token is passed via env-assignment prefix — the probe runs
// BEFORE the token is written anywhere (probe-first invariant), so it
// cannot rely on the session env the configured flows use. Briefly visible
// in /proc/<pid>/environ during the SSH session (same envelope as the old
// `export` form). Acceptable trade-off — probe is short-lived (~1s
// network round-trip).
//
// HTTPS-only enforcement is the caller's responsibility — this builder
// does NOT validate URL scheme. SCP-form SSH remotes (`git@host:owner/repo`)
// don't authenticate via PAT-over-HTTPS; caller must reject before calling.
func BuildGitAuthProbeCommand(remoteURL, token string) string {
	return fmt.Sprintf(
		"GIT_TOKEN=%s GIT_TERMINAL_PROMPT=0 git %s ls-remote %s HEAD",
		shellQuote(token), gitCredentialHelperArgs(), shellQuote(remoteURL),
	)
}

// BuildGitOriginSyncCommand builds an SSH command body that idempotently
// sets `origin` in workingDir's .git/config to remoteURL. Uses the same
// `(add 2>/dev/null || set-url)` pattern as BuildGitPushCommand so the
// container's persistent .git/config carries the same canonical origin
// the deploy path expects.
//
// GAP4-1: guards `.git` with `(test -d .git || git init -q -b main)` —
// the SAME guard the deploy path (buildSSHCommand) carries. git-push-setup
// is the FIRST step of the source-control chain (no prior deploy), and
// bootstrap's git-init is fire-and-forget (its failure is swallowed), so
// /var/www may have no .git yet; without the guard both `git remote add`
// and `git remote set-url` fail with "not a git repository" and the
// handler's own recovery text ("confirm /var/www/.git initialized") asks
// for the precondition this command now self-heals.
//
// Origin sync is also the single ASSERTION OWNER for the persistent
// url-scoped credential helper (+ the one-way stray-.netrc cleanup) —
// the credential analog of InitServiceGit's identity write. See
// gitCredentialHelperConfigFragment.
//
// Caller passes workingDir absolute path (e.g. /var/www). remoteURL is
// shell-quoted.
func BuildGitOriginSyncCommand(workingDir, remoteURL string) string {
	quoted := shellQuote(remoteURL)
	return fmt.Sprintf(
		`cd %s && (test -d .git || git init -q -b main) && (git remote add origin %s 2>/dev/null || git remote set-url origin %s) && %s`,
		shellQuote(workingDir), quoted, quoted, gitCredentialHelperConfigFragment(remoteURL),
	)
}

// RunGitAuthProbeLocal runs `git ls-remote $remoteURL HEAD` from
// workingDir using the user's local git config + credential helper. ZCP
// does NOT supply credentials in local mode — local git already holds
// them (SSH keys, OS credential manager, cached PAT).
//
// Safety flags:
//   - `GIT_TERMINAL_PROMPT=0` prevents git from prompting on stdin (would
//     hang the MCP session indefinitely).
//   - `GIT_SSH_COMMAND='ssh -o BatchMode=yes'` propagates the same
//     non-interactive guarantee to SSH-form remotes so a missing key
//     fails fast instead of hanging on a key passphrase prompt.
//
// Returns the combined stdout+stderr on failure so caller can surface
// meaningful error context to the agent.
func RunGitAuthProbeLocal(ctx context.Context, workingDir, remoteURL string) error {
	cmd := exec.CommandContext(ctx, "git", "ls-remote", remoteURL, "HEAD")
	cmd.Dir = workingDir
	cmd.Env = append(cmd.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git ls-remote %s: %w (output: %s)", remoteURL, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RunGitOriginSyncLocal sets `origin` in workingDir's .git/config to
// remoteURL. Idempotent: tries `git remote add origin` first (silently),
// falls back to `git remote set-url origin` if origin already exists.
// Same pattern as the container path's BuildGitOriginSyncCommand.
func RunGitOriginSyncLocal(ctx context.Context, workingDir, remoteURL string) error {
	addCmd := exec.CommandContext(ctx, "git", "remote", "add", "origin", remoteURL)
	addCmd.Dir = workingDir
	if err := addCmd.Run(); err == nil {
		return nil
	}
	// `add` failed (origin exists) — fall back to set-url.
	setCmd := exec.CommandContext(ctx, "git", "remote", "set-url", "origin", remoteURL)
	setCmd.Dir = workingDir
	out, err := setCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git remote set-url origin %s: %w (output: %s)", remoteURL, err, strings.TrimSpace(string(out)))
	}
	return nil
}
