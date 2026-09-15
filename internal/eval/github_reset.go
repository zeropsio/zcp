package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// GitHubPATEnvVar names the environment variable carrying the GitHub
// personal access token a `gitRepoReset` scenario needs — both for the
// farm controller (which injects it into the run project as a sensitive
// env when a scenario's requiredEnvVars names it, internal/eval/farm) and
// for this package's own pre-seed/cleanup reset (docs/spec-eval-farm.md
// §3.3).
const GitHubPATEnvVar = "ZCP_E2E_GITHUB_PAT"

// githubResetBaselineMessage is both the commit message and the README.md
// body ResetGitHubRepo writes — a run reading it can tell at a glance the
// repo was reset by the farm, not left over from a prior run's own work.
const githubResetBaselineMessage = "farm baseline — reset by zcp eval farm"

// githubAPIBaseURL is the production GitHub REST API host; tests construct
// a githubResetClient directly with a httptest base instead of going
// through ResetGitHubRepo.
const githubAPIBaseURL = "https://api.github.com"

// ResetGitHubRepo resets repoURL (a `https://github.com/<owner>/<repo>`
// URL) to a clean single-commit baseline via the GitHub REST Git Data API
// (net/http; no gh, no git binary): a new parentless commit containing only
// README.md ("farm baseline — reset by zcp eval farm") force-replaces
// refs/heads/main, then every other branch and every tag is deleted. Used
// by `gitRepoReset` scenarios that share one GitHub repo across farm cells
// (docs/spec-eval-farm.md §3.3) — called before seed and again in cleanup.
// token is never included in any error this returns (it rides only the
// request's Authorization header, never echoed back).
func ResetGitHubRepo(ctx context.Context, repoURL, token string) error {
	owner, repo, err := parseGitHubRepo(repoURL)
	if err != nil {
		return err
	}
	c := &githubResetClient{base: githubAPIBaseURL, owner: owner, repo: repo, token: token, hc: http.DefaultClient}
	return c.reset(ctx)
}

// parseGitHubRepo extracts owner/repo from a `https://github.com/<owner>/<repo>`
// URL, tolerating a trailing slash or ".git" suffix.
func parseGitHubRepo(repoURL string) (owner, repo string, err error) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return "", "", fmt.Errorf("git reset: parse repo url %q: %w", repoURL, err)
	}
	if u.Host != "github.com" {
		return "", "", fmt.Errorf("git reset: repo url %q is not a github.com URL", repoURL)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("git reset: repo url %q: want https://github.com/<owner>/<repo>", repoURL)
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
}

// githubResetClient is ResetGitHubRepo's implementation, parameterized on
// base so tests substitute a httptest server for https://api.github.com.
type githubResetClient struct {
	base  string
	owner string
	repo  string
	token string
	hc    *http.Client
}

// reset runs the full sequence: build the baseline commit, force-update
// main to it, then delete every other branch and every tag.
func (c *githubResetClient) reset(ctx context.Context) error {
	commitSHA, err := c.createBaselineCommit(ctx)
	if err != nil {
		return err
	}
	if err := c.forceUpdateMain(ctx, commitSHA); err != nil {
		return err
	}
	if err := c.deleteOtherRefs(ctx, "heads"); err != nil {
		return err
	}
	return c.deleteOtherRefs(ctx, "tags")
}

func (c *githubResetClient) reposPath(suffix string) string {
	return fmt.Sprintf("/repos/%s/%s%s", c.owner, c.repo, suffix)
}

// createBaselineCommit builds a blob (README.md), a tree containing only
// that blob, and a parentless commit over that tree — the three-call Git
// Data API sequence for a from-scratch commit, never touching the working
// tree the platform's own clone-and-build path would otherwise see mid-reset.
func (c *githubResetClient) createBaselineCommit(ctx context.Context) (string, error) {
	var blob struct {
		SHA string `json:"sha"`
	}
	if err := c.request(ctx, http.MethodPost, c.reposPath("/git/blobs"), map[string]string{
		"content":  githubResetBaselineMessage + "\n",
		"encoding": "utf-8",
	}, &blob); err != nil {
		return "", err
	}

	var tree struct {
		SHA string `json:"sha"`
	}
	if err := c.request(ctx, http.MethodPost, c.reposPath("/git/trees"), map[string]any{
		"tree": []map[string]string{{
			"path": "README.md", "mode": "100644", "type": "blob", "sha": blob.SHA,
		}},
	}, &tree); err != nil {
		return "", err
	}

	var commit struct {
		SHA string `json:"sha"`
	}
	if err := c.request(ctx, http.MethodPost, c.reposPath("/git/commits"), map[string]any{
		"message": githubResetBaselineMessage,
		"tree":    tree.SHA,
		"parents": []string{},
	}, &commit); err != nil {
		return "", err
	}
	return commit.SHA, nil
}

// forceUpdateMain force-replaces refs/heads/main with sha — the run's new
// baseline commit becomes main's tip regardless of what main pointed to
// before.
func (c *githubResetClient) forceUpdateMain(ctx context.Context, sha string) error {
	return c.request(ctx, http.MethodPatch, c.reposPath("/git/refs/heads/main"), map[string]any{
		"sha": sha, "force": true,
	}, nil)
}

// deleteOtherRefs lists every ref under refs/<kind>/ (kind "heads" or
// "tags") and deletes each one except refs/heads/main — every tag is
// deleted regardless of name.
func (c *githubResetClient) deleteOtherRefs(ctx context.Context, kind string) error {
	var refs []struct {
		Ref string `json:"ref"`
	}
	if err := c.request(ctx, http.MethodGet, c.reposPath("/git/matching-refs/"+kind+"/"), nil, &refs); err != nil {
		return err
	}
	prefix := "refs/" + kind + "/"
	for _, r := range refs {
		name := strings.TrimPrefix(r.Ref, prefix)
		if kind == "heads" && name == "main" {
			continue
		}
		if err := c.request(ctx, http.MethodDelete, c.reposPath("/git/refs/"+kind+"/"+name), nil, nil); err != nil {
			return err
		}
	}
	return nil
}

// request issues one GitHub REST call, decoding a JSON response into out
// when non-nil. On a non-2xx status the returned error names the method,
// path (never the full URL with any query secrets, and the token never
// rides anywhere but the Authorization header this never echoes back),
// status code, and response body.
func (c *githubResetClient) request(ctx context.Context, method, path string, reqBody, out any) error {
	var bodyReader io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("git reset: encode %s %s body: %w", method, path, err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bodyReader)
	if err != nil {
		return fmt.Errorf("git reset: build request %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("git reset: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return fmt.Errorf("git reset: %s %s: read response: %w", method, path, readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("git reset: %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("git reset: %s %s: decode response: %w", method, path, err)
		}
	}
	return nil
}
