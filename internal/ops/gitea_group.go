package ops

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // git's own object hash; identity, not a security primitive
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/recipe"
)

// The group repo — where the recipe lives (D13) — and how a Mate's bot gets a
// proposal into it.
//
// A Mate's bot is a READER on the group's org: it is a collaborator on the
// repositories it created, never on `{slug}/group`, which only the group's
// releasers write. Measured on Gitea 1.27.2 (2026-09-16): a bot in the org's
// `read` team pushing a branch to the group repo with a `write:repository`
// token is refused by the pre-receive hook ("User permission denied for
// writing"). So the bot FORKS the group repo into its own namespace, commits
// there, and opens a cross-fork pull request — which the same lab confirms
// Gitea accepts (`head: "{bot}:{branch}"`), including for a restricted user
// whose `max_repo_creation` is 0: a fork is not a repository creation as far
// as that limit is concerned.
//
// The commit goes through Gitea's API rather than a working copy on purpose.
// A clone would put the bot's token in a remote URL inside `.git/config` — a
// token in a file ZCP wrote, which is exactly what A1 went out of its way to
// avoid. The multi-file contents endpoint takes every file of the export in
// ONE request and makes ONE commit, authored as the bot, with the token only
// ever in a header.

// groupRepoName is the repository every group's recipe lives in:
// `{slug}/group` (gitea-mate docs/vocabulary.md).
const groupRepoName = "group"

// GroupRepoFullName maps any repository of a group's org to that group's
// repo. A Mate learns its org from the repository the broker gave one of its
// pairs (A1), so the group repo needs no variable of its own. Returns "" when
// the input does not name an org.
func GroupRepoFullName(repoFullName string) string {
	org, name, found := strings.Cut(strings.TrimSpace(repoFullName), "/")
	if !found || org == "" || name == "" {
		return ""
	}
	return org + "/" + groupRepoName
}

// giteaRepo is the subset of Gitea's repository object this file reads.
type giteaRepo struct {
	FullName      string `json:"full_name"`      //nolint:tagliatelle // Gitea's wire schema
	DefaultBranch string `json:"default_branch"` //nolint:tagliatelle // Gitea's wire schema
}

// EnsureGiteaFork returns the bot's fork of upstream, creating it when it is
// not there. Idempotent in the ordinary direction: the fork is looked up
// first, so a Mate that already has one makes no write call at all, and a
// 409 from a race is read as "someone got there first", not as a failure.
func EnsureGiteaFork(ctx context.Context, httpClient HTTPDoer, giteaURL, token, upstream, botLogin string) (string, error) {
	if httpClient == nil {
		return "", fmt.Errorf("no HTTP client configured")
	}
	apiBase, err := giteaAPIBase(giteaURL)
	if err != nil {
		return "", err
	}
	_, name, found := strings.Cut(upstream, "/")
	if !found || botLogin == "" {
		return "", fmt.Errorf("a fork needs an {owner}/{repo} upstream and the bot's login")
	}
	fork := botLogin + "/" + name

	body, status, err := giteaAPICall(ctx, httpClient, http.MethodGet, apiBase+"/repos/"+fork, token, nil)
	if err != nil {
		return "", err
	}
	if status == http.StatusOK {
		var repo giteaRepo
		if json.Unmarshal(body, &repo) == nil && repo.FullName != "" {
			return repo.FullName, nil
		}
		return fork, nil
	}

	payload, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return "", fmt.Errorf("encode fork request failed")
	}
	body, status, err = giteaAPICall(ctx, httpClient, http.MethodPost, apiBase+"/repos/"+upstream+"/forks", token, payload)
	if err != nil {
		return "", err
	}
	switch status {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted:
		var repo giteaRepo
		if json.Unmarshal(body, &repo) == nil && repo.FullName != "" {
			return repo.FullName, nil
		}
		return fork, nil
	case http.StatusConflict:
		// Already forked — by an earlier pass, or by this one racing itself.
		return fork, nil
	default:
		return "", fmt.Errorf("the Gitea fork of %s returned status %d", upstream, status)
	}
}

