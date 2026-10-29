package hq

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GitUser is the user git presents to HQ: `/git` reads Basic auth as
// `mate:<credential>`.
const GitUser = "mate"

// GitHubHost is the one git host HQ mints repository credentials for.
const GitHubHost = "github.com"

// GitHubHelperConfig is the git configuration that has `zcp hq git-credential`
// answer github.com: git names the repository to a helper only with
// useHttpPath, and HQ mints a token for one repository at a time. The empty
// helper first clears every helper the machine or the person configured, so
// none answers github.com ahead of it or keeps the minted token; running it
// again changes nothing.
const GitHubHelperConfig = "git config --replace-all credential.https://github.com.helper '' && git config --add credential.https://github.com.helper '!zcp hq git-credential' && git config credential.https://github.com.useHttpPath true"

// gitHubAnswerBound is how long the helper waits on HQ minting a GitHub
// token — HQ itself asks GitHub, with a bounded wait of its own; git waits on
// the helper, so an HQ that never answers is given up and said.
const gitHubAnswerBound = 30 * time.Second

// GitAnswer is the user and password the helper hands git.
type GitAnswer struct {
	Username string
	Password string
}

// DeclinedError is the helper answering git nothing, so git goes on to its
// next helper and never sends an empty password. Reason is said to the
// person on stderr; "" when the remote is simply none of this helper's.
type DeclinedError struct{ Reason string }

func (d *DeclinedError) Error() string {
	if d.Reason == "" {
		return "not a remote zcp answers for"
	}
	return d.Reason
}

// gitRequest is git's credential request: the remote's protocol, host and —
// with useHttpPath — the repository's path. Other attributes git sends
// (capability[], wwwauth[]) are not this helper's.
type gitRequest struct{ protocol, host, path string }

func readGitRequest(request io.Reader) gitRequest {
	var req gitRequest
	scanner := bufio.NewScanner(request)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		key, value, _ := strings.Cut(line, "=")
		switch key {
		case "protocol":
			req.protocol = value
		case "host":
			req.host = value
		case "path":
			req.path = value
		}
	}
	return req
}

// AnswerGit answers git's credential request (the attributes git writes to a
// helper's stdin; `zcp hq git-credential`) for the Mate that id names:
//   - HQ itself, by its scheme, host and port, gets the Mate's Git credential
//     under GitUser, read from the file id names at each request so
//     one HQ replaced over the link is the one used. Never the project key;
//   - a repository on https://github.com gets the token HQ mints for this
//     Mate and that repository at this moment (Client.GitHubCredential) —
//     never kept, never written anywhere but git's pipe;
//   - any other remote is declined.
func AnswerGit(ctx context.Context, request io.Reader, id Identity, httpClient Doer) (GitAnswer, error) {
	req := readGitRequest(request)
	if isHQ(req, id.Origin) {
		credential, err := ReadGitCredential(id.GitCredentialPath)
		if err != nil {
			return GitAnswer{}, &DeclinedError{Reason: err.Error()}
		}
		return GitAnswer{Username: GitUser, Password: credential}, nil
	}
	if !strings.EqualFold(req.protocol, "https") || hostPort(req.host, "https") != GitHubHost {
		return GitAnswer{}, &DeclinedError{}
	}
	if req.path == "" {
		return GitAnswer{}, &DeclinedError{Reason: "git named no repository on github.com, and HQ answers for one repository at a time: " + GitHubHelperConfig}
	}
	client, err := Open(httpClient, id)
	if err != nil {
		return GitAnswer{}, &DeclinedError{Reason: fmt.Sprintf("%v, so nothing mints a token for github.com/%s", err, req.path)}
	}
	answer, err := client.Once().Bounded(hqConnectBound, gitHubAnswerBound).GitHubCredential(ctx, req.path)
	if err != nil {
		return GitAnswer{}, &DeclinedError{Reason: gitHubRefusal(err, req.path)}
	}
	return answer, nil
}

// hqConnectBound is how long the helper waits for a connection to HQ.
const hqConnectBound = 5 * time.Second

// isHQ reports whether req names the HQ at address by its scheme, host and
// port.
func isHQ(req gitRequest, address string) bool {
	hq, err := url.Parse(address)
	if err != nil || hq.Host == "" || req.host == "" {
		return false
	}
	return strings.EqualFold(req.protocol, hq.Scheme) && hostPort(req.host, req.protocol) == hostPort(hq.Host, hq.Scheme)
}

// GitHubCredential is the token HQ mints for this Mate, at this call, for the
// GitHub repository repo (`<owner>/<repo>[.git]`, as git names it): one with
// write to a repository HQ mounts through its GitHub App, a read-only one for
// a repository the Mate only follows when HQ holds one. Never cached: each
// fetch and push asks again.
func (c Client) GitHubCredential(ctx context.Context, repo string) (GitAnswer, error) {
	var out struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	query := url.Values{"host": {GitHubHost}, "path": {repo}}
	if err := c.call.json(ctx, http.MethodGet, "/api/mate/git-credential?"+query.Encode(), c.authorization(), nil, &out); err != nil {
		return GitAnswer{}, err
	}
	if out.Password == "" {
		return GitAnswer{}, errors.New("HQ answered with no password")
	}
	return GitAnswer{Username: out.Username, Password: out.Password}, nil
}

// The access a token HQ mints for a GitHub repository carries, as the call
// asking needs it (mate-hq protocol S17a): read to fetch, write to push, and
// workflows to push commits that touch .github/workflows/ — which the caller
// checks before it pushes, as HQ cannot when it mints.
const (
	GitHubAccessRead      = "read"
	GitHubAccessWrite     = "write"
	GitHubAccessWorkflows = "workflows"
)

