package ops

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// PoolMigrateWorkDir is the scratch directory on a dev service where the
// retire builds the mirror it bundles: outside the repository (GF-2), removed
// before each bundle and after it.
const PoolMigrateWorkDir = "/tmp/zcp-pool-migrate"

// The keys of `git worktree list --porcelain` the census and the bundle read.
const (
	porcelainWorktree = "worktree"
	porcelainHead     = "HEAD"
	porcelainBranch   = "branch"
)

// poolMigrateRefNS is the namespace the bundle keeps everything under that no
// ref of the repository names: stash entries, reflog tips, each tree's HEAD,
// WIP commit and untracked files.
const poolMigrateRefNS = "refs/zcp-migrate/"

// poolMigrateBundleFunctions are the shell functions the bundle and the
// fingerprint share, run in the repository's main checkout with $Z the
// scratch directory and $E the empty tree. zfp prints the fingerprint: every
// ref (R), every commit a reflog mentions — every worktree's HEAD reflog and
// every stash entry included (L) — and per tree, in `git worktree list`
// order: its path (T), a directory gone (M with the HEAD git recorded), or its
// HEAD (H), the blob ids of its `git status --porcelain=v2` and `git ls-files
// -s` (S, I), the trees of its working files and index as `git stash create`
// sees them (W), and the tree of its untracked files (U). Nothing it runs
// writes into the repository but unreferenced objects: git status runs with
// optional locks off, `git stash create` runs against a copy of the index
// (a plain one rewrites the index file), and the untracked files go through
// an index of the scratch directory's own. With zfp's argument set it also
// records, per tree, the WIP commit and untracked tree the bundle keeps, and
// the untracked repositories of its own it does not ($Z/nested).
const poolMigrateBundleFunctions = `zfptree() {
  h=$(git rev-parse --verify -q HEAD) || h=unborn
  printf 'H %s\n' "$h"
  s=$(git status --porcelain=v2 --branch --untracked-files=all) || return 1
  printf 'S %s\n' "$(printf '%s\n' "$s" | git hash-object --stdin)"
  x=$(git ls-files -s) || return 1
  printf 'I %s\n' "$(printf '%s\n' "$x" | git hash-object --stdin)"
  ix=$(git rev-parse --git-path index) || return 1
  w=
  if [ -f "$ix" ]; then
    cp "$ix" "$Z/index" || return 1
    w=$(GIT_INDEX_FILE="$Z/index" git stash create "zcp pool-migrate: the work in $3") || { rm -f "$Z/index"; return 1; }
    rm -f "$Z/index"
  fi
  if [ -n "$w" ]; then printf 'W %s %s\n' "$(git rev-parse "$w^{tree}")" "$(git rev-parse "$w^2^{tree}")"; else echo 'W clean'; fi
  rm -f "$Z/uindex"
  git ls-files -z -o --exclude-standard -- . ':(exclude,glob)**/.env' ':(exclude,glob)**/.env.*' > "$Z/untracked" || return 1
  GIT_INDEX_FILE="$Z/uindex" git update-index --add -z --stdin < "$Z/untracked" 2>/dev/null || return 1
  u=$(GIT_INDEX_FILE="$Z/uindex" git write-tree) || return 1
  rm -f "$Z/uindex" "$Z/untracked"
  printf 'U %s\n' "$u"
  if [ -n "$1" ]; then
    printf '%s %s %s %s\n' "$2" "$h" "${w:--}" "$u" >> "$1"
    git ls-files -o --exclude-standard | sed -n 's#/$##p' | while IFS= read -r n; do printf '%s/%s\n' "$3" "$n"; done >> "$Z/nested"
  fi
}
zfpone() {
  [ -n "$p" ] || return 0
  printf 'T %s\n' "$p"
  if [ ! -d "$p" ]; then
    printf 'M %s\n' "${ph:-unborn}"
    if [ -n "$zside" ]; then printf '%s %s - %s\n' "$i" "${ph:-unborn}" "$E" >> "$zside"; fi
  elif t=$(cd "$p" && zfptree "$zside" "$i" "$p"); then
    printf '%s\n' "$t"
  else
    echo "zcp-bundle-error tree $p"; zfail=1
  fi
  i=$((i+1)); p=; ph=
}
zfp() {
  zside=$1; zfail=
  o=$(git for-each-ref --format='%(objectname) %(refname)') || { echo 'zcp-bundle-error refs'; return 1; }
  if [ -n "$o" ]; then printf '%s\n' "$o" | sed 's/^/R /'; fi
  o=$(git rev-list --no-walk --reflog) || { echo 'zcp-bundle-error reflogs'; return 1; }
  if [ -n "$o" ]; then printf '%s\n' "$o" | sort -u | sed 's/^/L /'; fi
  git worktree list --porcelain > "$Z/worktrees" || { echo 'zcp-bundle-error worktree list'; return 1; }
  i=0; p=; ph=
  while IFS= read -r l; do
    case $l in
      'worktree '*) p=${l#worktree } ;;
      'HEAD '*) ph=${l#HEAD } ;;
      '') zfpone ;;
    esac
  done < "$Z/worktrees"
  zfpone
  [ -z "$zfail" ]
}
`

