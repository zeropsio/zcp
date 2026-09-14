package ops

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/ops/git"
	"github.com/zeropsio/zcp/internal/topology"
)

// EnsureScaffoldRepo guarantees /var/www on hostname is a git repository
// with a scaffold commit (docs/spec-workflows.md §4.10, G1) — the
// container-side counterpart of ops/git.InitRepo, run over SSH once the
// bootstrap scaffold has landed on disk. Idempotent: safe to call again
// on a service that already has a commit.
func EnsureScaffoldRepo(ctx context.Context, ssh SSHDeployer, hostname string, class topology.RuntimeClass) error {
	if hostname == "" {
		return fmt.Errorf("EnsureScaffoldRepo: hostname is required")
	}
	runner := git.SSHRunner{Executor: ssh, Hostname: hostname}
	if err := git.InitRepo(ctx, runner, defaultWorkingDir, class); err != nil {
		return fmt.Errorf("ensure scaffold repo on %s: %w", hostname, err)
	}
	return nil
}

// AdoptRepoBaseline tags /var/www's current HEAD on hostname with the
// appVersionID-scoped baseline tag, initializing a repo first when one
// doesn't yet exist (docs/spec-workflows.md §4.10, G2) — the container-
// side counterpart of ops/git.AdoptBaseline.
func AdoptRepoBaseline(ctx context.Context, ssh SSHDeployer, hostname, appVersionID string, class topology.RuntimeClass) (alreadyRepo bool, err error) {
	if hostname == "" {
		return false, fmt.Errorf("AdoptRepoBaseline: hostname is required")
	}
	runner := git.SSHRunner{Executor: ssh, Hostname: hostname}
	alreadyRepo, err = git.AdoptBaseline(ctx, runner, defaultWorkingDir, appVersionID, class)
	if err != nil {
		return alreadyRepo, fmt.Errorf("adopt repo baseline on %s: %w", hostname, err)
	}
	return alreadyRepo, nil
}

// RepoStatus is the live, per-service repo state exposed on the
// zerops_workflow envelope (docs/spec-workflows.md §4.10). Read fresh on
// every call — never cached on ServiceMeta or the bootstrap session.
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
// sha if so (docs/spec-workflows.md §4.10). Read-only — unlike
// EnsureScaffoldRepo, it never runs `git init`: dir is the user's own
// local checkout, not container state zcp owns, so the bootstrap checker
// only reports the gap and lets the agent run `git init` itself.
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
