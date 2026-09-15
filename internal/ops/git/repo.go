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
	// AdoptCaseSnapshot means AdoptBaseline found no content (no repo, an
	// unborn HEAD, or a HEAD over the empty tree) and minted a commit from
	// whatever it found on disk.
	AdoptCaseSnapshot AdoptCase = "snapshot"
)

// AdoptResult is AdoptBaseline's outcome.
type AdoptResult struct {
	Case AdoptCase
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

// ExcludeSeedFragment appends missing patterns without replacing user exclusions.
// A leading newline also preserves the final pattern in files without a newline.
func ExcludeSeedFragment(class topology.RuntimeClass) string {
	patterns := ExcludePatterns(class)
	parts := make([]string, 0, len(patterns)+1)
	parts = append(parts, "mkdir -p .git/info && touch .git/info/exclude")
	for _, pattern := range patterns {
		q := shellQuote(pattern)
		parts = append(parts, "(grep -qxF -- "+q+" .git/info/exclude || printf '\\n%s\\n' "+q+" >> .git/info/exclude)")
	}
	return strings.Join(parts, " && ")
}

// SeedExclude preserves existing patterns and appends the runtime defaults.
func SeedExclude(ctx context.Context, r Runner, dir string, class topology.RuntimeClass) error {
	if _, stderr, err := r.Run(ctx, dir, ExcludeSeedFragment(class)); err != nil {
		return wrapErr("seed .git/info/exclude", err, stderr)
	}
	return nil
}

// AdoptBaseline preserves an existing content HEAD. When the repository has no
// content history it commits a snapshot of the files found on disk, with robot
// attribution. This snapshot does not claim to have built the running appVersion.
// It never creates, moves, or deletes tags. The caller records adoption metadata.
func AdoptBaseline(ctx context.Context, r Runner, dir, appVersionID string, class topology.RuntimeClass) (AdoptResult, error) {
	exists, hasContent := probeContentCase(ctx, r, dir)

	if exists && hasContent {
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
	if err := commitSnapshot(ctx, r, dir, appVersionID); err != nil {
		return AdoptResult{}, err
	}
	return AdoptResult{Case: AdoptCaseSnapshot}, nil
}

// commitSnapshot stages everything in dir and commits it with the robot
// identity inlined via per-invocation `git -c` (never persisted config —
// it's ZCP's commit, not the user's, same posture as gitHeadEnsureFragment
// in ops/git_identity.go). Falls back to --allow-empty when the normal
// commit fails because nothing was staged (an empty working tree) — a
// genuine commit failure surfaces from that second attempt instead.
func commitSnapshot(ctx context.Context, r Runner, dir, appVersionID string) error {
	if _, stderr, addErr := r.Run(ctx, dir, "git add -A"); addErr != nil {
		return wrapErr("git add -A", addErr, stderr)
	}
	msg := "zcp: snapshot of " + dir + " as found at adopt (appVersion " + appVersionID + ")"
	commitFlags := "-c user.name=" + shellQuote(robotIdentityName) + " -c user.email=" + shellQuote(robotIdentityEmail)
	if _, _, commitErr := r.Run(ctx, dir, "git "+commitFlags+" commit -q -m "+shellQuote(msg)); commitErr != nil {
		if _, stderr, emptyErr := r.Run(ctx, dir, "git "+commitFlags+" commit -q --allow-empty -m "+shellQuote(msg)); emptyErr != nil {
			return wrapErr("commit snapshot (allow-empty fallback)", emptyErr, stderr)
		}
	}
	return nil
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
