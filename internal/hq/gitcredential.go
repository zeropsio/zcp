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
// useHttpPath, and HQ mints a token for one repository at a time.
const GitHubHelperConfig = "git config credential.https://github.com.helper '!zcp hq git-credential' && git config credential.https://github.com.useHttpPath true"

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
// helper's stdin; `zcp hq git-credential`) as the Mate the enrollment at path
// holds, read at each request so a credential a re-enrollment rotated is the
// one used:
//   - the HQ this Mate enrolled with, by its scheme, host and port, gets the
//     Mate credential under GitUser;
//   - a repository on https://github.com gets the token HQ mints for this
//     Mate and that repository at this moment (Client.GitHubCredential) —
//     never kept, never written anywhere but git's pipe;
//   - any other remote is declined.
func AnswerGit(ctx context.Context, request io.Reader, path string, httpClient Doer) (GitAnswer, error) {
	req := readGitRequest(request)
	kept, found, err := LoadEnrollment(path)
	enrolled := err == nil && found && kept.Credential != ""
	if enrolled && isEnrolledHQ(req, kept.HQ) {
		return GitAnswer{Username: GitUser, Password: kept.Credential}, nil
	}
	if !strings.EqualFold(req.protocol, "https") || hostPort(req.host, "https") != GitHubHost {
		return GitAnswer{}, &DeclinedError{}
	}
	if req.path == "" {
		return GitAnswer{}, &DeclinedError{Reason: "git named no repository on github.com, and HQ answers for one repository at a time: " + GitHubHelperConfig}
	}
	if !enrolled {
		return GitAnswer{}, &DeclinedError{Reason: "this container is not enrolled with an HQ, so nothing mints a token for github.com/" + req.path}
	}
	client := Client{call: hqClient{http: httpClient, address: kept.HQ}, enrollment: kept}
	answer, err := client.Once().Bounded(hqConnectBound, gitHubAnswerBound).GitHubCredential(ctx, req.path)
	if err != nil {
		return GitAnswer{}, &DeclinedError{Reason: gitHubRefusal(err, req.path)}
	}
	return answer, nil
}

// hqConnectBound is how long the helper waits for a connection to HQ.
const hqConnectBound = 5 * time.Second

// isEnrolledHQ reports whether req names the HQ at address by its scheme,
// host and port.
func isEnrolledHQ(req gitRequest, address string) bool {
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
