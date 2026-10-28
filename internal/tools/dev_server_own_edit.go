package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/ops"
)

// What zcp edits in a person's repository unasked — the dev setup's
// supervisor lines, a declaration a move onto the supervisor writes — it says
// once, naming the files and why, and lands as a commit of its own on the
// checkout's work branch (docs/spec-mate.md §2.8a.2 "Binary and delivery").
// That fits a change model where each branch is one change the person merges:
// the commit rides with the branch's change, apart from the person's own
// commits, and once merged main carries it, so no branch cut from main needs
// the edit again. Left uncommitted, the file blocks every branch switch that
// touches it and ships with every deploy from outside git.

// ownEdit is one edit zcp made unasked in hostname's repository.
type ownEdit struct {
	// host is the dev service; dir the folder the files are in, "" for
	// /var/www.
	host, dir string
	// files are the edited files, relative to dir.
	files []string
	// subject and body are the commit message.
	subject, body string
}

// ownEditPaths are the files zcp may edit unasked in a dev service's
// repository, read before it edits them.
var ownEditPaths = []string{"zerops.yaml", "zerops.yml", devsup.DeclarationPath}

// ownEditBase is the repository before zcp's edit; err when it was not read.
type ownEditBase struct {
	ops.OwnEditBase
	err error
}

// readOwnEditBase reads hostname's repository in dir before zcp edits files
// there.
func readOwnEditBase(ctx context.Context, ssh ops.SSHDeployer, hostname, dir string) ownEditBase {
	base, err := ops.ReadOwnEditBase(ctx, ssh, hostname, dir, ownEditPaths)
	return ownEditBase{OwnEditBase: base, err: err}
}

// supervisorEdit is the edit that puts setup on hostname under the
// supervisor download installs; more says what else it holds.
func supervisorEdit(hostname, dir, setup, download string, files []string, more string) ownEdit {
	body := "zcp-devsup is the dev-server supervisor zcp drives: run.prepareCommands installs it into the runtime image and run.start runs it. "
	if more != "" {
		body += more + " "
	}
	return ownEdit{host: hostname, dir: dir, files: files,
		subject: fmt.Sprintf("zcp: run setup %s under the dev-server supervisor (zcp-devsup %s)", setup, devsupRelease(download)),
		body:    body + "This commit holds only what zcp wrote."}
}

// repoDir is dir with "" read as /var/www.
func repoDir(dir string) string {
	if dir == "" {
		return ops.DevRepoRoot
	}
	return strings.TrimSuffix(dir, "/")
}

// defaultBranches are the branches only a merged change moves: zcp never
// commits there.
var defaultBranches = []string{"main", "master"}

// landOwnEdit commits e as a commit of zcp's own on the work branch base
// names, when that commit would hold nothing but zcp's edit, and says where
// the edit went: committed, or what to commit and why zcp did not.
func landOwnEdit(ctx context.Context, ssh ops.SSHDeployer, base ownEditBase, e ownEdit) string {
	files := strings.Join(e.files, " and ")
	it, they, is, blocks, ships, lands, own := "it", "it", "is", "blocks", "ships", "lands", "a commit of its own"
	if len(e.files) > 1 {
		it, they, is, blocks, ships, lands, own = "them", "they", "are", "block", "ship", "land", "one commit of their own"
	}
	dir := repoDir(e.dir)
	paths := strings.Join(e.files, " ")
	commit := fmt.Sprintf(`ssh %s "cd %s && git add -- %s && git commit -m '%s' -- %s"`, e.host, dir, paths, e.subject, paths)
	notCommitted := func(why, where string) string {
		return fmt.Sprintf("%s %s not committed: %s — commit %s as %s %s: %s. Left uncommitted, %s %s switching branches and %s with every deploy from outside git.",
			files, is, why, it, own, where, commit, they, blocks, ships)
	}
	const onWorkBranch = "on your work branch"
	switch {
	case base.err != nil:
		return notCommitted("zcp could not read the repository ("+base.err.Error()+")", onWorkBranch)
	case !base.Repo:
		return fmt.Sprintf("%s %s not committed: %s on %s is no git repository.", files, is, dir, e.host)
	case base.Branch == "":
		return notCommitted("HEAD is detached", onWorkBranch)
	case slices.Contains(defaultBranches, base.Branch):
		return notCommitted(fmt.Sprintf("the checkout is on %s, which only a merged change moves", base.Branch),
			"on the work branch you cut next (git checkout -b keeps it)")
	}
	if mixed := heldUncommitted(base.Uncommitted, e.files); len(mixed) > 0 {
		holder, holds := "it", "holds"
		if len(e.files) > 1 {
			holder = strings.Join(mixed, " and ")
		}
		if len(mixed) > 1 {
			holds = "hold"
		}
		return fmt.Sprintf("%s %s not committed: %s also %s uncommitted lines of yours — commit zcp's lines apart from yours, as a commit of their own: git commit -m '%s'.",
			files, is, holder, holds, e.subject)
	}
	sha, err := ops.CommitOwnEdit(ctx, ssh, e.host, e.dir, e.files, e.subject, e.body)
	if err != nil {
		return notCommitted("the commit failed: "+err.Error(), onWorkBranch)
	}
	return fmt.Sprintf("zcp committed %s on branch %s as a commit of its own (%s %q), so %s %s with that branch's change and main carries %s once the change is merged.",
		files, base.Branch, sha, e.subject, they, lands, it)
}

// heldUncommitted are the files among files that uncommitted names (paths as
// git status gives them, from the repository's root).
func heldUncommitted(uncommitted, files []string) []string {
	var held []string
	for _, f := range files {
		if slices.ContainsFunc(uncommitted, func(p string) bool { return p == f || strings.HasSuffix(p, "/"+f) }) {
			held = append(held, f)
		}
	}
	return held
}
