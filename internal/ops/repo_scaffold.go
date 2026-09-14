package ops

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/ops/git"
	"github.com/zeropsio/zcp/internal/topology"
)

// AdoptRepoBaseline tags /var/www's current HEAD on hostname with the
// appVersionID-scoped baseline tag, initializing a repo first when one
// doesn't yet exist (docs/spec-workflows.md's Git Lifecycle section,
// GLC-7) — the container-side counterpart of ops/git.AdoptBaseline.
// Returns the topology.RepoProvenance the caller should persist: which
// AdoptBaseline case ran is exactly what determines it, so this converts
// git.AdoptResult.Case directly rather than making the caller re-derive it
// from platform facts (the old ClassifyProvenance placeholder this
// replaces read no such facts — see docs/spec-workflows.md §8 GLC-7).
func AdoptRepoBaseline(ctx context.Context, ssh SSHDeployer, hostname, appVersionID string, class topology.RuntimeClass) (topology.RepoProvenance, error) {
	if hostname == "" {
		return "", fmt.Errorf("AdoptRepoBaseline: hostname is required")
	}
	runner := git.SSHRunner{Executor: ssh, Hostname: hostname}
	result, err := git.AdoptBaseline(ctx, runner, defaultWorkingDir, appVersionID, class)
	if err != nil {
		return "", fmt.Errorf("adopt repo baseline on %s: %w", hostname, err)
	}
	if result.Case == git.AdoptCaseExisting {
		return topology.RepoProvenanceExisting, nil
	}
	return topology.RepoProvenanceSnapshot, nil
}

// RepoStatus is the live, per-service repo state exposed on the
// zerops_workflow envelope (docs/spec-workflows.md's Git Lifecycle
// section). Read fresh on every call — never cached on ServiceMeta or the
// bootstrap session.
type RepoStatus struct {
	Present  bool
	Head     string
	Baseline string
}

// ReadRepoStatus reads /var/www's current repo state on hostname over
// SSH: whether it's a repository at all (Present), its HEAD sha (Head,
// empty on an unborn or absent repo), and the appVersion id its
// zcp/baseline/* tag names, if any (Baseline — empty when no baseline tag
// exists, e.g. a bootstrapped-not-adopted service).
func ReadRepoStatus(ctx context.Context, ssh SSHDeployer, hostname string) (RepoStatus, error) {
	if hostname == "" {
		return RepoStatus{}, fmt.Errorf("ReadRepoStatus: hostname is required")
	}
	runner := git.SSHRunner{Executor: ssh, Hostname: hostname}
	head, err := git.ResolveSHA(ctx, runner, defaultWorkingDir, "HEAD")
	if err != nil {
		// No repo, or a repo with no reachable HEAD yet — either way,
		// "not present" from the envelope's point of view. Not itself an
		// error the caller needs to react to.
		return RepoStatus{}, nil //nolint:nilerr // absence is the expected, non-error outcome — see doc-comment
	}
	baseline := readBaselineTag(ctx, runner)
	return RepoStatus{Present: true, Head: head, Baseline: baseline}, nil
}

// LocalRepoHead reports whether dir (a local working directory, not a
// container) is already a git repository with a reachable HEAD, and its
// sha if so (docs/spec-workflows.md's Git Lifecycle section, GLC-6).
// Read-only — unlike ops.InitServiceGit's container-side self-heal, it
// never runs `git init`: dir is the user's own local checkout, not
// container state zcp owns, so the bootstrap checker only reports the gap
// and lets the agent run `git init` itself.
func LocalRepoHead(ctx context.Context, dir string) (present bool, head string) {
	head, err := git.ResolveSHA(ctx, git.LocalRunner{}, dir, "HEAD")
	if err != nil {
		return false, ""
	}
	return true, head
}

// readBaselineTag returns the appVersion id embedded in dir's
// zcp/baseline/<id> tag pointing at the current HEAD, or "" when none
// exists. Best-effort: a Runner failure (no such tag) just means no
// baseline was ever recorded — not an error worth propagating.
func readBaselineTag(ctx context.Context, runner git.Runner) string {
	out, _, err := runner.Run(ctx, defaultWorkingDir, "git tag --points-at HEAD --list 'zcp/baseline/*'")
	if err != nil {
		return ""
	}
	return topology.BaselineIDFromTag(out)
}