// poolMigrateBundleHeader starts both scripts in the repository's main
// checkout. The identity names the WIP and untracked commits; it reaches no
// ref of the repository.
const poolMigrateBundleHeader = `export GIT_OPTIONAL_LOCKS=0 LC_ALL=C GIT_TERMINAL_PROMPT=0
export GIT_AUTHOR_NAME=zcp GIT_AUTHOR_EMAIL=zcp@zerops.invalid GIT_COMMITTER_NAME=zcp GIT_COMMITTER_EMAIL=zcp@zerops.invalid
set -f
if [ ! -e .git ]; then echo "$(pwd) holds no .git" >&2; exit 3; fi
R=$(pwd)
mkdir -p "$Z" || { echo "zcp-bundle-error scratch directory $Z"; exit 4; }
E=$(git hash-object -t tree /dev/null) || { echo 'zcp-bundle-error empty tree'; exit 4; }
`

// poolMigrateBundleScript builds the mirror the bundle streams from: the
// fingerprint first, recording each tree's WIP commit and untracked tree (so
// their objects exist before the clone); then a `git clone --mirror --local
// --no-hardlinks` of the repository in the scratch directory — every ref, its
// objects directory copied whole, so the objects no ref names come along, and
// nothing hard-linked: a repository on a code volume is a filesystem of its
// own, where a hard link from /tmp fails and a clone told --local dies on it —
// with a ref for every place work hides that no ref names (each stash entry,
// each reflog tip, each tree's HEAD, WIP commit and a commit of its untracked
// files); fsck of the mirror; and the fingerprint again, which must read the
// same, or the repository moved while it was bundled.
//
//	W <line>        git worktree list --porcelain
//	F <line>        the fingerprint (zfp)
//	G <line>        the fingerprint read again, when it differs
//	B <sha> <ref>   the mirror's refs: what the bundle carries
//	N <path>        an untracked repository of its own, not carried
const poolMigrateBundleScript = `echo 'zcp-bundle 1'
rm -rf "$Z/mirror.git"
: > "$Z/trees"; : > "$Z/nested"
fp1=$(zfp "$Z/trees") || { printf '%s\n' "$fp1" | grep '^zcp-bundle-error'; exit 4; }
sed 's/^/W /' "$Z/worktrees"
printf '%s\n' "$fp1" | sed 's/^/F /'
git clone -q --mirror --local --no-hardlinks "$R" "$Z/mirror.git" || { echo 'zcp-bundle-error mirror'; exit 4; }
M=$Z/mirror.git
st=$(git stash list --format=%H) || { echo 'zcp-bundle-error stash list'; exit 4; }
rl=$(git rev-list --no-walk --reflog) || { echo 'zcp-bundle-error reflogs'; exit 4; }
: > "$Z/refs"
n=0
for s in $st; do printf 'update refs/zcp-migrate/stash/%s %s\n' "$n" "$s" >> "$Z/refs"; n=$((n+1)); done
for s in $rl; do printf 'update refs/zcp-migrate/reflog/%s %s\n' "$s" "$s" >> "$Z/refs"; done
while read -r ti th tw tu; do
  if [ "$th" != unborn ]; then printf 'update refs/zcp-migrate/trees/%s/head %s\n' "$ti" "$th" >> "$Z/refs"; fi
  if [ "$tw" != - ]; then printf 'update refs/zcp-migrate/trees/%s/wip %s\n' "$ti" "$tw" >> "$Z/refs"; fi
  if [ "$tu" != "$E" ]; then
    c=$(git -C "$M" commit-tree "$tu" -m "zcp pool-migrate: the untracked files of tree $ti") || { echo "zcp-bundle-error untracked commit of tree $ti"; exit 4; }
    printf 'update refs/zcp-migrate/trees/%s/untracked %s\n' "$ti" "$c" >> "$Z/refs"
  fi
done < "$Z/trees"
git -C "$M" update-ref --stdin < "$Z/refs" || { echo 'zcp-bundle-error refs to keep'; exit 4; }
o=$(git -C "$M" fsck --no-dangling --no-progress 2>&1) || { echo 'zcp-bundle-error fsck of the mirror'; printf '%s\n' "$o"; exit 4; }
fp2=$(zfp '') || { printf '%s\n' "$fp2" | grep '^zcp-bundle-error'; exit 4; }
if [ "$fp1" != "$fp2" ]; then printf '%s\n' "$fp2" | sed 's/^/G /'; echo 'zcp-bundle-error moved'; exit 4; fi
git -C "$M" for-each-ref --format='B %(objectname) %(refname)' || { echo 'zcp-bundle-error manifest'; exit 4; }
sed 's/^/N /' "$Z/nested"
echo 'zcp-bundle-end'`

