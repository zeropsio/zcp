package knowledge

// Tests for: runtime guide structural integrity.
//
// Validates every runtime slug in runtimeNormalizer maps to a resolvable guide
// (recipes/{slug}-hello-world or recipes/{slug}) with required structure.
//
// Run: go test ./internal/knowledge/ -run TestRuntimeLint -v

import (
	"testing"
)

// expectedRuntimeSlugs lists all unique slugs from runtimeNormalizer.
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

			// Resolve via getRuntimeGuide which checks recipes/{slug}-hello-world then recipes/{slug}
			guide := store.getRuntimeGuide(slug)
			if guide == "" {
				t.Fatalf("runtime guide for %q not resolvable", slug)
			}

			// Parse the resolved document for structural checks
			var doc *Document
			if d, err := store.Get("zerops://recipes/" + slug + "-hello-world"); err == nil {
				doc = d
			} else if d, err := store.Get("zerops://recipes/" + slug); err == nil {
				doc = d
			} else {
				t.Fatalf("could not load document for %q", slug)
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
