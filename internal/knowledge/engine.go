package knowledge

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/blevesearch/bleve/v2"
	"github.com/zeropsio/zcp/internal/platform"
)

const analyzerStandard = "standard"

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
	GenerateSuggestions(query string, results []SearchResult) []string
}

// Store holds the knowledge base with BM25 index.
type Store struct {
	docs  map[string]*Document
	index bleve.Index
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

// NewStore creates a new Store from pre-loaded documents and builds a BM25 index.
func NewStore(docs map[string]*Document) (*Store, error) {
	s := &Store{docs: docs}
	if err := s.buildIndex(); err != nil {
		return nil, fmt.Errorf("knowledge: build index: %w", err)
	}
	return s, nil
}

func (s *Store) buildIndex() error {
	titleMapping := bleve.NewTextFieldMapping()
	titleMapping.Analyzer = analyzerStandard
	titleMapping.Store = false
	titleMapping.IncludeInAll = true

	kwMapping := bleve.NewTextFieldMapping()
	kwMapping.Analyzer = analyzerStandard
	kwMapping.Store = false

	contentMapping := bleve.NewTextFieldMapping()
	contentMapping.Analyzer = analyzerStandard
	contentMapping.Store = false

	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("title", titleMapping)
	docMapping.AddFieldMappingsAt("keywords", kwMapping)
	docMapping.AddFieldMappingsAt("content", contentMapping)

	indexMapping := bleve.NewIndexMapping()
	indexMapping.DefaultMapping = docMapping
	indexMapping.DefaultAnalyzer = analyzerStandard

	index, err := bleve.NewMemOnly(indexMapping)
	if err != nil {
		return fmt.Errorf("create index: %w", err)
	}

	batch := index.NewBatch()
	for uri, doc := range s.docs {
		_ = batch.Index(uri, map[string]any{
			"title":    doc.Title,
			"keywords": strings.Join(doc.Keywords, " "),
			"content":  doc.Content,
		})
	}
	if err := index.Batch(batch); err != nil {
		return fmt.Errorf("index batch: %w", err)
	}

	s.index = index
	return nil
}

// Search performs a BM25 search with field boosts and query expansion.
func (s *Store) Search(query string, limit int) []SearchResult {
	if limit <= 0 {
		limit = 5
	}

	expanded := expandQuery(query)

	titleQuery := bleve.NewMatchQuery(expanded)
	titleQuery.SetField("title")
	titleQuery.SetBoost(2.0)

	kwQuery := bleve.NewMatchQuery(expanded)
	kwQuery.SetField("keywords")
	kwQuery.SetBoost(1.5)

	contentQuery := bleve.NewMatchQuery(expanded)
	contentQuery.SetField("content")
	contentQuery.SetBoost(1.0)

	disjunction := bleve.NewDisjunctionQuery(titleQuery, kwQuery, contentQuery)

	searchRequest := bleve.NewSearchRequest(disjunction)
	searchRequest.Size = limit

	results, err := s.index.Search(searchRequest)
	if err != nil || results.Total == 0 {
		return nil
	}

	out := make([]SearchResult, 0, len(results.Hits))
	for _, hit := range results.Hits {
		doc := s.docs[hit.ID]
		if doc == nil {
			continue
		}
		out = append(out, SearchResult{
			URI:     doc.URI,
			Title:   doc.Title,
			Score:   hit.Score,
			Snippet: extractSnippet(doc.Content, query, 300),
		})
	}
	return out
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

// ExpandQuery is exported for testing.
func ExpandQuery(query string) string {
	return expandQuery(query)
}

// GetFoundation returns the full foundation/core.md content.
// This is the base layer for all infrastructure tasks.
func (s *Store) GetFoundation() (string, error) {
	doc, err := s.Get("zerops://foundation/core")
	if err != nil {
		return "", fmt.Errorf("foundation core not found: %w", err)
	}
	return doc.Content, nil
}

// GetCorePrinciples is an alias for GetFoundation (backward compatibility).
func (s *Store) GetCorePrinciples() (string, error) {
	return s.GetFoundation()
}

// runtimeRecipeHints maps runtime base names to recipe name prefixes/matches.
var runtimeRecipeHints = map[string][]string{
	"bun":    {"bun"},
	"nodejs": {"nestjs", "nextjs", "svelte-nodejs", "react-nodejs", "qwik-nodejs", "payload", "ghost", "medusa"},
	"go":     {"echo-go"},
	"python": {"django"},
	"elixir": {"phoenix"},
	"php":    {"laravel", "symfony", "nette", "filament", "twill", "php-"},
	"java":   {"java-spring", "spring-boot"},
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

// GetBriefing assembles contextual knowledge for a specific stack.
// Order: runtime exceptions (most actionable) → matching recipes → service cards → core principles → version check.
// runtime: e.g. "php-nginx@8.4" (normalized internally to "PHP" section)
// services: e.g. ["postgresql@16", "valkey@7.2"] (normalized to section names)
// liveTypes: optional live service stack types for version validation (nil = skip)
// Returns assembled markdown content ready for LLM consumption.
func (s *Store) GetBriefing(runtime string, services []string, liveTypes []platform.ServiceStackType) (string, error) {
	var sb strings.Builder

	// 1. Runtime-specific exceptions first (most actionable, LLM attention highest)
	runtimeBase := ""
	if runtime != "" {
		runtimeBase, _, _ = strings.Cut(runtime, "@")
		normalized := normalizeRuntimeName(runtime)
		if normalized != "" {
			if section := s.getRuntimeException(normalized); section != "" {
				sb.WriteString("## Runtime-Specific: ")
				sb.WriteString(normalized)
				sb.WriteString("\n\n")
				sb.WriteString(section)
				sb.WriteString("\n\n---\n\n")
			}
		}
	}

	// 2. Matching recipes hint
	if runtimeBase != "" {
		if recipes := s.matchingRecipes(runtimeBase); len(recipes) > 0 {
			sb.WriteString("## Matching Recipes\n\n")
			sb.WriteString("Available recipes for this runtime (use `zerops_knowledge recipe=\"name\"` to load):\n")
			for _, r := range recipes {
				sb.WriteString("- `")
				sb.WriteString(r)
				sb.WriteString("`\n")
			}
			sb.WriteString("\n---\n\n")
		}
	}

	// 3. Service cards + wiring patterns
	if len(services) > 0 {
		sb.WriteString("## Service Cards\n\n")
		for _, svc := range services {
			normalized := normalizeServiceName(svc)
			if card := s.getServiceCard(normalized); card != "" {
				sb.WriteString("### ")
				sb.WriteString(normalized)
				sb.WriteString("\n\n")
				sb.WriteString(card)
				sb.WriteString("\n\n")
			}
		}
		sb.WriteString("---\n\n")

		// Wiring patterns from services.md
		if wiring := s.getWiringPatterns(); wiring != "" {
			sb.WriteString("## Wiring Patterns\n\n")
			sb.WriteString(wiring)
			sb.WriteString("\n\n")
		}
	}

	// 4. Foundation core (reference)
	core, err := s.GetFoundation()
	if err != nil {
		return "", err
	}
	sb.WriteString(core)
	sb.WriteString("\n\n")

	// 5. Append version check if live types available
	if versionCheck := FormatVersionCheck(runtime, services, liveTypes); versionCheck != "" {
		sb.WriteString("---\n\n")
		sb.WriteString(versionCheck)
	}

	sb.WriteString("\nNext: Generate import.yml and zerops.yml using the rules above. Use only validated versions. Then validate with zerops_import dryRun=true.")

	return sb.String(), nil
}

// getRuntimeException returns the section content for a normalized runtime name.
// Returns empty string if no exceptions for this runtime (not an error).
func (s *Store) getRuntimeException(normalizedName string) string {
	doc, err := s.Get("zerops://foundation/runtimes")
	if err != nil {
		return ""
	}
	sections := parseH2Sections(doc.Content)
	return sections[normalizedName]
}

// getServiceCard returns the section content for a normalized service name.
// Returns empty string if service not found (graceful degradation).
func (s *Store) getServiceCard(normalizedName string) string {
	doc, err := s.Get("zerops://foundation/services")
	if err != nil {
		return ""
	}
	sections := parseH2Sections(doc.Content)
	return sections[normalizedName]
}

// getWiringPatterns returns the "Wiring Patterns" section from services.md.
func (s *Store) getWiringPatterns() string {
	doc, err := s.Get("zerops://foundation/services")
	if err != nil {
		return ""
	}
	sections := parseH2Sections(doc.Content)
	return sections["Wiring Patterns"]
}

// GetRecipe returns the full content of a named recipe.
// name: recipe filename without extension (e.g., "laravel-jetstream")
// Returns error if recipe not found.
func (s *Store) GetRecipe(name string) (string, error) {
	uri := "zerops://recipes/" + name
	doc, err := s.Get(uri)
	if err != nil {
		available := s.ListRecipes()
		return "", fmt.Errorf("recipe %q not found (available: %s)", name, strings.Join(available, ", "))
	}
	return doc.Content, nil
}

// ListRecipes returns names of all available recipes (without extension).
func (s *Store) ListRecipes() []string {
	var recipes []string
	prefix := "zerops://recipes/"
	for uri := range s.docs {
		if name, ok := strings.CutPrefix(uri, prefix); ok {
			recipes = append(recipes, name)
		}
	}
	sort.Strings(recipes)
	return recipes
}
