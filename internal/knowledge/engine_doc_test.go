// Tests for: knowledge engine — document access, parsing, URI, theme, snippet tests
package knowledge

import (
	"slices"
	"strings"
	"testing"
)

func TestStore_DocumentCount(t *testing.T) {
	store := newTestStore(t)
	count := store.DocumentCount()
	if count < 30 {
		t.Errorf("DocumentCount = %d, want >= 30", count)
	}
}

func TestStore_List(t *testing.T) {
	store := newTestStore(t)
	resources := store.List()
	if len(resources) < 30 {
		t.Errorf("List() returned %d resources, want >= 30", len(resources))
	}
	for _, r := range resources {
		if !strings.HasPrefix(r.URI, "zerops://") {
			t.Errorf("resource URI %q doesn't start with zerops://", r.URI)
		}
		if r.Name == "" {
			t.Errorf("resource %s has empty name", r.URI)
		}
		if r.MimeType != "text/markdown" {
			t.Errorf("resource %s mimeType = %q, want text/markdown", r.URI, r.MimeType)
		}
	}
}

func TestStore_Get(t *testing.T) {
	store := newTestStore(t)
	doc, err := store.Get("zerops://themes/services")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Content, "PostgreSQL") {
		t.Error("services doc should contain 'PostgreSQL'")
	}
	if len(doc.Keywords) == 0 {
		t.Error("services doc should have keywords")
	}
}

func TestStore_GetNotFound(t *testing.T) {
	store := newTestStore(t)
	_, err := store.Get("zerops://nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent document")
	}
}

// --- Theme Document Embed Tests ---

func TestStore_ThemeDocsEmbedded(t *testing.T) {
	store := newTestStore(t)
	themeURIs := []string{
		"zerops://themes/core",
		"zerops://themes/runtimes",
		"zerops://themes/services",
		"zerops://themes/wiring",
		"zerops://themes/operations",
	}
	for _, uri := range themeURIs {
		doc, err := store.Get(uri)
		if err != nil {
			t.Errorf("theme doc %s not found: %v", uri, err)
			continue
		}
		if len(doc.Content) < 50 {
			t.Errorf("theme doc %s content too short (%d bytes)", uri, len(doc.Content))
		}
	}
}

func TestStore_RecipesEmbedded(t *testing.T) {
	store := newTestStore(t)
	recipes := store.ListRecipes()
	if len(recipes) < 20 {
		t.Errorf("ListRecipes() = %d, want >= 20", len(recipes))
	}
	// Spot-check a known recipe
	if !slices.Contains(recipes, "laravel-jetstream") {
		t.Errorf("expected laravel-jetstream in recipes, got: %v", recipes)
	}
}

// --- URI Conversion Tests ---

func TestPathToURI(t *testing.T) {
	tests := []struct {
		path string
		uri  string
	}{
		{"themes/core.md", "zerops://themes/core"},
		{"themes/runtimes.md", "zerops://themes/runtimes"},
		{"themes/services.md", "zerops://themes/services"},
		{"themes/wiring.md", "zerops://themes/wiring"},
		{"recipes/laravel-jetstream.md", "zerops://recipes/laravel-jetstream"},
	}
	for _, tt := range tests {
		got := pathToURI(tt.path)
		if got != tt.uri {
			t.Errorf("pathToURI(%q) = %q, want %q", tt.path, got, tt.uri)
		}
	}
}

func TestURIToPath(t *testing.T) {
	tests := []struct {
		uri  string
		path string
	}{
		{"zerops://themes/core", "themes/core.md"},
		{"zerops://recipes/laravel-jetstream", "recipes/laravel-jetstream.md"},
	}
	for _, tt := range tests {
		got := uriToPath(tt.uri)
		if got != tt.path {
			t.Errorf("uriToPath(%q) = %q, want %q", tt.uri, got, tt.path)
		}
	}
}

// --- Document Parsing Tests ---

func TestParseDocument_Keywords(t *testing.T) {
	store := newTestStore(t)
	doc, err := store.Get("zerops://themes/services")
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Keywords) == 0 {
		t.Fatal("expected keywords for services")
	}
	if !slices.Contains(doc.Keywords, "postgresql") {
		t.Errorf("services keywords should contain 'postgresql', got %v", doc.Keywords)
	}
}

func TestParseDocument_TLDR(t *testing.T) {
	store := newTestStore(t)
	doc, err := store.Get("zerops://themes/services")
	if err != nil {
		t.Fatal(err)
	}
	if doc.TLDR == "" {
		t.Error("expected TL;DR for services")
	}
}

func TestParseDocument_Title(t *testing.T) {
	store := newTestStore(t)
	doc, err := store.Get("zerops://themes/core")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title == "" {
		t.Error("expected title for core")
	}
	if !strings.Contains(doc.Title, "Zerops") {
		t.Errorf("title = %q, expected to contain 'Zerops'", doc.Title)
	}
}

// --- Snippet Tests ---

func TestExtractSnippet(t *testing.T) {
	content := "# Title\n\nSome text about postgresql connection string here.\n\nMore text."
	snippet := extractSnippet(content, "postgresql", 100)
	if !strings.Contains(strings.ToLower(snippet), "postgresql") {
		t.Errorf("snippet should contain query term, got: %s", snippet)
	}
}

func TestExtractSnippet_NoMatch(t *testing.T) {
	content := "# Title\n\nSome generic text here.\n\nMore text."
	snippet := extractSnippet(content, "nonexistent", 100)
	if snippet == "" {
		t.Error("expected fallback snippet even without match")
	}
}
