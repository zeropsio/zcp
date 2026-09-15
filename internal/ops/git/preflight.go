package git

import (
	"bufio"
	"context"
	"strconv"
	"strings"
)

// notCarriedSampleLimit caps SelfDeployPreflight.NotCarried.Sample — the
// point is a representative sample the agent can act on, not a full
// listing (spec-mate.md §1 reducer rule: small keys only).
const notCarriedSampleLimit = 10

// NotCarried summarizes git-ignored paths present in a self-deploy
// source's working tree that zcli's git archiver (temp index + `git add
// -A` + `git stash create` + `git archive`) never ships — they exist in
// the dev container today and will NOT exist in the replacement container
// after this deploy (the Owner Principle, docs/spec-workflows.md §12.6
// GF-12: a dev checkout is reproducible from its repository and nothing
// else on it is persistent).
type NotCarried struct {
	Count  int      `json:"count"`
	Bytes  int64    `json:"bytes"`
	Sample []string `json:"sample,omitempty"`
}

// SelfDeployPreflight is the combined pre-flight read for a working-tree
// self-deploy (docs/spec-workflows.md §8 DM / §12.6 GF-12): the same HEAD
// sha + dirty facts HeadStatus reads for every deploy, plus repo state
// (clean/dirty/merging/rebasing/detached), the paths that won't survive
// this deploy (NotCarried) and any .env* files present regardless of
// ignore state, and the two hard-refusal conditions (.git is a regular
// file; .gitmodules present). One SSH round trip, never a second script —
// self-deploy only; cross-deploy keeps calling HeadStatus unchanged.
type SelfDeployPreflight struct {
	// GitIsFile and HasSubmodules are read (and must be checked) even
	// when HasRepo is false — a linked-worktree pointer or a stray
	// .gitmodules can exist independently of whether HEAD resolves.
	GitIsFile     bool
	HasSubmodules bool

	HasRepo    bool
	SHA        string
	Dirty      bool
	RepoState  string // "clean" | "dirty" | "merging" | "rebasing" | "detached"
	NotCarried NotCarried
	EnvFiles   []string
}

// selfDeployPreflightScript is the single combined script backing
// ReadSelfDeployPreflight. Every fact is its own "ZCP:"-prefixed marker
// line so a partial/unusual repo state degrades to "field absent" instead
// of corrupting a fixed-column parse.
const selfDeployPreflightScript = `if [ -f .git ]; then echo 'ZCP:GITFILE:1'; else echo 'ZCP:GITFILE:0'; fi
if [ -f .gitmodules ]; then echo 'ZCP:SUBMODULES:1'; else echo 'ZCP:SUBMODULES:0'; fi
HEAD_SHA=$(git rev-parse --verify HEAD 2>/dev/null)
if [ -z "$HEAD_SHA" ]; then
  echo 'ZCP:HASREPO:0'
else
  echo 'ZCP:HASREPO:1'
  echo "ZCP:SHA:$HEAD_SHA"
  if [ -n "$(git status --porcelain | head -c1)" ]; then echo 'ZCP:DIRTY:1'; else echo 'ZCP:DIRTY:0'; fi
  echo "ZCP:REPOSTATE:$(` + repoStateExpr + `)"
  git ls-files --others --ignored --exclude-standard | while IFS= read -r f; do
    sz=$(wc -c <"$f" 2>/dev/null || echo 0)
    echo "ZCP:IGNORED:$sz:$f"
  done
  find . -mindepth 1 -name .git -prune -o -type f \( -name '.env' -o -name '.env.*' \) -print | while IFS= read -r f; do
    echo "ZCP:ENVFILE:${f#./}"
  done
fi`

// ReadSelfDeployPreflight runs selfDeployPreflightScript rooted at dir and
// parses its marker-prefixed output. Facts, never a gate (docs/spec-
// workflows.md §8 DM): the two hard refusals (GitIsFile, HasSubmodules)
// are decided by the CALLER from the parsed struct, never by this
// function failing — a transport error, or a source with no repo at all,
// both come back as a zero-value SelfDeployPreflight with err=nil,
// exactly like HeadStatus's non-fatal-absence contract. The push itself
// still surfaces a real SSH failure; this preflight must never be the
// reason a plain self-deploy stops working.
func ReadSelfDeployPreflight(ctx context.Context, r Runner, dir string) (SelfDeployPreflight, error) {
	out, _, runErr := r.Run(ctx, dir, selfDeployPreflightScript)
	if runErr != nil {
		return SelfDeployPreflight{}, nil //nolint:nilerr // no repo/transport hiccup here must not block deploy — see doc-comment
	}
	return parseSelfDeployPreflight(out), nil
}

func parseSelfDeployPreflight(out string) SelfDeployPreflight {
	var p SelfDeployPreflight
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		rest, ok := strings.CutPrefix(sc.Text(), "ZCP:")
		if !ok {
			continue
		}
		key, val, _ := strings.Cut(rest, ":")
		switch key {
		case "GITFILE":
			p.GitIsFile = val == "1"
		case "SUBMODULES":
			p.HasSubmodules = val == "1"
		case "HASREPO":
			p.HasRepo = val == "1"
		case "SHA":
			p.SHA = val
		case "DIRTY":
			p.Dirty = val == "1"
		case "REPOSTATE":
			p.RepoState = val
		case "IGNORED":
			szStr, path, found := strings.Cut(val, ":")
			if !found {
				continue
			}
			sz, _ := strconv.ParseInt(szStr, 10, 64)
			p.NotCarried.Count++
			p.NotCarried.Bytes += sz
			if len(p.NotCarried.Sample) < notCarriedSampleLimit {
				p.NotCarried.Sample = append(p.NotCarried.Sample, path)
			}
		case "ENVFILE":
			p.EnvFiles = append(p.EnvFiles, val)
		}
	}
	return p
}
