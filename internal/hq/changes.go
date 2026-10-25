package hq

import (
	"context"
	"errors"
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
// repository of its application, bound to a branch — one of the agent's own,
// or `mate/<project id>/<number>` when none is named; a Mate has at most one
// open change per repository. A Mate pushes any branch but `main`, a tag, or
// another Mate's open change's, so a branch may be pushed before or after its
// change is opened.

// Doer sends one HTTP request.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client speaks HQ's Mate API as the project whose key it was opened with:
// `Authorization: Project <key>` on every call, always within the application
// HQ holds the Mate in.
type Client struct {
	call hqClient
	id   Identity
}

// Open is a client of the HQ id names, presenting id's key; ErrNoIdentity
// when id lacks the key, the project or HQ's origin.
func Open(httpClient Doer, id Identity) (Client, error) {
	if err := id.check(); err != nil {
		return Client{}, err
	}
	return Client{call: hqClient{http: httpClient, address: id.Origin}, id: id}, nil
}

// Once is this client sending each call once: a 503 is UnavailableError at
// once rather than waited out, for a caller that paces its own tries — a
// describe, which waits HQ out on its own pace.
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
// `/git/<orgId>/<appId>/<name>.git`.
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
	repoURL, err := c.RepoURL(appID, repo)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, repoURL+"/info/refs?service=git-upload-pack", nil)
	if err != nil {
		return false, fmt.Errorf("hq git %s: %w", repo, err)
	}
	req.Header.Set("Authorization", c.authorization())
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

// authorization is the key as every call and zcp's own git presents it. The
// header is the only place it travels: never a URL, a log line or a file.
func (c Client) authorization() string { return "Project " + c.id.Key }

// Address is the HQ's own address, without a trailing slash.
func (c Client) Address() string { return strings.TrimRight(c.id.Origin, "/") }

// ProjectID is the Mate's own project: HQ's id of the Mate.
func (c Client) ProjectID() string { return c.id.ProjectID }

// OrgID is the Zerops organisation the Mate's repository addresses name; ""
// where the container has none (RepoURL then reports ErrNoOrganisation).
func (c Client) OrgID() string { return c.id.OrgID }

// RepoURL is where git reaches the repository repo of the application appID.
func (c Client) RepoURL(appID, repo string) (string, error) {
	return RepoURLAt(c.Address(), c.id.OrgID, appID, repo)
}

// ErrNoOrganisation is an address HQ would answer 404: it names the
// organisation, and the caller has none.
var ErrNoOrganisation = errors.New("hq repository address needs the Zerops organisation, and this container has none")

// RepoURLAt is where git reaches the repository repo of the application
// appID of the organisation orgID at the HQ whose address is address:
// `/git/<orgId>/<appId>/<repo>.git` (HQ's named difference 3). ErrNoOrganisation
// when orgID is empty — never an address without it.
func RepoURLAt(address, orgID, appID, repo string) (string, error) {
	if orgID == "" {
		return "", ErrNoOrganisation
	}
	return strings.TrimRight(address, "/") + "/git/" + url.PathEscape(orgID) + "/" + url.PathEscape(appID) + "/" + url.PathEscape(repo) + ".git", nil
}

// ChangeURL is a change's own address at HQ, which HQ leads into the client:
// what zcp hands a person (hqChanges.ts changeUrl).
func (c Client) ChangeURL(appID, repo string, number int) string {
	return c.Address() + "/changes/" + url.PathEscape(appID) + "/" + url.PathEscape(repo) + "/" + strconv.Itoa(number)
}

// ChangeBranch is the branch HQ binds the Mate's change number to when it is
// opened naming none.
func (c Client) ChangeBranch(number int) string {
	return "mate/" + c.id.ProjectID + "/" + strconv.Itoa(number)
}

// BranchOf is the branch of the Mate's change number by its short name: the
// one HQ names for it, branch — the Mate's own, or, for a change handed to it,
// the branch it was on — and, from an HQ that names none, ChangeBranch.
func (c Client) BranchOf(number int, branch string) string {
	if branch = strings.TrimPrefix(branch, "refs/heads/"); branch != "" {
		return branch
	}
	return c.ChangeBranch(number)
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
	AppID         string `json:"appId"`
	Repo          string `json:"repo"`
	Number        int    `json:"number"`
	MateProjectID string `json:"mateProjectId"`
	Title         string `json:"title"`
	// Branch is the change's branch, a full ref under refs/heads/; "" from
	// an HQ older than branches of the Mate's own.
	Branch       string  `json:"branch,omitempty"`
	Body         string  `json:"body"`
	State        string  `json:"state"`
	Head         *string `json:"head"`
	MergedSha    *string `json:"mergedSha"`
	LandedHead   *string `json:"landedHead"`
	OpenedAt     string  `json:"openedAt"`
	MergedAt     *string `json:"mergedAt"`
	ClosedAt     *string `json:"closedAt"`
	UpdatedAt    string  `json:"updatedAt"`
	Mergeability string  `json:"mergeability"`
	Behind       bool    `json:"behind"`
}

// OpenedChange is HQ's answer to opening a change: the Mate's open change in
// the repository, and whether this call opened it.
type OpenedChange struct {
	Change  Change `json:"change"`
	Created bool   `json:"created"`
}

// OpenChange is the Mate's open change in repo, or the next number opened
// with title on branch — a branch of the Mate's own, which HQ binds the
// change to; "" lets HQ name it. An open change keeps its title and branch:
// retitling it is EditChange's. HQ refuses a branch another open change
// holds (409, reason "branch_taken"), one it keeps for itself — `main`, a
// full ref, its mate/ namespace (409, reason "protected_ref") — and one its
// git does not take (400, reason "invalid_ref").
func (c Client) OpenChange(ctx context.Context, repo, title, branch string) (OpenedChange, error) {
	var opened OpenedChange
	body := map[string]string{"repo": repo, "title": title}
	if branch != "" {
		body["branch"] = branch
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
