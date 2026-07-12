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
	return gitIdentityEnsureFragmentFor(DeployGitIdentity)
}

// gitIdentityEnsureFragmentFor is gitIdentityEnsureFragment parameterized
// by the fallback identity to fill when absent. BuildGitReconstructCommand
// uses this directly (F3): a reconstruction with a GitHub-derived identity
// available should land the repo human-attributed from the first init,
// not robot-then-migrate; a reconstruction with no derived identity falls
// back to the package default via gitIdentityEnsureFragment. Same
// load-bearing shell properties as the zero-arg form (per-key grouping,
// value-non-emptiness probe) — this is the one implementation both wrap.
func gitIdentityEnsureFragmentFor(identity GitIdentity) string {
	email := shellQuote(identity.Email)
	name := shellQuote(identity.Name)
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

// Dispatch tokens emitted by BuildGitIdentitySeedCommand — ALWAYS exactly
// two lines of output, one token per line: line 1 is the email outcome,
// line 2 is the name outcome (fixed order, same "always exactly N lines"
// guarantee BuildGitIdentityReadCommand uses). Every branch of the
// generated shell terminates in exactly one of these three echoes per
// key, so a write failure can never fall through silently as if it were a
// clean preserve (Codex diff-review finding 2) — the caller must treat
// anything other than an exact, single recognized token per line
// (missing, duplicate, unrecognized, or a WriteFailed token) as an
// anomaly to report, never as a silent "preserved" claim.
const (
	GitIdentitySeedEmailSeeded      = "ZCP_EMAIL_SEEDED"
	GitIdentitySeedEmailPreserved   = "ZCP_EMAIL_PRESERVED"
	GitIdentitySeedEmailWriteFailed = "ZCP_EMAIL_WRITE_FAILED"
	GitIdentitySeedNameSeeded       = "ZCP_NAME_SEEDED"
	GitIdentitySeedNamePreserved    = "ZCP_NAME_PRESERVED"
	GitIdentitySeedNameWriteFailed  = "ZCP_NAME_WRITE_FAILED"
)

// BuildGitIdentitySeedCommand builds an SSH command that seeds identity
// into workingDir's git config IFF the CURRENT value is absent OR EXACTLY
// equals the robot identity (DeployGitIdentity) — the stomped-repo
// migration case (F3 item 2). A genuinely custom identity (present, not
// exactly-robot) is left untouched — this is deliberately NOT the same
// predicate as gitIdentityEnsureFragment's set-if-absent (which would
// never replace an already-present robot value); seeding is a one-time
// migration off the robot default, not just a fill-if-missing.
//
// user.email and user.name are decided independently (same per-key
// grouping rationale as gitIdentityEnsureFragment). Every per-key branch
// — seed-attempted-and-succeeded, seed-attempted-and-FAILED (Codex
// diff-review finding 2: the earlier shape let a `git config` write
// failure fall through with NO token at all, which the caller's
// Contains-based parse then silently misread as "preserved"), or
// left-untouched-as-custom — terminates in exactly ONE echo, so the
// command's stdout is ALWAYS exactly two lines regardless of outcome: the
// email token, then the name token. The caller parses positionally, not
// by loose substring search, so a write failure is always distinguishable
// from a clean preserve.
func BuildGitIdentitySeedCommand(workingDir string, identity GitIdentity) string {
	robotEmail := shellQuote(DeployGitIdentity.Email)
	robotName := shellQuote(DeployGitIdentity.Name)
	newEmail := shellQuote(identity.Email)
	newName := shellQuote(identity.Name)
	return fmt.Sprintf(
		`cd %s && cur_email=$(git config user.email); if [ -z "$cur_email" ] || [ "$cur_email" = %s ]; then git config user.email %s && echo %s || echo %s; else echo %s; fi; cur_name=$(git config user.name); if [ -z "$cur_name" ] || [ "$cur_name" = %s ]; then git config user.name %s && echo %s || echo %s; else echo %s; fi`,
		shellQuote(workingDir),
		robotEmail, newEmail, GitIdentitySeedEmailSeeded, GitIdentitySeedEmailWriteFailed, GitIdentitySeedEmailPreserved,
		robotName, newName, GitIdentitySeedNameSeeded, GitIdentitySeedNameWriteFailed, GitIdentitySeedNamePreserved,
	)
}

// BuildGitIdentityReadCommand builds a read-only SSH command that prints
// workingDir's current user.email then user.name, ALWAYS exactly two
// lines (an empty line when a key is absent) — `printf '%s\n' "$(...)"`
// captures git config's stdout (empty on absence) and still emits its own
// newline, unlike a bare `git config user.email` whose absent-key case
// produces NO output line at all and would silently shift a naive
// line-indexed parse. No state mutation. Used by git-push-setup's
// tokenless configured-recall path to detect a still-robot identity and
// prompt a one-time gitToken re-run to migrate attribution (F3 item 4) —
// a token-less call cannot derive anything itself, so it can only read
// and report, never seed.
func BuildGitIdentityReadCommand(workingDir string) string {
	return fmt.Sprintf(`cd %s && printf '%%s\n' "$(git config user.email)" && printf '%%s\n' "$(git config user.name)"`, shellQuote(workingDir))
}
