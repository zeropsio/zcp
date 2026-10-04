package hq

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// A Mate's changes in HQ (@t3tools/shared/hqChanges, which zcp reads as the
// spec): a change is HQ's record of the Mate's work toward `main` in one
// repository of its application, on the branch `mate/<project id>/<number>`;
// a Mate has at most one open change per repository. HQ refuses a push to a
// branch with no open change of its own, so a change is opened before its
// branch is pushed.

// Doer sends one HTTP request.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client speaks HQ's Mate API as the Mate the enrollment it was opened with
// holds: `Authorization: Mate <credential>`, always within the application
// HQ holds the Mate in.
type Client struct {
	call       hqClient
	enrollment Enrollment
}

// Open is a client of the HQ the enrollment at path names, with its
// credential; ErrNotEnrolled when there is no enrollment yet.
func Open(httpClient Doer, path string) (Client, error) {
	kept, found, err := LoadEnrollment(path)
	if err != nil {
		return Client{}, err
	}
	if !found {
		return Client{}, ErrNotEnrolled
	}
	return Client{call: hqClient{http: httpClient, address: kept.HQ}, enrollment: kept}, nil
}

// Once is this client sending each call once: a 503 is UnavailableError at
// once rather than waited out, for a caller that paces its own tries — a
// delivery, which fails fast rather than waiting HQ out.
func (c Client) Once() Client {
	c.call.once = true
	return c
}

// Bounded is this client ending each try of a call that has no connection
// within connect, TLS included, or no answer within answer, as an
// UnavailableError carrying a NoAnswerError — for a caller that fails fast
// rather than leave a call to the Doer's own, longer, timeouts. The connect
// bound reads the connection from net/http's trace.
func (c Client) Bounded(connect, answer time.Duration) Client {
	c.call.connect, c.call.answer = connect, answer
	return c
}

// Repo is a repository HQ keeps for an application, served at
// `/git/<appId>/<name>.git`.
type Repo struct {
	AppID string `json:"appId"`
	Name  string `json:"name"`
}

// EnsureRepo is the repository name in the Mate's application, made if new:
// its `main` then begins with HQ's commit of an empty tree.
func (c Client) EnsureRepo(ctx context.Context, name string) (Repo, error) {
	var repo Repo
	err := c.call.json(ctx, http.MethodPost, "/api/mate/repos", c.authorization(), map[string]string{"name": name}, &repo)
	return repo, err
}

// RepoExists reports whether HQ keeps the repository repo of the application
// appID, without making it: git's own first ask of a clone, as the Mate —
// found, or not found. Any other answer is an error, never "absent".
func (c Client) RepoExists(ctx context.Context, appID, repo string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.RepoURL(appID, repo)+"/info/refs?service=git-upload-pack", nil)
	if err != nil {
		return false, fmt.Errorf("hq git %s: %w", repo, err)
	}
	req.SetBasicAuth(GitUser, c.Credential())
	resp, err := c.call.http.Do(req)
	if err != nil {
		return false, &UnavailableError{Err: fmt.Errorf("hq git %s: %w", repo, err)}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, answerLimit))
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	case http.StatusServiceUnavailable:
		return false, &UnavailableError{Err: fmt.Errorf("hq git %s: HQ is not active", repo)}
	}
	return false, fmt.Errorf("hq git %s: HQ answered %d", repo, resp.StatusCode)
}

func (c Client) authorization() string { return "Mate " + c.enrollment.Credential }

// Address is the HQ's own address, without a trailing slash.
func (c Client) Address() string { return strings.TrimRight(c.enrollment.HQ, "/") }

// ProjectID is the Mate's own project: HQ's id of the Mate.
func (c Client) ProjectID() string { return c.enrollment.ProjectID }

// Credential is the Mate credential, the password git presents to HQ as
// GitUser. Never printed.
func (c Client) Credential() string { return c.enrollment.Credential }

// RepoURL is where git reaches the repository repo of the application appID.
func (c Client) RepoURL(appID, repo string) string { return RepoURLAt(c.Address(), appID, repo) }

