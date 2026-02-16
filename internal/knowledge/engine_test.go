// Tests for: knowledge engine — search, document access, briefing, recipes
package knowledge

import (
	"slices"
	"strings"
	"testing"
)

// newTestStore creates a Store from embedded docs for testing.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(loadFromEmbedded())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

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
		"zerops://themes/platform",
		"zerops://themes/rules",
		"zerops://themes/grammar",
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

func TestStore_GetBriefing_RealDocs(t *testing.T) {
	store := newTestStore(t)
	briefing, err := store.GetBriefing("php-nginx@8.4", []string{"postgresql@16"}, nil)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	if !strings.Contains(briefing, "Zerops Platform Model") {
		t.Error("briefing missing platform model content")
	}
	if !strings.Contains(briefing, "Zerops Rules") {
		t.Error("briefing missing rules content")
	}
	if !strings.Contains(briefing, "Zerops Grammar") {
		t.Error("briefing missing grammar content")
	}
	if !strings.Contains(briefing, "PHP") {
		t.Error("briefing missing PHP runtime delta")
	}
	if !strings.Contains(briefing, "PostgreSQL") {
		t.Error("briefing missing PostgreSQL service card")
	}
	if !strings.Contains(briefing, "Wiring") {
		t.Error("briefing missing wiring section")
	}
}

// --- SimpleSearch Tests ---

func TestSearch_PostgreSQLConnectionString(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("postgresql connection string", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'postgresql connection string'")
	}
	found := false
	for _, r := range results[:min(3, len(results))] {
		if strings.Contains(r.URI, "services") || strings.Contains(strings.ToLower(r.Snippet), "postgresql") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected postgresql-related result in top 3, got: %v", urisFromResults(results[:min(3, len(results))]))
	}
}

func TestSearch_RedisCache(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("redis cache", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'redis cache'")
	}
	found := false
	for _, r := range results[:min(3, len(results))] {
		if strings.Contains(strings.ToLower(r.Snippet), "valkey") || strings.Contains(r.URI, "services") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected valkey/cache result in top 3 for 'redis cache', got: %v", urisFromResults(results[:min(3, len(results))]))
	}
}

func TestSearch_NodejsDeploy(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("nodejs deploy", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'nodejs deploy'")
	}
	found := false
	for _, r := range results[:min(3, len(results))] {
		if strings.Contains(strings.ToLower(r.Snippet), "node") || strings.Contains(r.URI, "runtimes") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected nodejs-related result in top 3, got: %v", urisFromResults(results[:min(3, len(results))]))
	}
}

func TestSearch_MysqlSetup(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("mysql setup", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'mysql setup'")
	}
	found := false
	for _, r := range results[:min(3, len(results))] {
		if strings.Contains(strings.ToLower(r.Snippet), "mariadb") || strings.Contains(r.URI, "services") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected mariadb result in top 3 for 'mysql setup', got: %v", urisFromResults(results[:min(3, len(results))]))
	}
}

func TestSearch_ElasticsearchFulltext(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("elasticsearch fulltext", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'elasticsearch fulltext'")
	}
	found := false
	for _, r := range results[:min(3, len(results))] {
		if strings.Contains(strings.ToLower(r.Snippet), "elasticsearch") || strings.Contains(r.URI, "services") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected elasticsearch result in top 3, got: %v", urisFromResults(results[:min(3, len(results))]))
	}
}

func TestSearch_S3ObjectStorage(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("s3 object storage", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 's3 object storage'")
	}
	found := false
	for _, r := range results[:min(3, len(results))] {
		if strings.Contains(r.URI, "services") || strings.Contains(r.URI, "wiring") || strings.Contains(strings.ToLower(r.Snippet), "object") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected object-storage doc in top 3 for 's3 object storage'")
	}
}

func TestSearch_ZeropsYmlBuildCache(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("zerops.yml build cache", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'zerops.yml build cache'")
	}
	found := false
	for _, r := range results[:min(3, len(results))] {
		if strings.Contains(r.URI, "grammar") || strings.Contains(strings.ToLower(r.Snippet), "cache") || strings.Contains(strings.ToLower(r.Snippet), "build") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected grammar or build-related result in top 3")
	}
}