// poolMigrateFingerprintScript reads the fingerprint alone, then removes the
// scratch directory.
const poolMigrateFingerprintScript = `echo 'zcp-fingerprint 1'
fp=$(zfp '') || { printf '%s\n' "$fp" | grep '^zcp-bundle-error'; exit 4; }
printf '%s\n' "$fp" | sed 's/^/F /'
rm -rf "$Z"
echo 'zcp-fingerprint-end'`

// poolMigrateScript is one of the scripts above run in the repository rooted
// at root with the scratch directory work.
func poolMigrateScript(root, work, body string) string {
	return "cd " + shellQuote(root) + " || exit 3\nZ=" + shellQuote(work) + "\n" +
		poolMigrateBundleHeader + poolMigrateBundleFunctions + body
}

// PoolMigrateBundleCommand is the SSH command that builds the mirror of the
// repository rooted at root in the scratch directory work.
func PoolMigrateBundleCommand(root, work string) string {
	return poolMigrateScript(root, work, poolMigrateBundleScript)
}

// PoolMigrateFingerprintCommand is the SSH command that reads the fingerprint
// of the repository rooted at root.
func PoolMigrateFingerprintCommand(root, work string) string {
	return poolMigrateScript(root, work, poolMigrateFingerprintScript)
}

// poolMigrateStreamCommand streams the bundle of the mirror in work to stdout.
func poolMigrateStreamCommand(work string) string {
	return "git -C " + shellQuote(work+"/mirror.git") + " bundle create - --all"
}

// poolMigrateCleanupCommand removes the scratch directory.
func poolMigrateCleanupCommand(work string) string {
	return "rm -rf " + shellQuote(work)
}

