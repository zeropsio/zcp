package ops

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Reads a Mate makes of its group before any of its pairs is wired: which
// group it belongs to, what the group's recipe says, and whether a repository
// the recipe names is really there. All three go to Gitea with the bot's own
// token in Gitea's `token` scheme (giteaAPICall), and every error is coarse —
// status or class only, never a body — because callers put them in
// agent-facing text.

// ReadGiteaFile reads one file of fullName at ref (a branch, tag or commit)
// through the contents API. A branch or a file that is not there reads as
// found=false, not an error: a group whose recipe is not merged yet is an
// ordinary state. Each path segment is escaped on its own, so a tier's
// directory — `0 — AI Agent`, spaces and an em dash — reaches Gitea intact.
func ReadGiteaFile(ctx context.Context, httpClient HTTPDoer, giteaURL, token, fullName, ref, path string) (body string, found bool, err error) {
	if httpClient == nil {
		return "", false, fmt.Errorf("no HTTP client configured")
	}
	if strings.TrimSpace(fullName) == "" || strings.TrimSpace(path) == "" {
		return "", false, fmt.Errorf("a file read needs a repository and a path")
	}
	apiBase, err := giteaAPIBase(giteaURL)
	if err != nil {
		return "", false, err
	}
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	endpoint := apiBase + "/repos/" + fullName + "/contents/" + strings.Join(segments, "/")
	if ref != "" {
		endpoint += "?ref=" + url.QueryEscape(ref)
	}
	raw, status, err := giteaAPICall(ctx, httpClient, http.MethodGet, endpoint, token, nil)
	if err != nil {
		return "", false, err
	}
	switch {
	case status == http.StatusNotFound, giteaRefAbsent(status, raw):
		return "", false, nil
	case status != http.StatusOK:
		return "", false, fmt.Errorf("the Gitea read of %s@%s:%s returned status %d", fullName, ref, path, status)
	}
	var file struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	if json.Unmarshal(raw, &file) != nil || file.Type != "file" {
		// A directory answers with a JSON array, a symlink or submodule with
		// another type — none of them is the file that was asked for.
		return "", false, fmt.Errorf("%s@%s:%s is not a file", fullName, ref, path)
	}
	if file.Encoding != "" && file.Encoding != "base64" {
		return "", false, fmt.Errorf("%s@%s:%s came back in an encoding zcp does not read (%s)", fullName, ref, path, file.Encoding)
	}
	decoded, err := base64.StdEncoding.DecodeString(file.Content)
	if err != nil {
		return "", false, fmt.Errorf("%s@%s:%s came back with content that is not base64", fullName, ref, path)
	}
	return string(decoded), true, nil
}

// GiteaGroupOrgs names the orgs whose group repository the token's user can
// read. A Mate's bot sits in exactly one group's `read` team (gitea-mate
// docs/vocabulary.md) and holds write on `{org}/group` (D31), so this is how
// it finds its group before any of its pairs names the org. It reads the
// bot's repositories, not its orgs: GET /user/orgs needs read:organization,
// which a bot's token does not carry (write:repository, write:issue,
// read:user — refused 403 on the first live stand-up, 2026-09-30).
func GiteaGroupOrgs(ctx context.Context, httpClient HTTPDoer, giteaURL, token string) ([]string, error) {
	if httpClient == nil {
		return nil, fmt.Errorf("no HTTP client configured")
	}
	apiBase, err := giteaAPIBase(giteaURL)
	if err != nil {
		return nil, err
	}
	const pageSize, maxPages = 50, 20
	var orgs []string
	seen := map[string]bool{}
	for page := 1; page <= maxPages; page++ {
		raw, status, err := giteaAPICall(ctx, httpClient, http.MethodGet,
			fmt.Sprintf("%s/user/repos?limit=%d&page=%d", apiBase, pageSize, page), token, nil)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("the Gitea read of this bot's repositories returned status %d", status)
		}
		var repos []struct {
			Name     string `json:"name"`
			FullName string `json:"full_name"` //nolint:tagliatelle // Gitea's wire schema
			Owner    struct {
				Login string `json:"login"`
			} `json:"owner"`
		}
		if err := json.Unmarshal(raw, &repos); err != nil {
			return nil, fmt.Errorf("the Gitea repositories response was not valid JSON")
		}
		for _, repo := range repos {
			if repo.Name != groupRepoName {
				continue
			}
			org := strings.TrimSpace(repo.Owner.Login)
			if org == "" {
				org, _, _ = strings.Cut(repo.FullName, "/")
			}
			if org != "" && !seen[org] {
				seen[org] = true
				orgs = append(orgs, org)
			}
		}
		if len(repos) < pageSize {
			break
		}
	}
	return orgs, nil
}

// GiteaRepositoryExists reports whether fullName is a repository the token
// can read. The broker CREATES the service repository it is asked for, so a
// Mate checks first that a repository its recipe names is really there — a
// recipe naming one by mistake must not leave an empty repository in the
// group's org.
func GiteaRepositoryExists(ctx context.Context, httpClient HTTPDoer, giteaURL, token, fullName string) (bool, error) {
	if httpClient == nil {
		return false, fmt.Errorf("no HTTP client configured")
	}
	if strings.TrimSpace(fullName) == "" {
		return false, fmt.Errorf("a repository read needs a repository")
	}
	apiBase, err := giteaAPIBase(giteaURL)
	if err != nil {
		return false, err
	}
	_, status, err := giteaAPICall(ctx, httpClient, http.MethodGet, apiBase+"/repos/"+fullName, token, nil)
	if err != nil {
		return false, err
	}
	switch status {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("the Gitea read of %s returned status %d", fullName, status)
	}
}
