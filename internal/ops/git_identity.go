package ops

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

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

// gitHeadCreateBody is the compound command that runs ONLY when HEAD is
// unborn (the right-hand side of gitHeadEnsureFragment's `||`): it decides
// which tree the marker commit is built from — the lone .gitignore blob
// when one already sits in the working directory (freshly written by
// gitignoreEnsureFragment moments earlier in the same chain, or
// pre-existing from before this feature shipped), or the fully empty tree
// exactly as before (`git mktree </dev/null`) when none exists — then
// creates the commit and moves HEAD to it.
//
// When a .gitignore IS used, `git update-index --add --cacheinfo` stages
// that ONE path into the index at the same blob the commit just recorded.
// This is load-bearing, not cosmetic: `git commit-tree` + `git update-ref`
// alone never touch the index, so on a fresh repo (empty index) a tree
// that suddenly contains .gitignore would make `git status` report it as
// BOTH a phantom staged deletion (index says absent, HEAD says present)
// AND untracked (index has no entry, working tree has the file) —
// precisely the "add -A sees a clean tree" contract this feature promises,
// broken. `--cacheinfo` touches only the .gitignore path, so it can never
// disturb any OTHER file's staged state — including the exact scenario
// TestGitHeadEnsureFragment_UnbornRepoWithStagedFiles_NoStagedContentCommitted
// pins (a caller's own staged files on an unborn repo, which never reach
// this branch anyway: no .gitignore means the empty-tree branch runs,
// identical to the pre-existing behavior).
//
// Kept as its own Go-string-concatenated (not fmt.Sprintf'd) constant/
// value: it contains a literal `%s` inside the `git mktree` line's printf
// format (the tree entry's mode/type/sha/name shape, tab-separated), and
// running that text through Sprintf's own format-string parsing would
// require escaping it as `%%s` — an easy thing to get wrong at a distance
// from the reason for the escape.
func gitHeadCreateBody(commitTreeCall string) string {
	return `if test -e .gitignore; then ` +
		`gi_blob=$(git hash-object -w .gitignore); ` +
		`tree=$(printf '100644 blob %s\t.gitignore\n' "$gi_blob" | git mktree); ` +
		`git update-index --add --cacheinfo 100644,"$gi_blob",.gitignore; ` +
		`else tree=$(git mktree </dev/null); fi; ` +
		`git update-ref HEAD "$(` + commitTreeCall + `)"`
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
// object from a tree (see gitHeadCreateBody — empty, or the lone
// .gitignore blob when one is present) and points HEAD at it directly via
// `git update-ref` — when the tree is empty, neither step touches the
// index or the working tree, so staged/uncommitted user content is never
// at risk; when a .gitignore is committed, only that one index entry is
// added (see gitHeadCreateBody).
//
// The marker commit carries the robot identity inline via per-invocation
// `-c` — it's ZCP's commit, not the user's, regardless of what
// gitIdentityEnsureFragment wrote or left alone.
func gitHeadEnsureFragment() string {
	commitTreeCall := fmt.Sprintf(
		`git -c user.email=%s -c user.name=%s commit-tree "$tree" -m 'zcp init'`,
		shellQuote(DeployGitIdentity.Email), shellQuote(DeployGitIdentity.Name),
	)
	return fmt.Sprintf(
		`(git rev-parse -q --verify HEAD >/dev/null || { %s; })`,
		gitHeadCreateBody(commitTreeCall),
	)
}

// gitignoreEnsureFragment returns the shell fragment that writes a
// language-aware .gitignore into the working directory IFF none already
// exists — `test -e .gitignore` guards against ever overwriting a file the
// user (or an earlier recipe clone) already put there. Runs on every
// self-heal invocation, not only a fresh `git init`, so a service
// bootstrapped before this feature shipped gets backfilled the next time
// any git self-heal site (GitEnsureRepoHeadCommand, BuildGitOriginSyncCommand,
// BuildGitReconstructCommand) touches it.
//
// serviceType selects the per-language block via topology.GitignoreFor; an
// empty or unrecognized type still gets the base hygiene lines (never
// nothing) — callers without a service type on hand pass "".
func gitignoreEnsureFragment(serviceType string) string {
	lines := topology.GitignoreFor(serviceType)
	quoted := make([]string, len(lines))
	for i, line := range lines {
		quoted[i] = shellQuote(line)
	}
	return fmt.Sprintf(`test -e .gitignore || printf '%%s\n' %s > .gitignore`, strings.Join(quoted, " "))
}

// GitEnsureRepoHeadCommand composes the full self-heal chain — init-if-
// missing, set-if-absent identity, gitignore backfill, HEAD guarantee — as
// one standalone SSH command body rooted at workingDir. Single owner for
// the "commit-ready repo" invariant: InitServiceGit (bootstrap),
// buildSSHCommand's safety-net (deploy), and git-push-setup's pre-probe
// ensure all compose from this same function so the four guarantees can
// never drift out of step with each other.
//
// serviceType (a raw Zerops type identifier, e.g. "nodejs@22") selects the
// language-aware .gitignore body via gitignoreEnsureFragment; pass "" when
// the caller has no type on hand (still gets the base hygiene lines).
// gitignore-ensure runs AFTER identity and BEFORE the HEAD guarantee so
// that, on a genuinely fresh repo, the .gitignore it just wrote is already
// on disk when gitHeadEnsureFragment decides what tree to commit — landing
// it INSIDE the first commit rather than as an untracked file the user's
// own first `git add -A` would otherwise have to pick up separately.
func GitEnsureRepoHeadCommand(workingDir, serviceType string) string {
	return fmt.Sprintf("cd %s && (test -d .git || git init -q -b main) && %s && %s && %s",
		shellQuote(workingDir), gitIdentityEnsureFragment(), gitignoreEnsureFragment(serviceType), gitHeadEnsureFragment())
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