func TestSearch_ImportYmlServices(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("import.yml services", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'import.yml services'")
	}
	found := false
	for _, r := range results[:min(3, len(results))] {
		if strings.Contains(r.URI, "grammar") || strings.Contains(r.URI, "services") || strings.Contains(strings.ToLower(r.Snippet), "import") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected grammar or services in top 3")
	}
}

func TestSearch_EnvironmentVariables(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("environment variables env", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'environment variables env'")
	}
	found := false
	for _, r := range results[:min(3, len(results))] {
		if strings.Contains(strings.ToLower(r.Snippet), "variable") || strings.Contains(strings.ToLower(r.Snippet), "env") || strings.Contains(r.URI, "grammar") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected env-variables content in top 3")
	}
}

func TestSearch_ScalingAutoscale(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("scaling autoscale ha", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'scaling autoscale ha'")
	}
	found := false
	for _, r := range results[:min(3, len(results))] {
		if strings.Contains(strings.ToLower(r.Snippet), "scaling") || strings.Contains(strings.ToLower(r.Snippet), "autoscal") || strings.Contains(r.URI, "grammar") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected scaling content in top 3")
	}
}

func TestSearch_ConnectionStringNodejsPostgresql(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("connection string nodejs postgresql", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'connection string nodejs postgresql'")
	}
	found := false
	for _, r := range results[:min(3, len(results))] {
		if strings.Contains(strings.ToLower(r.Snippet), "postgresql") || strings.Contains(strings.ToLower(r.Snippet), "connection") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected postgresql or connection result in top 3")
	}
}

func TestSearch_TopResultHasFullContent(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("postgresql", 5)
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	doc, err := store.Get(results[0].URI)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Content, "## Keywords") && !strings.Contains(doc.Content, "## PostgreSQL") && !strings.Contains(doc.Content, "postgresql") {
		t.Error("top result doc should contain '## Keywords' or '## PostgreSQL' or 'postgresql'")
	}
}

// --- Query Expansion Tests ---

func TestExpandQuery(t *testing.T) {
	tests := []struct {
		input    string
		contains []string
	}{
		{"postgres", []string{"postgresql"}},
		{"redis", []string{"valkey"}},
		{"mysql", []string{"mariadb"}},
		{"node", []string{"nodejs"}},
		{"ssl", []string{"tls"}},
		{"env", []string{"environment", "variable"}},
		{"postgres node ssl", []string{"postgresql", "nodejs", "tls"}},
	}
	for _, tt := range tests {
		expanded := ExpandQuery(tt.input)
		for _, want := range tt.contains {
			if !strings.Contains(expanded, want) {
				t.Errorf("ExpandQuery(%q) = %q, missing %q", tt.input, expanded, want)
			}
		}
	}
}

// --- URI Conversion Tests ---

