package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// What zcp edits in a person's repository unasked — the dev setup's two
// supervisor lines, the declaration of the server a start moved onto the
// supervisor — it lands as a commit of its own on the checked-out work branch
// (docs/spec-mate.md §2.8a.2): the edit then rides with that branch's change
// and main carries it once the person merges the change, so no file of zcp's
// stays modified in the checkout to block a branch switch or make a deploy
// unreproducible from git. It commits only when the commit would hold nothing
// but its own edit, and only past the repository's own commit checks.

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
// sha: only those paths, whatever else is staged, signed as zcp. The
// repository's commit checks run — a person's hooks are their policy, never
// bypassed — within OwnEditCommitBudget; a check that refuses, or runs past
// the budget, leaves nothing committed (the paths stay staged) and its
// output's tail is the error.
func CommitOwnEdit(ctx context.Context, ssh SSHDeployer, hostname, dir string, paths []string, subject, body string) (string, error) {
	return commitOwnEdit(ctx, ssh, hostname, dir, paths, subject, body, OwnEditCommitBudget)
}

// OwnEditCommitBudget bounds zcp's commit of its own edit, the repository's
// commit checks included: the build-poll horizon (waitTotalBudget), as long as
// zcp waits on a build.
const OwnEditCommitBudget = waitTotalBudget

// ownEditBudgetMargin is how much longer zcp waits on the SSH call than the
// container's own bound on the commit, so the container's bound ends it.
const ownEditBudgetMargin = 30 * time.Second

// ownEditTimedOut are the exit codes of `timeout` ending a command it bounds:
// coreutils' 124, busybox's SIGTERM status.
var ownEditTimedOut = []string{"124", "143"}

func commitOwnEdit(ctx context.Context, ssh SSHDeployer, hostname, dir string, paths []string, subject, body string, budget time.Duration) (string, error) {
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
	// The container bounds the commit itself, so a check that runs past the
	// budget ends there and commits nothing later; `timeout` is coreutils' on
	// Ubuntu and busybox's on Alpine.
	secs := strconv.Itoa(max(1, int(budget.Seconds()))) // `timeout 0` bounds nothing
	cmd := "cd " + shellQuote(dir) + " && git add -- " + q + " && " +
		`t=; command -v timeout >/dev/null 2>&1 && t="timeout ` + secs + `"; ` +
		"$t git -c user.email=" + shellQuote(DeployGitIdentity.Email) + " -c user.name=" + shellQuote(DeployGitIdentity.Name) +
		" commit -q -m " + shellQuote(subject) + " -m " + shellQuote(body) + " -- " + q +
		`; c=$?; if [ "$c" -ne 0 ]; then echo "` + ownEditMark + `failed $c"; exit "$c"; fi; ` +
		`echo "` + ownEditMark + `committed $(git rev-parse --short HEAD)"`
	commitCtx, cancel := context.WithTimeout(ctx, budget+ownEditBudgetMargin)
	defer cancel()
	out, err := ssh.ExecSSH(commitCtx, hostname, cmd)
	if err != nil {
		return "", ownEditCommitFailure(err, out, budget)
	}
	for line := range strings.Lines(string(out)) {
		if sha, ok := strings.CutPrefix(strings.TrimSpace(line), ownEditMark+"committed "); ok && sha != "" {
			return sha, nil
		}
	}
	return "", fmt.Errorf("unreadable answer %q", strings.TrimSpace(string(out)))
}

// ownEditCommitFailure says why zcp's commit did not go through: the commit
// checks running past budget, else the tail of what the commit printed — a
// refusing hook's own reason — without zcp's marker line.
func ownEditCommitFailure(err error, out []byte, budget time.Duration) error {
	text := string(out)
	var sshErr *platform.SSHExecError
	if errors.As(err, &sshErr) && sshErr.Output != "" {
		text = sshErr.Output
	}
	var kept []string
	code := ""
	for line := range strings.Lines(text) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), ownEditMark+"failed "); ok {
			code = rest
			continue
		}
		kept = append(kept, strings.TrimRight(line, "\r\n"))
	}
	if slices.Contains(ownEditTimedOut, code) {
		return fmt.Errorf("the repository's commit checks ran past %s", budget)
	}
	if reason := boundedTail(strings.Join(kept, "\n")); reason != "" {
		return errors.New(reason)
	}
	return errors.New(sshFailureText(err))
}

// quotedWords is each of words single-quoted, space-separated.
func quotedWords(words []string) string {
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = shellQuote(w)
	}
	return strings.Join(quoted, " ")
}
