// Tests for: design/zcp-prd.md § Knowledge (BM25 search engine)
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
	if count < 60 {
		t.Errorf("DocumentCount = %d, want >= 60", count)
	}
}

func TestStore_List(t *testing.T) {
	store := newTestStore(t)
	resources := store.List()
	if len(resources) < 60 {
		t.Errorf("List() returned %d resources, want >= 60", len(resources))
	}
	for _, r := range resources {
		if !strings.HasPrefix(r.URI, "zerops://docs/") {
			t.Errorf("resource URI %q doesn't start with zerops://docs/", r.URI)
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
	doc, err := store.Get("zerops://docs/services/postgresql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Content, "PostgreSQL") {
		t.Error("postgresql doc should contain 'PostgreSQL'")
	}
	if len(doc.Keywords) == 0 {
		t.Error("postgresql doc should have keywords")
	}
}

func TestStore_GetNotFound(t *testing.T) {
	store := newTestStore(t)
	_, err := store.Get("zerops://docs/nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent document")
	}
}

// --- Core Document Embed Tests ---

func TestStore_CoreDocsEmbedded(t *testing.T) {
	store := newTestStore(t)
	coreURIs := []string{
		"zerops://docs/core/core-principles",
		"zerops://docs/core/runtime-exceptions",
		"zerops://docs/core/service-cards",
		"zerops://docs/core/wiring-patterns",
	}
	for _, uri := range coreURIs {
		doc, err := store.Get(uri)
		if err != nil {
			t.Errorf("core doc %s not found: %v", uri, err)
			continue
		}
		if len(doc.Content) < 50 {
			t.Errorf("core doc %s content too short (%d bytes)", uri, len(doc.Content))
		}
	}
}

func TestStore_CoreRecipesEmbedded(t *testing.T) {
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
	if !strings.Contains(briefing, "Core Principles") && !strings.Contains(briefing, "core-principles") {
		t.Error("briefing missing core principles content")
	}
	if !strings.Contains(briefing, "PHP") {
		t.Error("briefing missing PHP runtime exceptions")
	}
	if !strings.Contains(briefing, "PostgreSQL") {
		t.Error("briefing missing PostgreSQL service card")
	}
}

// --- BM25 Search Tests ---

func TestSearch_PostgreSQLConnectionString(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("postgresql connection string", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'postgresql connection string'")
	}
	if !containsURIInTop(results, "zerops://docs/services/postgresql", 3) {
		t.Errorf("expected postgresql in top 3, got: %v", urisFromResults(results[:min(3, len(results))]))
	}
}

func TestSearch_PostgresPort(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("postgres port", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'postgres port'")
	}
	if !containsURI(results, "zerops://docs/services/postgresql") {
		t.Error("expected postgresql in results for 'postgres port'")
	}
}

func TestSearch_RedisCache(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("redis cache", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'redis cache'")
	}
	if !containsURIInTop(results, "zerops://docs/services/valkey", 3) {
		t.Error("expected valkey in top 3 for 'redis cache'")
	}
}

func TestSearch_NodejsDeploy(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("nodejs deploy", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'nodejs deploy'")
	}
	if !containsURIInTop(results, "zerops://docs/services/nodejs", 3) {
		t.Error("expected nodejs in top 3 for 'nodejs deploy'")
	}
}

func TestSearch_MysqlSetup(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("mysql setup", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'mysql setup'")
	}
	if !containsURIInTop(results, "zerops://docs/services/mariadb", 3) {
		t.Error("expected mariadb in top 3 for 'mysql setup' (alias)")
	}
}

func TestSearch_ElasticsearchFulltext(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("elasticsearch fulltext", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'elasticsearch fulltext'")
	}
	if !containsURIInTop(results, "zerops://docs/services/elasticsearch", 3) {
		t.Error("expected elasticsearch in top 3")
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
		if strings.Contains(r.URI, "object-storage") {
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
		if strings.Contains(r.URI, "zerops-yml") || strings.Contains(r.URI, "build-cache") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected zerops-yml or build-cache in top 3")
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
		if strings.Contains(r.URI, "import-yml") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected import-yml in top 3")
	}
}

func TestSearch_EnvironmentVariables(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("environment variables env", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'environment variables env'")
	}
	if !containsURIInTop(results, "zerops://docs/platform/env-variables", 3) {
		t.Error("expected env-variables in top 3")
	}
}

func TestSearch_ScalingAutoscale(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("scaling autoscale ha", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'scaling autoscale ha'")
	}
	if !containsURIInTop(results, "zerops://docs/platform/scaling", 3) {
		t.Error("expected platform/scaling in top 3")
	}
}

func TestSearch_ConnectionStringNodejsPostgresql(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("connection string nodejs postgresql", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'connection string nodejs postgresql'")
	}
	hasPostgres := containsURIInTop(results, "zerops://docs/services/postgresql", 3)
	hasConnStrings := containsURIInTop(results, "zerops://docs/examples/connection-strings", 3)
	if !hasPostgres && !hasConnStrings {
		t.Error("expected postgresql or connection-strings in top 3")
	}
}

func TestSearch_NoResults_MongoDB(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("mongodb", 5)
	suggestions := store.GenerateSuggestions("mongodb", results)
	if len(suggestions) == 0 {
		t.Error("expected suggestions for unsupported 'mongodb'")
	}
	found := false
	for _, s := range suggestions {
		if strings.Contains(s, "MongoDB") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected suggestion mentioning MongoDB")
	}
}

func TestSearch_NoResults_Kubernetes(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("kubernetes deployment", 5)
	suggestions := store.GenerateSuggestions("kubernetes deployment", results)
	if len(suggestions) == 0 {
		t.Error("expected suggestions for unsupported 'kubernetes'")
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
	if !strings.Contains(doc.Content, "## Keywords") {
		t.Error("top result doc should contain '## Keywords' section")
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
		{"embed/services/postgresql.md", "zerops://docs/services/postgresql"},
		{"embed/platform/scaling.md", "zerops://docs/platform/scaling"},
		{"embed/examples/zerops-yml-runtimes.md", "zerops://docs/examples/zerops-yml-runtimes"},
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
		{"zerops://docs/services/postgresql", "embed/services/postgresql.md"},
		{"zerops://docs/platform/scaling", "embed/platform/scaling.md"},
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
	doc, err := store.Get("zerops://docs/services/postgresql")
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Keywords) == 0 {
		t.Fatal("expected keywords for postgresql")
	}
	if !slices.Contains(doc.Keywords, "postgresql") {
		t.Errorf("postgresql keywords should contain 'postgresql', got %v", doc.Keywords)
	}
}

func TestParseDocument_TLDR(t *testing.T) {
	store := newTestStore(t)
	doc, err := store.Get("zerops://docs/services/postgresql")
	if err != nil {
		t.Fatal(err)
	}
	if doc.TLDR == "" {
		t.Error("expected TL;DR for postgresql")
	}
}

func TestParseDocument_Title(t *testing.T) {
	store := newTestStore(t)
	doc, err := store.Get("zerops://docs/services/postgresql")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title == "" {
		t.Error("expected title for postgresql")
	}
	if !strings.Contains(doc.Title, "PostgreSQL") {
		t.Errorf("title = %q, expected to contain 'PostgreSQL'", doc.Title)
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

// --- Suggestions Tests ---

func TestGenerateSuggestions_UnsupportedService(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("dynamodb", 5)
	suggestions := store.GenerateSuggestions("dynamodb", results)
	if len(suggestions) == 0 {
		t.Error("expected suggestions for dynamodb")
	}
	if !strings.Contains(suggestions[0], "DynamoDB") {
		t.Errorf("suggestion should mention DynamoDB, got: %s", suggestions[0])
	}
}

func TestGenerateSuggestions_WithResults(t *testing.T) {
	store := newTestStore(t)
	results := store.Search("postgresql", 5)
	suggestions := store.GenerateSuggestions("postgresql", results)
	_ = suggestions // verify no panic
}

func TestGenerateSuggestions_NoResults(t *testing.T) {
	store := newTestStore(t)
	var results []SearchResult
	suggestions := store.GenerateSuggestions("xyznonexistent", results)
	if len(suggestions) == 0 {
		t.Error("expected fallback suggestion for no results")
	}
}

// --- Hit Rate Tests ---

func TestHitRate(t *testing.T) {
	store := newTestStore(t)

	type testCase struct {
		name     string
		query    string
		wantURI  string // expected in top 1
		wantTop3 string // expected in top 3 (if different)
	}

	cases := []testCase{
		{"postgresql_connection", "postgresql connection string", "zerops://docs/services/postgresql", ""},
		{"postgres_port", "postgres port", "zerops://docs/services/postgresql", ""},
		{"nodejs_deploy", "nodejs deploy", "", "zerops://docs/services/nodejs"},
		{"mysql_setup", "mysql setup", "", "zerops://docs/services/mariadb"},
		{"redis_cache", "redis cache", "", "zerops://docs/services/valkey"},
		{"elasticsearch", "elasticsearch fulltext", "", "zerops://docs/services/elasticsearch"},
		{"env_variables", "environment variables env", "", "zerops://docs/platform/env-variables"},
		{"scaling", "scaling autoscale", "", "zerops://docs/platform/scaling"},
	}

	hit1 := 0
	hit3 := 0
	total := len(cases)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			results := store.Search(tc.query, 5)
			if len(results) == 0 {
				t.Logf("MISS: no results for %q", tc.query)
				return
			}

			expectedURI := tc.wantURI
			if expectedURI == "" {
				expectedURI = tc.wantTop3
			}

			if results[0].URI == expectedURI || (tc.wantURI != "" && results[0].URI == tc.wantURI) {
				hit1++
				hit3++
				return
			}

			checkURI := expectedURI
			if tc.wantTop3 != "" {
				checkURI = tc.wantTop3
			}

			if containsURIInTop(results, checkURI, 3) {
				hit3++
			} else {
				t.Logf("MISS top3: query=%q, got=%v", tc.query, urisFromResults(results[:min(3, len(results))]))
			}
		})
	}

	hit1Rate := float64(hit1) / float64(total) * 100
	hit3Rate := float64(hit3) / float64(total) * 100
	t.Logf("Hit@1: %.0f%% (%d/%d)", hit1Rate, hit1, total)
	t.Logf("Hit@3: %.0f%% (%d/%d)", hit3Rate, hit3, total)
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

// --- Helpers ---

func containsURI(results []SearchResult, uri string) bool {
	for _, r := range results {
		if r.URI == uri {
			return true
		}
	}
	return false
}

func containsURIInTop(results []SearchResult, uri string, _ int) bool {
	for i, r := range results {
		if i >= 3 {
			break
		}
		if r.URI == uri {
			return true
		}
	}
	return false
}

func urisFromResults(results []SearchResult) []string {
	uris := make([]string, 0, len(results))
	for _, r := range results {
		uris = append(uris, r.URI)
	}
	return uris
}
