package workflow

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/zeropsio/zcp/internal/content"
)

// atomEmbedPrefix is the path prefix inside content.RecipeAtomsFS. The
// embed directive preserves relative paths from the content package root,
// so atom paths declared as "phases/research/entry.md" are read from
// "workflows/recipe/phases/research/entry.md" inside the FS.
const atomEmbedPrefix = "workflows/recipe/"

// LoadAtom reads the atom with the given manifest ID and returns its body.
// Returns an error if the ID is not registered or the file is missing from
// the embedded tree. Callers treat the error as a hard failure — every
// atom ID in the manifest must be backed by an embedded file.
func LoadAtom(id string) (string, error) {
	path, ok := AtomPath(id)
	if !ok {
		return "", fmt.Errorf("atom %q not registered in manifest", id)
	}
	return loadAtomByPath(path)
}

// LoadAtomBody is a defensive wrapper that returns the atom body without
// any trailing newline stripping. Use this when concatenating atoms at
// stitch time so consumers see content bodies as authored.
func LoadAtomBody(id string) (string, error) {
	body, err := LoadAtom(id)
	if err != nil {
		return "", err
	}
	// Strip at most one trailing newline so successive stitched atoms
	// separated by "\n---\n" don't accumulate blank lines between them.
	return strings.TrimSuffix(body, "\n"), nil
}

// loadAtomByPath is the internal path-based reader. Exported helpers go
// through AtomPath first so unregistered paths are rejected consistently.
func loadAtomByPath(path string) (string, error) {
	fullPath := atomEmbedPrefix + path
	data, err := fs.ReadFile(content.RecipeAtomsFS, fullPath)
	if err != nil {
		return "", fmt.Errorf("read atom %q: %w", path, err)
	}
	return string(data), nil
}

// AtomExists reports whether the atom ID resolves to an embedded file.
// Used by tests + the dry-run harness (C-14) to cross-check manifest
// against filesystem state without raising a hard error per missing file.
func AtomExists(id string) bool {
	path, ok := AtomPath(id)
	if !ok {
		return false
	}
	_, err := fs.ReadFile(content.RecipeAtomsFS, atomEmbedPrefix+path)
	return err == nil
}

// concatAtoms loads the named atom IDs and concatenates their bodies with
// "\n---\n" separators. Empty atom IDs are skipped (used by tier-branching
// callers that pass "" when an atom doesn't apply). Returns the first
// load error encountered.
func concatAtoms(ids ...string) (string, error) {
	var parts []string
	for _, id := range ids {
		if id == "" {
			continue
		}
		body, err := LoadAtomBody(id)
		if err != nil {
			return "", err
		}
		parts = append(parts, body)
	}
	return strings.Join(parts, "\n\n---\n\n"), nil
}
