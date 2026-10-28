package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// What zcp edits in a person's repository unasked — the dev setup's two
// supervisor lines, the declaration of the server a start moved onto the
// supervisor — it lands as a commit of its own on the checked-out work branch
// (docs/spec-mate.md §2.8a.2): the edit then rides with that branch's change
// and main carries it once the person merges the change, so no file of zcp's
// stays modified in the checkout to block a branch switch or make a deploy
// unreproducible from git. It commits only when the commit would hold nothing
// but its own edit.

// OwnEditBase is a dev service's repository as it stood before zcp edited
// files there unasked.
type OwnEditBase struct {
	// Repo: the folder is a git work tree.
	Repo bool
	// Branch is the checked-out branch, "" when HEAD is detached.
	Branch string
	// Uncommitted are the asked-about paths that held uncommitted changes,
	// tracked or not, as `git status` names them.
	Uncommitted []string
}

// ownEditMark opens the lines zcp's own-edit scripts answer with.
const ownEditMark = "@@zcp-own-edit "

// ownEditCommitTimeout bounds zcp's commit of its own edit: no commit check of
// the repository runs (--no-verify), so it is git's own work on a few files.
const ownEditCommitTimeout = 30 * time.Second

// ReadOwnEditBase reads the repository in hostname's dir ("" is /var/www)
// before zcp edits paths there (relative to dir).
func ReadOwnEditBase(ctx context.Context, ssh SSHDeployer, hostname, dir string, paths []string) (OwnEditBase, error) {
	if dir == "" {
		dir = DevRepoRoot
	}
	cmd := "cd " + shellQuote(dir) + " 2>/dev/null && git rev-parse --is-inside-work-tree >/dev/null 2>&1 || { echo '" + ownEditMark + "base none'; exit 0; }; " +
		`echo "` + ownEditMark + `base branch $(git symbolic-ref -q --short HEAD)"; ` +
		"GIT_OPTIONAL_LOCKS=0 git status --porcelain --untracked-files=all -- " + quotedWords(paths)
	readCtx, cancel := context.WithTimeout(ctx, containerIdentityTimeout)
	defer cancel()
	out, err := ssh.ExecSSH(readCtx, hostname, cmd)
	if err != nil {
		return OwnEditBase{}, fmt.Errorf("read the repository in %s on %s: %s", dir, hostname, sshFailureText(err))
	}
	var base OwnEditBase
	marked := false
	for line := range strings.Lines(string(out)) {
		line = strings.TrimRight(line, "\r\n")
		if rest, ok := strings.CutPrefix(line, ownEditMark+"base "); ok {
			marked = true
			branch, isRepo := strings.CutPrefix(rest, "branch")
			base.Repo, base.Branch = isRepo, strings.TrimSpace(branch)
			continue
		}
		if marked && base.Repo && len(line) > 3 {
			base.Uncommitted = append(base.Uncommitted, line[3:])
		}
	}
	if !marked {
		return OwnEditBase{}, fmt.Errorf("read the repository in %s on %s: unreadable answer %q", dir, hostname, strings.TrimSpace(string(out)))
	}
	return base, nil
}

// CommitOwnEdit commits paths (relative to hostname's dir, "" is /var/www)
// as a commit of zcp's own on the checked-out branch and returns its short
// sha: only those paths, whatever else is staged; signed as zcp; without the
// repository's commit checks, which check the person's work — a deploy never
// waits on, or fails by, the app's typecheck for zcp's own lines.
func CommitOwnEdit(ctx context.Context, ssh SSHDeployer, hostname, dir string, paths []string, subject, body string) (string, error) {
	if dir == "" {
		dir = DevRepoRoot
	}
	// A deploy from this service runs git in the same tree.
	unlock, err := deploySourceGitLocks.lock(ctx, hostname)
	if err != nil {
		return "", err
	}
	defer unlock()
	q := quotedWords(paths)
	cmd := "cd " + shellQuote(dir) + " && git add -- " + q + " && git -c user.email=" + shellQuote(DeployGitIdentity.Email) +
		" -c user.name=" + shellQuote(DeployGitIdentity.Name) + " commit --no-verify -q -m " + shellQuote(subject) +
		" -m " + shellQuote(body) + " -- " + q + ` && echo "` + ownEditMark + `committed $(git rev-parse --short HEAD)"`
	commitCtx, cancel := context.WithTimeout(ctx, ownEditCommitTimeout)
	defer cancel()
	out, err := ssh.ExecSSH(commitCtx, hostname, cmd)
	if err != nil {
		return "", errors.New(sshFailureText(err))
	}
	for line := range strings.Lines(string(out)) {
		if sha, ok := strings.CutPrefix(strings.TrimSpace(line), ownEditMark+"committed "); ok && sha != "" {
			return sha, nil
		}
	}
	return "", fmt.Errorf("unreadable answer %q", strings.TrimSpace(string(out)))
}

// quotedWords is each of words single-quoted, space-separated.
func quotedWords(words []string) string {
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = shellQuote(w)
	}
	return strings.Join(quoted, " ")
}