// PoolMigrateBundleInput names what a retire bundles and where it keeps it.
type PoolMigrateBundleInput struct {
	// Dev is the dev service, reached over SSH by its hostname.
	Dev string
	// Root is its repository's main checkout (/var/www).
	Root string
	// WorkDir is the scratch directory on the dev service; "" means
	// PoolMigrateWorkDir.
	WorkDir string
	// Path is the file on this container the bundle is written to (0600).
	Path string
}

// PoolMigrateBundle is a verified bundle of a dev service's repository
// (docs/spec-workflows.md §13 PM-3): every ref, every stash entry, every
// reflog tip, each tree's HEAD, a WIP commit of each tree's index and tracked
// files and a commit of its untracked files — never a .env file, an ignored
// file or an untracked repository of its own (Nested names those). It
// carries no reflog history beyond the tips, no remote URL or other config,
// no hooks and no info/exclude.
type PoolMigrateBundle struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
	// Refs counts what it carries; Stashes and ReflogTips count the refs it
	// added for stash entries and reflog tips.
	Refs       int                     `json:"refs"`
	Stashes    int                     `json:"stashes"`
	ReflogTips int                     `json:"reflogTips"`
	Trees      []PoolMigrateBundleTree `json:"trees"`
	// Nested are untracked directories holding a repository of their own:
	// named, not carried.
	Nested []string `json:"nested,omitempty"`
	// Fingerprint is the repository as it was bundled; the retire reads it
	// again right before the delete.
	Fingerprint PoolMigrateFingerprint `json:"fingerprint"`
}

// PoolMigrateBundleTree is one checkout the bundle kept: the main checkout
// (index 0) or a linked worktree, in `git worktree list` order.
type PoolMigrateBundleTree struct {
	Index int    `json:"index"`
	Path  string `json:"path"`
	Head  string `json:"head,omitempty"`
	// Branch is the branch HEAD is on, "" when detached.
	Branch string `json:"branch,omitempty"`
	// Missing says the worktree is registered but its directory is gone: the
	// bundle keeps its HEAD alone.
	Missing bool `json:"missing,omitempty"`
	// WIP says the bundle keeps a commit of the tree's index and tracked
	// files; Untracked, one of its untracked files.
	WIP       bool `json:"wip,omitempty"`
	Untracked bool `json:"untracked,omitempty"`
}

// PoolMigrateFingerprint is a repository's state reduced to digests: its
// refs, its reflog tips and each tree's HEAD, status, index, working files
// and untracked files (Parts), and one digest over them all.
type PoolMigrateFingerprint struct {
	Digest string            `json:"digest"`
	Parts  map[string]string `json:"parts"`
}

const (
	poolMigratePartRefs    = "refs"
	poolMigratePartReflogs = "reflogs"
	poolMigratePartTree    = "tree "
)

// Moved names what differs between f and now, "" for nothing.
func (f PoolMigrateFingerprint) Moved(now PoolMigrateFingerprint) []string {
	keys := map[string]bool{}
	for k := range f.Parts {
		keys[k] = true
	}
	for k := range now.Parts {
		keys[k] = true
	}
	var moved []string
	for _, k := range slices.Sorted(func(yield func(string) bool) {
		for k := range keys {
			if !yield(k) {
				return
			}
		}
	}) {
		before, wasThere := f.Parts[k]
		after, isThere := now.Parts[k]
		if wasThere && isThere && before == after {
			continue
		}
		switch path, isTree := strings.CutPrefix(k, poolMigratePartTree); {
		case k == poolMigratePartRefs:
			moved = append(moved, "a ref moved: a branch, a tag, a remote branch or the newest stash entry")
		case k == poolMigratePartReflogs:
			moved = append(moved, "a reflog gained or lost an entry: a commit, a reset, a checkout or a stash")
		case isTree && !wasThere:
			moved = append(moved, "the worktree at "+path+" was added")
		case isTree && !isThere:
			moved = append(moved, "the worktree at "+path+" was removed")
		case isTree:
			moved = append(moved, "the checkout at "+path+" changed: its HEAD, index, tracked or untracked files")
		}
	}
	return moved
}

