package skillpacks

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Resource caps, pinned constants per spec-welcome-mode's design consult:
// exceeding any one aborts discovery before any workspace write.
const (
	maxSkills       = 512
	maxEntries      = 20_000
	maxFileBytes    = 32 * 1024 * 1024
	maxContentBytes = 256 * 1024 * 1024
	maxRepoBytes    = 512 * 1024 * 1024
	maxWalkDepth    = 8
)

// vcsAndStateDirNames are the only directory names discovery skips outright
// — every other directory, dot-prefixed or not, is a legitimate place a
// skill root may live (".agents" and ".claude" are themselves valid source
// layouts for a pack whose own repo mirrors ZCP's target layout).
var vcsAndStateDirNames = map[string]bool{".git": true, ".hg": true, ".svn": true, ".zcp": true}

// Candidate is one discovered, fully validated skill root, still living at
// its cloned location — nothing has been copied into the workspace yet.
type Candidate struct {
	Name       string // destination name (flattened: the leaf dir name, or the declared name for a root skill)
	SourcePath string // slash-relative path from the repo root; "." for a whole-repo (root SKILL.md) pack
	SourceDir  string // absolute filesystem path to the skill root directory
}

// rawCandidate is a discovered skill root before the cross-candidate
// checks (duplicate names, caps) have run.
type rawCandidate struct {
	name       string
	sourcePath string
	sourceDir  string
}

// discoverSkills walks repoRoot for skill roots exactly per
// spec-welcome-mode's discovery pseudocode, validates every rule, enforces
// every resource cap, and returns the resulting candidates sorted by
// sourcePath. repoRoot is a freshly cloned, untrusted source tree — nothing
// here touches the workspace.
func discoverSkills(repoRoot string) ([]Candidate, error) {
	repoBytes, err := repoSizeExcludingVCS(repoRoot)
	if err != nil {
		return nil, wrapCoded(CodeFilesystem, err, "measure repository size")
	}
	if repoBytes > maxRepoBytes {
		return nil, codedErrorf(CodeInvalidSource, "checked-out repository is %d bytes, exceeds the %d byte cap", repoBytes, maxRepoBytes)
	}

	var raw []rawCandidate
	if err := walkForSkillRoots(repoRoot, ".", 0, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, codedErrorf(CodeNoSkills, "no SKILL.md found anywhere in the repository")
	}
	sort.Slice(raw, func(i, j int) bool { return raw[i].sourcePath < raw[j].sourcePath })
	if err := rejectDuplicateNames(raw); err != nil {
		return nil, err
	}
	if len(raw) > maxSkills {
		return nil, codedErrorf(CodeInvalidSource, "found %d skills, exceeds the %d-skill cap", len(raw), maxSkills)
	}

	var totalEntries int
	var totalContentBytes int64
	candidates := make([]Candidate, 0, len(raw))
	for _, rc := range raw {
		entries, contentBytes, err := scanAndCapSkillTree(rc.sourceDir)
		if err != nil {
			return nil, err
		}
		totalEntries += entries
		totalContentBytes += contentBytes
		if totalEntries > maxEntries {
			return nil, codedErrorf(CodeInvalidSource, "materialized entries (%d) exceed the %d cap", totalEntries, maxEntries)
		}
		if totalContentBytes > maxContentBytes {
			return nil, codedErrorf(CodeInvalidSource, "materialized content (%d bytes) exceeds the %d byte cap", totalContentBytes, maxContentBytes)
		}
		candidates = append(candidates, Candidate{Name: rc.name, SourcePath: rc.sourcePath, SourceDir: rc.sourceDir})
	}
	return candidates, nil
}

// walkForSkillRoots implements the discovery pseudocode: depth-capped,
// dot-directories traversed, only VCS/state internals skipped, a directory
// whose exact entry "SKILL.md" exists becomes one skill root and is never
// descended into further (a skill owns its subtree; a root SKILL.md
// therefore dominates the whole repository).
func walkForSkillRoots(dir, rel string, depth int, out *[]rawCandidate) error {
	if depth > maxWalkDepth {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return wrapCoded(CodeFilesystem, err, "read directory %s", rel)
	}

	for _, e := range entries {
		if e.Name() != "SKILL.md" {
			continue
		}
		return handleSkillRoot(dir, rel, e, out)
	}

	for _, e := range entries {
		name := e.Name()
		if vcsAndStateDirNames[name] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return wrapCoded(CodeFilesystem, err, "stat %s", filepath.Join(rel, name))
		}
		if info.Mode()&os.ModeSymlink != 0 || !e.IsDir() {
			continue // not a directory we can descend into
		}
		childRel := name
		if rel != "." {
			childRel = filepath.Join(rel, name)
		}
		if err := walkForSkillRoots(filepath.Join(dir, name), childRel, depth+1, out); err != nil {
			return err
		}
	}
	return nil
}

