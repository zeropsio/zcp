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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
)

// DeploySource yields the deploy source of a commit by HQ's rule: the
// repository's tree at the commit minus what its root `.deployignore` names,
// `export-ignore` off, and the zerops.yaml the build reads from the commit
// itself. It is the one interface CommitDeploy reads a commit through;
// today's implementation is zcp's relayed depth-1 scratch applying that
// rule, and an HQ read of the filtered source replaces it in one wiring
// place.
type DeploySource interface {
	// Open reads commit in repository (as ServiceMeta holds it for the
	// service: `<appId>/<name>`): the archive the Zerops upload takes and
	// the commit's zerops.yaml. The caller closes the archive.
	Open(ctx context.Context, repository, commit string) (CommitSource, error)
}

// CommitSource is a commit's deploy source, opened.
type CommitSource struct {
	// Archive streams the deploy source as tar.gz.
	Archive io.ReadCloser
	// ZeropsYaml is the commit's root zerops.yaml (zerops.yml when it has
	// none), read from the commit as HQ reads it, whatever .deployignore
	// leaves out of the archive.
	ZeropsYaml string
}

// NewHQDeploySource is the deploy source read from HQ with the project's
// key: a bare scratch repository fetched `--depth=1` at the commit through
// HQ at the address and with the authorization zcp's other git against HQ
// uses (hq.RepoURLAt, the builder hq.Scratch reads through; `Authorization:
// Project <key>` scoped to HQ's origin), archived as HQ's git box archives
// (`--worktree-attributes` over an empty work tree and index,
// `tar.umask=0022`) and streamed, nothing of the archive on disk. HQ's
// project door is not asked (it admits a plain project only, never a Mate):
// the application is the repository's, as the stand-up read it from HQ,
// and the organisation is id's.
func NewHQDeploySource(httpClient hq.Doer, id hq.Identity) DeploySource {
	project, err := hq.OpenProject(httpClient, id)
	return hqDeploySource{project: project, opened: err, id: id}
}

type hqDeploySource struct {
	// project lends only its git environment (as hq.RunGit takes it).
	project hq.ProjectClient
	opened  error
	id      hq.Identity
}

// scratchPrefix names a deploy source's scratch directory by the process
// that made it, so one whose process is gone is known as left behind.
const scratchPrefix = "zcp-deploy-"

func (s hqDeploySource) Open(ctx context.Context, repository, commit string) (CommitSource, error) {
	if s.opened != nil {
		return CommitSource{}, &deployError{class: platform.ClassRefused, step: "deploy source", err: s.opened}
	}
	appID, name, ok := strings.Cut(repository, "/")
	if !ok || appID == "" || name == "" {
		return CommitSource{}, &deployError{class: platform.ClassRefused, step: "deploy source",
			words: fmt.Sprintf("repository %q is not <appId>/<name>", repository)}
	}
	repoURL, err := hq.RepoURLAt(s.id.Origin, s.id.OrgID, appID, name)
	if err != nil {
		return CommitSource{}, &deployError{class: platform.ClassRefused, step: "deploy source", err: err}
	}
	removeLeftScratches()
	dir, err := os.MkdirTemp("", scratchPrefix+strconv.Itoa(os.Getpid())+"-")
	if err != nil {
		return CommitSource{}, &deployError{class: platform.ClassFailed, step: "deploy source", err: err}
	}
	g := sourceGit{dir: dir, env: append(s.project.GitEnv(), "LC_ALL=C"), key: s.id.Key}
	opened, err := g.open(ctx, repoURL, commit)
	if err != nil {
		_ = os.RemoveAll(dir)
		return CommitSource{}, err
	}
	return opened, nil
}

