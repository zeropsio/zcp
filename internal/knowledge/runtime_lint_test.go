package knowledge

// Tests for: runtime guide structural integrity.
//
// Validates every runtime slug in runtimeNormalizer maps to an embedded guide
// with required structure (H1 title, Keywords, TL;DR).
//
// Run: go test ./internal/knowledge/ -run TestRuntimeLint -v

import (
	"testing"
)

// expectedRuntimeSlugs lists all unique slugs from runtimeNormalizer.
// Each must have a corresponding runtimes/{slug}.md file.
var expectedRuntimeSlugs = []string{
	"alpine", "bun", "deno", "docker", "dotnet", "elixir", "gleam",
	"go", "java", "nginx", "nodejs", "php", "python", "ruby",
	"rust", "static", "ubuntu",
}

func TestRuntimeLint(t *testing.T) {
	t.Parallel()

	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("load store: %v", err)
	}

	for _, slug := range expectedRuntimeSlugs {
		t.Run(slug, func(t *testing.T) {
			t.Parallel()

			uri := "zerops://runtimes/" + slug
			doc, err := store.Get(uri)
			if err != nil {
				t.Fatalf("runtime guide %s not found: %v", uri, err)
			}

			t.Run("HasTitle", func(t *testing.T) {
				if doc.Title == "" {
					t.Error("missing H1 title")
				}
			})

			t.Run("HasKeywords", func(t *testing.T) {
				if len(doc.Keywords) < 3 {
					t.Errorf("want >= 3 keywords, got %d: %v", len(doc.Keywords), doc.Keywords)
				}
			})

			t.Run("HasTLDR", func(t *testing.T) {
				if doc.TLDR == "" {
					t.Error("missing ## TL;DR section")
				}
			})

			t.Run("MinContent", func(t *testing.T) {
				if len(doc.Content) < 100 {
					t.Errorf("content too short (%d bytes), expected >= 100", len(doc.Content))
				}
			})
		})
	}
}
