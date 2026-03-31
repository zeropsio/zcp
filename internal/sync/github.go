package sync

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// GH wraps gh CLI operations for a single repo.
type GH struct {
	Repo string // "owner/repo" format
}

// ReadFile returns file content and blob SHA from default branch.
func (g *GH) ReadFile(path string) (content string, sha string, err error) {
	out, err := g.api("repos/" + g.Repo + "/contents/" + path)
	if err != nil {
		return "", "", fmt.Errorf("read file %s: %w", path, err)
	}

	var resp struct {
		Content string `json:"content"`
		SHA     string `json:"sha"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return "", "", fmt.Errorf("parse response %s: %w", path, err)
	}

	// GitHub returns base64 with embedded newlines
	cleaned := strings.ReplaceAll(resp.Content, "\n", "")
	decoded, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return "", "", fmt.Errorf("decode content %s: %w", path, err)
	}

	return string(decoded), resp.SHA, nil
}

// CreateBranch creates a new branch from default branch HEAD.
func (g *GH) CreateBranch(name string) error {
	headSHA, err := g.DefaultBranchSHA()
	if err != nil {
		return fmt.Errorf("get HEAD SHA: %w", err)
	}

	_, err = g.api("repos/"+g.Repo+"/git/refs",
		"-f", "ref=refs/heads/"+name,
		"-f", "sha="+headSHA)
	if err != nil {
		return fmt.Errorf("create branch %s: %w", name, err)
	}
	return nil
}

// UpdateFile commits a file change to a branch.
func (g *GH) UpdateFile(path, branch, message, content, blobSHA string) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(content))

	args := []string{
		"--method", "PUT",
		"repos/" + g.Repo + "/contents/" + path,
		"-f", "message=" + message,
		"-f", "content=" + encoded,
		"-f", "branch=" + branch,
	}
	if blobSHA != "" {
		args = append(args, "-f", "sha="+blobSHA)
	}

	_, err := g.api(args...)
	if err != nil {
		return fmt.Errorf("update file %s: %w", path, err)
	}
	return nil
}

// CreatePR opens a pull request and returns the URL.
func (g *GH) CreatePR(branch, title, body string) (string, error) {
	cmd := exec.Command("gh", "pr", "create", //nolint:gosec,noctx // controlled internal values
		"--repo", g.Repo,
		"--head", branch,
		"--title", title,
		"--body", body)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("create PR: %w\nstderr: %s", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

// DefaultBranchSHA returns the HEAD SHA of the default branch.
func (g *GH) DefaultBranchSHA() (string, error) {
	// Get default branch name
	branchName, err := g.api("repos/"+g.Repo, "--jq", ".default_branch")
	if err != nil {
		return "", fmt.Errorf("get default branch: %w", err)
	}
	branchName = strings.TrimSpace(branchName)

	// Get HEAD SHA of that branch
	sha, err := g.api("repos/"+g.Repo+"/git/refs/heads/"+branchName, "--jq", ".object.sha")
	if err != nil {
		return "", fmt.Errorf("get branch SHA: %w", err)
	}

	return strings.TrimSpace(sha), nil
}

// RepoExists checks if a repo exists.
func (g *GH) RepoExists(repo string) bool {
	gh := &GH{Repo: repo}
	_, err := gh.api("repos/" + repo)
	return err == nil
}

// CreateTree creates a Git tree with multiple file entries.
func (g *GH) CreateTree(baseSHA string, files map[string]string) (string, error) {
	type treeEntry struct {
		Path    string `json:"path"`
		Mode    string `json:"mode"`
		Type    string `json:"type"`
		Content string `json:"content"`
	}

	entries := make([]treeEntry, 0, len(files))
	for path, content := range files {
		entries = append(entries, treeEntry{
			Path:    path,
			Mode:    "100644",
			Type:    "blob",
			Content: content,
		})
	}

	payload := struct {
		BaseTree string      `json:"base_tree"` //nolint:tagliatelle
		Tree     []treeEntry `json:"tree"`
	}{
		BaseTree: baseSHA,
		Tree:     entries,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal tree: %w", err)
	}

	out, err := g.apiRaw("repos/"+g.Repo+"/git/trees", data)
	if err != nil {
		return "", fmt.Errorf("create tree: %w", err)
	}

	var resp struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return "", fmt.Errorf("parse tree response: %w", err)
	}

	return resp.SHA, nil
}

// CreateCommit creates a Git commit.
func (g *GH) CreateCommit(treeSHA, parentSHA, message string) (string, error) {
	payload := struct {
		Message string   `json:"message"`
		Tree    string   `json:"tree"`
		Parents []string `json:"parents"`
	}{
		Message: message,
		Tree:    treeSHA,
		Parents: []string{parentSHA},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal commit: %w", err)
	}

	out, err := g.apiRaw("repos/"+g.Repo+"/git/commits", data)
	if err != nil {
		return "", fmt.Errorf("create commit: %w", err)
	}

	var resp struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return "", fmt.Errorf("parse commit response: %w", err)
	}

	return resp.SHA, nil
}

// UpdateRef updates a branch ref to point to a new SHA.
func (g *GH) UpdateRef(branch, sha string) error {
	_, err := g.api("repos/"+g.Repo+"/git/refs/heads/"+branch,
		"--method", "PATCH",
		"-f", "sha="+sha)
	if err != nil {
		return fmt.Errorf("update ref %s: %w", branch, err)
	}
	return nil
}

func (g *GH) api(args ...string) (string, error) {
	cmdArgs := append([]string{"api"}, args...)
	cmd := exec.Command("gh", cmdArgs...) //nolint:noctx // short-lived CLI command

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w\nstderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

func (g *GH) apiRaw(endpoint string, body []byte) (string, error) {
	cmd := exec.Command("gh", "api", endpoint, "--input", "-") //nolint:noctx // short-lived CLI command
	cmd.Stdin = bytes.NewReader(body)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w\nstderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}
