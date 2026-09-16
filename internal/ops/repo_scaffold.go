package ops

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/ops/git"
	"github.com/zeropsio/zcp/internal/topology"
)

// AdoptRepoBaseline preserves /var/www's content HEAD as-is, or brings the
// repo to a commit-ready state without committing anything, initializing a
// repo first when one doesn't yet exist (docs/spec-workflows.md's Git
// Lifecycle section, GLC-7) — the container-side counterpart of
// ops/git.AdoptBaseline. Returns the topology.RepoProvenance the caller
// should persist: which AdoptBaseline case ran is exactly what determines
// it, so this converts git.AdoptResult.Case directly (docs/spec-
// workflows.md §8 GLC-7).
func AdoptRepoBaseline(ctx context.Context, ssh SSHDeployer, hostname string) (topology.RepoProvenance, error) {
	if hostname == "" {
		return "", fmt.Errorf("AdoptRepoBaseline: hostname is required")
	}
	runner := git.SSHRunner{Executor: ssh, Hostname: hostname}
	result, err := git.AdoptBaseline(ctx, runner, defaultWorkingDir)
	if err != nil {
		return "", fmt.Errorf("adopt repo baseline on %s: %w", hostname, err)
	}
	if result.Case == git.AdoptCaseExisting {
		return topology.RepoProvenanceExisting, nil
	}
	return topology.RepoProvenanceInitialized, nil
}

// RepoStatus is the live, per-service repo state exposed on the
// zerops_workflow envelope (docs/spec-workflows.md's Git Lifecycle
// section). Read fresh on every call — never cached on ServiceMeta or the
// bootstrap session.
type RepoStatus struct {
	Present bool
	Head    string
	// RepoState classifies the working tree at read time —
	// "clean" | "dirty" | "merging" | "rebasing" | "detached" (docs/
	// spec-workflows.md §12.6 GF-12). Empty when Present is false.
	RepoState string
}

// ReadRepoStatus reads whether /var/www has a reachable HEAD, its SHA, and
// its repo state, in one SSH round trip.
func ReadRepoStatus(ctx context.Context, ssh SSHDeployer, hostname string) (RepoStatus, error) {
	if hostname == "" {
		return RepoStatus{}, fmt.Errorf("ReadRepoStatus: hostname is required")
	}
	runner := git.SSHRunner{Executor: ssh, Hostname: hostname}
	// ReadHeadAndState never itself returns an error — no repo, or a repo
	// with no reachable HEAD yet, is "not present" from the envelope's
	// point of view, not a failure the caller needs to react to.
	head, state, ok, _ := git.ReadHeadAndState(ctx, runner, defaultWorkingDir)
	if !ok {
		return RepoStatus{}, nil
	}
	return RepoStatus{Present: true, Head: head, RepoState: state}, nil
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
