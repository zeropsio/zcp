package eval

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// GitHubAdminPATEnvVar names the environment variable carrying the
// administration-scoped GitHub PAT a `gitRepoCreate` scenario needs — same
// pass-through shape as GitHubPATEnvVar (the farm controller injects it into
// a run project only when the scenario names it in requiredEnvVars;
// wrapper.sh redacts it before upload), but with the broader scopes
// (Administration, Contents, Workflows, Secrets, Actions write) that
// creating and deleting a repository — and `farm gc`'s repo cleanup pass —
// need. The plain gitRepoReset PAT (GitHubPATEnvVar) is deliberately
// narrower and cannot create or delete a repository.
// docs/spec-eval-farm.md §2.4/§3.3.
const GitHubAdminPATEnvVar = "ZCP_E2E_GITHUB_PAT_ADMIN"

// CreateGitHubRepo creates a new, empty, private GitHub repository named
// name under owner and returns its `https://github.com/<owner>/<name>` URL
// (no .git suffix, taken from the response's own html_url). See
// githubResetClient.createRepo for the request shape.
func CreateGitHubRepo(ctx context.Context, owner, name, token string) (string, error) {
	c := &githubResetClient{base: githubAPIBaseURL, owner: owner, repo: name, token: token, hc: http.DefaultClient}
	return c.createRepo(ctx)
}

// DeleteGitHubRepo deletes owner/name — CreateGitHubRepo's cleanup half,
// called once the run is over in every verification mode.
func DeleteGitHubRepo(ctx context.Context, owner, name, token string) error {
	c := &githubResetClient{base: githubAPIBaseURL, owner: owner, repo: name, token: token, hc: http.DefaultClient}
	return c.deleteRepo(ctx)
}

// GitHubRepoSummary is one repository GET /user/repos returned to
// ListGitHubRepos.
type GitHubRepoSummary struct {
	Owner    string
	Name     string
	PushedAt time.Time
}

// ListGitHubRepos lists every repository under token's own GitHub account —
// for `farm gc`'s zcp-farm-*-prefixed repository cleanup pass
// (docs/spec-eval-farm.md §3.6 sibling to the project GC, FM-67's
// gitRepoCreate).
func ListGitHubRepos(ctx context.Context, token string) ([]GitHubRepoSummary, error) {
	c := &githubResetClient{base: githubAPIBaseURL, token: token, hc: http.DefaultClient}
	return c.listRepos(ctx)
}

// createRepo is CreateGitHubRepo's implementation, parameterized on c.base
// so tests substitute a httptest server (mirrors githubResetClient.reset for
// ResetGitHubRepo). The GitHub REST `POST /user/repos` endpoint always
// creates the repository under the authenticated PAT's own account — there
// is no destination-owner request parameter; c.owner only shapes the error
// message here, since the caller already knows which owner it asked for.
// auto_init is false: the repo starts with zero commits — "the user just
// created a repo" shape (docs/spec-eval-farm.md §3.3 FM-67 sibling),
// distinct from ResetGitHubRepo's shared repo, which always carries a
// baseline commit.
func (c *githubResetClient) createRepo(ctx context.Context) (string, error) {
	var created struct {
		HTMLURL string `json:"html_url"` //nolint:tagliatelle // upstream GitHub REST schema
	}
	if err := c.request(ctx, http.MethodPost, "/user/repos", map[string]any{
		"name":      c.repo,
		"private":   true,
		"auto_init": false,
	}, &created); err != nil {
		return "", err
	}
	if created.HTMLURL == "" {
		return "", fmt.Errorf("git create: repo %s/%s: response carried no html_url", c.owner, c.repo)
	}
	return created.HTMLURL, nil
}

// deleteRepo is DeleteGitHubRepo's implementation.
func (c *githubResetClient) deleteRepo(ctx context.Context) error {
	return c.request(ctx, http.MethodDelete, c.reposPath(""), nil, nil)
}

// listRepos is ListGitHubRepos's implementation: a paginated
// `GET /user/repos?affiliation=owner`. A repository with no parseable
// pushed_at gets the zero time — the caller's age check treats that as "not
// old enough to prove", never as "old".
func (c *githubResetClient) listRepos(ctx context.Context) ([]GitHubRepoSummary, error) {
	var all []GitHubRepoSummary
	const perPage = 100
	for page := 1; ; page++ {
		var repos []struct {
			Name     string `json:"name"`
			PushedAt string `json:"pushed_at"` //nolint:tagliatelle // upstream GitHub REST schema
			Owner    struct {
				Login string `json:"login"`
			} `json:"owner"`
		}
		path := fmt.Sprintf("/user/repos?affiliation=owner&per_page=%d&page=%d", perPage, page)
		if err := c.request(ctx, http.MethodGet, path, nil, &repos); err != nil {
			return nil, err
		}
		if len(repos) == 0 {
			break
		}
		for _, r := range repos {
			pushedAt, _ := time.Parse(time.RFC3339, r.PushedAt)
			all = append(all, GitHubRepoSummary{Owner: r.Owner.Login, Name: r.Name, PushedAt: pushedAt})
		}
		if len(repos) < perPage {
			break
		}
	}
	return all, nil
}