// removeLeftScratches removes the scratch directories of deploy sources
// whose process is gone (a zcp killed mid-deploy); a live process's,
// this one's among them, stay.
func removeLeftScratches() {
	dirs, _ := filepath.Glob(filepath.Join(os.TempDir(), scratchPrefix+"*"))
	for _, dir := range dirs {
		pidText, _, _ := strings.Cut(strings.TrimPrefix(filepath.Base(dir), scratchPrefix), "-")
		pid, err := strconv.Atoi(pidText)
		if err != nil || pid == os.Getpid() || !processGone(pid) {
			continue
		}
		_ = os.RemoveAll(dir)
	}
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

// gitWaitDelay bounds how long git may hold its pipes after the caller's
// stop.
const gitWaitDelay = 5 * time.Second

// fetchLowSpeed is the fetch's bound: git gives up once it moved under
// 1 KiB/s for a minute, so a fetch HQ stopped answering ends.
var fetchLowSpeed = []string{"-c", "http.lowSpeedLimit=1024", "-c", "http.lowSpeedTime=60"}

// open fetches commit and answers its zerops.yaml and its archive: the
// commit's tree, less what its root .deployignore names when it has one.
// git's own ignore matching reads the file, case-insensitive as HQ's is,
// and leaves out everything under a directory it leaves out whatever a
// later rule says, as HQ does.
func (g sourceGit) open(ctx context.Context, repoURL, commit string) (CommitSource, error) {
	if _, err := g.run(ctx, nil, "init", "--bare", "-q"); err != nil {
		return CommitSource{}, err
	}
	fetch := append(append([]string{"-c", "http.followRedirects=false"}, fetchLowSpeed...),
		"fetch", "-q", "--no-tags", "--depth=1", repoURL, commit)
	if _, err := g.run(ctx, nil, fetch...); err != nil {
		return CommitSource{}, err
	}
	yaml, err := g.zeropsYaml(ctx, commit)
	if err != nil {
		return CommitSource{}, err
	}
	empty := filepath.Join(g.dir, "no-worktree")
	if err := os.Mkdir(empty, 0o700); err != nil {
		return CommitSource{}, &deployError{class: platform.ClassFailed, step: "deploy source", err: err}
	}
	tree, err := g.deployTree(ctx, commit, empty)
	if err != nil {
		return CommitSource{}, err
	}
	archive, err := g.archive(ctx, tree, empty)
	if err != nil {
		return CommitSource{}, err
	}
	return CommitSource{Archive: archive, ZeropsYaml: yaml}, nil
}

// zeropsYaml is the commit's root zerops.yaml, zerops.yml when it has
// none, in HQ's order (internal/deploys yamlNames). A commit with neither is
// refused: nothing would build it.
func (g sourceGit) zeropsYaml(ctx context.Context, commit string) (string, error) {
	for _, name := range []string{zeropsYmlPrimary, zeropsYmlFallback} {
		entry, err := g.run(ctx, nil, "ls-tree", "-z", commit, "--", name)
		if err != nil {
			return "", err
		}
		if entry != "" {
			return g.run(ctx, nil, "cat-file", "blob", commit+":"+name)
		}
	}
	return "", &deployError{class: platform.ClassRefused, step: "commit deploy",
		words: fmt.Sprintf("commit %s has no %s or %s at its root", commit, zeropsYmlPrimary, zeropsYmlFallback)}
}

// deployTree is the tree to archive: commit's own, or, when it has a root
// .deployignore, a tree written without the paths that file leaves out.
func (g sourceGit) deployTree(ctx context.Context, commit, empty string) (string, error) {
	rules, err := g.run(ctx, nil, "ls-tree", "-z", commit, "--", deployIgnore)
	if err != nil || rules == "" {
		return commit, err
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
	cmd.Dir, cmd.Env, cmd.Stdin, cmd.WaitDelay = g.dir, g.env, stdin, gitWaitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err() // the caller's stop, no failure of git's
		}
		return "", g.failure(args, stderr.String(), err)
	}
	return stdout.String(), nil
}

// failure is a git step that did not run through, classed: a fetch HQ
// refused (a missing commit, a refused key) will not change by waiting;
// any other failed fetch (no connection, a transfer cut, a 5xx) may; any
// other git step failed here.
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
// gives: HQ's refusal (a 4xx other than 429, a commit or repository it does
// not have) will not change by waiting; anything else may.
func fetchClass(said string) platform.ErrorClass {
	if strings.Contains(said, "returned error: 429") {
		return platform.ClassUnreachable
	}
	for _, refusal := range []string{"returned error: 4", "not our ref", "Repository not found"} {
		if strings.Contains(said, refusal) {
			return platform.ClassRefused
		}
	}
	return platform.ClassUnreachable
}

// archive starts git archive of tree as HQ's git box runs it and answers
// its stream: the archive's end is git's exit, so a failed archive never
// reads as a whole one; Close stops git and removes the scratch.
func (g sourceGit) archive(ctx context.Context, tree, empty string) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, "git", "-c", "core.bare=false", "-c", "tar.umask=0022",
		"archive", "--worktree-attributes", "--format=tar.gz", tree)
	cmd.Dir, cmd.WaitDelay = g.dir, gitWaitDelay
	cmd.Env = append(append([]string(nil), g.env...),
		"GIT_DIR="+g.dir, "GIT_WORK_TREE="+empty, "GIT_INDEX_FILE="+filepath.Join(empty, "index"))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, &deployError{class: platform.ClassFailed, step: "git archive", err: err}
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, &deployError{class: platform.ClassFailed, step: "git archive", err: err}
	}
	return &archiveStream{stopped: ctx.Err, git: g, cmd: cmd, out: stdout, stderr: stderr}, nil
}

type archiveStream struct {
	// stopped is the Open context's Err: a git killed by the caller's stop
	// exits as killed, which is no failure of git's.
	stopped func() error
	git     sourceGit
	cmd     *exec.Cmd
	out     io.ReadCloser
	stderr  *bytes.Buffer

	once   sync.Once
	waited error
}

func (a *archiveStream) Read(p []byte) (int, error) {
	n, err := a.out.Read(p)
	if err == io.EOF {
		if werr := a.wait(); werr != nil {
			if stop := a.stopped(); stop != nil {
				return n, stop
			}
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
