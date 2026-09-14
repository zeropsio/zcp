package git

import (
	"context"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

// baseExcludePatterns are seeded into .git/info/exclude for every runtime
// class — content that is never source (docs/spec-workflows.md §4.10). zcp
// seeds `.git/info/exclude`, NEVER a tracked `.gitignore` — the exclude
// file is repo-local and invisible to the user's own history.
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

// InitRepo guarantees dir is a git repository with a scaffold commit,
// idempotently (docs/spec-workflows.md §4.10, G1). Called once bootstrap's
// scaffold has landed on disk:
//   - not yet a repo: `git init -q -b main`, seed the exclude file, then
//     (since a fresh init has no HEAD) `git add -A && git commit -m
//     "scaffold"` to commit whatever the scaffold wrote.
//   - already a repo: exclude is re-seeded (cheap, idempotent); if HEAD
//     already exists, InitRepo does nothing further — a prior call (or a
//     user's own git usage) already committed something, and InitRepo must
//     never create a second scaffold commit or touch history again.
func InitRepo(ctx context.Context, r Runner, dir string, class topology.RuntimeClass) error {
	isRepo := probeIsRepo(ctx, r, dir)
	if !isRepo {
		if _, stderr, err := r.Run(ctx, dir, "git init -q -b main"); err != nil {
			return wrapErr("git init", err, stderr)
		}
	}
	if err := SeedExclude(ctx, r, dir, class); err != nil {
		return err
	}
	if probeHasHEAD(ctx, r, dir) {
		return nil
	}
	if _, stderr, err := r.Run(ctx, dir, "git add -A && git commit -q -m "+shellQuote("scaffold")); err != nil {
		return wrapErr("commit scaffold", err, stderr)
	}
	return nil
}

// AdoptBaseline tags dir's current HEAD with the appVersion-scoped
// baseline tag (topology.BaselineTagName), guaranteeing a repo exists
// first (docs/spec-workflows.md §4.10, G2):
//   - not yet a repo: init + seed exclude + a baseline commit (message
//     records the adopted appVersion id), then tag it.
//   - already a repo: only the tag moves (force — re-adopting the same or
//     a newer appVersion must not fail on a pre-existing tag) — no commit,
//     no history change. The existing HEAD is trusted as the baseline.
//
// Returns whether dir was already a repo before this call, so the caller
// can classify how the tag's tree relates to what's deployed
// (topology.ClassifyProvenance is orthogonal — it reads platform facts,
// not repo state).
func AdoptBaseline(ctx context.Context, r Runner, dir, appVersionID string, class topology.RuntimeClass) (alreadyRepo bool, err error) {
	alreadyRepo = probeIsRepo(ctx, r, dir)
	if !alreadyRepo {
		if _, stderr, initErr := r.Run(ctx, dir, "git init -q -b main"); initErr != nil {
			return false, wrapErr("git init", initErr, stderr)
		}
		if seedErr := SeedExclude(ctx, r, dir, class); seedErr != nil {
			return false, seedErr
		}
		msg := "baseline: adopted appVersion " + appVersionID
		if _, stderr, commitErr := r.Run(ctx, dir, "git add -A && git commit -q -m "+shellQuote(msg)); commitErr != nil {
			return false, wrapErr("commit baseline", commitErr, stderr)
		}
	}
	tag := topology.BaselineTagName(appVersionID)
	if _, stderr, tagErr := r.Run(ctx, dir, "git tag -f "+shellQuote(tag)+" HEAD"); tagErr != nil {
		return alreadyRepo, wrapErr("tag baseline", tagErr, stderr)
	}
	return alreadyRepo, nil
}

// probeIsRepo reports whether dir already has a .git directory. A Runner
// failure (script exits non-zero) means "not a repo" — the probe has no
// other failure mode worth distinguishing.
func probeIsRepo(ctx context.Context, r Runner, dir string) bool {
	_, _, err := r.Run(ctx, dir, "test -d .git")
	return err == nil
}

// probeHasHEAD reports whether dir's repo has a reachable HEAD commit.
func probeHasHEAD(ctx context.Context, r Runner, dir string) bool {
	_, _, err := r.Run(ctx, dir, "git rev-parse -q --verify HEAD")
	return err == nil
}
