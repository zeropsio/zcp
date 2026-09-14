package git

import (
	"context"
	"strings"
)

// ResolveSHA resolves sha to a full commit hash via `git rev-parse --verify
// <sha>^{commit}`, rooted at dir. The ^{commit} suffix rejects a sha that
// names a tree/blob/tag rather than a commit — the only object
// ExtractCommitToTemp and the ledger can act on.
func ResolveSHA(ctx context.Context, r Runner, dir, sha string) (string, error) {
	script := "git rev-parse --verify " + shellQuote(sha+"^{commit}")
	out, stderr, err := r.Run(ctx, dir, script)
	if err != nil {
		return "", wrapErr("resolve sha "+sha, err, stderr)
	}
	return strings.TrimSpace(out), nil
}

// ExtractCommitToTemp materializes sha's tree into tmpDir via
// `git archive --format=tar <sha> | tar -x -C <tmpDir>`. tmpDir MUST be
// outside dir's working tree — create it with MkTempDir. This sidesteps
// zcli's own `--workspace-state all` default archiver, which fails when
// dir's .git is a gitdir: pointer file (a known trap, not this slice's to
// fix): pushing from a plain extracted directory with --no-git never
// touches dir's .git at all.
func ExtractCommitToTemp(ctx context.Context, r Runner, dir, sha, tmpDir string) error {
	script := "git archive --format=tar " + shellQuote(sha) + " | tar -x -C " + shellQuote(tmpDir)
	_, stderr, err := r.Run(ctx, dir, script)
	if err != nil {
		return wrapErr("extract commit "+sha, err, stderr)
	}
	return nil
}
