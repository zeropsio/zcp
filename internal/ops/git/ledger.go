package git

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
)

// ledgerIdentityName/ledgerIdentityEmail are the author/committer identity
// WriteLedger stamps on every ledger commit — matching ops.DeployGitIdentity
// (this package cannot import internal/ops; kept in sync by inspection,
// same values). commit-tree creates a real commit object, which git
// refuses without SOME identity; a buildFromGit-provisioned container has
// no ~/.gitconfig and no GIT_AUTHOR_*/GIT_COMMITTER_* env (unlike a
// self-deploy container, which InitServiceGit seeds), so relying on
// ambient config fails there with "unable to auto-detect email address".
const (
	ledgerIdentityName  = "Zerops Agent"
	ledgerIdentityEmail = "agent@zerops.io"
)

// WriteLedger records one deploy-from-commit attempt: moves
// refs/zcp/env/<target> to entry.SHA, and appends a new
// refs/zcp/deploy/<n> ref pointing at a commit (same tree as entry.SHA,
// parented on it) whose message is entry's single-line JSON — so
// `git log refs/zcp/deploy/*` is the ledger's history. No working-tree
// change, no branch moves (docs/spec-workflows.md §4.5).
func WriteLedger(ctx context.Context, r Runner, dir string, entry topology.LedgerEntry) error {
	treeOut, stderr, err := r.Run(ctx, dir, "git rev-parse "+shellQuote(entry.SHA+"^{tree}"))
	if err != nil {
		return wrapErr("resolve tree for "+entry.SHA, err, stderr)
	}
	tree := strings.TrimSpace(treeOut)

	msg, err := json.Marshal(entry)
	if err != nil {
		return wrapErr("marshal ledger entry", err, "")
	}

	commitScript := "git -c user.name=" + shellQuote(ledgerIdentityName) +
		" -c user.email=" + shellQuote(ledgerIdentityEmail) +
		" commit-tree " + shellQuote(tree) + " -p " + shellQuote(entry.SHA) + " -m " + shellQuote(string(msg))
	commitOut, stderr, err := r.Run(ctx, dir, commitScript)
	if err != nil {
		return wrapErr("commit-tree", err, stderr)
	}
	commitSHA := strings.TrimSpace(commitOut)

	deployRef := topology.DeployRefPrefix + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, stderr, err := r.Run(ctx, dir, "git update-ref "+shellQuote(deployRef)+" "+shellQuote(commitSHA)); err != nil {
		return wrapErr("update-ref "+deployRef, err, stderr)
	}

	envRef := topology.EnvRefName(entry.Target)
	if _, stderr, err := r.Run(ctx, dir, "git update-ref "+shellQuote(envRef)+" "+shellQuote(entry.SHA)); err != nil {
		return wrapErr("update-ref "+envRef, err, stderr)
	}
	return nil
}

// ReadEnvRef reads which sha currently runs on target, per
// refs/zcp/env/<target>. Returns "" with no error if the ref does not exist
// yet — target has never received a sha deploy, which is not itself a
// failure.
func ReadEnvRef(ctx context.Context, r Runner, dir, target string) (string, error) {
	out, _, err := r.Run(ctx, dir, "git rev-parse --verify "+shellQuote(topology.EnvRefName(target)))
	if err != nil {
		return "", nil //nolint:nilerr // "no ledger ref yet" is not a failure — see the doc-comment
	}
	return strings.TrimSpace(out), nil
}
