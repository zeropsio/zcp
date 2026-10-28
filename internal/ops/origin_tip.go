package ops

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

// An origin read without any checkout (docs/spec-workflows.md §10 P-LP-19,
// P-LP-20): what a part whose dev half is a dev pool is launched, exported
// and released from. A pool has no checkout of its own — its hostname
// reaches a random one of its containers — so zcp works on the part's origin
// itself: a scratch repository in its own process, never a container.

// OriginTip is what an origin's ref holds at its tip: the commit and the
// root zerops.yaml — else zerops.yml — that commit holds, "" for neither.
type OriginTip struct {
	Ref        string
	CommitSHA  string
	ZeropsYAML string
}

// ErrOriginRefMissing is an origin that holds no such ref.
var ErrOriginRefMissing = errors.New("the origin holds no such ref")

// ErrOriginTagExists is a tag the origin already holds.
var ErrOriginTagExists = errors.New("the origin already holds this tag")

// originTipRef is where the scratch keeps the fetched tip.
const originTipRef = "refs/zcp/origin-tip"

// ReadOriginTip reads ref of the origin at remoteURL at its tip. The commit
// and the zerops.yaml come from one fetch, so the zerops.yaml is the one the
// commit holds. remoteURL is fetched exactly as given (DP-18: a plain git
// server refuses `…/repo` for `…/repo.git`). token, when not empty, is the
// credential git answers the origin's host with, through its environment
// alone (originTipGit). An absent ref is ErrOriginRefMissing; any other
// failure carries git's own words, never the token (originScratch.run).
func ReadOriginTip(ctx context.Context, remoteURL, ref, token string) (OriginTip, error) {
	s, err := openOriginScratch(ctx, remoteURL, token)
	if err != nil {
		return OriginTip{}, err
	}
	defer s.close()
	tip, err := s.fetchTip(ref)
	if err != nil {
		return OriginTip{}, err
	}
	for _, name := range []string{"zerops.yaml", "zerops.yml"} {
		entry, err := s.run("ls-tree", "--name-only", tip.CommitSHA, "--", name)
		if err != nil {
			return OriginTip{}, fmt.Errorf("list %s at %s: %w", name, tip.CommitSHA, err)
		}
		if strings.TrimSpace(entry) == "" {
			continue
		}
		body, err := s.run("cat-file", "blob", tip.CommitSHA+":"+name)
		if err != nil {
			return OriginTip{}, fmt.Errorf("read %s at %s: %w", name, tip.CommitSHA, err)
		}
		tip.ZeropsYAML = strings.TrimRight(body, "\n")
		break
	}
	return tip, nil
}

// ReadOriginTags lists the origin's tags matching pattern (a `git ls-remote
// --tags` pattern such as `v*`), sorted, each named once — an annotated
// tag's peeled line folds into its name. Credential and failures as
// ReadOriginTip.
func ReadOriginTags(ctx context.Context, remoteURL, pattern, token string) ([]string, error) {
	s, err := openOriginScratch(ctx, remoteURL, token)
	if err != nil {
		return nil, err
	}
	defer s.close()
	out, err := s.run("ls-remote", "--tags", "--", remoteURL, pattern)
	if err != nil {
		return nil, fmt.Errorf("list the tags of %s: %w", s.where(), err)
	}
	seen := map[string]bool{}
	var tags []string
	for line := range strings.SplitSeq(out, "\n") {
		_, ref, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(ref, "refs/tags/"), "^{}")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		tags = append(tags, name)
	}
	sort.Strings(tags)
	return tags, nil
}

// TagOriginTip tags the tip of the origin's ref with the annotated tag tag —
// by zcp's deploy identity (DeployGitIdentity), with message — and pushes it
// to the origin, answering the tip it tagged: the release of a pool-keyed
// part (docs/spec-workflows.md §10 P-LP-20), whose pushed state is the
// origin's tip itself. A tag the origin already holds is refused by the
// push, never moved. Credential and failures as ReadOriginTip.
func TagOriginTip(ctx context.Context, remoteURL, ref, tag, message, token string) (OriginTip, error) {
	if tag == "" || strings.HasPrefix(tag, "-") {
		return OriginTip{}, fmt.Errorf("tag the origin: %q is no tag name", tag)
	}
	s, err := openOriginScratch(ctx, remoteURL, token)
	if err != nil {
		return OriginTip{}, err
	}
	defer s.close()
	// Check before mutate: a push of an identical tag object answers
	// "up to date", so a tag the origin holds is refused by name first.
	held, err := s.run("ls-remote", "--tags", "--", remoteURL, "refs/tags/"+tag)
	if err != nil {
		return OriginTip{}, fmt.Errorf("read the tags of %s: %w", s.where(), err)
	}
	if strings.TrimSpace(held) != "" {
		return OriginTip{}, fmt.Errorf("tag %s on %s: %w", tag, s.where(), ErrOriginTagExists)
	}
	tip, err := s.fetchTip(ref)
	if err != nil {
		return OriginTip{}, err
	}
	if _, err := s.run("-c", "user.name="+DeployGitIdentity.Name, "-c", "user.email="+DeployGitIdentity.Email,
		"tag", "-a", "-m", message, "--", tag, tip.CommitSHA); err != nil {
		return OriginTip{}, fmt.Errorf("tag %s at %s: %w", tag, tip.CommitSHA, err)
	}
	if _, err := s.run("push", "-q", "--", remoteURL, "refs/tags/"+tag+":refs/tags/"+tag); err != nil {
		return OriginTip{}, fmt.Errorf("push %s to %s: %w", tag, s.where(), err)
	}
	return tip, nil
}