// RepoURLAt is where git reaches the repository repo of the application
// appID at the HQ whose address is address.
func RepoURLAt(address, appID, repo string) string {
	return strings.TrimRight(address, "/") + "/git/" + url.PathEscape(appID) + "/" + url.PathEscape(repo) + ".git"
}

// ChangeURL is a change's own address at HQ, which HQ leads into the client:
// what zcp hands a person (hqChanges.ts changeUrl).
func (c Client) ChangeURL(appID, repo string, number int) string {
	return c.Address() + "/changes/" + url.PathEscape(appID) + "/" + url.PathEscape(repo) + "/" + strconv.Itoa(number)
}

// ChangeBranch is the branch of the Mate's change number: the one ref HQ lets
// it push, while the change is open, only forward.
func (c Client) ChangeBranch(number int) string {
	return "mate/" + c.enrollment.ProjectID + "/" + strconv.Itoa(number)
}

// AttachmentURL is where a picture HQ keeps at path is shown from.
func (c Client) AttachmentURL(path string) string { return c.Address() + path }

// The states of a change: open until it is merged into `main`, or closed
// without merging.
const (
	ChangeOpen   = "open"
	ChangeMerged = "merged"
	ChangeClosed = "closed"
)

// Change is a change as HQ records it (hqChanges.ts HqChange). Head is the
// commit its branch was last pushed to, nil until the first push lands; once
// merged, MergedSha is the squash on `main` and LandedHead the head it
// squashed.
type Change struct {
	AppID         string  `json:"appId"`
	Repo          string  `json:"repo"`
	Number        int     `json:"number"`
	MateProjectID string  `json:"mateProjectId"`
	Title         string  `json:"title"`
	Body          string  `json:"body"`
	State         string  `json:"state"`
	Head          *string `json:"head"`
	MergedSha     *string `json:"mergedSha"`
	LandedHead    *string `json:"landedHead"`
	OpenedAt      string  `json:"openedAt"`
	MergedAt      *string `json:"mergedAt"`
	ClosedAt      *string `json:"closedAt"`
	UpdatedAt     string  `json:"updatedAt"`
	Mergeability  string  `json:"mergeability"`
	Behind        bool    `json:"behind"`
}

// OpenedChange is HQ's answer to opening a change: the Mate's open change in
// the repository, and whether this call opened it.
type OpenedChange struct {
	Change  Change `json:"change"`
	Created bool   `json:"created"`
	Reason  string `json:"reason,omitempty"`
}

// OpenChange is the Mate's open change in repo, or the next number opened
// with title. An open change keeps its title: retitling it is EditChange's.
func (c Client) OpenChange(ctx context.Context, repo, title string, tree ...string) (OpenedChange, error) {
	var opened OpenedChange
	body := map[string]string{"repo": repo, "title": title}
	if len(tree) != 0 {
		body["tree"] = tree[0]
	}
	err := c.call.json(ctx, http.MethodPost, "/api/mate/changes", c.authorization(),
		body, &opened)
	return opened, err
}

// ChangeEdit is what an edit of an open change sets: its title, its
// description (markdown), or both; a nil field stays as it is.
type ChangeEdit struct {
	Title *string `json:"title,omitempty"`
	Body  *string `json:"body,omitempty"`
}

// EditChange retitles or describes the Mate's open change number in repo.
func (c Client) EditChange(ctx context.Context, repo string, number int, edit ChangeEdit) (Change, error) {
	var change Change
	err := c.call.json(ctx, http.MethodPatch, changePath(repo, number), c.authorization(), edit, &change)
	return change, err
}

// changePath is the Mate's change number in repo, as its API names it.
func changePath(repo string, number int) string {
	return "/api/mate/changes/" + url.PathEscape(repo) + "/" + strconv.Itoa(number)
}

// Attachment is a picture HQ keeps for a change's description: its id, and
// the path HQ serves it at, which a description shows after HQ's address.
type Attachment struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// Attach keeps png, a PNG of at most 20 MiB, for the Mate's open change
// number in repo.
func (c Client) Attach(ctx context.Context, repo string, number int, png []byte) (Attachment, error) {
	var kept Attachment
	err := c.call.send(ctx, http.MethodPost, changePath(repo, number)+"/attachments", c.authorization(), "image/png", png, &kept)
	return kept, err
}
