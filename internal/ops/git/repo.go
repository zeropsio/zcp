package git

import (
	"context"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

// emptyTreeSHA is git's well-known hash for the empty tree object — the
// exact HEAD^{tree} value ops.InitServiceGit's GLC-1 marker commit leaves
// behind on a service that had no repo before adopt (docs/spec-
// workflows.md's Git Lifecycle section, GLC-7). AdoptBaseline's content
// probe compares against this literal rather than trusting "a repo already
// exists" — an empty-tree HEAD is content-equivalent to no repo at all.
const emptyTreeSHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// snapshotIdentityName/snapshotIdentityEmail are the identity AdoptBaseline
// inlines onto its snapshot commit — matching ops.DeployGitIdentity (this
// package cannot import internal/ops; kept in sync by inspection, same
// values as ledger.go's ledgerIdentityName/ledgerIdentityEmail). A
// buildFromGit-provisioned container has no ~/.gitconfig and no
// GIT_AUTHOR_*/GIT_COMMITTER_* env, so relying on ambient config fails
// there with "unable to auto-detect email address".
const (
	snapshotIdentityName  = "Zerops Agent"
	snapshotIdentityEmail = "agent@zerops.io"
)

// AdoptCase records which branch AdoptBaseline took, so the caller can
// classify provenance from the return value alone — no separate probe of
// repo state needed (docs/spec-workflows.md's Git Lifecycle section, GLC-7).
type AdoptCase string

const (
	// AdoptCaseExisting means AdoptBaseline found a HEAD whose tree already
	// carried content — only the tag moved; no commit, no history change.
	AdoptCaseExisting AdoptCase = "existing"
	// AdoptCaseSnapshot means AdoptBaseline found no content (no repo, an
	// unborn HEAD, or a HEAD over the empty tree) and minted a commit from
	// whatever it found on disk before tagging.
	AdoptCaseSnapshot AdoptCase = "snapshot"
)

// AdoptResult is AdoptBaseline's outcome.
type AdoptResult struct {
	Case AdoptCase
	// EmptyCommit is true when the snapshot commit (AdoptCaseSnapshot only)
	// was minted with --allow-empty because `git add -A` staged nothing —
	// an empty working tree still gets tagged, so the case is recorded
	// rather than silently skipped.
	EmptyCommit bool
}

// baseExcludePatterns are seeded into .git/info/exclude for every runtime
// class — content that is never source (docs/spec-workflows.md's Git
// Lifecycle section, GLC-1). zcp seeds `.git/info/exclude`, NEVER a tracked
// `.gitignore` — the exclude file is repo-local and invisible to the
// user's own history.
var baseExcludePatterns = []string{".env", ".env.*", "*.log", ".zcp/"}

// codeExcludePatterns are added on top of baseExcludePatterns for runtime
// classes that actually run application code (dependencies + build
// outputs are regenerable, never worth tracking). Managed and unknown
// classes get the base set only — they have no application working tree
// to speak of.
var codeExcludePatterns = []string{"node_modules/", "dist/", "build/"}

// ExcludePatterns returns the .git/info/exclude content for a runtime
// class: base patterns (env files, logs, zcp's own state dir) for every
// class, plus dependency/build-output patterns for classes that run
// application code.
func ExcludePatterns(class topology.RuntimeClass) []string {
	switch class {
	case topology.RuntimeDynamic, topology.RuntimeStatic, topology.RuntimeImplicitWeb:
		out := make([]string, 0, len(codeExcludePatterns)+len(baseExcludePatterns))
		out = append(out, codeExcludePatterns...)
		out = append(out, baseExcludePatterns...)
		return out
	case topology.RuntimeManaged, topology.RuntimeUnknown:
		out := make([]string, len(baseExcludePatterns))
		copy(out, baseExcludePatterns)
		return out
	default:
		out := make([]string, len(baseExcludePatterns))
		copy(out, baseExcludePatterns)
		return out
	}
}

// SeedExclude (over)writes dir's .git/info/exclude with ExcludePatterns(class).
// Idempotent by construction — a re-run with the same class produces the
// same file content. info/exclude is repo-local (never shared, never
// tracked), so overwriting it on every call never clobbers anything a
// collaborator committed.
func SeedExclude(ctx context.Context, r Runner, dir string, class topology.RuntimeClass) error {
	patterns := ExcludePatterns(class)
	script := "mkdir -p .git/info && printf '%s\\n' " + shellQuote(strings.Join(patterns, "\n")) + " > .git/info/exclude"
	if _, stderr, err := r.Run(ctx, dir, script); err != nil {
		return wrapErr("seed .git/info/exclude", err, stderr)
	}
	return nil
}

// AdoptBaseline tags dir's current HEAD with the appVersion-scoped
// baseline tag (topology.BaselineTagName), deciding by CONTENT rather than
// by "is a repo already" (docs/spec-workflows.md's Git Lifecycle section,
// GLC-7) — the canonical `autoMountTargets` flow runs ops.InitServiceGit
// (GLC-1) first, which leaves a reachable HEAD over the EMPTY tree on a
// service that had no git before adopt; treating "a repo exists" as proof
// of content would tag that empty tree and call it a baseline.
//
//   - AdoptCaseExisting: HEAD^{tree} resolves to something other than the
//     empty tree — only the tag moves (force — re-adopting the same or a
//     newer appVersion must not fail on a pre-existing tag); no commit, no
//     history change. The existing HEAD is trusted as the baseline.
//   - AdoptCaseSnapshot: no repo, an unborn HEAD, or a HEAD over the empty
//     tree — init only if no repo exists at all (GLC-1 normally already
//     ran; AdoptBaseline is correct in either order), seed exclude, then
//     `git add -A` and a commit carrying the robot identity INLINE (this
//     package cannot import internal/ops for ops.DeployGitIdentity) with
//     message "zcp: snapshot of <dir> as found at adopt (appVersion <id>)"
//     — it is a snapshot of whatever adopt found on disk, never the source
//     commit of the running appVersion. An empty working tree (`git add
//     -A` stages nothing) still commits, with --allow-empty, so the case
//     is recorded rather than silently skipped (AdoptResult.EmptyCommit).
func AdoptBaseline(ctx context.Context, r Runner, dir, appVersionID string, class topology.RuntimeClass) (AdoptResult, error) {
	exists, hasContent := probeContentCase(ctx, r, dir)
	tag := topology.BaselineTagName(appVersionID)

	if exists && hasContent {
		if _, stderr, err := r.Run(ctx, dir, "git tag -f "+shellQuote(tag)+" HEAD"); err != nil {
			return AdoptResult{}, wrapErr("tag baseline", err, stderr)
		}
		return AdoptResult{Case: AdoptCaseExisting}, nil
	}

	if !exists {
		if _, stderr, err := r.Run(ctx, dir, "git init -q -b main"); err != nil {
			return AdoptResult{}, wrapErr("git init", err, stderr)
		}
	}
	if err := SeedExclude(ctx, r, dir, class); err != nil {
		return AdoptResult{}, err
	}
	emptyCommit, err := commitSnapshot(ctx, r, dir, appVersionID)
	if err != nil {
		return AdoptResult{}, err
	}
	if _, stderr, err := r.Run(ctx, dir, "git tag -f "+shellQuote(tag)+" HEAD"); err != nil {
		return AdoptResult{}, wrapErr("tag baseline", err, stderr)
	}
	return AdoptResult{Case: AdoptCaseSnapshot, EmptyCommit: emptyCommit}, nil
}

// commitSnapshot stages everything in dir and commits it with the robot
// identity inlined via per-invocation `git -c` (never persisted config —
// it's ZCP's commit, not the user's, same posture as gitHeadEnsureFragment
// in ops/git_identity.go). Falls back to --allow-empty when the normal
// commit fails because nothing was staged (an empty working tree) — a
// genuine commit failure surfaces from that second attempt instead.
func commitSnapshot(ctx context.Context, r Runner, dir, appVersionID string) (emptyCommit bool, err error) {
	if _, stderr, addErr := r.Run(ctx, dir, "git add -A"); addErr != nil {
		return false, wrapErr("git add -A", addErr, stderr)
	}
	msg := "zcp: snapshot of " + dir + " as found at adopt (appVersion " + appVersionID + ")"
	commitFlags := "-c user.name=" + shellQuote(snapshotIdentityName) + " -c user.email=" + shellQuote(snapshotIdentityEmail)
	if _, _, commitErr := r.Run(ctx, dir, "git "+commitFlags+" commit -q -m "+shellQuote(msg)); commitErr != nil {
		if _, stderr, emptyErr := r.Run(ctx, dir, "git "+commitFlags+" commit -q --allow-empty -m "+shellQuote(msg)); emptyErr != nil {
			return false, wrapErr("commit snapshot (allow-empty fallback)", emptyErr, stderr)
		}
		return true, nil
	}
	return false, nil
}

// probeIsRepo reports whether dir already has a .git directory. A Runner
// failure (script exits non-zero) means "not a repo" — the probe has no
// other failure mode worth distinguishing.
func probeIsRepo(ctx context.Context, r Runner, dir string) bool {
	_, _, err := r.Run(ctx, dir, "test -d .git")
	return err == nil
}

// probeContentCase reports whether dir is already a repo, and — only when
// it is — whether HEAD^{tree} resolves to something other than the empty
// tree. A repo that isn't a repo at all never gets the tree probe (exists
// implies it). An unborn HEAD (rev-parse fails) counts as "no content",
// same as an empty-tree HEAD — both mean AdoptBaseline has nothing trustworthy
// to tag as-is.
func probeContentCase(ctx context.Context, r Runner, dir string) (exists, hasContent bool) {
	exists = probeIsRepo(ctx, r, dir)
	if !exists {
		return false, false
	}
	out, _, err := r.Run(ctx, dir, "git rev-parse --verify "+shellQuote("HEAD^{tree}"))
	if err != nil {
		return true, false
	}
	return true, strings.TrimSpace(out) != emptyTreeSHA
}