// originScratch is a bare repository of zcp's own working on one origin.
type originScratch struct {
	ctx        context.Context
	dir        string
	remoteURL  string
	token      string
	args, env  []string
	tokenForms []string
}

// openOriginScratch makes an empty scratch repository for the origin at
// remoteURL; close removes it.
func openOriginScratch(ctx context.Context, remoteURL, token string) (*originScratch, error) {
	dir, err := os.MkdirTemp("", "zcp-origin-")
	if err != nil {
		return nil, fmt.Errorf("origin scratch repository: %w", err)
	}
	args, env := originTipGit(remoteURL, token)
	s := &originScratch{ctx: ctx, dir: dir, remoteURL: remoteURL, token: token, args: args, env: env, tokenForms: originTokenForms(token)}
	if _, err := s.run("init", "--bare", "-q"); err != nil {
		s.close()
		return nil, fmt.Errorf("origin scratch repository: %w", err)
	}
	return s, nil
}

func (s *originScratch) close() { _ = os.RemoveAll(s.dir) }

// where is the origin as a message names it: credentials redacted.
func (s *originScratch) where() string { return topology.RedactRepoURLCredentials(s.remoteURL) }

// fetchTip fetches ref's tip alone — depth 1, no tags — and answers its
// commit; an absent ref is ErrOriginRefMissing.
func (s *originScratch) fetchTip(ref string) (OriginTip, error) {
	if _, err := s.run("fetch", "-q", "--no-tags", "--depth=1", "--", s.remoteURL, "+"+ref+":"+originTipRef); err != nil {
		if strings.Contains(err.Error(), "couldn't find remote ref") {
			return OriginTip{}, fmt.Errorf("fetch %s of %s: %w", ref, s.where(), ErrOriginRefMissing)
		}
		return OriginTip{}, fmt.Errorf("fetch %s of %s: %w", ref, s.where(), err)
	}
	sha, err := s.run("rev-parse", "--verify", originTipRef+"^{commit}")
	if err != nil {
		return OriginTip{}, fmt.Errorf("read the fetched tip of %s: %w", s.where(), err)
	}
	return OriginTip{Ref: ref, CommitSHA: strings.TrimSpace(sha)}, nil
}

// run runs git in the scratch and answers its output. A failure carries
// git's own words with the token taken out in every form a remote could
// echo it back in (originTokenForms) — git's stderr is surfaced, never the
// credential it carried.
func (s *originScratch) run(gitArgs ...string) (string, error) {
	cmd := exec.CommandContext(s.ctx, "git", append(append([]string(nil), s.args...), gitArgs...)...)
	cmd.Dir = s.dir
	cmd.Env = s.env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if said := s.scrub(strings.TrimSpace(stderr.String())); said != "" {
			return "", fmt.Errorf("git: %s: %w", said, err)
		}
		return "", fmt.Errorf("git: %w", err)
	}
	return stdout.String(), nil
}

// scrub takes every form of the token out of text.
func (s *originScratch) scrub(text string) string {
	for _, form := range s.tokenForms {
		text = strings.ReplaceAll(text, form, "<redacted>")
	}
	return text
}

// originTokenForms are the forms a remote could echo token back in:
// verbatim, URL-escaped, and as the Basic credential the helper's answer
// rides in (`oauth2:<token>`, base64) — longest first, so no form is left
// half replaced. None for no token.
func originTokenForms(token string) []string {
	if token == "" {
		return nil
	}
	basic := "oauth2:" + token
	forms := []string{
		base64.StdEncoding.EncodeToString([]byte(basic)),
		base64.RawStdEncoding.EncodeToString([]byte(basic)),
		base64.URLEncoding.EncodeToString([]byte(basic)),
		url.QueryEscape(token),
		url.PathEscape(token),
		token,
	}
	sort.SliceStable(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	return forms
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
