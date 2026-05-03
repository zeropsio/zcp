// Tests for: knowledge engine — document access, parsing, URI, theme, snippet tests
package knowledge

import (
	"slices"
	"strings"
	"testing"
)

func TestStore_Get(t *testing.T) {
	store := newTestStore(t)
	doc, err := store.Get("zerops://themes/services")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Content, "PostgreSQL") {
		t.Error("services doc should contain 'PostgreSQL'")
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
		"zerops://themes/services",
		"zerops://themes/operations",
		"zerops://themes/design-system",
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

// TestStore_DesignSystemThemeIsFrameworkNeutral pins the cross-recipe
// visual style consistency contract: the design-system theme must teach
// principles that hold regardless of framework binding (Angular Material,
// Tailwind, shadcn, Laravel Blade). The Components section must reach
// across multiple framework lineages — naming only Angular Material
// would under-serve Laravel/Blade and pure Tailwind showcases.
func TestStore_DesignSystemThemeIsFrameworkNeutral(t *testing.T) {
	store := newTestStore(t)
	doc, err := store.Get("zerops://themes/design-system")
	if err != nil {
		t.Fatalf("design-system theme not found: %v", err)
	}
	// Per-framework lineage section must enumerate at least Angular,
	// Tailwind, and Blade so showcases of every shape have a starting
	// point.
	for _, lineage := range []string{
		"Angular + Material 3",
		"React/Vue + Tailwind",
		"Laravel Blade + Tailwind",
	} {
		if !strings.Contains(doc.Content, lineage) {
			t.Errorf("design-system theme missing per-framework lineage %q", lineage)
		}
	}
	// Load-bearing body anchors — the doc Content has frontmatter
	// stripped, so assertions read against the human-readable body
	// only. The frontmatter dictionary is exercised separately by the
	// recipe-engine BuildDesignTokenTable composer.
	for _, anchor := range []string{
		"#00CCBB",        // brand seeds line
		"JetBrains Mono", // typography rule
		"Geologica",      // typography rule
		"12px",           // canonical card radius
		"pill",           // button shape language
	} {
		if !strings.Contains(doc.Content, anchor) {
			t.Errorf("design-system theme missing load-bearing anchor %q", anchor)
		}
	}
}

func TestStore_RecipesEmbedded(t *testing.T) {
	store := newTestStore(t)
	recipes := store.ListRecipes()
	if len(recipes) < 20 {
		t.Errorf("ListRecipes() = %d, want >= 20", len(recipes))
	}
	// Spot-check a known recipe (pulled from API)
	if !slices.Contains(recipes, "bun-hello-world") {
		t.Errorf("expected bun-hello-world in recipes, got: %v", recipes)
	}
}

// --- URI Conversion Tests ---

func TestPathToURI(t *testing.T) {
	tests := []struct {
		path string
		uri  string
	}{
		{"themes/core.md", "zerops://themes/core"},
		{"themes/services.md", "zerops://themes/services"},
		{"recipes/bun-hello-world.md", "zerops://recipes/bun-hello-world"},
		{"bases/nginx.md", "zerops://bases/nginx"},
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
		{"zerops://recipes/bun-hello-world", "recipes/bun-hello-world.md"},
	}
	for _, tt := range tests {
		got := uriToPath(tt.uri)
		if got != tt.path {
			t.Errorf("uriToPath(%q) = %q, want %q", tt.uri, got, tt.path)
		}
	}
}

// --- Document Parsing Tests ---

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

// --- Guides & Decisions Embed Tests ---

func TestStore_GuidesEmbedded(t *testing.T) {
	store := newTestStore(t)
	guideURIs := []string{
		"zerops://guides/firewall",
		"zerops://guides/environment-variables",
		"zerops://guides/cloudflare",
		"zerops://guides/vpn",
		"zerops://guides/backup",
		"zerops://guides/logging",
		"zerops://guides/build-cache",
		"zerops://guides/object-storage-integration",
		"zerops://guides/zerops-yaml-advanced",
		"zerops://guides/ci-cd",
		"zerops://guides/cdn",
		"zerops://guides/scaling",
		"zerops://guides/networking",
		"zerops://guides/production-checklist",
		"zerops://guides/deployment-lifecycle",
		"zerops://guides/public-access",
		"zerops://guides/smtp",
		"zerops://guides/metrics",
		"zerops://guides/verify-web-agent-protocol",
	}
	for _, uri := range guideURIs {
		doc, err := store.Get(uri)
		if err != nil {
			t.Errorf("guide %s not found: %v", uri, err)
			continue
		}
		if len(doc.Content) < 50 {
			t.Errorf("guide %s content too short (%d bytes)", uri, len(doc.Content))
		}
		if doc.Title == "" {
			t.Errorf("guide %s has no title", uri)
		}
	}
}

// TestStore_VerifyWebAgentProtocolSearchable pins the cross-reference
// contract between develop-verify-matrix atom and the
// verify-web-agent-protocol guide. The atom instructs the agent to call
// `zerops_knowledge query="verify web agent protocol"` to fetch the
// sub-agent dispatch protocol on demand. Search scores by title +
// content text-match (engine.go:123-135), NOT by URI slug — so the
// query phrase MUST match words in the guide title or body. Pre-fix
// regression: querying with the hyphenated URI slug returned no hit
// because Search splits on whitespace, not hyphens.
func TestStore_VerifyWebAgentProtocolSearchable(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("verify web agent protocol", 5)
	if len(results) == 0 {
		t.Fatal("Search(\"verify web agent protocol\") returned no results — develop-verify-matrix atom's cross-reference would fail at runtime")
	}
	for _, r := range results {
		if r.URI == "zerops://guides/verify-web-agent-protocol" {
			return
		}
	}
	t.Errorf("Search(\"verify web agent protocol\") did not surface zerops://guides/verify-web-agent-protocol; results: %+v", results)
}

func TestStore_DecisionsEmbedded(t *testing.T) {
	store := newTestStore(t)
	decisionURIs := []string{
		"zerops://decisions/choose-database",
		"zerops://decisions/choose-cache",
		"zerops://decisions/choose-queue",
		"zerops://decisions/choose-search",
		"zerops://decisions/choose-runtime-base",
	}
	for _, uri := range decisionURIs {
		doc, err := store.Get(uri)
		if err != nil {
			t.Errorf("decision %s not found: %v", uri, err)
			continue
		}
		if len(doc.Content) < 50 {
			t.Errorf("decision %s content too short (%d bytes)", uri, len(doc.Content))
		}
		if doc.Title == "" {
			t.Errorf("decision %s has no title", uri)
		}
	}
}

func TestSearch_GuideSpecificQueries(t *testing.T) {
	store := newTestStore(t)
	tests := []struct {
		name    string
		query   string
		wantTop string // expected top result URI
	}{
		{
			name:    "firewall query finds firewall guide",
			query:   "firewall",
			wantTop: "zerops://guides/firewall",
		},
		{
			name:    "environment variables query finds env guide",
			query:   "environment variables",
			wantTop: "zerops://guides/environment-variables",
		},
		{
			name:    "choose database query finds database decision",
			query:   "choose database",
			wantTop: "zerops://decisions/choose-database",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := store.Search(tt.query, 5)
			if len(results) == 0 {
				t.Fatal("search returned no results")
			}
			if results[0].URI != tt.wantTop {
				t.Errorf("top result = %s (score %.1f), want %s", results[0].URI, results[0].Score, tt.wantTop)
				for i, r := range results {
					t.Logf("  #%d: %s (%.1f)", i+1, r.URI, r.Score)
				}
			}
		})
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