// poolMigrateFingerprintOf reduces zfp's lines to a fingerprint.
func poolMigrateFingerprintOf(lines []string) PoolMigrateFingerprint {
	hashers := map[string]io.Writer{}
	sums := map[string]func() []byte{}
	add := func(key, line string) {
		w, ok := hashers[key]
		if !ok {
			h := sha256.New()
			hashers[key], sums[key] = h, func() []byte { return h.Sum(nil) }
			w = h
		}
		_, _ = io.WriteString(w, line+"\n")
	}
	tree := ""
	for _, l := range lines {
		tag, rest, _ := strings.Cut(l, " ")
		switch tag {
		case "R":
			add(poolMigratePartRefs, l)
		case "L":
			add(poolMigratePartReflogs, l)
		case "T":
			tree = poolMigratePartTree + rest
			add(tree, l)
		case "M", "H", "S", "I", "W", "U":
			if tree != "" {
				add(tree, l)
			}
		}
	}
	fp := PoolMigrateFingerprint{Parts: make(map[string]string, len(sums))}
	all := sha256.New()
	for _, k := range slices.Sorted(func(yield func(string) bool) {
		for k := range sums {
			if !yield(k) {
				return
			}
		}
	}) {
		fp.Parts[k] = hex.EncodeToString(sums[k]())
		_, _ = io.WriteString(all, k+"="+fp.Parts[k]+"\n")
	}
	fp.Digest = hex.EncodeToString(all.Sum(nil))
	return fp
}

// poolMigrateBundleRead is what the bundle script printed.
type poolMigrateBundleRead struct {
	worktrees []string
	fp        []string
	manifest  []string
	nested    []string
}

// parsePoolMigrateBundle reads the bundle script's output. A step that failed,
// or an answer cut short, is an error naming it, never a bundle.
func parsePoolMigrateBundle(out string) (*poolMigrateBundleRead, error) {
	read := &poolMigrateBundleRead{}
	var header, end bool
	var failed, noise, again []string
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "zcp-bundle 1":
			header = true
			continue
		case line == "zcp-bundle-end":
			end = true
			continue
		case strings.HasPrefix(line, "zcp-bundle-error "):
			failed = append(failed, strings.TrimPrefix(line, "zcp-bundle-error "))
			continue
		}
		tag, rest, _ := strings.Cut(line, " ")
		switch tag {
		case "W":
			read.worktrees = append(read.worktrees, rest)
		case "F":
			read.fp = append(read.fp, rest)
		case "G":
			again = append(again, rest)
		case "B":
			read.manifest = append(read.manifest, rest)
		case "N":
			read.nested = append(read.nested, rest)
		default:
			if strings.TrimSpace(line) != "" {
				noise = append(noise, line)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read the bundle's answer: %w", err)
	}
	switch {
	case !header:
		return nil, fmt.Errorf("the dev service answered no bundle: %s", strings.Join(noise, "; "))
	case slices.Contains(failed, "moved"):
		moved := poolMigrateFingerprintOf(read.fp).Moved(poolMigrateFingerprintOf(again))
		return nil, fmt.Errorf("the repository moved while it was bundled — %s; call again once nothing writes to it", strings.Join(moved, "; "))
	case len(failed) > 0:
		return nil, fmt.Errorf("the bundle could not take %s: %s", strings.Join(failed, ", "), strings.Join(noise, "; "))
	case !end:
		return nil, errors.New("the bundle's answer came back incomplete")
	}
	return read, nil
}

