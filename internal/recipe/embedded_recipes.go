package recipe

import (
	"fmt"

	"github.com/zeropsio/zcp/internal/knowledge"
)

// loadEmbeddedRecipeMD returns the body of `internal/knowledge/recipes/<slug>.md`
// from the embedded knowledge corpus. Returns an error when the slug
// has no embedded recipe (parent recipes that haven't been published
// to the knowledge corpus yet, or non-published frameworks).
//
// Run-22 R3-RC-0 — closes the parent-recipe channel mismatch where the
// binary IS carrying the baseline recipe (`//go:embed all:recipes` in
// internal/knowledge/documents.go), but the v3 chain resolver only reads
// the filesystem mount at $HOME/recipes/. The dogfood dev container
// usually has nothing mounted there, so showcase recipes ran without
// any baseline; this helper reads the embedded `.md` so the scaffold
// brief can include the parent-convention baseline as a fallback.
//
// Used by `BuildScaffoldBriefWithResolver` only — fires when the chain
// resolver returns no parent AND the slug has a recognized chain
// parent (`*-showcase` → `*-minimal`).
func loadEmbeddedRecipeMD(slug string) (string, error) {
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		return "", fmt.Errorf("get embedded knowledge store: %w", err)
	}
	uri := "zerops://recipes/" + slug
	doc, err := store.Get(uri)
	if err != nil {
		return "", fmt.Errorf("embedded recipe %q not found: %w", slug, err)
	}
	return doc.Content, nil
}
