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
// answer github.com for a read: git names the repository to a helper only
// with useHttpPath, and HQ mints a token for one repository at a time. The
// empty helper first clears every helper the machine or the person
// configured, so none answers github.com ahead of it or keeps the minted
// token; running it again changes nothing. No pass writes it: a checkout's
// origin is its repository in HQ, and a checkout an older zcp wrote it into
// keeps it, where it brings a read token and nothing more.
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
//   - HQ itself, by its scheme, host and port, gets the Mate's project key
//     under GitUser (hqAnswer);
//   - a repository on https://github.com gets the read token HQ mints for
//     this Mate and that repository at this moment (Client.GitHubCredential)
//     — never kept, never written anywhere but git's pipe. It reads and
//     never pushes: HQ alone pushes to GitHub;
//   - any other remote is declined.
//
// A relayed `zcp git` call answers each of its requests with it too.
func AnswerGit(ctx context.Context, request io.Reader, id Identity, httpClient Doer) (GitAnswer, error) {
	req := readGitRequest(request)
	if isHQ(req, id.Origin) {
		return hqAnswer(id)
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

// hqAnswer is HQ's own git answered: the project key id carries — read from
// the live env store, so a regenerated key is the one presented — under
// GitUser, which HQ asks Zerops about on the push (docs/spec-mate.md §2.8).
// No key is a decline, so git never sends an empty password.
func hqAnswer(id Identity) (GitAnswer, error) {
	if id.Key == "" {
		return GitAnswer{}, &DeclinedError{Reason: fmt.Sprintf("%s is not in this container's environment, so git has nothing to present to HQ", EnvKey)}
	}
	return GitAnswer{Username: GitUser, Password: id.Key}, nil
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

// gitHubAccessRead is the one access zcp asks a GitHub token for (mate-hq
// protocol S17a): a Mate's container fetches from GitHub and never pushes
// there — HQ alone does, so no token that can write ever reaches it.
const gitHubAccessRead = "read"

// GitHubCredential is the read token HQ mints for this Mate, at this call,
// for the GitHub repository repo (`<owner>/<repo>[.git]`, as git names it): a
// repository HQ mounts through its GitHub App, or one the Mate only follows
// when HQ holds a read token for it. Asked with access=read, so the token
// can fetch and nothing else. Never cached: each call asks again.
func (c Client) GitHubCredential(ctx context.Context, repo string) (GitAnswer, error) {
	var out struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	query := url.Values{"host": {GitHubHost}, "path": {repo}, "access": {gitHubAccessRead}}
	if err := c.call.json(ctx, http.MethodGet, "/api/mate/git-credential?"+query.Encode(), c.authorization(), nil, &out); err != nil {
		return GitAnswer{}, err
	}
	if out.Password == "" {
		return GitAnswer{}, errors.New("HQ answered with no password")
	}
	return GitAnswer{Username: out.Username, Password: out.Password}, nil
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
