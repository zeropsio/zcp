package skillpacks

import (
	"bytes"
	"context"
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
