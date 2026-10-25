package ops

import (
	"bufio"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// poolMigrateCensusSample caps each path list, as the script's `git log -n 10`
// caps the reflog-only sample: the census names what is there, it does not
// list a whole tree (spec-mate.md §1 reducer rule — small keys only). Each
// list's count still covers all of it.
const poolMigrateCensusSample = 10

// PoolMigrateCensus is the unsaved-work census of a dev service's repository
// (docs/spec-workflows.md §13 PM-2): everything its checkout holds that the
// origin does not, across the main checkout and every linked worktree, read
// in one SSH round trip. Reading it changes nothing in the repository — no
// ref, no object, no reflog, no lock, no index.
type PoolMigrateCensus struct {
	// Clean is true when the census names no unsaved work: no staged,
	// modified, conflicted or untracked file, no merge, rebase or similar in
	// progress, no .env file, no commit a detached HEAD alone holds, no
	// local branch commit on no remote branch, no stash entry, no commit
	// only a reflog reaches and no submodule — a repository of its own,
	// whose work the census cannot count and the bundle does not carry.
	// Refs under refs/t3/ and ignored files never make it unclean: Mate's own
	// refs, and files a checkout reproduces from its repository.
	Clean bool `json:"clean"`
	// Trees is the main checkout first, then every linked worktree.
	Trees []PoolMigrateTree `json:"trees"`
	// Branches are the local branches holding commits on no remote branch.
	Branches []PoolMigrateBranch `json:"branches,omitempty"`
	// Stashes are every stash entry, stash@{0} first.
	Stashes []PoolMigrateStash `json:"stashes,omitempty"`
	// ReflogOnly counts the commits only a reflog still reaches — reset,
	// amended or rebased away — with the newest few.
	ReflogOnly *PoolMigrateCommits `json:"reflogOnly,omitempty"`
	// T3Refs counts the refs under refs/t3/ by their namespace
	// (checkpoints, crew, crew-state).
	T3Refs map[string]int `json:"t3Refs,omitempty"`
}

// PoolMigrateTree is one checkout of the repository: the main checkout or a
// linked worktree.
type PoolMigrateTree struct {
	Path string `json:"path"`
	Head string `json:"head,omitempty"`
	// Branch is the branch HEAD is on, "" when detached.
	Branch string `json:"branch,omitempty"`
	// Upstream is the remote branch Branch tracks, nil for none.
	Upstream *PoolMigrateUpstream `json:"upstream,omitempty"`
	Detached bool                 `json:"detached,omitempty"`
	// DetachedCommits counts, on a detached HEAD, the commits it alone holds:
	// on no local branch and no remote branch.
	DetachedCommits int `json:"detachedCommits,omitempty"`
	// Missing says the worktree is registered but its directory is gone.
	Missing bool `json:"missing,omitempty"`
	// InProgress names what the tree is in the middle of: merging, rebasing,
	// cherry-picking, reverting, bisecting.
	InProgress []string          `json:"inProgress,omitempty"`
	Staged     *PoolMigratePaths `json:"staged,omitempty"`
	Modified   *PoolMigratePaths `json:"modified,omitempty"`
	Conflicted *PoolMigratePaths `json:"conflicted,omitempty"`
	// Untracked are the untracked files and directories git does not ignore,
	// a directory holding nothing tracked named once; .env files are named in
	// EnvFiles instead.
	Untracked *PoolMigratePaths `json:"untracked,omitempty"`
	// EnvFiles are the .env and .env.* files git does not carry: untracked or
	// ignored. One inside a directory git ignores as a whole rides that
	// directory's entry in Ignored.
	EnvFiles []string `json:"envFiles,omitempty"`
	// Ignored summarizes what git ignores, a wholly ignored directory named
	// once.
	Ignored *PoolMigratePaths `json:"ignored,omitempty"`
	// Submodules are the tree's submodules (gitlinks in its index): each a
	// repository of its own, its objects never in this repository's, so
	// work inside one — a commit, a stash, an edit — leaves this tree's
	// status clean and rides no bundle. A submodule's status entry is named
	// here, never as a staged or modified path.
	Submodules *PoolMigratePaths `json:"submodules,omitempty"`
}

// PoolMigratePaths is a list of paths: how many, and the first few.
type PoolMigratePaths struct {
	Count int      `json:"count"`
	Paths []string `json:"paths"`
}

// PoolMigrateUpstream is a remote branch a local branch tracks.
type PoolMigrateUpstream struct {
	Remote string `json:"remote"`
	Branch string `json:"branch"`
}

// PoolMigrateBranch is a local branch holding commits on no remote branch.
type PoolMigrateBranch struct {
	Name     string               `json:"name"`
	Unpushed int                  `json:"unpushed"`
	Upstream *PoolMigrateUpstream `json:"upstream,omitempty"`
}

// PoolMigrateStash is one stash entry.
type PoolMigrateStash struct {
	Ref     string `json:"ref"`
	Commit  string `json:"commit"`
	Message string `json:"message"`
}

// PoolMigrateCommits counts commits, with the newest few.
type PoolMigrateCommits struct {
	Count   int                 `json:"count"`
	Commits []PoolMigrateCommit `json:"commits,omitempty"`
}

// PoolMigrateCommit is one commit by its sha and subject.
type PoolMigrateCommit struct {
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
}

// poolMigrateEnvFilePattern is a .env or .env.* file at any depth (an extended
// regex for grep, the shape DM-7's self-deploy pre-flight names).
const poolMigrateEnvFilePattern = `(^|/)\.env(\..*)?$`

// poolMigrateCensusScript is the census, run in the repository's root. Every
// git command in it only reads: GIT_OPTIONAL_LOCKS=0 keeps `git status` from
// refreshing and rewriting the index it reads, and nothing else it runs writes
// at all — no stash create, no update-ref, no fetch. Each fact is a line
// tagged by its kind; a step that fails prints a zcp-census-error line, and
// the last line is zcp-census-end, so a census cut short never reads as
// clean. `git worktree list --porcelain` prints a path verbatim, a newline in
// it included, so a worktree path holding one — which the lines would cut,
// reading the worktree as missing — is no census: its -z listing finds it.
//
//	W <line>                    git worktree list --porcelain
//	B <ref>\t<remote>\t<ref>    a local branch and the remote branch it tracks
//	U <ref>\t<n>                its commits on no remote branch
//	D <sha>\t<n>                a detached HEAD's commits on no branch and no remote
//	S <ref>\t<sha>\t<subject>   a stash entry
//	RN <n>, RS <sha>\t<subject> commits only a reflog reaches, and the newest
//	X <ref>                     a ref under refs/t3/
//	T <path>                    the tree the following tree lines describe:
//	TM                          its directory is gone
//	TS <line>                   git status --porcelain=v2 --branch
//	TX <operation>              merging, rebasing, cherry-picking, reverting, bisecting
//	TE <path>                   a .env file git does not carry
//	TI <path>                   an ignored path
//	TG <path>                   a submodule: a gitlink in the tree's index
const poolMigrateCensusScript = `export GIT_OPTIONAL_LOCKS=0 LC_ALL=C
set -f
if [ ! -e .git ]; then echo "$(pwd) holds no .git" >&2; exit 3; fi
zl() { while IFS= read -r l; do printf '%s %s\n' "$1" "$l"; done; }
zrun() { t=$1; shift; o=$("$@") || { echo "zcp-census-error $t"; return 1; }; if [ -n "$o" ]; then printf '%s\n' "$o" | zl "$t"; fi; return 0; }
echo 'zcp-census 1'
wts=$(git worktree list --porcelain) || { echo 'zcp-census-error worktree list'; exit 4; }
if git worktree list --porcelain -z 2>/dev/null | tr '\n\0' '\001\n' | grep -q "^worktree .*$(printf '\001')"; then echo 'zcp-census-error a worktree path holding a newline (move that worktree to a path without one)'; exit 4; fi
printf '%s\n' "$wts" | zl W
zrun B git for-each-ref --format='%(refname)%09%(upstream:remotename)%09%(upstream:remoteref)' refs/heads/ || exit 4
heads=$(git for-each-ref --format='%(refname)' refs/heads/) || { echo 'zcp-census-error branches'; exit 4; }
for r in $heads; do
  n=$(git rev-list --count "$r" --not --remotes) || { echo "zcp-census-error unpushed $r"; exit 4; }
  printf 'U %s\t%s\n' "$r" "$n"
done
printf '%s\n' "$wts" | while IFS= read -r l; do
  case $l in
    'HEAD '*) h=${l#HEAD } ;;
    detached) n=$(git rev-list --count "$h" --not --branches --remotes) || { echo "zcp-census-error detached $h"; exit 4; }; printf 'D %s\t%s\n' "$h" "$n" ;;
  esac
done
zrun S git stash list --format='%gd%x09%H%x09%gs' || exit 4
stashes=$(git stash list --format=%H) || { echo 'zcp-census-error stashes'; exit 4; }
zrun RN git rev-list --count --reflog --not --all $stashes || exit 4
zrun RS git log --no-color --no-show-signature -n 10 --format='%H%x09%s' --reflog --not --all $stashes || exit 4
zrun X git for-each-ref --format='%(refname)' refs/t3/ || exit 4
printf '%s\n' "$wts" | sed -n 's/^worktree //p' | while IFS= read -r p; do
  printf 'T %s\n' "$p"
  if [ ! -d "$p" ]; then echo 'TM'; continue; fi
  ( cd "$p" || exit 4
    zrun TS git status --porcelain=v2 --branch --untracked-files=normal || exit 4
    g=$(git rev-parse --git-dir) || exit 4
    if [ -f "$g/MERGE_HEAD" ]; then echo 'TX merging'; fi
    if [ -d "$g/rebase-merge" ] || [ -d "$g/rebase-apply" ]; then echo 'TX rebasing'; fi
    if [ -f "$g/CHERRY_PICK_HEAD" ]; then echo 'TX cherry-picking'; fi
    if [ -f "$g/REVERT_HEAD" ]; then echo 'TX reverting'; fi
    if [ -f "$g/BISECT_LOG" ]; then echo 'TX bisecting'; fi
    gl=$(git ls-files -s) || exit 4
    printf '%s\n' "$gl" | grep '^160000 ' | cut -f2 | zl TG
    ig=$(git ls-files --others --ignored --exclude-standard --directory) || exit 4
    un=$(git ls-files --others --exclude-standard) || exit 4
    if [ -n "$ig" ]; then printf '%s\n' "$ig" | zl TI; fi
    printf '%s\n%s\n' "$un" "$ig" | grep -E '` + poolMigrateEnvFilePattern + `' | zl TE
    exit 0 ) || echo "zcp-census-error tree $p"
done
echo 'zcp-census-end'`

// PoolMigrateCensusCommand is the one SSH command that reads the census of the
// repository rooted at root (the dev service's /var/www).
func PoolMigrateCensusCommand(root string) string {
	return "cd " + shellQuote(root) + " || exit 3\n" + poolMigrateCensusScript
}

// ReadPoolMigrateCensus reads the unsaved-work census of the repository rooted
// at root on the dev service hostname, in one SSH round trip of read-only git.
// An SSH failure keeps its *platform.SSHExecError (git's stderr rides its
// Output); a census the script could not finish is an error naming the step,
// never a clean census.
func ReadPoolMigrateCensus(ctx context.Context, ssh SSHDeployer, hostname, root string) (*PoolMigrateCensus, error) {
	out, err := ssh.ExecSSH(ctx, hostname, PoolMigrateCensusCommand(root))
	if err != nil {
		return nil, fmt.Errorf("census of %s: %w", hostname, err)
	}
	census, err := parsePoolMigrateCensus(string(out))
	if err != nil {
		return nil, fmt.Errorf("census of %s: %w", hostname, err)
	}
	return census, nil
}

// poolMigrateCensusParse is the parser's working state: trees by path in
// worktree order, and what the repository-level lines say per ref and sha.
type poolMigrateCensusParse struct {
	trees     []*PoolMigrateTree
	byPath    map[string]*PoolMigrateTree
	current   *PoolMigrateTree
	upstreams map[string]*PoolMigrateUpstream // by refs/heads/<name>
	unpushed  []PoolMigrateBranch
	detached  map[string]int // by HEAD sha
	staged    map[*PoolMigrateTree][]string
	modified  map[*PoolMigrateTree][]string
	conflict  map[*PoolMigrateTree][]string
	untracked map[*PoolMigrateTree][]string
	ignored   map[*PoolMigrateTree][]string
	gitlinks  map[*PoolMigrateTree][]string
	census    PoolMigrateCensus
	failed    []string
	noise     []string
}

// parsePoolMigrateCensus reads the census script's output.
func parsePoolMigrateCensus(out string) (*PoolMigrateCensus, error) {
	p := &poolMigrateCensusParse{
		byPath:    map[string]*PoolMigrateTree{},
		upstreams: map[string]*PoolMigrateUpstream{},
		detached:  map[string]int{},
		staged:    map[*PoolMigrateTree][]string{},
		modified:  map[*PoolMigrateTree][]string{},
		conflict:  map[*PoolMigrateTree][]string{},
		untracked: map[*PoolMigrateTree][]string{},
		ignored:   map[*PoolMigrateTree][]string{},
		gitlinks:  map[*PoolMigrateTree][]string{},
	}
	var header, end bool
	var worktree []string
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "zcp-census 1":
			header = true
			continue
		case line == "zcp-census-end":
			end = true
			continue
		case strings.HasPrefix(line, "zcp-census-error "):
			p.failed = append(p.failed, strings.TrimPrefix(line, "zcp-census-error "))
			continue
		}
		tag, rest, _ := strings.Cut(line, " ")
		switch tag {
		case "W":
			worktree = append(worktree, rest)
		case "B", "U", "D", "S", "RN", "RS", "X":
			p.repoLine(tag, rest)
		case "T":
			p.current = p.tree(rest)
		case "TM", "TS", "TX", "TE", "TI", "TG":
			p.treeLine(tag, rest)
		default:
			if strings.TrimSpace(line) != "" {
				p.noise = append(p.noise, line)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read the census: %w", err)
	}
	if !header {
		return nil, fmt.Errorf("the dev service answered no census: %s", strings.Join(p.noise, "; "))
	}
	if len(p.failed) > 0 {
		return nil, fmt.Errorf("the census could not read %s: %s", strings.Join(p.failed, ", "), strings.Join(p.noise, "; "))
	}
	if !end {
		return nil, fmt.Errorf("the census came back incomplete")
	}
	p.worktrees(worktree)
	return p.finish(), nil
}

// tree returns the tree at path, adding it in the order first seen.
func (p *poolMigrateCensusParse) tree(path string) *PoolMigrateTree {
	if t, ok := p.byPath[path]; ok {
		return t
	}
	t := &PoolMigrateTree{Path: path}
	p.trees = append(p.trees, t)
	p.byPath[path] = t
	return t
}

// worktrees reads `git worktree list --porcelain`: blocks of worktree, HEAD,
// branch or detached, and prunable lines. Its order — the main checkout
// first — is the trees' order.
func (p *poolMigrateCensusParse) worktrees(lines []string) {
	byPath, sections := p.byPath, p.trees
	p.trees, p.byPath = nil, map[string]*PoolMigrateTree{}
	var cur *PoolMigrateTree
	for _, l := range lines {
		key, val, _ := strings.Cut(l, " ")
		switch key {
		case porcelainWorktree:
			cur = byPath[val]
			if cur == nil {
				cur = &PoolMigrateTree{Path: val}
			}
			p.trees = append(p.trees, cur)
			p.byPath[val] = cur
		case porcelainHead:
			if cur != nil {
				cur.Head = val
			}
		case porcelainBranch:
			if cur != nil {
				cur.Branch = strings.TrimPrefix(val, "refs/heads/")
			}
		case "detached":
			if cur != nil {
				cur.Detached = true
			}
		}
	}
	// A tree section the porcelain did not list is still named.
	for _, t := range sections {
		if _, listed := p.byPath[t.Path]; !listed {
			p.trees = append(p.trees, t)
			p.byPath[t.Path] = t
		}
	}
}

func (p *poolMigrateCensusParse) repoLine(tag, rest string) {
	fields := strings.Split(rest, "\t")
	switch tag {
	case "B":
		if len(fields) == 3 && fields[1] != "" {
			p.upstreams[fields[0]] = &PoolMigrateUpstream{Remote: fields[1], Branch: strings.TrimPrefix(fields[2], "refs/heads/")}
		}
	case "U":
		if len(fields) == 2 {
			if n, _ := strconv.Atoi(fields[1]); n > 0 {
				p.unpushed = append(p.unpushed, PoolMigrateBranch{Name: fields[0], Unpushed: n})
			}
		}
	case "D":
		if len(fields) == 2 {
			p.detached[fields[0]], _ = strconv.Atoi(fields[1])
		}
	case "S":
		if len(fields) == 3 {
			p.census.Stashes = append(p.census.Stashes, PoolMigrateStash{Ref: fields[0], Commit: fields[1], Message: fields[2]})
		}
	case "RN":
		if n, _ := strconv.Atoi(rest); n > 0 {
			if p.census.ReflogOnly == nil {
				p.census.ReflogOnly = &PoolMigrateCommits{}
			}
			p.census.ReflogOnly.Count = n
		}
	case "RS":
		if len(fields) == 2 {
			if p.census.ReflogOnly == nil {
				p.census.ReflogOnly = &PoolMigrateCommits{}
			}
			p.census.ReflogOnly.Commits = append(p.census.ReflogOnly.Commits, PoolMigrateCommit{Commit: fields[0], Subject: fields[1]})
		}
	case "X":
		ns, _, _ := strings.Cut(strings.TrimPrefix(rest, "refs/t3/"), "/")
		if ns != "" {
			if p.census.T3Refs == nil {
				p.census.T3Refs = map[string]int{}
			}
			p.census.T3Refs[ns]++
		}
	}
}

func (p *poolMigrateCensusParse) treeLine(tag, rest string) {
	t := p.current
	if t == nil {
		return
	}
	switch tag {
	case "TM":
		t.Missing = true
	case "TX":
		t.InProgress = append(t.InProgress, rest)
	case "TE":
		t.EnvFiles = append(t.EnvFiles, rest)
	case "TI":
		p.ignored[t] = append(p.ignored[t], rest)
	case "TG":
		p.gitlinks[t] = append(p.gitlinks[t], rest)
	case "TS":
		p.statusLine(t, rest)
	}
}

// statusLine reads one `git status --porcelain=v2` entry: `1`/`2` changed
// (X staged, Y unstaged), `u` unmerged, `?` untracked.
func (p *poolMigrateCensusParse) statusLine(t *PoolMigrateTree, line string) {
	kind, rest, _ := strings.Cut(line, " ")
	switch kind {
	case "1", "2":
		// 1 XY sub mH mI mW hH hI path; 2 adds Xscore and ends path\torig.
		n := 8
		if kind == "2" {
			n = 9
		}
		f := strings.SplitN(rest, " ", n)
		if len(f) < n || len(f[0]) != 2 {
			return
		}
		path, _, _ := strings.Cut(f[n-1], "\t")
		if strings.HasPrefix(f[1], "S") {
			// A submodule: Submodules names it, whatever it holds.
			return
		}
		if f[0][0] != '.' {
			p.staged[t] = append(p.staged[t], path)
		}
		if f[0][1] != '.' {
			p.modified[t] = append(p.modified[t], path)
		}
	case "u":
		// u XY sub m1 m2 m3 mW h1 h2 h3 path
		if f := strings.SplitN(rest, " ", 10); len(f) == 10 {
			p.conflict[t] = append(p.conflict[t], f[9])
		}
	case "?":
		p.untracked[t] = append(p.untracked[t], rest)
	}
}

// finish assembles the census and decides Clean.
func (p *poolMigrateCensusParse) finish() *PoolMigrateCensus {
	c := p.census
	clean := len(p.unpushed) == 0 && len(c.Stashes) == 0 && c.ReflogOnly == nil
	for _, b := range p.unpushed {
		b.Name = strings.TrimPrefix(b.Name, "refs/heads/")
		b.Upstream = p.upstreams["refs/heads/"+b.Name]
		c.Branches = append(c.Branches, b)
	}
	for _, t := range p.trees {
		if t.Branch != "" {
			t.Upstream = p.upstreams["refs/heads/"+t.Branch]
		}
		if t.Detached {
			t.DetachedCommits = p.detached[t.Head]
		}
		untracked := slices.DeleteFunc(p.untracked[t], func(path string) bool { return slices.Contains(t.EnvFiles, path) })
		t.Staged = poolMigratePathList(p.staged[t])
		t.Modified = poolMigratePathList(p.modified[t])
		t.Conflicted = poolMigratePathList(p.conflict[t])
		t.Untracked = poolMigratePathList(untracked)
		t.Ignored = poolMigratePathList(p.ignored[t])
		t.Submodules = poolMigratePathList(p.gitlinks[t])
		if t.Staged != nil || t.Modified != nil || t.Conflicted != nil || t.Untracked != nil ||
			len(t.InProgress) > 0 || len(t.EnvFiles) > 0 || t.DetachedCommits > 0 || t.Submodules != nil {
			clean = false
		}
		c.Trees = append(c.Trees, *t)
	}
	c.Clean = clean
	return &c
}

// poolMigratePathList is paths counted and capped, nil for none.
func poolMigratePathList(paths []string) *PoolMigratePaths {
	if len(paths) == 0 {
		return nil
	}
	return &PoolMigratePaths{Count: len(paths), Paths: slices.Clone(paths[:min(len(paths), poolMigrateCensusSample)])}
}
