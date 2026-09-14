package git

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

// ledgerIdentityName/ledgerIdentityEmail are the author/committer identity
// WriteLedger stamps on every ledger tag — matching ops.DeployGitIdentity
// (this package cannot import internal/ops; kept in sync by inspection,
// same values). `git tag -a` creates a tag OBJECT (tagger line), which
// needs SOME identity — a buildFromGit-provisioned container has no
// ~/.gitconfig and no GIT_AUTHOR_*/GIT_COMMITTER_* env (unlike a
// self-deploy container, which InitServiceGit seeds), so relying on
// ambient config fails there with "unable to auto-detect email address".
const (
	ledgerIdentityName  = "Zerops Agent"
	ledgerIdentityEmail = "agent@zerops.io"
)

// WriteLedger records one zcp deploy: creates/overwrites ONE annotated git
// tag — topology.DeployTagName(entry.Project, entry.Target,
// entry.AppVersionID) — pointing at entry.SHA, whose message is entry's
// single-line JSON. No commit-tree, no update-ref, no working-tree change,
// HEAD untouched (docs/spec-workflows.md §4.9). `-f` because a retried
// deploy of the same appVersion must not fail on an already-existing tag.
func WriteLedger(ctx context.Context, r Runner, dir string, entry topology.LedgerEntry) error {
	msg, err := json.Marshal(entry)
	if err != nil {
		return wrapErr("marshal ledger entry", err, "")
	}
	tag := topology.DeployTagName(entry.Project, entry.Target, entry.AppVersionID)
	script := "git -c user.name=" + shellQuote(ledgerIdentityName) +
		" -c user.email=" + shellQuote(ledgerIdentityEmail) +
		" tag -a -f -m " + shellQuote(string(msg)) + " " + shellQuote(tag) + " " + shellQuote(entry.SHA)
	if _, stderr, err := r.Run(ctx, dir, script); err != nil {
		return wrapErr("tag "+tag, err, stderr)
	}
	return nil
}

// LastDeployOnRecord returns the newest zcp deploy on record for
// (projectID, target) — the newest tag (by tagger date) under
// topology.DeployTagPrefix(projectID, target), its JSON message decoded
// back into a LedgerEntry. This is the ledger join's right-hand side
// (docs/spec-workflows.md §4.9): "what runs" is (platform's active
// appVersion) JOIN (tag of that name), never a moving pointer.
// ok=false with no error when nothing is on record yet — a target that
// never received a zcp deploy is not itself a failure.
func LastDeployOnRecord(ctx context.Context, r Runner, dir, projectID, target string) (entry topology.LedgerEntry, ok bool, err error) {
	pattern := "refs/tags/" + topology.DeployTagPrefix(projectID, target)
	script := "git for-each-ref --sort=-taggerdate --count=1 --format='%(*objectname)%09%(contents:subject)' " + shellQuote(pattern)
	out, stderr, runErr := r.Run(ctx, dir, script)
	if runErr != nil {
		return topology.LedgerEntry{}, false, wrapErr("read ledger for "+target, runErr, stderr)
	}
	line := strings.TrimSpace(out)
	if line == "" {
		return topology.LedgerEntry{}, false, nil
	}
	parts := strings.SplitN(line, "\t", 2)
	if len(parts) != 2 {
		return topology.LedgerEntry{}, false, fmt.Errorf("malformed ledger tag line: %q", line)
	}
	var decoded topology.LedgerEntry
	if jsonErr := json.Unmarshal([]byte(parts[1]), &decoded); jsonErr != nil {
		return topology.LedgerEntry{}, false, fmt.Errorf("decode ledger tag message: %w", jsonErr)
	}
	return decoded, true, nil
}
