package ops

import "fmt"

// gitIdentityEnsureFragment returns the shell fragment that sets
// user.email/user.name ONLY when currently absent — never stomping an
// operator-set identity. Per-key grouping is load-bearing: an ungrouped
// `a && b || c && d` fires `c` (and skips `d`) whenever `a` fails, because
// `&&`/`||` share precedence and associate left-to-right — that shape would
// force-overwrite user.name whenever the email branch's probe fails.
// Grouping each key in its own `(probe || set)` keeps the two keys
// independent.
//
// The probe checks VALUE non-emptiness (`test -n "$(...)"`), not exit code:
// `git config user.email` on a key set to the EMPTY string still exits 0
// (the probe would wrongly treat that as "present"), but a later commit
// dies with "empty ident not allowed" — `test -n` catches unset AND empty.
//
// Single owner: every site that must guarantee a commit-ready identity
// (InitServiceGit, buildSSHCommand's safety-net, BuildGitReconstructCommand,
// BuildGitOriginSyncCommand, git-push-setup's pre-probe ensure) composes
// from this one fragment, so the tell (what ZCP claims it does) and the
// check (what it actually runs) cannot drift.
func gitIdentityEnsureFragment() string {
	email := shellQuote(DeployGitIdentity.Email)
	name := shellQuote(DeployGitIdentity.Name)
	return fmt.Sprintf(
		`(test -n "$(git config user.email)" || git config user.email %s) && (test -n "$(git config user.name)" || git config user.name %s)`,
		email, name,
	)
}

// gitHeadEnsureFragment returns the shell fragment that guarantees a
// reachable HEAD without reading the index or the working tree. zcli's
// `--workspace-state all` archiver needs an existing HEAD (`git read-tree
// HEAD`) to snapshot a dirty tree; an unborn repo has none.
//
// `git commit --allow-empty` was rejected for this: on an unborn repo with
// STAGED files (`git add` already ran, no commit yet) it would commit
// whatever is currently in the index as the marker commit — a real hazard,
// not a hypothetical one. This fragment instead builds a parentless commit
// object from the EMPTY tree (`git mktree </dev/null` + `git commit-tree`)
// and points HEAD at it directly via `git update-ref` — neither step touches
// the index or the working tree, so staged/uncommitted user content is
// never at risk.
//
// The marker commit carries the robot identity inline via per-invocation
// `-c` — it's ZCP's commit, not the user's, regardless of what
// gitIdentityEnsureFragment wrote or left alone.
func gitHeadEnsureFragment() string {
	email := shellQuote(DeployGitIdentity.Email)
	name := shellQuote(DeployGitIdentity.Name)
	return fmt.Sprintf(
		`(git rev-parse -q --verify HEAD >/dev/null || git update-ref HEAD "$(git -c user.email=%s -c user.name=%s commit-tree "$(git mktree </dev/null)" -m 'zcp init')")`,
		email, name,
	)
}

// GitEnsureRepoHeadCommand composes the full self-heal chain — init-if-
// missing, set-if-absent identity, HEAD guarantee — as one standalone SSH
// command body rooted at workingDir. Single owner for the "commit-ready
// repo" invariant: InitServiceGit (bootstrap), buildSSHCommand's safety-net
// (deploy), and git-push-setup's pre-probe ensure all compose from this same
// function so the three guarantees can never drift out of step with each
// other.
func GitEnsureRepoHeadCommand(workingDir string) string {
	return fmt.Sprintf("cd %s && (test -d .git || git init -q -b main) && %s && %s",
		shellQuote(workingDir), gitIdentityEnsureFragment(), gitHeadEnsureFragment())
}
