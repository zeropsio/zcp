package farm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval"
)

// RepoGCCandidate is one zcp-farm-*-prefixed GitHub repository `farm gc`
// considered, under the admin PAT's own account (GET /user/repos) — the
// FM-67 gitRepoCreate sibling to GCCandidate/GC above: same posture, listed
// here, deleted only by RepoGCApply, gated by the CLI's --yes.
type RepoGCCandidate struct {
	Owner  string
	Name   string
	Exempt string // "" = eligible; "too recent"/"unknown finish time" otherwise
}

// RepoGCOptions is the input to RepoGC.
type RepoGCOptions struct {
	// Token is ZCP_E2E_GITHUB_PAT_ADMIN. RepoGC no-ops (nil, nil) when
	// empty: repo GC is opt-in on the admin PAT's presence, never a hard
	// requirement of `farm gc`.
	Token string
	// OlderThan, when non-zero, exempts a repository until its own
	// pushed_at is at least this old (mirrors GCOptions.OlderThan's §3.6
	// FM-26 posture).
	OlderThan time.Duration
	Now       func() time.Time
	// ListRepos overrides eval.ListGitHubRepos — nil (the production
	// default) makes the real GitHub REST call; tests substitute a fake so
	// no network reaches GitHub.
	ListRepos func(ctx context.Context, token string) ([]eval.GitHubRepoSummary, error)
}

// RepoGC lists every zcp-farm-*-prefixed repository under opts.Token's own
// GitHub account and classifies each by its pushed_at age against
// opts.OlderThan (docs/spec-eval-farm.md §3.6 posture, mirrored for FM-67's
// gitRepoCreate sibling repos — §3.3). Read-only — it never deletes;
// RepoGCApply does that, through DeleteGitHubRepo, only for the candidates
// the caller chooses (the CLI's --yes gate).
func RepoGC(ctx context.Context, opts RepoGCOptions) ([]RepoGCCandidate, error) {
	if opts.Token == "" {
		return nil, nil
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	listRepos := opts.ListRepos
	if listRepos == nil {
		listRepos = eval.ListGitHubRepos
	}
	repos, err := listRepos(ctx, opts.Token)
	if err != nil {
		return nil, fmt.Errorf("farm gc: list repos: %w", err)
	}
	var candidates []RepoGCCandidate
	for _, r := range repos {
		if !strings.HasPrefix(r.Name, ProjectPrefix) {
			continue
		}
		c := RepoGCCandidate{Owner: r.Owner, Name: r.Name}
		if opts.OlderThan > 0 && !sufficientFinishAge(now(), r.PushedAt, opts.OlderThan) {
			c.Exempt = finishAgeExemption(now(), r.PushedAt)
		}
		candidates = append(candidates, c)
	}
	return candidates, nil
}

// RepoGCApply deletes every non-exempt candidate via deleteRepo (nil means
// the production default, eval.DeleteGitHubRepo — tests substitute a fake so
// no network reaches GitHub), returning one error per candidate that failed
// to delete.
func RepoGCApply(ctx context.Context, token string, candidates []RepoGCCandidate, deleteRepo func(ctx context.Context, owner, name, token string) error) []error {
	if deleteRepo == nil {
		deleteRepo = eval.DeleteGitHubRepo
	}
	var errs []error
	for _, c := range candidates {
		if c.Exempt != "" {
			continue
		}
		if err := deleteRepo(ctx, c.Owner, c.Name, token); err != nil {
			errs = append(errs, fmt.Errorf("farm gc: delete repo %s/%s: %w", c.Owner, c.Name, err))
		}
	}
	return errs
}
