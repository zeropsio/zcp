package recipe

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Resolver locates a parent recipe on disk. MountRoot is a directory
// containing one subdirectory per published slug (the zeropsio/recipes
// clone root, or a staging mount during chain tests).
type Resolver struct {
	MountRoot string
}

// ErrNoParent is returned by ResolveChain when the recipe has no parent
// or the parent tree is not present on disk. Callers treat this as a
// signal to start first-time framework discovery, not as an error.
var ErrNoParent = errors.New("recipe has no parent")

// ReachableSlugs returns the sorted list of recipe slugs that have an
// import.yaml at <MountRoot>/<slug>/import.yaml — the canonical
// "this slug exists in the recipes mount, you can call
// zerops_knowledge recipe=<slug> against it" set. Empty mount root
// returns nil. Run-14 §B.3 (R-13-21) — the scaffold brief composer
// emits these as bullets so a sub-agent never guesses at a slug
// that isn't there.
//
// I/O boundary: ReadDir + Stat over <MountRoot>; called at brief
// composition time (per-dispatch). The mount lives on the local
// filesystem in current runs; coherence is local-fs.
func (r Resolver) ReachableSlugs() ([]string, error) {
	if r.MountRoot == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(r.MountRoot)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read mount root %s: %w", r.MountRoot, err)
	}
	var slugs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(r.MountRoot, e.Name(), "import.yaml")); err == nil {
			slugs = append(slugs, e.Name())
		}
	}
	sort.Strings(slugs)
	return slugs, nil
}

// ResolveChain returns the ParentRecipe for a recipe slug. The chain
// is deterministic and flat per plan §7:
//
//   - {framework}-showcase  → {framework}-minimal
//   - {framework}-minimal   → no parent
//   - hello-world-{lang}    → no parent
//
// Resolution order (Run-40 post-ship — parent IS the embedded
// knowledge corpus, not a filesystem mount):
//
//  1. Embedded corpus FIRST. The binary ships
//     `internal/knowledge/recipes/<parent-slug>.md` (+ companion
//     `.import.yml`) for every published parent. That body is the
//     parent. If it exists, synthesize a ParentRecipe and return —
//     parentStatus reports "embedded" so the agent reads the
//     baseline that downstream brief composers will surface anyway
//     via appendEmbeddedParentBaseline.
//
//  2. Filesystem mount fallback for legacy CDE setups that
//     `git clone zeropsio/recipes ~/recipes` and want the full
//     published tree (per-codebase READMEs + tier import.yamls
//     beyond what the curated `.md` carries). Returns the same
//     ParentRecipe shape with the on-disk SourceRoot populated.
//
//  3. ErrNoParent only when both paths come up empty — the recipe
//     genuinely has no parent (hello-world, *-minimal) or no
//     published parent corpus.
//
// The prior implementation only checked the filesystem mount;
// `parentStatus: "absent"` fired on every local dev box because
// `~/recipes/` doesn't exist outside the CDE shape, even though the
// embedded `.md` was sitting right there in the binary the whole time.
func ResolveChain(r Resolver, slug string) (*ParentRecipe, error) {
	parentSlug := parentSlugFor(slug)
	if parentSlug == "" {
		return nil, ErrNoParent
	}
	if parent, ok := tryLoadEmbeddedParent(parentSlug); ok {
		return parent, nil
	}
	if r.MountRoot != "" {
		parentDir := filepath.Join(r.MountRoot, parentSlug)
		if _, err := os.Stat(parentDir); err == nil {
			return loadParent(parentSlug, parentDir)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("stat parent dir %s: %w", parentDir, err)
		}
	}
	return nil, ErrNoParent
}

// tryLoadEmbeddedParent synthesizes a ParentRecipe from the embedded
// knowledge corpus when `internal/knowledge/recipes/<parent-slug>.md`
// ships in the binary. Returns the synthesized parent + true on hit,
// zero value + false on miss.
//
// The synthesized ParentRecipe has Slug + Tier populated, but
// Codebases/EnvImports left empty: downstream brief composers
// detect the "embedded shape" via SourceRoot == "" and route through
// the existing appendEmbeddedParentBaseline path that already reads
// the same .md body inline. SourceRoot stays empty so that fallback
// fires; the new EmbeddedBody field carries the full `.md` for any
// caller that wants it without re-reading.
func tryLoadEmbeddedParent(parentSlug string) (*ParentRecipe, bool) {
	body, err := loadEmbeddedRecipeMD(parentSlug)
	if err != nil {
		return nil, false
	}
	return &ParentRecipe{
		Slug:         parentSlug,
		Tier:         parentTierForSlug(parentSlug),
		Codebases:    map[string]ParentCodebase{},
		EnvImports:   map[string]string{},
		EmbeddedBody: body,
	}, true
}

// Tier suffix slugs used by the chain resolver and by tier labelling.
// Brief composers + validators that branch on Plan.Tier reference these
// constants rather than literal strings.
const (
	tierMinimal    = "minimal"
	tierHelloWorld = "hello-world"
	tierShowcase   = "showcase"
)

// parentSlugFor applies the fixed chain rules. Returns "" for no parent.
func parentSlugFor(slug string) string {
	if base, ok := strings.CutSuffix(slug, "-showcase"); ok {
		return base + "-" + tierMinimal
	}
	return ""
}

// loadParent reads a parent recipe's published tree from disk. Not every
// codebase or env file must exist — missing files are skipped. Returns an
// error only for read failures on existing files.
func loadParent(slug, dir string) (*ParentRecipe, error) {
	out := &ParentRecipe{
		Slug:       slug,
		Tier:       parentTierForSlug(slug),
		Codebases:  make(map[string]ParentCodebase),
		EnvImports: make(map[string]string),
		SourceRoot: dir,
	}

	codebasesDir := filepath.Join(dir, "codebases")
	if entries, err := os.ReadDir(codebasesDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			cb, err := loadParentCodebase(filepath.Join(codebasesDir, e.Name()))
			if err != nil {
				return nil, fmt.Errorf("parent codebase %s: %w", e.Name(), err)
			}
			out.Codebases[e.Name()] = cb
		}
	}

	for i := range 6 {
		tier, _ := TierAt(i)
		envDir := filepath.Join(dir, tier.Folder)
		content, err := os.ReadFile(filepath.Join(envDir, "import.yaml"))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("parent tier %d import.yaml: %w", i, err)
		}
		out.EnvImports[envKey(tier)] = string(content)
	}
	return out, nil
}

func parentTierForSlug(slug string) string {
	switch {
	case strings.HasSuffix(slug, "-"+tierMinimal):
		return tierMinimal
	case strings.HasPrefix(slug, tierHelloWorld+"-"):
		return tierHelloWorld
	default:
		return ""
	}
}

func loadParentCodebase(dir string) (ParentCodebase, error) {
	cb := ParentCodebase{SourceRoot: dir}
	if b, err := os.ReadFile(filepath.Join(dir, "README.md")); err == nil {
		cb.README = string(b)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return cb, err
	}
	if b, err := os.ReadFile(filepath.Join(dir, "zerops.yaml")); err == nil {
		cb.ZeropsYAML = string(b)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return cb, err
	}
	return cb, nil
}