// handleSkillRoot validates the SKILL.md found directly in dir and, if
// valid, appends the resulting candidate to out. rel is "." for the repo
// root itself.
func handleSkillRoot(dir, rel string, skillEntry os.DirEntry, out *[]rawCandidate) error {
	info, err := skillEntry.Info()
	if err != nil {
		return wrapCoded(CodeFilesystem, err, "stat %s", filepath.Join(rel, "SKILL.md"))
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return codedErrorf(CodeInvalidSource, "%s is a symlink or special file, not a regular file", filepath.Join(rel, "SKILL.md"))
	}

	content, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return wrapCoded(CodeFilesystem, err, "read %s", filepath.Join(rel, "SKILL.md"))
	}
	fm, err := parseFrontmatter(content)
	if err != nil {
		return wrapCoded(CodeInvalidSource, err, "%s", filepath.Join(rel, "SKILL.md"))
	}

	name := fm.Name
	if rel != "." {
		base := filepath.Base(dir)
		if name != base {
			return codedErrorf(CodeInvalidSource, "skill at %s declares name %q but its directory is named %q", rel, name, base)
		}
	}
	if err := validateDestinationName(name); err != nil {
		return wrapCoded(CodeInvalidSource, err, "skill at %s", rel)
	}

	*out = append(*out, rawCandidate{name: name, sourcePath: filepath.ToSlash(rel), sourceDir: dir})
	return nil // never descend into a skill root: it owns its whole subtree
}

// rejectDuplicateNames fails the whole discovery if two candidates would
// flatten to the same destination name, comparing case-folded (though the
// portable name charset is lowercase-only by construction, so this is
// equivalent to a plain comparison today — kept case-insensitive to match
// the design's stated rule literally).
func rejectDuplicateNames(raw []rawCandidate) error {
	seen := make(map[string]string, len(raw))
	for _, rc := range raw {
		key := strings.ToLower(rc.name)
		if prevPath, ok := seen[key]; ok {
			return codedErrorf(CodeInvalidSource, "duplicate skill name %q at both %s and %s", rc.name, prevPath, rc.sourcePath)
		}
		seen[key] = rc.sourcePath
	}
	return nil
}

// scanAndCapSkillTree walks the complete selected subtree at dir (the
// content that will actually be copied), hard-erroring on any symlink or
// non-regular entry — "Source symlinks and special files inside a selected
// skill tree are hard errors, not silently skipped" — and enforcing the
// per-file byte cap. It returns the entry and content-byte counts this one
// subtree contributes, for discoverSkills's running aggregate caps.
func scanAndCapSkillTree(dir string) (entries int, contentBytes int64, err error) {
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		entries++
		if entries > maxEntries {
			return codedErrorf(CodeInvalidSource, "skill tree at %s has more than %d entries", dir, maxEntries)
		}
		if path == dir {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return wrapCoded(CodeFilesystem, err, "stat %s", path)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return codedErrorf(CodeInvalidSource, "%s is a symlink; symlinks inside a selected skill tree are not allowed", path)
		}
		if d.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return codedErrorf(CodeInvalidSource, "%s is not a regular file or directory", path)
		}
		if info.Size() > maxFileBytes {
			return codedErrorf(CodeInvalidSource, "%s is %d bytes, exceeds the %d byte per-file cap", path, info.Size(), maxFileBytes)
		}
		contentBytes += info.Size()
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return entries, contentBytes, nil
}

// filterDiscoveredToCatalog applies pack's review granularity as an
// intersection over discoverSkills's raw output — spec-skill-packs.md §1/
// §3. The walker itself never learns about packs or catalogs: it always
// discovers everything a repository contains (structural rules, resource
// caps, symlink refusals, and name validation apply unconditionally,
// first). This function is the one place a pack's catalog actually
// constrains what gets installed:
//
//   - ReviewRepositoryLevel: no filtering. The complete discovered set
//     passes through unchanged — this is the pre-catalog behavior, and
//     remains it for andrej-karpathy-skills and anthropic-skills.
//   - ReviewSkillLevel: only candidates matching a catalogued (name,
//     sourcePath) pair are kept. Discovered content outside the catalog is
//     simply not a candidate — never an error, never a warning. A
//     catalogued skill with no matching discovered candidate IS an error:
//     the catalog promises the skill exists at that location, so a silent
//     partial install would violate "installation succeeds only when the
//     complete supported set is installed" (spec-skill-packs.md §5).
func filterDiscoveredToCatalog(pack Pack, discovered []Candidate) ([]Candidate, error) {
	if pack.Review != ReviewSkillLevel {
		return discovered, nil
	}

	byKey := make(map[string]Candidate, len(discovered))
	for _, c := range discovered {
		byKey[c.Name+"\x00"+c.SourcePath] = c
	}

	filtered := make([]Candidate, 0, len(pack.Skills))
	var missing []string
	for _, sk := range pack.Skills {
		c, ok := byKey[sk.Name+"\x00"+sk.SourcePath]
		if !ok {
			missing = append(missing, sk.Name)
			continue
		}
		filtered = append(filtered, c)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, codedErrorf(CodeInvalidSource,
			"catalogued skill(s) not found upstream at their expected location: %s", strings.Join(missing, ", "))
	}

	sort.Slice(filtered, func(i, j int) bool { return filtered[i].SourcePath < filtered[j].SourcePath })
	return filtered, nil
}

// repoSizeExcludingVCS sums regular-file bytes under repoRoot, skipping
// .git — a coarse guard on how large a clone we are even willing to scan,
// checked before the (finer, per-skill) content caps below.
func repoSizeExcludingVCS(repoRoot string) (int64, error) {
	var total int64
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}
