// Tests for: knowledge engine — search, query expansion
package knowledge

import (
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

func urisFromResults(results []SearchResult) []string {
	uris := make([]string, 0, len(results))
	for _, r := range results {
		uris = append(uris, r.URI)
	}
	return uris
}