// GitHubCredentialFor is GitHubCredential carrying access on HQ's query —
// GitHubAccessRead, GitHubAccessWrite or GitHubAccessWorkflows — so the
// token carries no more; "" asks as GitHubCredential does, with none.
func (c Client) GitHubCredentialFor(ctx context.Context, repo, access string) (GitAnswer, error) {
	var out struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	query := url.Values{"host": {GitHubHost}, "path": {repo}}
	if access != "" {
		query.Set("access", access)
	}
	if err := c.call.json(ctx, http.MethodGet, "/api/mate/git-credential?"+query.Encode(), c.authorization(), nil, &out); err != nil {
		return GitAnswer{}, err
	}
	if out.Password == "" {
		return GitAnswer{}, errors.New("HQ answered with no password")
	}
	return GitAnswer{Username: out.Username, Password: out.Password}, nil
}

// AnswerGitFor is AnswerGit for a relayed `zcp git` call, asking HQ for a
// GitHub token carrying access (GitHubCredentialFor). HQ itself is answered
// as AnswerGit answers it — access means nothing for HQ's own git. HQ
// refusing the access (bad_access) or a push to a repository connected with
// a person's token (read_only_remote) is a decline naming it, never retried
// with another access. AnswerGit, the `zcp hq git-credential` fallback, asks
// with none.
func AnswerGitFor(ctx context.Context, request io.Reader, id Identity, httpClient Doer, access string) (GitAnswer, error) {
	req := readGitRequest(request)
	if isHQ(req, id.Origin) {
		credential, err := ReadGitCredential(id.GitCredentialPath)
		if err != nil {
			return GitAnswer{}, &DeclinedError{Reason: err.Error()}
		}
		return GitAnswer{Username: GitUser, Password: credential}, nil
	}
	if !strings.EqualFold(req.protocol, "https") || hostPort(req.host, "https") != GitHubHost {
		return GitAnswer{}, &DeclinedError{}
	}
	if req.path == "" {
		return GitAnswer{}, &DeclinedError{Reason: "git named no repository on github.com, and HQ answers for one repository at a time"}
	}
	client, err := Open(httpClient, id)
	if err != nil {
		return GitAnswer{}, &DeclinedError{Reason: fmt.Sprintf("%v, so nothing mints a token for github.com/%s", err, req.path)}
	}
	answer, err := client.Once().Bounded(hqConnectBound, gitHubAnswerBound).GitHubCredentialFor(ctx, req.path, access)
	if err != nil {
		return GitAnswer{}, &DeclinedError{Reason: gitHubAccessRefusal(err, req.path)}
	}
	return answer, nil
}

// gitHubAccessRefusal is gitHubRefusal knowing the refusals only an access
// asked for brings.
func gitHubAccessRefusal(err error, repo string) string {
	var refused *RefusedError
	if errors.As(err, &refused) {
		switch refused.Reason {
		case "read_only_remote":
			return fmt.Sprintf("github.com/%s is read-only for this Mate: HQ keeps it connected with a person's token and alone pushes there, so this push cannot be answered", repo)
		case "bad_access":
			return fmt.Sprintf("HQ refused the token access zcp asked for github.com/%s (bad_access)", repo)
		}
	}
	return gitHubRefusal(err, repo)
}

// GitHubAPIOrigin is GitHub's REST API, where a token's holder revokes it.
const GitHubAPIOrigin = "https://api.github.com"

// RevokeGitHubToken revokes token at the GitHub API at apiOrigin as HQ
// revokes the tokens it mints: `DELETE /installation/token`, authenticated by
// the token itself, so one minted for a single call does not outlive it.
// GitHub's 204 and its 401 (the token already gone) end it; anything else is
// an error, which never carries the token. ctx bounds it.
func RevokeGitHubToken(ctx context.Context, httpClient Doer, apiOrigin, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, strings.TrimRight(apiOrigin, "/")+"/installation/token", nil)
	if err != nil {
		return fmt.Errorf("revoke the GitHub token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "zcp")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("revoke the GitHub token: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, revokeAnswerLimit))
	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusUnauthorized:
		return nil
	}
	return fmt.Errorf("revoke the GitHub token: GitHub answered %d", resp.StatusCode)
}

// revokeAnswerLimit bounds what is read of GitHub's answer to a revoke.
const revokeAnswerLimit = 64 << 10

// gitHubRefusal is what HQ's answer for github.com/repo means to the person
// whose git asked.
func gitHubRefusal(err error, repo string) string {
	var refused *RefusedError
	switch {
	case IsUnavailable(err):
		return fmt.Sprintf("HQ did not answer with a token for github.com/%s; try again", repo)
	case !errors.As(err, &refused):
		return fmt.Sprintf("HQ gave no token for github.com/%s: %v", repo, err)
	case refused.Reason == "not_mounted":
		return fmt.Sprintf("HQ mounts no repository github.com/%s for this Mate's application, and holds no read token for it", repo)
	case refused.Reason == "no_access":
		return fmt.Sprintf("this Mate no longer has access to github.com/%s in HQ: its project's access is gone", repo)
	case refused.Reason == "app_not_installed":
		return fmt.Sprintf("the Zerops GitHub App no longer reaches github.com/%s; a person installs it on the repository again", repo)
	}
	return fmt.Sprintf("HQ refused a token for github.com/%s: %v", repo, err)
}

// hostPort is host lower-cased with the scheme's own port dropped, so
// "hq.example:443" and "hq.example" are one https host — git names the port
// when the remote URL does.
func hostPort(host, scheme string) string {
	host = strings.ToLower(host)
	if strings.EqualFold(scheme, "http") {
		return strings.TrimSuffix(host, ":80")
	}
	return strings.TrimSuffix(host, ":443")
}
