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
