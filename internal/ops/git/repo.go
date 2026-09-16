package git

import (
	"context"
	"fmt"
	"strings"
)

// emptyTreeSHA is git's well-known hash for the empty tree object — the
// exact HEAD^{tree} value a HEAD-ensure marker commit leaves behind on a
// service that had no repo before adopt (docs/spec-workflows.md's Git
// Lifecycle section, GLC-7). AdoptBaseline's content probe compares
// against this literal rather than trusting "a repo already exists" — an
// empty-tree HEAD is content-equivalent to no repo at all.
const emptyTreeSHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// Snapshot commits use the same robot identity as ops.DeployGitIdentity.
const (
	robotIdentityName  = "Zerops Agent"
	robotIdentityEmail = "agent@zerops.io"
)

// AdoptCase records which branch AdoptBaseline took, so the caller can
// classify provenance from the return value alone — no separate probe of
// repo state needed (docs/spec-workflows.md's Git Lifecycle section, GLC-7).
type AdoptCase string

const (
	// AdoptCaseExisting means AdoptBaseline found a HEAD whose tree already
	// carried content — no commit or history change.
	AdoptCaseExisting AdoptCase = "existing"
	// AdoptCaseInitialized means AdoptBaseline found no content (no repo,
	// an unborn HEAD, or a HEAD over the empty tree) and brought the repo
	// to the same commit-ready state bootstrap leaves a fresh service in:
	// init-if-missing, identity set-if-absent, an empty marker HEAD if
	// none was reachable. It never stages or commits the files it found.
	AdoptCaseInitialized AdoptCase = "initialized"
)

// AdoptResult is AdoptBaseline's outcome.
type AdoptResult struct {
	Case AdoptCase
}

// identityEnsureFragment sets user.email/user.name to the robot identity
// ONLY when currently absent — mirrors ops/git_identity.go's
// gitIdentityEnsureFragment (per-key grouping, value-non-emptiness probe).
// Duplicated rather than imported: this package must not import
// internal/ops (package doc above) — same reason shellQuote is
// duplicated in runner.go.
func identityEnsureFragment() string {
	return fmt.Sprintf(
		`(test -n "$(git config user.email)" || git config user.email %s) && (test -n "$(git config user.name)" || git config user.name %s)`,
		shellQuote(robotIdentityEmail), shellQuote(robotIdentityName),
	)
}

// headEnsureFragment guarantees a reachable HEAD without touching the
// index or the working tree, by pointing HEAD at a parentless commit over
// the empty tree when none is reachable — mirrors ops/git_identity.go's
// gitHeadEnsureFragment (duplicated for the same reason as
// identityEnsureFragment above).
func headEnsureFragment() string {
	return fmt.Sprintf(
		`(git rev-parse -q --verify HEAD >/dev/null || git update-ref HEAD "$(git -c user.email=%s -c user.name=%s commit-tree "$(git mktree </dev/null)" -m 'zcp init')")`,
		shellQuote(robotIdentityEmail), shellQuote(robotIdentityName),
	)
}

// AdoptBaseline preserves an existing content HEAD as-is. When the repository
// has no content history, it brings the repo to the same commit-ready state
// bootstrap leaves a fresh service in — init-if-missing, identity
// set-if-absent, an empty marker HEAD if none was reachable — WITHOUT
// staging or committing any of the files it found: the working tree stays
// exactly as adopt found it, uncommitted. zcp never commits user files
// (docs/spec-workflows.md's Git Lifecycle section, GLC-7). It never
// creates, moves, or deletes tags. The caller records adoption metadata.
func AdoptBaseline(ctx context.Context, r Runner, dir string) (AdoptResult, error) {
	exists, hasContent := probeContentCase(ctx, r, dir)

	if exists && hasContent {
		return AdoptResult{Case: AdoptCaseExisting}, nil
	}

	if !exists {
		if _, stderr, err := r.Run(ctx, dir, "git init -q -b main"); err != nil {
			return AdoptResult{}, wrapErr("git init", err, stderr)
		}
	}
	if _, stderr, err := r.Run(ctx, dir, identityEnsureFragment()); err != nil {
		return AdoptResult{}, wrapErr("ensure identity", err, stderr)
	}
	if _, stderr, err := r.Run(ctx, dir, headEnsureFragment()); err != nil {
		return AdoptResult{}, wrapErr("ensure HEAD", err, stderr)
	}
	return AdoptResult{Case: AdoptCaseInitialized}, nil
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
// to preserve as-is.
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
