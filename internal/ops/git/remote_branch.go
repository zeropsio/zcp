package git

import (
	"context"
	"strings"
)

// remoteBranchMarker is CommitOnRemoteBranch's answer line when the commit is
// on the remote's branch.
const remoteBranchMarker = "ZCP:ON-REMOTE-BRANCH"

// CommitOnRemoteBranch reports whether sha is on remote's branch as the
// checkout at dir last saw it: its remote-tracking ref
// refs/remotes/<remote>/<branch> is sha or a descendant of it. It reads the
// checkout alone — no fetch, no ls-remote — so a branch the checkout never
// saw, a commit it cannot name, or one pushed from elsewhere since its last
// fetch reads false: false means "not shown to be on it", never proof that it
// is not. Read-only: rev-parse and merge-base take no lock.
func CommitOnRemoteBranch(ctx context.Context, r Runner, dir, remote, branch, sha string) bool {
	if r == nil || remote == "" || branch == "" || !fullSHA.MatchString(sha) {
		return false
	}
	script := "m=$(git rev-parse --verify -q " + shellQuote("refs/remotes/"+remote+"/"+branch+"^{commit}") + ") && " +
		"git merge-base --is-ancestor " + shellQuote(sha) + ` "$m" && echo ` + remoteBranchMarker
	out, _, err := r.Run(ctx, dir, script)
	if err != nil {
		return false
	}
	for line := range strings.Lines(out) {
		if strings.TrimSpace(line) == remoteBranchMarker {
			return true
		}
	}
	return false
}
