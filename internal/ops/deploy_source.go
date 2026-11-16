package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
)

// DeploySource yields the deploy source of a commit by HQ's rule: the
// repository's tree at the commit minus what its root `.deployignore` names,
// `export-ignore` off. It is the one interface CommitDeploy reads a commit
// through; today's implementation is zcp's relayed depth-1 scratch applying
// that rule, and an HQ read of the filtered source replaces it in one wiring
// place.
type DeploySource interface {
	// Open streams the deploy source of commit in repository (the name
	// ServiceMeta holds for the service) as the archive the Zerops upload
	// takes. The caller closes it.
	Open(ctx context.Context, repository, commit string) (io.ReadCloser, error)
}

// NewHQDeploySource is the deploy source read from HQ with the project's
// key: a bare scratch repository fetched `--depth=1` at the commit through
// HQ (`Authorization: Project <key>` scoped to HQ's origin, as hq.Scratch
// and hq.RunGit fetch), archived as `git archive --format=tar.gz` and
// streamed, nothing of the archive on disk. The repository's organisation
// and application are HQ's answer to Self at every Open, never the caller's.
func NewHQDeploySource(httpClient hq.Doer, id hq.Identity) DeploySource {
	project, err := hq.OpenProject(httpClient, id)
	return hqDeploySource{project: project, opened: err, key: id.Key}
}

type hqDeploySource struct {
	project hq.ProjectClient
	opened  error
	// key is only cut out of git's words before they leave this file.
	key string
}

func (s hqDeploySource) Open(ctx context.Context, repository, commit string) (io.ReadCloser, error) {
	if s.opened != nil {
		return nil, &deployError{class: platform.ClassRefused, step: "deploy source", err: s.opened}
	}
	self, err := s.project.Self(ctx)
	if err != nil {
		return nil, &deployError{class: hqClass(err), step: "deploy source", err: err}
	}
	repoURL, err := s.project.RepoURL(self.OrgID, self.AppID, repository)
	if err != nil {
		return nil, &deployError{class: platform.ClassRefused, step: "deploy source", err: err}
	}
	dir, err := os.MkdirTemp("", "zcp-deploy-")
	if err != nil {
		return nil, &deployError{class: platform.ClassFailed, step: "deploy source", err: err}
	}
	g := sourceGit{dir: dir, env: append(s.project.GitEnv(), "LC_ALL=C"), key: s.key}
	tree, err := g.prepare(ctx, repoURL, commit)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	archive, err := g.archive(ctx, tree)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return archive, nil
}

// hqClass is an HQ answer's class: HQ out of reach may change by waiting;
// any refusal of HQ's will not.
func hqClass(err error) platform.ErrorClass {
	if hq.IsUnavailable(err) {
		return platform.ClassUnreachable
	}
	return platform.ClassRefused
}

// sourceGit is git in one deploy source's scratch directory.
type sourceGit struct {
	dir string
	env []string
	key string
}

// deployIgnore is the root file whose lines, in gitignore syntax, leave
// paths out of a deploy (HQ internal/deploys/source).
const deployIgnore = ".deployignore"

// prepare fetches commit and answers the tree to archive: the commit's
// tree, less what its root .deployignore names when it has one. git's own
// ignore matching reads the file, case-insensitive as HQ's is, and leaves
// out everything under a directory it leaves out whatever a later rule
// says, as HQ does.
func (g sourceGit) prepare(ctx context.Context, repoURL, commit string) (string, error) {
	if _, err := g.run(ctx, nil, "init", "--bare", "-q"); err != nil {
		return "", err
	}
	if _, err := g.run(ctx, nil, "-c", "http.followRedirects=false", "fetch", "-q", "--no-tags", "--depth=1", repoURL, commit); err != nil {
		return "", err
	}
	// export-ignore off: an in-tree .gitattributes cannot drop a path from
	// the archive; info/attributes outranks it.
	if err := os.WriteFile(filepath.Join(g.dir, "info", "attributes"), []byte("* -export-ignore\n"), 0o600); err != nil {
		return "", &deployError{class: platform.ClassFailed, step: "deploy source", err: err}
	}
	rules, err := g.run(ctx, nil, "ls-tree", "-z", commit, "--", deployIgnore)
	if err != nil {
		return "", err
	}
	if rules == "" {
		return commit, nil
	}
	excludes := filepath.Join(g.dir, "deployignore")
	body, err := g.run(ctx, nil, "cat-file", "blob", commit+":"+deployIgnore)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(excludes, []byte(body), 0o600); err != nil {
		return "", &deployError{class: platform.ClassFailed, step: "deploy source", err: err}
	}
	paths, err := g.run(ctx, nil, "ls-tree", "-r", "-z", "--name-only", commit)
	if err != nil {
		return "", err
	}
	empty := filepath.Join(g.dir, "no-worktree")
	if err := os.Mkdir(empty, 0o700); err != nil {
		return "", &deployError{class: platform.ClassFailed, step: "deploy source", err: err}
	}
	ignored, err := g.run(ctx, strings.NewReader(paths), "--work-tree="+empty, "-c", "core.excludesFile="+excludes,
		"-c", "core.ignoreCase=true", "check-ignore", "--no-index", "--stdin", "-z")
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return commit, nil // no path matched
	}
	if err != nil {
		return "", err
	}
	index := filepath.Join(g.dir, "deploy-index")
	withIndex := sourceGit{dir: g.dir, env: append(append([]string(nil), g.env...), "GIT_INDEX_FILE="+index), key: g.key}
	if _, err := withIndex.run(ctx, nil, "read-tree", commit); err != nil {
		return "", err
	}
	if _, err := withIndex.run(ctx, strings.NewReader(ignored), "--work-tree="+empty, "update-index", "--force-remove", "-z", "--stdin"); err != nil {
		return "", err
	}
	tree, err := withIndex.run(ctx, nil, "write-tree")
	return strings.TrimSpace(tree), err
}

