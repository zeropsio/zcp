package skillpacks

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// cloneArgs builds the argv for a shallow, single-branch clone pinned to
// ref. The literal "--" ends option parsing before the repository URL: url
// comes from the catalog today, but the shape holds regardless — without
// it, an option-shaped value (e.g. "--upload-pack=...") could be
// interpreted as a git flag instead of a positional repository argument.
func cloneArgs(url, ref, dest string) []string {
	return []string{"clone", "--depth", "1", "--branch", ref, "--single-branch", "--", url, dest}
}

// gitEnv disables any interactive credential/terminal prompt — a clone that
// would otherwise hang waiting for input instead fails immediately.
func gitEnv() []string {
	return append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
}

// cloneRepo performs a shallow, single-branch, context-bound
// `git clone --depth 1 --branch <ref> -- <url> <dest>` via a fixed argv (no
// shell composition). A missing git binary is checked explicitly first so
// the caller gets CodeGitMissing instead of an opaque exec failure.
func cloneRepo(ctx context.Context, url, ref, dest string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return codedErrorf(CodeGitMissing, "git is not installed or not on PATH")
	}
	cmd := exec.CommandContext(ctx, "git", cloneArgs(url, ref, dest)...) //nolint:gosec // url/ref are catalog-controlled, cloneArgs puts "--" before the URL
	cmd.Env = gitEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return wrapCoded(CodeDownloadFailed, err, "git clone %s (ref %s): %s", url, ref, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// remoteAddArgs builds the argv for adding url as dest's "origin" remote.
// The literal "--" ends option parsing before url, mirroring cloneArgs.
func remoteAddArgs(url string) []string {
	return []string{"remote", "add", "origin", "--", url}
}

// fetchCommitArgs builds the argv for a shallow fetch of exactly one commit
// SHA from the "origin" remote. commit is always caller-validated against
// commitPattern before fetchCommit is ever invoked (it comes from a
// manifest's own Source.Commit, itself schema-validated on load), so it can
// never be flag-shaped — the "--" is kept anyway to match cloneArgs's
// discipline of never relying on that alone.
func fetchCommitArgs(commit string) []string {
	return []string{"fetch", "--depth", "1", "origin", "--", commit}
}

// checkoutCommitArgs builds the argv that populates the working tree at
// exactly commit. Unlike clone/fetch/remote-add, git-checkout's own "--"
// marks everything after it as a PATH, not a commit-ish — so it must NOT be
// used here (commit is still pre-validated against commitPattern, so an
// option-shaped value is structurally impossible regardless).
func checkoutCommitArgs(commit string) []string {
	return []string{"checkout", "-q", commit}
}

// fetchCommit populates dest's working tree with the exact content of
// commit from url, via `git init` + `git remote add` + a shallow
// `git fetch <commit>` + `git checkout <commit>` — never a branch/tag ref,
// so it can never resolve to anything but the one pinned commit a manifest
// recorded. This is the addition-side counterpart to cloneRepo: cloneRepo
// always resolves the catalog's branch tip (used only for a from-scratch
// install, where nothing is pinned yet); fetchCommit is used whenever
// PackSet adds a skill to an ALREADY-installed pack, so the addition
// installs from the commit already pinned in the manifest, never current
// upstream HEAD (spec-skill-packs.md §3.1). A commit git cannot fetch is a
// named, hard error — there is no fallback to the branch tip.
func fetchCommit(ctx context.Context, url, commit, dest string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return codedErrorf(CodeGitMissing, "git is not installed or not on PATH")
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return wrapCoded(CodeFilesystem, err, "create fetch destination %s", dest)
	}

	if err := runGitStep(ctx, dest, []string{"init", "-q"}); err != nil {
		return wrapCoded(CodeDownloadFailed, err, "git init in %s", dest)
	}
	if err := runGitStep(ctx, dest, remoteAddArgs(url)); err != nil {
		return wrapCoded(CodeDownloadFailed, err, "git remote add origin %s", url)
	}
	if err := runGitStep(ctx, dest, fetchCommitArgs(commit)); err != nil {
		return wrapCoded(CodeDownloadFailed, err, "git fetch pinned commit %s from %s", commit, url)
	}
	if err := runGitStep(ctx, dest, checkoutCommitArgs(commit)); err != nil {
		return wrapCoded(CodeDownloadFailed, err, "git checkout pinned commit %s", commit)
	}
	return nil
}

// runGitStep runs one `git -C dir <args...>` step via a fixed argv (no
// shell composition), returning stderr's text as the error's cause so a
// caller's wrapCoded message stays specific.
func runGitStep(ctx context.Context, dir string, args []string) error {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // dir is a caller-owned temp dir, args are built by the fixed *Args helpers below from catalog/manifest values — never user input, never shell-composed
	cmd.Env = gitEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return err
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

// headCommit returns the cloned repo's checked-out commit SHA.
func headCommit(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD")
	cmd.Env = gitEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", wrapCoded(CodeInternal, err, "git rev-parse HEAD in %s: %s", dir, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
