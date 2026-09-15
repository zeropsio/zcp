package ops

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// gitWriteAuthProbeBranch is the throwaway remote ref the write-auth probe
// targets. `git push --dry-run` to it exercises git-receive-pack (the WRITE
// service — auth is enforced even on public repos, unlike the read-only
// git-upload-pack) but, being --dry-run, sends no pack and creates no ref. A
// non-existent branch is used deliberately: creating a NEW branch needs
// contents:write but is not subject to per-branch protection rules, so the
// probe isolates the AUTH check (403 for a read-only/garbage token) from
// unrelated policy rejections. Empirically confirmed on eval2 2026-06-17:
// ls-remote returns 0 for a garbage token on a public repo, push --dry-run
// returns non-zero — the asymmetry this probe relies on.
const gitWriteAuthProbeBranch = "zcp-write-auth-probe"

// BuildGitWritePushProbeCommand builds an SSH command body that proves the
// candidate GIT_TOKEN has WRITE (push) capability against the remote, without
// mutating anything. It is the probe-first gate's proof: a passing probe is
// what authorizes git-push-setup to stamp `configured` and write the secret.
//
// Why write-proof, not read: the read-only `git ls-remote` it replaced passes
// for ANY token (garbage, expired, read-only PAT) on a PUBLIC repo, because the
// unauthenticated upload-pack path already serves refs — so the old probe's
// PROOF (remote is readable) was weaker than the CLAIM it underwrote (the next
// push will authenticate). A garbage token then passed the probe and the handler
// proceeded to OVERWRITE a previously-working GIT_TOKEN secret (the destruction
// bug). `git push --dry-run` hits git-receive-pack, which requires auth even on
// public repos, so a read-only/garbage token fails the probe BEFORE any secret
// is written (probe-first preserves the existing secret automatically).
//
// `--dry-run` is non-mutating by git's contract: it performs the full
// authenticated negotiation but sends no pack and updates no ref. Needs a local
// HEAD (a commit to offer); when HEAD is unborn (fresh empty repo, no commit
// yet) write capability cannot be proven without mutating, so it falls back to
// the read probe — the caller keeps the honest "write not yet proven" hedge and
// must not stamp the stronger claim.
//
// HTTPS-only enforcement + caller responsibility unchanged from the read probe.
func BuildGitWritePushProbeCommand(workingDir, remoteURL, token string) string {
	qtok, qhelper, qurl := shellQuote(token), gitCredentialHelperArgs(), shellQuote(remoteURL)
	return fmt.Sprintf(
		`cd %s && if git rev-parse --verify -q HEAD >/dev/null 2>&1; then `+
			`GIT_TOKEN=%s GIT_TERMINAL_PROMPT=0 git %s push --dry-run %s HEAD:refs/heads/%s; `+
			`else GIT_TOKEN=%s GIT_TERMINAL_PROMPT=0 git %s ls-remote %s HEAD; fi`,
		shellQuote(workingDir),
		qtok, qhelper, qurl, gitWriteAuthProbeBranch,
		qtok, qhelper, qurl,
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
// for the precondition this command now self-heals. Identity is filled
// set-if-absent right after the init guard (same shape as InitServiceGit /
// buildSSHCommand) — a repo this command just init'd has no identity yet,
// and the git-push flow's first commit (the user's, over SSH) would
// otherwise fail with "unable to auto-detect email address".
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
	// Non-destructive (F1b): before pointing origin at the user's repo,
	// preserve any pre-existing origin (e.g. a recipe-bootstrapped service's
	// source remote) under `zerops-original-origin`, so overwriting origin
	// never silently discards the only URL a `git fetch --unshallow` could
	// recover from. Best-effort; the backup add is idempotent (remove-then-add)
	// and a no-op when origin is absent or already equals the target.
	preserve := fmt.Sprintf(
		`{ cur=$(git remote get-url origin 2>/dev/null || true); if [ -n "$cur" ] && [ "$cur" != %s ]; then git remote remove zerops-original-origin 2>/dev/null || true; git remote add zerops-original-origin "$cur" 2>/dev/null || true; fi; }`,
		quoted,
	)
	return fmt.Sprintf(
		`cd %s && (test -d .git || git init -q -b main) && %s && %s && (git remote add origin %s 2>/dev/null || git remote set-url origin %s) && %s`,
		shellQuote(workingDir), gitIdentityEnsureFragment(), preserve, quoted, quoted, gitCredentialHelperConfigFragment(remoteURL),
	)
}

// BuildGitShallowFixCommand detects a shallow clone at workingDir and, if
// present, attempts to complete it with `git fetch --unshallow` from the
// CURRENT origin. It MUST run BEFORE origin is rewritten to the user's repo —
// a recipe-bootstrapped service's shallow clone can only be `--unshallow`-ed
// from its original (recipe) origin, and BuildGitOriginSyncCommand would
// otherwise overwrite that URL. The token authenticates the fetch (public
// recipe remotes ignore it). Echoes ONE dispatch token on stdout:
//
//	ZCP_NOT_SHALLOW            — no .git/shallow; nothing to do
//	ZCP_UNSHALLOW_OK           — was shallow, fetch --unshallow succeeded (now complete)
//	ZCP_UNSHALLOW_FAIL <orig>  — was shallow, fetch failed (corrupt/missing object/auth);
//	                             <orig> = the original origin URL (still intact)
//
// On ZCP_UNSHALLOW_FAIL the caller must return a blocker BEFORE the origin
// sync, so the original remote stays available for manual recovery.
func BuildGitShallowFixCommand(workingDir, token string) string {
	qwd := shellQuote(workingDir)
	return fmt.Sprintf(
		`cd %s && if [ -f .git/shallow ]; then orig=$(git remote get-url origin 2>/dev/null || true); `+
			`if GIT_TOKEN=%s GIT_TERMINAL_PROMPT=0 git %s fetch --unshallow origin >/dev/null 2>&1; then echo ZCP_UNSHALLOW_OK; `+
			`else echo "ZCP_UNSHALLOW_FAIL $orig"; fi; else echo ZCP_NOT_SHALLOW; fi`,
		qwd, shellQuote(token), gitCredentialHelperArgs(),
	)
}

// RunGitAuthProbeLocal proves WRITE (push) capability against remoteURL from
// workingDir using the user's local git config + credential helper. ZCP does
// NOT supply credentials in local mode — local git already holds them (SSH
// keys, OS credential manager, cached PAT).
//
// Write-proof, not read: `git push --dry-run` (non-mutating — sends no pack,
// creates no ref) exercises git-receive-pack, which requires auth even on a
// public repo, so a read-only credential fails the probe. The read-only
// `git ls-remote` it replaced passed for any credential on a public repo,
// over-claiming write capability. Falls back to ls-remote when HEAD is unborn
// (no commit to push) — caller keeps the honest "write not yet proven" hedge.
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
	headCheck := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "-q", "HEAD")
	headCheck.Dir = workingDir
	var probe *exec.Cmd
	if headCheck.Run() == nil {
		probe = exec.CommandContext(ctx, "git", "push", "--dry-run", remoteURL, "HEAD:refs/heads/"+gitWriteAuthProbeBranch)
	} else {
		probe = exec.CommandContext(ctx, "git", "ls-remote", remoteURL, "HEAD")
	}
	probe.Dir = workingDir
	probe.Env = append(probe.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes",
	)
	out, err := probe.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git write-auth probe %s: %w (output: %s)", remoteURL, err, strings.TrimSpace(string(out)))
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