// PublishGiteaFiles puts files on branch of fullName in ONE commit, branching
// off base when the branch is not there yet, and reports whether it committed.
//
// Idempotent on CONTENT, not on having run: each file's git blob hash is
// compared with the branch's tree, and a pass whose export is byte-identical
// makes no request at all. That is what lets this be called after every
// service change without turning the pull request into a wall of empty
// commits.
//
// Files the export does not name are left alone — the group repo holds an
// `environments.yaml` that the app writes and zcp must never touch
// (gitea-mate docs/group-repo.md), so this adds and updates, never deletes.
func PublishGiteaFiles(
	ctx context.Context,
	httpClient HTTPDoer,
	giteaURL, token, fullName, branch, base, message string,
	files []recipe.File,
) (bool, error) {
	if httpClient == nil {
		return false, fmt.Errorf("no HTTP client configured")
	}
	apiBase, err := giteaAPIBase(giteaURL)
	if err != nil {
		return false, err
	}
	if fullName == "" || branch == "" || base == "" {
		return false, fmt.Errorf("publishing files needs a repository, a branch and a base")
	}
	if len(files) == 0 {
		return false, fmt.Errorf("nothing to publish")
	}

	existing, branchExists, err := giteaTreeBlobs(ctx, httpClient, apiBase, token, fullName, branch)
	if err != nil {
		return false, err
	}
	if !branchExists {
		// A `new_branch` commit starts from the base, so the base's tree — not
		// an empty one — is what the change set has to be diffed against.
		// Diffing against nothing emits `create` for a file the new branch
		// already inherited, which Gitea refuses with 422 ("repository file
		// already exists") and which would leave the recipe unproposable on
		// every Mate whose group repo was initialised with a README.
		existing, _, err = giteaTreeBlobs(ctx, httpClient, apiBase, token, fullName, base)
		if err != nil {
			return false, err
		}
	}

	type changeFile struct {
		Operation string `json:"operation"`
		Path      string `json:"path"`
		Content   string `json:"content"`
	}
	changes := make([]changeFile, 0, len(files))
	for _, file := range files {
		blob, present := existing[file.Path]
		if present && blob == gitBlobSHA(file.Body) {
			continue
		}
		operation := "create"
		if present {
			operation = "update"
		}
		changes = append(changes, changeFile{
			Operation: operation,
			Path:      file.Path,
			Content:   base64.StdEncoding.EncodeToString([]byte(file.Body)),
		})
	}
	if len(changes) == 0 {
		return false, nil
	}
	// Stable order so a diff of two passes is a diff of the export, not of a
	// map iteration.
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })

	request := map[string]any{"branch": branch, "message": message, "files": changes}
	if !branchExists {
		// Gitea branches off `branch` and commits onto `new_branch`.
		request["branch"] = base
		request["new_branch"] = branch
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return false, fmt.Errorf("encode the file change request failed")
	}
	_, status, err := giteaAPICall(ctx, httpClient, http.MethodPost, apiBase+"/repos/"+fullName+"/contents", token, payload)
	if err != nil {
		return false, err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return false, fmt.Errorf("the Gitea file change on %s returned status %d", fullName, status)
	}
	return true, nil
}

// giteaTreeBlobs reads ref's tree as path → blob hash. A ref that does not
// exist is not an error — it is the ordinary first pass, reported by the
// second return being false.
func giteaTreeBlobs(ctx context.Context, httpClient HTTPDoer, apiBase, token, fullName, ref string) (map[string]string, bool, error) {
	endpoint := apiBase + "/repos/" + fullName + "/git/trees/" + url.PathEscape(ref) + "?recursive=true&per_page=1000"
	body, status, err := giteaAPICall(ctx, httpClient, http.MethodGet, endpoint, token, nil)
	if err != nil {
		return nil, false, err
	}
	if status == http.StatusNotFound || giteaRefAbsent(status, body) {
		return map[string]string{}, false, nil
	}
	if status != http.StatusOK {
		return nil, false, fmt.Errorf("the Gitea tree read of %s@%s returned status %d", fullName, ref, status)
	}
	var tree struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if jsonErr := json.Unmarshal(body, &tree); jsonErr != nil {
		return nil, false, fmt.Errorf("the Gitea tree response was not valid JSON")
	}
	if tree.Truncated {
		// A truncated tree would read as "these files are missing" and make
		// the next pass re-create them — a create on an existing path is a
		// 422, so the pull request would stop updating. Refuse instead.
		return nil, false, fmt.Errorf("the Gitea tree of %s@%s came back truncated", fullName, ref)
	}
	blobs := make(map[string]string, len(tree.Tree))
	for _, entry := range tree.Tree {
		if entry.Type == "blob" {
			blobs[entry.Path] = entry.SHA
		}
	}
	return blobs, true, nil
}

// giteaRefAbsent recognises Gitea's answer to a read of a ref that is not
// there. Gitea 1.27.2 answers `400 {"message":"sha not found [<ref>]"}` — NOT
// 404 — for a branch, tag or sha it cannot resolve (measured live
// 2026-09-16 on a Mate's fork whose `mate/{bot}` branch did not exist yet).
// A reader that only knew 404 turned "this branch is new" into a hard
// failure, and the group recipe could never be published from a fresh fork.
// Narrow on purpose: a 400 that says anything else is still an error.
func giteaRefAbsent(status int, body []byte) bool {
	return status == http.StatusBadRequest && bytes.Contains(body, []byte("sha not found"))
}