// run is git in the scratch with its output; a failure is git's words,
// the key cut out.
func (g sourceGit) run(ctx context.Context, stdin io.Reader, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir, cmd.Env, cmd.Stdin = g.dir, g.env, stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", g.failure(args, stderr.String(), err)
	}
	return stdout.String(), nil
}

// failure is a git step that did not run through, classed: a fetch HQ did
// not answer may change by waiting; a fetch HQ refused (a missing commit,
// a refused key) will not; any other git step failed here.
func (g sourceGit) failure(args []string, said string, err error) error {
	said = strings.TrimSpace(said)
	if g.key != "" {
		said = strings.ReplaceAll(said, g.key, "<key>")
	}
	step := "git"
	for _, a := range args {
		if !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
			step = "git " + a
			break
		}
	}
	class := platform.ClassFailed
	if step == "git fetch" {
		class = fetchClass(said)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		said = strings.TrimSpace(said + " " + err.Error())
	}
	return &deployError{class: class, step: step, words: said, err: err}
}

// fetchClass reads git's own words on a failed fetch, the only signal git
// gives: no connection or a 5xx/429 of HQ may change by waiting; anything
// else (401, 403, 404, a commit HQ does not have) will not.
func fetchClass(said string) platform.ErrorClass {
	for _, transient := range []string{
		"Could not resolve host", "Failed to connect", "Connection timed out", "Operation timed out",
		"Connection reset", "returned error: 5", "returned error: 429",
	} {
		if strings.Contains(said, transient) {
			return platform.ClassUnreachable
		}
	}
	return platform.ClassRefused
}

// archive starts `git archive --format=tar.gz tree` and answers its stream:
// the archive's end is git's exit, so a failed archive never reads as a
// whole one; Close stops git and removes the scratch.
func (g sourceGit) archive(ctx context.Context, tree string) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, "git", "archive", "--format=tar.gz", tree)
	cmd.Dir, cmd.Env = g.dir, g.env
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, &deployError{class: platform.ClassFailed, step: "git archive", err: err}
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, &deployError{class: platform.ClassFailed, step: "git archive", err: err}
	}
	return &archiveStream{git: g, cmd: cmd, out: stdout, stderr: stderr}, nil
}

type archiveStream struct {
	git    sourceGit
	cmd    *exec.Cmd
	out    io.ReadCloser
	stderr *bytes.Buffer

	once   sync.Once
	waited error
}

func (a *archiveStream) Read(p []byte) (int, error) {
	n, err := a.out.Read(p)
	if err == io.EOF {
		if werr := a.wait(); werr != nil {
			return n, a.git.failure([]string{"archive"}, a.stderr.String(), werr)
		}
	}
	return n, err
}

// wait reaps git once: what Read's end and Close share.
func (a *archiveStream) wait() error {
	a.once.Do(func() { a.waited = a.cmd.Wait() })
	return a.waited
}

func (a *archiveStream) Close() error {
	_ = a.out.Close()
	_ = a.cmd.Process.Kill()
	_ = a.wait()
	if err := os.RemoveAll(a.git.dir); err != nil {
		return fmt.Errorf("remove deploy source scratch: %w", err)
	}
	return nil
}
