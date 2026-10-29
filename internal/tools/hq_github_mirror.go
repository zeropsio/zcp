package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A repository the organisation's GitHub app connects is mirrored by HQ
// itself (mate-hq protocol, GitHub "Main" and "Change branches"): HQ pushes
// `main` to GitHub's default branch while GitHub is behind, pushes each open
// change's head to its branch there, opens its pull request once it is
// described, and squash-merges it there when the person merges it in Mate.
// A Mate that syncs GitHub by hand — Filip on flip.studio cherry-picked
// every merged change onto GitHub's master in a clone of its own, as its
// memory said to — fights that follow: a commit on GitHub's default branch
// HQ's `main` lacks leaves neither holding the other, and HQ then moves
// neither (divergedSince). The pass says once that HQ mirrors a repository,
// and once that the two diverged, with the one change that brings them back
// in step (protocol "Realignment").

// realignBranch is the branch the realignment's change is cut on.
const realignBranch = "realign-github"

// hqMirrorPass is the GitHub mirror's turn of a repository pass, after its
// changes': for each repository of the application HQ holds the Mate in that
// the pass turns to (hqChangeRepos), what the agent was not told yet of its
// mirror (workflow.TellHQRepo) — that HQ mirrors it to GitHub now, and that
// GitHub's default branch and HQ's `main` diverged — each said once, and
// forgotten once it no longer holds, so a mirror connected again or a later
// divergence is said anew. The Mate's state names a repository's origin
// `github` exactly when the organisation's app connects its remote, so the
// application's repositories with their remotes (GET /api/apps/:appId/repos)
// are read only while the state names one so; a read HQ does not answer says
// nothing, and the next pass reads again.
func hqMirrorPass(ctx context.Context, stateDir string, hqc hq.Client, state hq.MateState) []string {
	if state.AppID == nil {
		return nil
	}
	app := *state.AppID
	mirrored := map[string]bool{}
	for _, r := range state.Repos {
		if r.Origin != nil && r.Origin.Mode == hq.OriginOnGitHub {
			mirrored[r.Name] = true
		}
	}
	repos := hqChangeRepos(stateDir, hqc.Address(), app, hqChanges(stateDir))
	var read []string
	for _, repo := range repos {
		if mirrored[repo] {
			read = append(read, repo)
			continue
		}
		// No longer mirrored, as the state says without a read: forgotten,
		// so a mirror connected again is said again.
		if workflow.ReadHQRepoTold(stateDir, app, repo) != (workflow.HQRepoTold{}) {
			_ = workflow.TellHQRepo(stateDir, app, repo, func(told *workflow.HQRepoTold) error {
				*told = workflow.HQRepoTold{}
				return nil
			})
		}
	}
	if len(read) == 0 {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, hqCallTimeout)
	listed, err := hqc.Once().AppRepos(callCtx, app)
	cancel()
	if err != nil {
		return nil
	}
	remotes := make(map[string]*hq.RepoRemote, len(listed))
	for _, r := range listed {
		remotes[r.Name] = r.Remote
	}
	var lines []string
	for _, repo := range read {
		remote, ok := remotes[repo]
		if !ok {
			continue
		}
		for _, line := range tellMirror(stateDir, hqc, app, repo, remote) {
			lines = append(lines, fmt.Sprintf("repository %q: %s", repo, line))
		}
	}
	return lines
}

// tellMirror is what the agent is told now of repo's remote: that HQ mirrors
// it to GitHub, when the organisation's app connects it and the agent was
// not told so of that GitHub repository; that GitHub and HQ diverged, when
// they did since a time the agent was not told of. What it says is recorded
// in the same hold; a record that cannot be written leaves it to be said
// again rather than never.
func tellMirror(stateDir string, hqc hq.Client, app, repo string, remote *hq.RepoRemote) []string {
	now := workflow.HQRepoTold{}
	if remote != nil && remote.Via == hq.RemoteViaApp {
		now = workflow.HQRepoTold{Mirror: gitHubRepoName(remote.URL), DivergedSince: remote.DivergedSince}
	}
	var lines []string
	_ = workflow.TellHQRepo(stateDir, app, repo, func(told *workflow.HQRepoTold) error {
		if *told == now {
			return workflow.ErrSkipWrite
		}
		if now.Mirror != "" && now.Mirror != told.Mirror {
			lines = append(lines, mirroredWords(now.Mirror))
		}
		if now.DivergedSince != "" && now.DivergedSince != told.DivergedSince {
			lines = append(lines, divergedWords(stateDir, hqc, app, repo, now.DivergedSince))
		}
		*told = now
		return nil
	})
	return lines
}

// mirroredWords is the pass's line for a repository HQ mirrors to GitHub
// now, name its GitHub repository as owner/name.
func mirroredWords(name string) string {
	return fmt.Sprintf("HQ mirrors it to GitHub now (%s), through the organisation's GitHub app: stop pushing to GitHub's default branch yourself, "+
		"and drop any note of your own that says to sync GitHub after a merge — HQ pushes `main` there itself, "+
		"and a merged change reaches GitHub as its pull request, squash-merged there. "+
		"Work still goes on a branch of your own, pushed to origin, its change opened with open-change.", name)
}

// divergedWords is the pass's line for a repository whose GitHub default
// branch and HQ's `main` diverged since since, with the one change that
// brings them back in step: a branch from GitHub's tip with HQ's `main`
// merged in, pushed to HQ — so HQ makes its branch on GitHub and pushes it,
// which the realignment reads (a branch HQ did not make there it never
// pushes) — opened as a change and merged in Mate; HQ squash-merges its pull
// request on GitHub and moves `main` onto that squash.
func divergedWords(_ string, hqc hq.Client, app, repo, since string) string {
	address, err := hqc.RepoURL(app, repo)
	if err != nil {
		address = "<its repository's address in HQ>"
	}
	return fmt.Sprintf("GitHub's default branch and HQ's `main` have diverged (since %s): neither holds the other, so HQ's follow moves neither. "+
		"One change brings them back in step — a branch holding both, which the person merges in Mate: HQ squash-merges its pull request on GitHub and moves `main` onto that squash. "+
		"In your own shell on the mounted checkout, its origin GitHub: `git fetch origin && git remote set-head origin --auto && git fetch %s +main:refs/remotes/hq/main`, "+
		"then `git checkout -b %s origin/HEAD && git merge refs/remotes/hq/main` — resolve any conflict so the branch keeps what both sides should, and commit — "+
		"then push it to HQ, never to GitHub (`git push %s %s`: HQ makes its branch on GitHub and pushes it, which the realignment needs), "+
		"open its change with zerops_workflow action=\"open-change\" branch=%q title=\"Bring HQ and GitHub back in step\" and describe it. "+
		"Never push GitHub's default branch and never force-push.",
		since, address, realignBranch, address, realignBranch, realignBranch)
}

// gitHubRepoName is a GitHub repository's address as owner/name, the
// address itself when it names none.
func gitHubRepoName(address string) string {
	u, err := url.Parse(address)
	if err != nil {
		return address
	}
	name := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	if name == "" {
		return address
	}
	return name
}