func TestPathToURI(t *testing.T) {
	tests := []struct {
		path string
		uri  string
	}{
		{"themes/grammar.md", "zerops://themes/grammar"},
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
		{"zerops://themes/grammar", "themes/grammar.md"},
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
	doc, err := store.Get("zerops://themes/grammar")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title == "" {
		t.Error("expected title for grammar")
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

// --- GetBriefing Version Integration Tests ---

func TestGetBriefing_IncludesVersionCheck(t *testing.T) {
	store := newTestStore(t)
	types := testStackTypes()

	briefing, err := store.GetBriefing("nodejs@22", []string{"postgresql@16"}, types)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	if !strings.Contains(briefing, "Version Check") {
		t.Error("briefing missing Version Check section")
	}
	if !strings.Contains(briefing, "\u2713") {
		t.Error("briefing missing checkmarks for valid types")
	}
}

func TestGetBriefing_VersionWarning(t *testing.T) {
	store := newTestStore(t)
	types := testStackTypes()

	briefing, err := store.GetBriefing("bun@1", []string{"postgresql@16"}, types)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	if !strings.Contains(briefing, "\u26a0") {
		t.Error("briefing missing warning for invalid bun@1")
	}
}

func TestGetBriefing_NilTypes_NoVersionSection(t *testing.T) {
	store := newTestStore(t)

	briefing, err := store.GetBriefing("nodejs@22", []string{"postgresql@16"}, nil)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	if strings.Contains(briefing, "Version Check") {
		t.Error("briefing should NOT contain Version Check when types is nil")
	}
}

// --- Knowledge Content & Briefing Order Tests ---

func TestGetBriefing_BunRuntime_ContainsBindingRule(t *testing.T) {
	store := newTestStore(t)
	briefing, err := store.GetBriefing("bun@1.2", []string{"postgresql@16"}, nil)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	if !strings.Contains(briefing, "0.0.0.0") {
		t.Error("Bun briefing missing 0.0.0.0 binding rule")
	}
	if !strings.Contains(briefing, "Bun.serve") {
		t.Error("Bun briefing missing Bun.serve reference")
	}
}

func TestStore_GetRecipe_Bun(t *testing.T) {
	store := newTestStore(t)
	content, err := store.GetRecipe("bun")
	if err != nil {
		t.Fatalf("GetRecipe(bun): %v", err)
	}
	if !strings.Contains(content, "0.0.0.0") {
		t.Error("bun recipe missing 0.0.0.0 binding rule")
	}
	if !strings.Contains(content, "zerops.yml") {
		t.Error("bun recipe missing zerops.yml example")
	}
}

func TestStore_GetBriefing_SurfacesMatchingRecipes(t *testing.T) {
	store := newTestStore(t)
	briefing, err := store.GetBriefing("bun@1.2", nil, nil)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	if !strings.Contains(briefing, "Matching Recipes") {
		t.Error("Bun briefing missing Matching Recipes section")
	}
	if !strings.Contains(briefing, "bun-hono") {
		t.Error("Bun briefing missing bun-hono recipe hint")
	}
}

func TestStore_GetBriefing_LayerOrderRealDocs(t *testing.T) {
	store := newTestStore(t)
	briefing, err := store.GetBriefing("bun@1.2", []string{"postgresql@16"}, nil)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	platformIdx := strings.Index(briefing, "Zerops Platform Model")
	rulesIdx := strings.Index(briefing, "Zerops Rules")
	grammarIdx := strings.Index(briefing, "Zerops Grammar")
	runtimeIdx := strings.Index(briefing, "Runtime-Specific: Bun")
	serviceIdx := strings.Index(briefing, "Service Cards")
	if platformIdx < 0 {
		t.Fatal("briefing missing Zerops Platform Model")
	}
	if rulesIdx < 0 {
		t.Fatal("briefing missing Zerops Rules")
	}
	if grammarIdx < 0 {
		t.Fatal("briefing missing Zerops Grammar")
	}
	if runtimeIdx < 0 {
		t.Fatal("briefing missing Runtime-Specific: Bun section")
	}
	if serviceIdx < 0 {
		t.Fatal("briefing missing Service Cards section")
	}
	// L0 platform model -> L1 rules -> L2 grammar -> L3 runtime -> L4 services
	if platformIdx >= rulesIdx {
		t.Errorf("platform model (pos %d) should come before rules (pos %d)", platformIdx, rulesIdx)
	}
	if rulesIdx >= grammarIdx {
		t.Errorf("rules (pos %d) should come before grammar (pos %d)", rulesIdx, grammarIdx)
	}
	if grammarIdx >= runtimeIdx {
		t.Errorf("grammar (pos %d) should come before runtime (pos %d)", grammarIdx, runtimeIdx)
	}
	if runtimeIdx >= serviceIdx {
		t.Errorf("runtime (pos %d) should come before services (pos %d)", runtimeIdx, serviceIdx)
	}
}

func urisFromResults(results []SearchResult) []string {
	uris := make([]string, 0, len(results))
	for _, r := range results {
		uris = append(uris, r.URI)
	}
	return uris
}
