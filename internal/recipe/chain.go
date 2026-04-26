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

// ResolveChain returns the ParentRecipe for a recipe slug. If the recipe
// has no parent (minimal / hello-world), or the parent tree is not
// present on disk, ResolveChain returns ErrNoParent with a nil parent.
// The chain is deterministic and flat per plan §7:
//
//   - {framework}-showcase  → {framework}-minimal
//   - {framework}-minimal   → no parent
//   - hello-world-{lang}    → no parent
func ResolveChain(r Resolver, slug string) (*ParentRecipe, error) {
	parentSlug := parentSlugFor(slug)
	if parentSlug == "" {
		return nil, ErrNoParent
	}
	parentDir := filepath.Join(r.MountRoot, parentSlug)
	if _, err := os.Stat(parentDir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNoParent
		}
		return nil, fmt.Errorf("stat parent dir %s: %w", parentDir, err)
	}
	return loadParent(parentSlug, parentDir)
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