// trees reads the worktree porcelain and the manifest into the bundle's trees.
func (r *poolMigrateBundleRead) trees() []PoolMigrateBundleTree {
	var trees []PoolMigrateBundleTree
	for _, l := range r.worktrees {
		key, val, _ := strings.Cut(l, " ")
		switch key {
		case porcelainWorktree:
			trees = append(trees, PoolMigrateBundleTree{Index: len(trees), Path: val})
		case porcelainHead:
			if len(trees) > 0 {
				trees[len(trees)-1].Head = val
			}
		case porcelainBranch:
			if len(trees) > 0 {
				trees[len(trees)-1].Branch = strings.TrimPrefix(val, "refs/heads/")
			}
		}
	}
	current := -1
	for _, l := range r.fp {
		tag, rest, _ := strings.Cut(l, " ")
		switch tag {
		case "T":
			current = slices.IndexFunc(trees, func(t PoolMigrateBundleTree) bool { return t.Path == rest })
		case "M":
			if current >= 0 {
				trees[current].Missing = true
			}
		}
	}
	for _, entry := range r.manifest {
		_, ref, _ := strings.Cut(entry, " ")
		rest, ok := strings.CutPrefix(ref, poolMigrateRefNS+"trees/")
		if !ok {
			continue
		}
		idx, what, _ := strings.Cut(rest, "/")
		i, err := strconv.Atoi(idx)
		if err != nil || i < 0 || i >= len(trees) {
			continue
		}
		switch what {
		case "wip":
			trees[i].WIP = true
		case "untracked":
			trees[i].Untracked = true
		}
	}
	return trees
}

// BuildPoolMigrateBundle bundles the dev service's repository into in.Path
// (docs/spec-workflows.md §13 PM-3): it builds the mirror on the dev service
// (PoolMigrateBundleCommand, one SSH round trip), streams `git bundle create -
// --all` of it over SSH stdout alone into a file beside in.Path, verifies the
// file here — `git bundle verify`, a fetch of every ref into a scratch
// repository, its refs against the mirror's, fsck — and only then puts it at
// in.Path (0600). The scratch directory on the dev service is removed
// whatever happens. A dev service ssh cannot stream from, a step that fails,
// or a bundle that does not verify is an error, and no file is left.
func BuildPoolMigrateBundle(ctx context.Context, ssh SSHDeployer, in PoolMigrateBundleInput) (*PoolMigrateBundle, error) {
	streamer, ok := ssh.(SSHStreamer)
	if !ok {
		return nil, errors.New("this zcp cannot stream a bundle off the dev service over SSH")
	}
	work := in.WorkDir
	if work == "" {
		work = PoolMigrateWorkDir
	}
	defer func() {
		_, _ = ssh.ExecSSH(context.WithoutCancel(ctx), in.Dev, poolMigrateCleanupCommand(work))
	}()

	out, err := ssh.ExecSSH(ctx, in.Dev, PoolMigrateBundleCommand(in.Root, work))
	read, perr := parsePoolMigrateBundle(string(out))
	switch {
	case err != nil && perr != nil:
		return nil, fmt.Errorf("bundle of %s: %s: %w", in.Dev, perr.Error(), err)
	case err != nil:
		return nil, fmt.Errorf("bundle of %s: %w", in.Dev, err)
	case perr != nil:
		return nil, fmt.Errorf("bundle of %s: %w", in.Dev, perr)
	}

	if err := os.MkdirAll(filepath.Dir(in.Path), 0o700); err != nil {
		return nil, fmt.Errorf("bundle of %s: %w", in.Dev, err)
	}
	part := in.Path + ".part"
	sum, size, err := streamPoolMigrateBundle(ctx, streamer, in.Dev, work, part)
	if err != nil {
		_ = os.Remove(part)
		return nil, fmt.Errorf("bundle of %s: stream: %w", in.Dev, err)
	}
	if err := verifyPoolMigrateBundle(ctx, part, read.manifest); err != nil {
		_ = os.Remove(part)
		return nil, fmt.Errorf("bundle of %s: %w", in.Dev, err)
	}
	if err := os.Rename(part, in.Path); err != nil {
		_ = os.Remove(part)
		return nil, fmt.Errorf("bundle of %s: %w", in.Dev, err)
	}

	b := &PoolMigrateBundle{
		Path: in.Path, SHA256: sum, Bytes: size,
		Refs:        len(read.manifest),
		Trees:       read.trees(),
		Fingerprint: poolMigrateFingerprintOf(read.fp),
	}
	for _, entry := range read.manifest {
		_, ref, _ := strings.Cut(entry, " ")
		switch {
		case strings.HasPrefix(ref, poolMigrateRefNS+"stash/"):
			b.Stashes++
		case strings.HasPrefix(ref, poolMigrateRefNS+"reflog/"):
			b.ReflogTips++
		}
	}
	for _, n := range read.nested {
		if !slices.ContainsFunc(b.Trees, func(t PoolMigrateBundleTree) bool { return t.Path == n }) {
			b.Nested = append(b.Nested, n)
		}
	}
	return b, nil
}

