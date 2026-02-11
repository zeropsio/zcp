package knowledge

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/blevesearch/bleve/v2"
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
