package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

// An origin read without any checkout (docs/spec-workflows.md §10 P-LP-19):
// what a part whose dev half is a dev pool is launched and exported from. A
// pool has no checkout of its own — its hostname reaches a random one of its
// containers — so zcp reads the part's origin itself: one shallow fetch of
// the ref's tip into a scratch repository in its own process, never in a
// container.

// OriginTip is what an origin's ref holds at its tip: the commit and the
// root zerops.yaml — else zerops.yml — that commit holds, "" for neither.
type OriginTip struct {
	Ref        string
	CommitSHA  string
	ZeropsYAML string
}

// ErrOriginRefMissing is an origin that holds no such ref.
var ErrOriginRefMissing = errors.New("the origin holds no such ref")

// originTipRef is where the scratch keeps the fetched tip.
const originTipRef = "refs/zcp/origin-tip"

// ReadOriginTip reads ref of the origin at remoteURL at its tip. The commit
// and the zerops.yaml come from one fetch, so the zerops.yaml is the one the
// commit holds. remoteURL is fetched exactly as given (DP-18: a plain git
// server refuses `…/repo` for `…/repo.git`). token, when not empty, is the
// credential git answers the origin's host with, through its environment
// alone (originTipGit). An absent ref is ErrOriginRefMissing; any other
// failure carries git's own words, never the token.
func ReadOriginTip(ctx context.Context, remoteURL, ref, token string) (OriginTip, error) {
	dir, err := os.MkdirTemp("", "zcp-origin-")
	if err != nil {
		return OriginTip{}, fmt.Errorf("origin scratch repository: %w", err)
	}
	defer os.RemoveAll(dir)
	args, env := originTipGit(remoteURL, token)
	git := func(gitArgs ...string) (string, error) {
		return originTipRun(ctx, dir, env, append(append([]string(nil), args...), gitArgs...))
	}
	if _, err := git("init", "--bare", "-q"); err != nil {
		return OriginTip{}, fmt.Errorf("origin scratch repository: %w", err)
	}
	where := topology.RedactRepoURLCredentials(remoteURL)
	if _, err := git("fetch", "-q", "--no-tags", "--depth=1", "--", remoteURL, "+"+ref+":"+originTipRef); err != nil {
		if strings.Contains(err.Error(), "couldn't find remote ref") {
			return OriginTip{}, fmt.Errorf("fetch %s of %s: %w", ref, where, ErrOriginRefMissing)
		}
		return OriginTip{}, fmt.Errorf("fetch %s of %s: %w", ref, where, err)
	}
	sha, err := git("rev-parse", "--verify", originTipRef+"^{commit}")
	if err != nil {
		return OriginTip{}, fmt.Errorf("read the fetched tip of %s: %w", where, err)
	}
	tip := OriginTip{Ref: ref, CommitSHA: strings.TrimSpace(sha)}
	for _, name := range []string{"zerops.yaml", "zerops.yml"} {
		entry, err := git("ls-tree", "--name-only", tip.CommitSHA, "--", name)
		if err != nil {
			return OriginTip{}, fmt.Errorf("list %s at %s: %w", name, tip.CommitSHA, err)
		}
		if strings.TrimSpace(entry) == "" {
			continue
		}
		body, err := git("cat-file", "blob", tip.CommitSHA+":"+name)
		if err != nil {
			return OriginTip{}, fmt.Errorf("read %s at %s: %w", name, tip.CommitSHA, err)
		}
		tip.ZeropsYAML = strings.TrimRight(body, "\n")
		break
	}
	return tip, nil
}

// originTipGit is the arguments every git call of an origin read starts with
// and its environment. The configured helpers are reset, so none answers
// ahead of zcp's; with a token, a helper scoped to the origin's https host
// answers it from $GIT_TOKEN, which the environment alone carries — never an
// argument a process listing shows. No prompt, no inherited GIT_TOKEN and no
// tracing that could print what the helper answers.
func originTipGit(remoteURL, token string) (args, env []string) {
	args = []string{"-c", "credential.helper="}
	if token != "" {
		if scope, helper, ok := persistedHelperScope(remoteURL, "", gitCredentialHelperShell, gitCredentialHelperShell); ok {
			args = append(args, "-c", "credential.https://"+scope+".helper="+helper)
		}
	}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "GIT_TRACE") || name == "GIT_CURL_VERBOSE" || name == GitTokenEnvKey ||
			name == "GIT_ASKPASS" || name == "SSH_ASKPASS" {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	if token != "" {
		env = append(env, GitTokenEnvKey+"="+token)
	}
	return args, env
}

// originTipRun runs git in dir and answers its output; a failure carries
// git's own words.
func originTipRun(ctx context.Context, dir string, env, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if said := strings.TrimSpace(stderr.String()); said != "" {
			return "", fmt.Errorf("git: %s: %w", said, err)
		}
		return "", fmt.Errorf("git: %w", err)
	}
	return stdout.String(), nil
}