// gitBlobSHA is git's own object hash for a file body — `blob <len>\0<body>`,
// SHA-1. The same value Gitea reports in a tree entry, which is what makes the
// "has this file changed" compare a local computation rather than a download
// of every file on every pass.
func gitBlobSHA(body string) string {
	sum := sha1.New() //nolint:gosec // git's object hash is SHA-1 by definition
	fmt.Fprintf(sum, "blob %d\x00", len(body))
	sum.Write([]byte(body))
	return hex.EncodeToString(sum.Sum(nil))
}

// GiteaBranchFiles is a branch as a proposal reads it: the commit at its tip,
// and the paths of the tree AT that commit.
type GiteaBranchFiles struct {
	// Head is the tip's commit id, "" when the branch is not there.
	Head string
	// Paths are every file of the tree at Head, sorted.
	Paths []string
}

// ReadGiteaBranchFiles reads branch of fullName: its tip, then the tree at
// that tip — so the files and the commit a proposal is cut from are one
// commit's, never a branch that moved between two reads. A branch that is not
// there reads as the zero value, not an error.
func ReadGiteaBranchFiles(ctx context.Context, httpClient HTTPDoer, giteaURL, token, fullName, branch string) (GiteaBranchFiles, error) {
	head, err := giteaBranchHead(ctx, httpClient, giteaURL, token, fullName, branch)
	if err != nil || head == "" {
		return GiteaBranchFiles{}, err
	}
	apiBase, err := giteaAPIBase(giteaURL)
	if err != nil {
		return GiteaBranchFiles{}, err
	}
	blobs, found, err := giteaTreeBlobs(ctx, httpClient, apiBase, token, fullName, head)
	if err != nil {
		return GiteaBranchFiles{}, err
	}
	if !found {
		return GiteaBranchFiles{}, fmt.Errorf("the Gitea tree of %s at %s is not there", fullName, head)
	}
	paths := make([]string, 0, len(blobs))
	for path := range blobs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return GiteaBranchFiles{Head: head, Paths: paths}, nil
}

// giteaBranchHead is branch's tip commit id on fullName, "" when the branch is
// not there (Gitea answers 404, or the 400 "sha not found" its ref reads use).
func giteaBranchHead(ctx context.Context, httpClient HTTPDoer, giteaURL, token, fullName, branch string) (string, error) {
	if httpClient == nil {
		return "", fmt.Errorf("no HTTP client configured")
	}
	apiBase, err := giteaAPIBase(giteaURL)
	if err != nil {
		return "", err
	}
	if fullName == "" || branch == "" {
		return "", fmt.Errorf("a branch read needs a repository and a branch")
	}
	body, status, err := giteaAPICall(ctx, httpClient, http.MethodGet,
		apiBase+"/repos/"+fullName+"/branches/"+url.PathEscape(branch), token, nil)
	if err != nil {
		return "", err
	}
	switch {
	case status == http.StatusNotFound, giteaRefAbsent(status, body):
		return "", nil
	case status != http.StatusOK:
		return "", fmt.Errorf("the Gitea branch read of %s@%s returned status %d", fullName, branch, status)
	}
	var read struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if json.Unmarshal(body, &read) != nil || read.Commit.ID == "" {
		return "", fmt.Errorf("the Gitea branch read of %s@%s named no commit", fullName, branch)
	}
	return read.Commit.ID, nil
}

// GiteaRecipeBranch is the fork branch a recipe proposal is made from, named
// after the upstream main commit it is cut from: `recipe/{12 hex}`.
//
// Named, not reused. A proposal must start at main as it is — a branch left
// from an earlier proposal diffs against the main it was cut from, and a pull
// request from it shows every file main has changed since as a modification.
// Resetting one fixed branch means deleting it, and Gitea closes the pull
// requests of a deleted branch from its push queue, after the delete returns
// (DeleteBranch → PushUpdate → CloseBranchPulls in its source; not measured):
// a pull request opened from the re-created branch in the same pass could be
// closed under it. A branch per main commit is never deleted and never moves back,
// so an open proposal stays open until zcp closes it by number. Not under
// `mate/`: `mate/{login}` is the Mate's own branch everywhere else, and a ref
// cannot be both a branch and a directory of branches.
func GiteaRecipeBranch(mainHead string) string {
	head := strings.TrimSpace(mainHead)
	if head == "" {
		return ""
	}
	if len(head) > 12 {
		head = head[:12]
	}
	return "recipe/" + head
}

