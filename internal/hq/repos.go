package hq

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// AppRepo is one of an application's repositories as HQ lists them
// (GET /api/apps/:appId/repos, which a Mate reads with its project key for
// its own application): its name and the remote connected to it, nil while
// HQ alone keeps the code.
type AppRepo struct {
	Name   string      `json:"name"`
	Remote *RepoRemote `json:"remote"`
}

// RepoRemote is a repository's connected remote, as HQ lists it: its https
// address, which never carries a credential; how HQ reaches it (Via); and,
// for one the organisation's GitHub app mirrors, since when GitHub's default
// branch and HQ's `main` have diverged — neither holding the other, so HQ's
// follow moves neither — "" while they are in step.
type RepoRemote struct {
	URL           string `json:"url"`
	Via           string `json:"via"`
	DivergedSince string `json:"divergedSince,omitempty"`
}

// The ways HQ reaches a repository's remote. Through the organisation's
// GitHub app HQ mirrors the repository: it pushes `main` to GitHub's default
// branch, each open change to its branch there, and opens each described
// change's pull request (mate-hq protocol, GitHub). Through a person's token
// it only follows it.
const (
	RemoteViaApp   = "app"
	RemoteViaToken = "token"
)

// appReposMaxPages bounds how many pages of an application's repositories a
// read follows: HQ answers up to 1000 a page.
const appReposMaxPages = 10

// AppRepos is every repository of the application appID with its remote,
// read page by page as HQ answers them.
func (c Client) AppRepos(ctx context.Context, appID string) ([]AppRepo, error) {
	var repos []AppRepo
	after := ""
	for range appReposMaxPages {
		path := "/api/apps/" + url.PathEscape(appID) + "/repos"
		if after != "" {
			path += "?after=" + url.QueryEscape(after)
		}
		var page struct {
			Repos []AppRepo `json:"repos"`
			Next  string    `json:"next"`
		}
		if err := c.call.json(ctx, http.MethodGet, path, c.authorization(), nil, &page); err != nil {
			return nil, fmt.Errorf("application repositories: %w", err)
		}
		repos = append(repos, page.Repos...)
		if page.Next == "" || page.Next == after {
			return repos, nil
		}
		after = page.Next
	}
	return repos, nil
}
