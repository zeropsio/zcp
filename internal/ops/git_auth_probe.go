package ops

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// BuildGitAuthProbeCommand builds an SSH command body that probes git
// remote auth using a one-shot inline GIT_TOKEN. Uses the same ephemeral
// .netrc + trap-cleanup pattern as BuildGitPushCommand so probe and real
// push share identical auth semantics — a passing probe is the strongest
// possible pre-stamp guarantee that the next push will authenticate.
//
// The probe is read-only (`git ls-remote HEAD`) — it does not mutate
// remote refs, does not push, does not touch container disk beyond the
// ephemeral .netrc.
//
// Safety flags:
//   - `GIT_TERMINAL_PROMPT=0` — never prompt for credentials. Without
//     this, a missing/wrong token can hang the SSH session waiting for
//     stdin, freezing the MCP call.
//   - `trap 'rm -f ~/.netrc' EXIT` — .netrc removed on any exit
//     (success, failure, signal).
//   - `umask 077` + `chmod 600` — token file world-unreadable.
//
// The token is passed inline via `export GIT_TOKEN=<quoted>`. Briefly
// visible in /proc/<pid>/environ during the SSH session, same exposure
// envelope as BuildGitPushCommand (which assumes $GIT_TOKEN is already
// in container env). Acceptable trade-off — probe is short-lived (~1s
// network round-trip).
//
// HTTPS-only enforcement is the caller's responsibility — this builder
// does NOT validate URL scheme. SCP-form SSH remotes (`git@host:owner/repo`)
// don't authenticate via .netrc + PAT; caller must reject before calling.
func BuildGitAuthProbeCommand(remoteURL, token string) string {
	host := parseGitHost(remoteURL)
	parts := []string{
		fmt.Sprintf("export GIT_TOKEN=%s", shellQuote(token)),
		"trap 'rm -f ~/.netrc' EXIT",
		fmt.Sprintf(`umask 077 && echo "machine %s login oauth2 password $GIT_TOKEN" > ~/.netrc && chmod 600 ~/.netrc`, host),
		fmt.Sprintf("GIT_TERMINAL_PROMPT=0 git ls-remote %s HEAD", shellQuote(remoteURL)),
	}
	return strings.Join(parts, " && ")
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
// Caller passes workingDir absolute path (e.g. /var/www). remoteURL is
// shell-quoted.
func BuildGitOriginSyncCommand(workingDir, remoteURL string) string {
	quoted := shellQuote(remoteURL)
	return fmt.Sprintf(
		`cd %s && (test -d .git || git init -q -b main) && (git remote add origin %s 2>/dev/null || git remote set-url origin %s)`,
		workingDir, quoted, quoted,
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
