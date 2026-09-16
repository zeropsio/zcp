package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// The pull request that lands a Mate's branch on its repository's protected
// `main` (guide 2.1). `main` takes no direct push from anyone, so the request
// is not a courtesy — it is the ONLY way the pair's code reaches the branch
// the group's environments are fed from.
//
// It has two triggers because A1 cannot be one of them: A1 wires the
// repository and creates `mate/{bot}` LOCALLY, and Gitea refuses a request
// whose head it cannot resolve, so the branch does not exist anywhere Gitea
// can see until the pair's first deploy pushes it. So:
//
//   - the git-push deploy opens it, right after the push that created the
//     branch on the remote;
//   - a later Gitea reconcile pass opens it for a Mate that pushed before
//     this existed, or whose push-time call failed.
//
// Both go through openGiteaPairPullRequest, and both record the number on the
// pair. The record is the idempotence: a pair that has one never makes Gitea
// answer about it again.

// giteaPullRequestRef is what a caller reports about the request. Number is
// never 0 in a returned value — nil says "there is none", and a zero-numbered
// one would read as an opened request with no address.
type giteaPullRequestRef struct {
	Repo    string `json:"repo"`
	Branch  string `json:"branch"`
	Base    string `json:"base"`
	Number  int    `json:"number"`
	Created bool   `json:"created"`
	URL     string `json:"url,omitempty"`
}

// giteaPairPullRequestTitle heads the request a pair's branch lands through.
func giteaPairPullRequestTitle(hostname string) string {
	return "Mate: " + hostname
}

// openGiteaPairPullRequest opens the pair's request when none is open, finds
// the open one when there is, and records its number on the pair either way.
// Returns nil when there is nothing to report — no wiring, no repository, or
// a Gitea that could not answer. Never an error: every caller is a
// best-effort hook on a path that has already succeeded, and a request that
// could not be opened is retried by the next reconcile pass.
func openGiteaPairPullRequest(
	ctx context.Context,
	httpClient ops.HTTPDoer,
	wiring ops.GiteaWiring,
	stateDir string,
	m *workflow.ServiceMeta,
) *giteaPullRequestRef {
	if httpClient == nil || !wiring.Ready() || m == nil || m.Gitea == nil {
		return nil
	}
	repo, branch := m.Gitea.FullName, m.Gitea.Branch
	if repo == "" || branch == "" {
		return nil
	}
	base := m.Gitea.DefaultBranch
	if base == "" {
		base = giteaProtectedBase
	}

	number, created, err := ops.EnsureGiteaPullRequest(
		ctx, httpClient, wiring.GiteaURL, wiring.Token, repo, repo, branch, base,
		giteaPairPullRequestTitle(m.Hostname),
	)
	if err != nil || number == 0 {
		return nil
	}
	recordGiteaPullRequest(stateDir, m, number)
	return &giteaPullRequestRef{
		Repo:    repo,
		Branch:  branch,
		Base:    base,
		Number:  number,
		Created: created,
		URL:     fmt.Sprintf("%s/%s/pulls/%d", strings.TrimRight(wiring.GiteaURL, "/"), repo, number),
	}
}

// recordGiteaPullRequest stamps the number on the pair, in memory and on
// disk. Best-effort on disk: a pass that could not write it opens the same
// request again next time and finds it, which costs a read and nothing else.
func recordGiteaPullRequest(stateDir string, m *workflow.ServiceMeta, number int) {
	m.Gitea.PullRequest = number
	_ = workflow.UpsertServiceMeta(stateDir, m.Hostname, func(meta *workflow.ServiceMeta, existed bool) error {
		if !existed || meta.Gitea == nil {
			return nil
		}
		meta.Gitea.PullRequest = number
		return nil
	})
}

// giteaPullRequestAfterPush is the deploy-side trigger: the push just put the
// Mate's branch on the remote, and the Mate is done — nothing else in the
// session will open the request that lands it.
//
// Gated on the remote actually being the account's Gitea, not merely on the
// pair carrying a Gitea record: an agent may push a wired pair to a remote of
// its own with an explicit remoteUrl, and that push is not something to
// propose to the group's repository.
func giteaPullRequestAfterPush(
	ctx context.Context,
	httpClient ops.HTTPDoer,
	stateDir, hostname, remoteURL string,
) *giteaPullRequestRef {
	meta, _ := workflow.FindServiceMeta(stateDir, hostname)
	if meta == nil || meta.Gitea == nil || meta.Gitea.FullName == "" {
		return nil
	}
	wiring := ops.ReadGiteaWiring(giteaEnvLookup(mate.LiveEnvStorePath))
	if !wiring.Ready() {
		return nil
	}
	if topology.ClassifyGitHost(remoteURL, wiring.GiteaURL) != topology.GitHostGitea {
		return nil
	}
	return openGiteaPairPullRequest(ctx, httpClient, wiring, stateDir, meta)
}

// reconcileGiteaPairPullRequest is the catch-up trigger, for a pair A1
// already finished with. It reads the remote first: Gitea refuses a request
// whose head it cannot resolve, so asking before the pair has ever pushed
// would turn the ordinary state — wired, not yet deployed — into a reported
// failure on every pass.
//
// Returns "" when there is nothing worth saying; the caller records the
// attempt either way, so a pair that never pushes is probed on a backoff
// rather than on every agent tool call.
func reconcileGiteaPairPullRequest(
	ctx context.Context,
	httpClient ops.HTTPDoer,
	stateDir string,
	wiring ops.GiteaWiring,
	m *workflow.ServiceMeta,
) string {
	if !wiring.Ready() {
		// A1 could not have wired this pair without the variables, so this is
		// unreachable in practice; saying nothing beats a second copy of the
		// waiting line the repository pass already emits.
		return ""
	}
	onRemote, err := ops.GiteaBranchExists(ctx, httpClient, wiring.GiteaURL, wiring.Token, m.Gitea.FullName, m.Gitea.Branch)
	if err != nil {
		return fmt.Sprintf("could not read whether %s is on %s (%v) — the pull request is retried on the next pass.",
			m.Gitea.Branch, m.Gitea.FullName, err)
	}
	if !onRemote {
		// The ordinary state between bootstrap and the first deploy.
		return ""
	}
	ref := openGiteaPairPullRequest(ctx, httpClient, wiring, stateDir, m)
	if ref == nil {
		return fmt.Sprintf("%s is on %s but no pull request could be opened onto %q — retrying on the next pass.",
			m.Gitea.Branch, m.Gitea.FullName, giteaBaseOf(m))
	}
	verb := "tracked by"
	if ref.Created {
		verb = "opened as"
	}
	return fmt.Sprintf("%s on %s is %s pull request #%d onto %q", ref.Branch, ref.Repo, verb, ref.Number, ref.Base)
}

// giteaBaseOf is the branch a pair's pull request targets.
func giteaBaseOf(m *workflow.ServiceMeta) string {
	if m != nil && m.Gitea != nil && m.Gitea.DefaultBranch != "" {
		return m.Gitea.DefaultBranch
	}
	return giteaProtectedBase
}
