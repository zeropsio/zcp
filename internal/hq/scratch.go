package hq

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Git on this container against HQ, for what zcp writes to HQ itself rather
// than through a pair's checkout — the group's recipe: a bare repository in
// a temporary directory, fetched from and pushed to by URL. The Mate
// credential reaches git through its environment only, as an Authorization
// header scoped to HQ's address — never in argv, a URL or a file.

// Scratch is a bare repository holding what it fetched of one repository of
// HQ.
type Scratch struct {
	dir string
	url string
	env []string
}

// CommitSpec is a commit a scratch makes: its tree, its parents in order,
// its message and whose it is.
type CommitSpec struct {
	Tree        string
	Parents     []string
	Message     string
	Name, Email string
}

// gitEnv is the environment of git against this HQ: the credential as the
// Basic Authorization of GitUser, for HQ's address alone, and no prompt.
func (c Client) gitEnv() []string {
	header := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(GitUser+":"+c.Credential()))
	return append(os.Environ(),
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http."+c.Address()+"/.extraHeader",
		"GIT_CONFIG_VALUE_0="+header,
		"GIT_TERMINAL_PROMPT=0",
	)
}

// OpenScratch is an empty scratch repository for the repository repo of the
// application appID. Close removes it.
func (c Client) OpenScratch(ctx context.Context, appID, repo string) (*Scratch, error) {
	dir, err := os.MkdirTemp("", "zcp-hq-")
	if err != nil {
		return nil, fmt.Errorf("scratch repository: %w", err)
	}
	s := &Scratch{dir: dir, url: c.RepoURL(appID, repo), env: c.gitEnv()}
	if _, err := s.git(ctx, nil, "init", "--bare", "-q"); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// Close removes the scratch repository.
func (s *Scratch) Close() { _ = os.RemoveAll(s.dir) }

// errNoSuchRef is git finding no branch of the name at HQ.
var errNoSuchRef = errors.New("no such branch")

// Fetch takes HQ's branch into the scratch under the same name and answers
// its head; found is false when HQ has no such branch.
func (s *Scratch) Fetch(ctx context.Context, branch string) (head string, found bool, err error) {
	ref := "refs/heads/" + branch
	if _, err := s.git(ctx, nil, "fetch", "-q", "--no-tags", s.url, "+"+ref+":"+ref); err != nil {
		if errors.Is(err, errNoSuchRef) {
			return "", false, nil
		}
		return "", false, err
	}
	head, err = s.git(ctx, nil, "rev-parse", "--verify", ref+"^{commit}")
	return head, err == nil, err
}

// Paths are the files rev holds, relative to its root.
func (s *Scratch) Paths(ctx context.Context, rev string) ([]string, error) {
	out, err := s.git(ctx, nil, "ls-tree", "-r", "--name-only", "-z", rev)
	if err != nil {
		return nil, err
	}
	var paths []string
	for path := range strings.SplitSeq(out, "\x00") {
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

// File is path as rev holds it, byte for byte; found is false when rev holds
// no such file.
func (s *Scratch) File(ctx context.Context, rev, path string) (body string, found bool, err error) {
	entry, err := s.git(ctx, nil, "ls-tree", "-z", rev, "--", path)
	if err != nil || entry == "" {
		return "", false, err
	}
	body, err = s.gitRaw(ctx, s.env, nil, "cat-file", "blob", rev+":"+path)
	return body, err == nil, err
}

// Tree is rev's tree.
func (s *Scratch) Tree(ctx context.Context, rev string) (string, error) {
	return s.git(ctx, nil, "rev-parse", "--verify", rev+"^{tree}")
}

// TreeWith is base's tree with files, path to body, written over it.
func (s *Scratch) TreeWith(ctx context.Context, base string, files map[string]string) (string, error) {
	index := filepath.Join(s.dir, "scratch-index")
	defer os.Remove(index)
	withIndex := append(append([]string(nil), s.env...), "GIT_INDEX_FILE="+index)
	if _, err := s.gitEnv(ctx, withIndex, nil, "read-tree", base); err != nil {
		return "", err
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		blob, err := s.git(ctx, strings.NewReader(files[path]), "hash-object", "-w", "--stdin")
		if err != nil {
			return "", err
		}
		if _, err := s.gitEnv(ctx, withIndex, nil, "update-index", "--add", "--cacheinfo", "100644,"+blob+","+path); err != nil {
			return "", err
		}
	}
	return s.gitEnv(ctx, withIndex, nil, "write-tree")
}

// IsAncestor reports whether ancestor is rev or one of its ancestors.
func (s *Scratch) IsAncestor(ctx context.Context, ancestor, rev string) (bool, error) {
	_, err := s.git(ctx, nil, "merge-base", "--is-ancestor", ancestor, rev)
	var exit *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return false, nil
	}
	return false, err
}

// Commit makes the commit spec describes and answers it.
func (s *Scratch) Commit(ctx context.Context, spec CommitSpec) (string, error) {
	args := make([]string, 0, 8+2*len(spec.Parents))
	args = append(args, "-c", "user.name="+spec.Name, "-c", "user.email="+spec.Email, "commit-tree", spec.Tree)
	for _, parent := range spec.Parents {
		args = append(args, "-p", parent)
	}
	args = append(args, "-F", "-")
	return s.git(ctx, strings.NewReader(spec.Message), args...)
}

// Push sends commit to HQ as branch. git's own words come back on a
// refusal: HQ's ref rules, a branch moved on by another push.
func (s *Scratch) Push(ctx context.Context, commit, branch string) error {
	_, err := s.git(ctx, nil, "push", "-q", s.url, commit+":refs/heads/"+branch)
	return err
}

func (s *Scratch) git(ctx context.Context, stdin *strings.Reader, args ...string) (string, error) {
	return s.gitEnv(ctx, s.env, stdin, args...)
}

func (s *Scratch) gitEnv(ctx context.Context, env []string, stdin *strings.Reader, args ...string) (string, error) {
	out, err := s.gitRaw(ctx, env, stdin, args...)
	return strings.TrimSpace(out), err
}

// gitRaw is gitEnv answering git's output as it wrote it.
func (s *Scratch) gitRaw(ctx context.Context, env []string, stdin *strings.Reader, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = s.dir
	cmd.Env = env
	if stdin != nil {
		cmd.Stdin = stdin
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		said := strings.TrimSpace(stderr.String())
		if strings.Contains(said, "couldn't find remote ref") {
			return "", errNoSuchRef
		}
		return "", &gitError{args: args[0], said: said, err: err}
	}
	return stdout.String(), nil
}

// gitError is git refusing or failing, in its own words.
type gitError struct {
	args string
	said string
	err  error
}

func (e *gitError) Error() string {
	if e.said == "" {
		return fmt.Sprintf("git %s: %v", e.args, e.err)
	}
	return fmt.Sprintf("git %s: %s", e.args, e.said)
}

func (e *gitError) Unwrap() error { return e.err }
