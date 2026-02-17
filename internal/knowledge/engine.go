package knowledge

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/zeropsio/zcp/internal/platform"
)

// SearchResult represents a single search result.
type SearchResult struct {
	URI     string  `json:"uri"`
	Title   string  `json:"title"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet"`
}

// Resource represents an MCP Resource entry.
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
}

// Provider interface for knowledge access.
type Provider interface {
	List() []Resource
	Get(uri string) (*Document, error)
	Search(query string, limit int) []SearchResult
	GetCore() (string, error)
	GetBriefing(runtime string, services []string, liveTypes []platform.ServiceStackType) (string, error)
	GetRecipe(name string) (string, error)
}

// Store holds the knowledge base with simple text-matching search.
type Store struct {
	docs map[string]*Document
}

// Verify Store implements Provider.
var _ Provider = (*Store)(nil)

var (
	embeddedStore     *Store
	embeddedStoreOnce sync.Once
	errEmbeddedStore  error
)

// GetEmbeddedStore returns the singleton Store instance, safe for concurrent use.
func GetEmbeddedStore() (*Store, error) {
	embeddedStoreOnce.Do(func() {
		embeddedStore, errEmbeddedStore = NewStore(loadFromEmbedded())
	})
	return embeddedStore, errEmbeddedStore
}

// NewStore creates a new Store from pre-loaded documents.
func NewStore(docs map[string]*Document) (*Store, error) {
	return &Store{docs: docs}, nil
}

// queryAliases maps common alternative terms to their Zerops equivalents.
var queryAliases = map[string]string{
	"postgres":  "postgres postgresql",
	"redis":     "redis valkey",
	"mysql":     "mysql mariadb",
	"node":      "node nodejs",
	"db":        "db database",
	"ssl":       "ssl tls",
	"env":       "env environment variable",
	"cert":      "cert certificate ssl tls",
	"ha":        "ha high-availability mode",
	"k8s":       "k8s kubernetes",
	"mongo":     "mongo mongodb",
	"docker":    "docker dockerfile",
	"pg":        "pg postgresql postgres",
	"js":        "js nodejs javascript",
	"ts":        "ts nodejs typescript",
	"s3":        "s3 object-storage",
	"cron":      "cron crontab schedule",
	"log":       "log logging logs",
	"logs":      "logs logging log",
	"dns":       "dns domain networking",
	"ci":        "ci ci-cd continuous integration",
	"cd":        "cd ci-cd continuous deployment",
	"dotnet":    "dotnet .net csharp",
	"csharp":    "csharp dotnet .net",
	"memcached": "memcached valkey cache",
}

func expandQuery(query string) string {
	words := strings.Fields(strings.ToLower(query))
	var expanded []string
	for _, w := range words {
		if alias, ok := queryAliases[w]; ok {
			expanded = append(expanded, alias)
		} else {
			expanded = append(expanded, w)
		}
	}
	return strings.Join(expanded, " ")
}

// ExpandQuery is exported for testing.
func ExpandQuery(query string) string {
	return expandQuery(query)
}

// Search performs a simple text-matching search with field boosts and query expansion.
func (s *Store) Search(query string, limit int) []SearchResult {
	if limit <= 0 {
		limit = 5
	}

	expanded := expandQuery(query)
	words := strings.Fields(strings.ToLower(expanded))

	type scored struct {
		uri   string
		score float64
	}
	var hits []scored

	for uri, doc := range s.docs {
		score := 0.0
		titleLower := strings.ToLower(doc.Title)
		kwLower := strings.ToLower(strings.Join(doc.Keywords, " "))
		contentLower := strings.ToLower(doc.Content)

		for _, word := range words {
			if strings.Contains(titleLower, word) {
				score += 2.0
			}
			if strings.Contains(kwLower, word) {
				score += 1.5
			}
			if strings.Contains(contentLower, word) {
				score += 1.0
			}
		}

		if score > 0 {
			hits = append(hits, scored{uri, score})
		}
	}

	// Sort by score descending, then by URI for determinism.
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].uri < hits[j].uri
	})

	if len(hits) > limit {
		hits = hits[:limit]
	}
	results := make([]SearchResult, len(hits))
	for i, h := range hits {
		doc := s.docs[h.uri]
		results[i] = SearchResult{
			URI:     doc.URI,
			Title:   doc.Title,
			Score:   h.score,
			Snippet: extractSnippet(doc.Content, expanded, 300),
		}
	}
	return results
}

