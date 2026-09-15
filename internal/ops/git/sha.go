package git

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

// ResolveSHA resolves sha to a full commit hash via `git rev-parse --verify
// <sha>^{commit}`, rooted at dir. The ^{commit} suffix rejects a sha that
// names a tree/blob/tag rather than a commit — the only object
// ExtractCommitToTemp can materialize.
func ResolveSHA(ctx context.Context, r Runner, dir, sha string) (string, error) {
	script := "git rev-parse --verify " + shellQuote(sha+"^{commit}")
	out, stderr, err := r.Run(ctx, dir, script)
	if err != nil {
		return "", wrapErr("resolve sha "+sha, err, stderr)
	}
	resolved := strings.TrimSpace(out)
	// rev-parse prints warnings (e.g. "refname … is ambiguous") on the same
	// combined stream over SSH; accept nothing but a full commit hash so a
	// warning never becomes part of a tag name or --version-name.
	if !fullSHA.MatchString(resolved) {
		return "", wrapErr("resolve sha "+sha, errNotACommitHash, out)
	}
	return resolved, nil
}

// fullSHA is the only shape ResolveSHA hands back: 7–40 lowercase hex chars (git prints 40; fixtures may abbreviate).
var fullSHA = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

var errNotACommitHash = errors.New("git rev-parse did not print a single full commit hash")

// HeadStatus reads the SOURCE's current HEAD sha and whether its working
// tree is dirty, via one combined `git rev-parse --verify HEAD && git
// status --porcelain | head -c1` round trip, rooted at dir. Used by a
// working-tree deploy (no explicit sha) to record what was actually
// shipped (docs/spec-workflows.md §4.9) — read-only, never touches the
// push itself. ok=false with no error when dir has no repo or no
// reachable HEAD yet (a fresh/unborn repo) — not itself a failure, just
// nothing to record.
func HeadStatus(ctx context.Context, r Runner, dir string) (sha string, dirty bool, ok bool, err error) {
	script := "git rev-parse --verify HEAD && git status --porcelain | head -c1"
	out, _, runErr := r.Run(ctx, dir, script)
	if runErr != nil {
		return "", false, false, nil //nolint:nilerr // no repo/no HEAD yet is not a failure
	}
	head, rest, _ := strings.Cut(out, "\n")
	return strings.TrimSpace(head), rest != "", true, nil
}

// ReadFileAtCommit reads path's content AT sha via `git show <sha>:<path>`,
// rooted at dir. Used to validate a deploy-from-commit's zerops.yaml
// against exactly what will ship, never the working tree/SSHFS mount —
// which may differ from the commit (docs/spec-workflows.md §4.9). A
// missing file at the commit surfaces as the underlying git failure
// (typically exit 128, "fatal: path '<path>' does not exist in '<sha>'");
// callers translate that into a deploy-specific message.
func ReadFileAtCommit(ctx context.Context, r Runner, dir, sha, path string) (string, error) {
	script := "git show " + shellQuote(sha+":"+path)
	out, stderr, err := r.Run(ctx, dir, script)
	if err != nil {
		return "", wrapErr("read "+path+" at "+sha, err, stderr)
	}
	return out, nil
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