// streamPoolMigrateBundle streams the mirror's bundle into path (0600) and
// answers its sha256 and size.
func streamPoolMigrateBundle(ctx context.Context, streamer SSHStreamer, dev, work, path string) (string, int64, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, fmt.Errorf("open %s: %w", path, err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return "", 0, fmt.Errorf("chmod %s: %w", path, err)
	}
	h := sha256.New()
	counter := &byteCounter{}
	streamErr := streamer.StreamSSH(ctx, dev, poolMigrateStreamCommand(work), io.MultiWriter(f, h, counter))
	closeErr := f.Close()
	if streamErr != nil {
		return "", 0, streamErr
	}
	if closeErr != nil {
		return "", 0, fmt.Errorf("close %s: %w", path, closeErr)
	}
	return hex.EncodeToString(h.Sum(nil)), counter.n, nil
}

type byteCounter struct{ n int64 }

func (c *byteCounter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

// verifyPoolMigrateBundle verifies the bundle at path on this container: git
// takes it as a bundle, every object in it arrives in a scratch repository
// and passes fsck, and its refs are exactly the mirror's (manifest).
func verifyPoolMigrateBundle(ctx context.Context, path string, manifest []string) error {
	dir, err := os.MkdirTemp(filepath.Dir(path), ".verify-")
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	defer os.RemoveAll(dir)
	git := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // dir and path are zcp's own files, args fixed
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	if out, err := git("init", "-q", "--bare"); err != nil {
		return fmt.Errorf("verify: git init: %w: %s", err, out)
	}
	if out, err := git("bundle", "verify", path); err != nil {
		return fmt.Errorf("verify: git bundle verify: %w: %s", err, out)
	}
	if out, err := git("fetch", "-q", path, "refs/*:refs/*"); err != nil {
		return fmt.Errorf("verify: fetch from the bundle: %w: %s", err, out)
	}
	got, err := git("for-each-ref", "--format=%(objectname) %(refname)")
	if err != nil {
		return fmt.Errorf("verify: list refs: %w: %s", err, got)
	}
	have := strings.Split(got, "\n")
	if got == "" {
		have = nil
	}
	want := slices.Clone(manifest)
	slices.Sort(have)
	slices.Sort(want)
	if !slices.Equal(have, want) {
		return fmt.Errorf("verify: the bundle carries %d refs, the mirror held %d", len(have), len(want))
	}
	if out, err := git("fsck", "--no-dangling", "--no-progress"); err != nil {
		return fmt.Errorf("verify: fsck: %w: %s", err, out)
	}
	return nil
}

// ReadPoolMigrateFingerprint reads the fingerprint of the repository rooted at
// root on the dev service in one SSH round trip, writing nothing into it but
// unreferenced objects, and removes the scratch directory work after.
func ReadPoolMigrateFingerprint(ctx context.Context, ssh SSHDeployer, hostname, root, work string) (PoolMigrateFingerprint, error) {
	if work == "" {
		work = PoolMigrateWorkDir
	}
	out, err := ssh.ExecSSH(ctx, hostname, PoolMigrateFingerprintCommand(root, work))
	if err != nil {
		return PoolMigrateFingerprint{}, fmt.Errorf("fingerprint of %s: %w", hostname, err)
	}
	var header, end bool
	var lines, failed []string
	for line := range strings.SplitSeq(string(out), "\n") {
		switch {
		case line == "zcp-fingerprint 1":
			header = true
		case line == "zcp-fingerprint-end":
			end = true
		case strings.HasPrefix(line, "zcp-bundle-error "):
			failed = append(failed, strings.TrimPrefix(line, "zcp-bundle-error "))
		case strings.HasPrefix(line, "F "):
			lines = append(lines, strings.TrimPrefix(line, "F "))
		}
	}
	switch {
	case len(failed) > 0:
		return PoolMigrateFingerprint{}, fmt.Errorf("fingerprint of %s could not read %s", hostname, strings.Join(failed, ", "))
	case !header || !end:
		return PoolMigrateFingerprint{}, fmt.Errorf("fingerprint of %s came back incomplete", hostname)
	}
	return poolMigrateFingerprintOf(lines), nil
}

// PoolMigrateRestoreSteps are the shell commands that put what the bundle
// keeps back, run in an empty directory: every ref under refs/migrated/, the
// branches, remote branches, tags and refs/t3 where they were, origin, the
// main checkout on its branch (or its detached HEAD), the stash list in its
// order, each tree's index and tracked files from its WIP commit and its
// untracked files, and each linked worktree beside it as it lay beside the
// main checkout. A worktree whose directory was gone keeps only its HEAD.
func PoolMigrateRestoreSteps(b *PoolMigrateBundle, origin string) []string {
	if b == nil || len(b.Trees) == 0 {
		return nil
	}
	bundle := shellQuote(b.Path)
	ref := func(tree int, what string) string {
		return "refs/migrated/zcp-migrate/trees/" + strconv.Itoa(tree) + "/" + what
	}
	steps := []string{
		"git init -q",
		"git fetch -q " + bundle + " 'refs/*:refs/migrated/*'",
		"git fetch -q --update-head-ok " + bundle + " 'refs/heads/*:refs/heads/*' 'refs/remotes/*:refs/remotes/*' 'refs/tags/*:refs/tags/*' 'refs/t3/*:refs/t3/*'",
	}
	if origin != "" {
		steps = append(steps, "git remote add origin "+shellQuote(origin))
	}
	main := b.Trees[0]
	if main.Branch != "" {
		steps = append(steps, "git checkout -q -f "+shellQuote(main.Branch))
	} else {
		steps = append(steps, "git checkout -q -f --detach "+ref(0, "head"))
	}
	for n := b.Stashes - 1; n >= 0; n-- {
		stash := "refs/migrated/zcp-migrate/stash/" + strconv.Itoa(n)
		steps = append(steps, `git stash store -m "$(git log -1 --format=%s `+stash+`)" `+stash)
	}
	for _, t := range b.Trees {
		in, into := "", ""
		if t.Index > 0 {
			rel, err := filepath.Rel(main.Path, t.Path)
			if err != nil {
				rel = filepath.Base(t.Path)
			}
			dir := shellQuote(rel)
			in, into = "-C "+dir+" ", " -C "+dir
			switch {
			case t.Missing:
				steps = append(steps, "# "+t.Path+" was a worktree whose directory was gone: its HEAD is "+ref(t.Index, "head"))
				continue
			case t.Branch != "":
				steps = append(steps, "git worktree add -q "+dir+" "+shellQuote(t.Branch))
			default:
				steps = append(steps, "git worktree add -q --detach "+dir+" "+ref(t.Index, "head"))
			}
		}
		if t.WIP {
			steps = append(steps, "git "+in+"stash apply -q --index "+ref(t.Index, "wip"))
		}
		if t.Untracked {
			steps = append(steps, "git archive "+ref(t.Index, "untracked")+" | tar -x"+into)
		}
	}
	return steps
}