// List returns all available resources for MCP list_resources().
func (s *Store) List() []Resource {
	resources := make([]Resource, 0, len(s.docs))
	for _, doc := range s.docs {
		resources = append(resources, Resource{
			URI:         doc.URI,
			Name:        doc.Title,
			Description: doc.Description,
			MimeType:    "text/markdown",
		})
	}
	sort.Slice(resources, func(i, j int) bool {
		return resources[i].URI < resources[j].URI
	})
	return resources
}

// Get returns a document by URI.
func (s *Store) Get(uri string) (*Document, error) {
	doc, ok := s.docs[uri]
	if !ok {
		return nil, fmt.Errorf("document not found: %s", uri)
	}
	return doc, nil
}

// DocumentCount returns the number of indexed documents.
func (s *Store) DocumentCount() int {
	return len(s.docs)
}

// GetCore returns the full themes/core.md content (merged platform + rules + grammar).
func (s *Store) GetCore() (string, error) {
	doc, err := s.Get("zerops://themes/core")
	if err != nil {
		return "", fmt.Errorf("core reference not found: %w", err)
	}
	return doc.Content, nil
}

// runtimeRecipeHints maps runtime base names to recipe name prefixes/matches.
var runtimeRecipeHints = map[string][]string{
	"bun":    {"bun"},
	"nodejs": {"nestjs", "nextjs", "svelte-nodejs", "react-nodejs", "qwik-nodejs", "payload", "ghost", "medusa", "nuxt"},
	"go":     {"echo-go"},
	"python": {"django"},
	"elixir": {"phoenix"},
	"php":    {"laravel", "symfony", "nette", "filament", "twill", "php-"},
	"java":   {"java-spring", "spring-boot"},
	"ruby":   {"rails"},
	"rust":   {"rust"},
	"dotnet": {"dotnet"},
	"deno":   {"deno"},
	"gleam":  {"gleam"},
	"static": {"svelte-static", "nextjs-static", "qwik-static"},
}

// matchingRecipes returns recipe names that match the given runtime base name.
func (s *Store) matchingRecipes(runtimeBase string) []string {
	prefixes, ok := runtimeRecipeHints[runtimeBase]
	if !ok {
		return nil
	}
	allRecipes := s.ListRecipes()
	var matched []string
	for _, recipe := range allRecipes {
		for _, prefix := range prefixes {
			if strings.HasPrefix(recipe, prefix) {
				matched = append(matched, recipe)
				break
			}
		}
	}
	return matched
}

// extractSnippet extracts a text snippet around the first query term match.
func extractSnippet(content, query string, maxLen int) string {
	lower := strings.ToLower(content)
	queryLower := strings.ToLower(query)

	bestPos := -1
	for word := range strings.FieldsSeq(queryLower) {
		pos := strings.Index(lower, word)
		if pos >= 0 && (bestPos < 0 || pos < bestPos) {
			bestPos = pos
		}
	}

	if bestPos < 0 {
		lines := strings.SplitN(content, "\n", 3)
		if len(lines) >= 3 {
			return truncate(lines[2], maxLen)
		}
		return truncate(content, maxLen)
	}

	start := bestPos - maxLen/3
	start = max(start, 0)
	end := start + maxLen
	end = min(end, len(content))

	snippet := content[start:end]

	if start > 0 {
		if idx := strings.IndexByte(snippet, ' '); idx >= 0 {
			snippet = "..." + snippet[idx+1:]
		}
	}
	if end < len(content) {
		if idx := strings.LastIndexByte(snippet, ' '); idx >= 0 {
			snippet = snippet[:idx] + "..."
		}
	}

	return snippet
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