// EnsureGiteaProposalBranch makes sure fork carries branch, cut at the
// upstream commit at, and reports whether this call cut it. A branch already
// there is left exactly as it is: its name says which commit it was cut from
// (GiteaRecipeBranch), and it may carry a proposal's commits.
//
// A fork is a copy, not a view: an upstream commit made after the fork is not
// in it. So the fork's base is synced from the upstream first (Gitea's
// merge-upstream; a fork zcp never commits to fast-forwards), and the branch
// is then cut at the commit itself rather than at the fork's base — if the
// upstream moved again in between, the proposal still starts where it was
// composed against.
func EnsureGiteaProposalBranch(ctx context.Context, httpClient HTTPDoer, giteaURL, token, fork, branch, base, at string) (bool, error) {
	if fork == "" || branch == "" || base == "" || at == "" {
		return false, fmt.Errorf("a proposal branch needs a fork, a branch, a base and a commit")
	}
	there, err := GiteaBranchExists(ctx, httpClient, giteaURL, token, fork, branch)
	if err != nil || there {
		return false, err
	}
	apiBase, err := giteaAPIBase(giteaURL)
	if err != nil {
		return false, err
	}
	payload, err := json.Marshal(map[string]string{"branch": base})
	if err != nil {
		return false, fmt.Errorf("encode the fork sync request failed")
	}
	_, status, err := giteaAPICall(ctx, httpClient, http.MethodPost, apiBase+"/repos/"+fork+"/merge-upstream", token, payload)
	if err != nil {
		return false, err
	}
	if status != http.StatusOK {
		return false, fmt.Errorf("syncing %s@%s from its upstream returned status %d", fork, base, status)
	}
	payload, err = json.Marshal(map[string]string{"new_branch_name": branch, "old_ref_name": at})
	if err != nil {
		return false, fmt.Errorf("encode the branch request failed")
	}
	_, status, err = giteaAPICall(ctx, httpClient, http.MethodPost, apiBase+"/repos/"+fork+"/branches", token, payload)
	if err != nil {
		return false, err
	}
	switch status {
	case http.StatusCreated, http.StatusOK:
		return true, nil
	case http.StatusConflict:
		// A concurrent pass cut it between the read and the create.
		return false, nil
	default:
		return false, fmt.Errorf("cutting %s@%s at %s returned status %d", fork, branch, at, status)
	}
}

// CloseGiteaPullRequests closes every pull request open on fullName against
// base that poster opened, except the one from keepHead ("" keeps none), and
// returns the numbers it closed, sorted. Anybody else's pull request — a
// person's, another Mate's — is never touched: on the group repo, which only
// the group's people write, what a Mate's bot opened is that Mate's proposal
// and zcp's to withdraw.
func CloseGiteaPullRequests(ctx context.Context, httpClient HTTPDoer, giteaURL, token, fullName, poster, base, keepHead string) ([]int, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("no HTTP client configured")
	}
	apiBase, err := giteaAPIBase(giteaURL)
	if err != nil {
		return nil, err
	}
	if fullName == "" || poster == "" || base == "" {
		return nil, fmt.Errorf("closing pull requests needs a repository, a poster and a base")
	}
	repoRoot := apiBase + "/repos/" + fullName
	body, status, err := giteaAPICall(ctx, httpClient, http.MethodGet, repoRoot+"/pulls?state=open&limit=50", token, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("the Gitea pull-request list returned status %d", status)
	}
	var open []struct {
		giteaPullRequest
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		Head struct {
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	if jsonErr := json.Unmarshal(body, &open); jsonErr != nil {
		return nil, fmt.Errorf("the Gitea pull-request list was not valid JSON")
	}
	sort.Slice(open, func(i, j int) bool { return open[i].Number < open[j].Number })
	payload, err := json.Marshal(map[string]string{"state": "closed"})
	if err != nil {
		return nil, fmt.Errorf("encode the pull-request close failed")
	}
	var closed []int
	for _, pr := range open {
		if !strings.EqualFold(pr.User.Login, poster) || pr.Base.Ref != base || (keepHead != "" && pr.Head.Ref == keepHead) {
			continue
		}
		_, status, err := giteaAPICall(ctx, httpClient, http.MethodPatch, fmt.Sprintf("%s/pulls/%d", repoRoot, pr.Number), token, payload)
		if err != nil {
			return closed, err
		}
		if status != http.StatusOK && status != http.StatusCreated {
			return closed, fmt.Errorf("closing pull request #%d on %s returned status %d", pr.Number, fullName, status)
		}
		closed = append(closed, pr.Number)
	}
	return closed, nil
}
