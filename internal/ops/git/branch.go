package git

import (
	"context"
	"strings"
)

// CurrentBranch resolves the tracked ref a checkout is on (GF-7, docs/spec-
// workflows.md §12.6): the attached HEAD's branch name via `git
// symbolic-ref --short HEAD`, or — when HEAD is detached or unborn — the
// remote's default branch via `git ls-remote --symref <remoteURL> HEAD`
// (skipped when remoteURL is empty). Returns "" when neither resolves;
// callers apply the final "main" fallback (git-push-setup is the sole
// writer of ServiceMeta.TrackedRef, every reader falls back independently).
//
// Both commands are read-only and tolerate failure via `|| true` inside the
// script, so a non-nil error here means the Runner itself failed
// (transport/exec), not "no branch" — that case surfaces as ("", nil).
func CurrentBranch(ctx context.Context, r Runner, dir, remoteURL string) (string, error) {
	script := "git symbolic-ref --short HEAD 2>/dev/null || true"
	out, stderr, err := r.Run(ctx, dir, script)
	if err != nil {
		return "", wrapErr("resolve current branch", err, stderr)
	}
	if branch := strings.TrimSpace(out); branch != "" {
		return branch, nil
	}
	if remoteURL == "" {
		return "", nil
	}
	symScript := "git ls-remote --symref " + shellQuote(remoteURL) + " HEAD 2>/dev/null || true"
	symOut, _, symErr := r.Run(ctx, dir, symScript)
	if symErr != nil {
		return "", nil //nolint:nilerr // remote default-branch lookup is best-effort; caller falls back to "main"
	}
	return parseSymrefDefaultBranch(symOut), nil
}

// parseSymrefDefaultBranch extracts the branch name from `git ls-remote
// --symref <url> HEAD` output, whose first line looks like:
//
//	ref: refs/heads/main	HEAD
//	<sha>	HEAD
//
// Returns "" when no "ref:" line is present (bare/empty remote, or the
// symref form isn't supported).
func parseSymrefDefaultBranch(out string) string {
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ref:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		return strings.TrimPrefix(fields[1], "refs/heads/")
	}
	return ""
}
