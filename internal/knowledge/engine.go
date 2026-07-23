package knowledge

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
)

// SearchResult represents a single search result.
type SearchResult struct {
	URI     string  `json:"uri"`
	Title   string  `json:"title"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet"`
}

// Provider interface for knowledge access.
type Provider interface {
	Get(uri string) (*Document, error)
	Search(query string, limit int) []SearchResult
	GetCore() (string, error)
	GetUniversals() (string, error)
	GetModel() (string, error)
	GetBriefing(runtime string, services []string, mode topology.Mode, schemas *schema.Schemas) (string, error)
	GetRecipe(name string, mode topology.Mode) (string, error)
	ListRecipes() []string
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
		if strings.HasPrefix(uri, "zerops://playbooks/") {
			continue
		}

		score := 0.0
		titleLower := strings.ToLower(doc.Title)
		contentLower := strings.ToLower(doc.Content)

		for _, word := range words {
			if strings.Contains(titleLower, word) {
				score += 2.0
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

	results := make([]SearchResult, 0, limit)
	for _, h := range hits {
		if len(results) >= limit {
			break
		}
		doc := s.docs[h.uri]
		results = append(results, SearchResult{
			URI:     doc.URI,
			Title:   doc.Title,
			Score:   h.score,
			Snippet: extractSnippet(doc.Content, expanded, 300),
		})
	}
	return results
}

// Get returns a document by URI.
func (s *Store) Get(uri string) (*Document, error) {
	doc, ok := s.docs[uri]
	if !ok {
		return nil, fmt.Errorf("document not found: %s", uri)
	}
	return doc, nil
}

// GetCore returns the full themes/core.md content (merged platform + rules + grammar).
func (s *Store) GetCore() (string, error) {
	doc, err := s.Get("zerops://themes/core")
	if err != nil {
		return "", fmt.Errorf("core reference not found: %w", err)
	}
	return doc.Content, nil
}

// GetUniversals returns the "Platform Constraints" section from model.md.
// These are the hard rules prepended to every recipe response.
func (s *Store) GetUniversals() (string, error) {
	doc, err := s.Get("zerops://themes/model")
	if err != nil {
		return "", fmt.Errorf("platform model not found: %w", err)
	}
	sections := doc.H2Sections()
	if constraints, ok := sections["Platform Constraints"]; ok {
		return "# Platform Constraints\n\n" + constraints, nil
	}
	return "", fmt.Errorf("platform constraints section not found in model.md")
}

// GetModel returns the platform model content (Container Universe, Lifecycle, Networking, etc.).
func (s *Store) GetModel() (string, error) {
	doc, err := s.Get("zerops://themes/model")
	if err != nil {
		return "", fmt.Errorf("platform model not found: %w", err)
	}
	return doc.Content, nil
}

// runtimeRecipeHints maps runtime base names to recipe name prefixes/matches.
var runtimeRecipeHints = map[string][]string{
	"bun":    {"bun-hello-world", "bun"},
	"nodejs": {"nodejs-hello-world", "nestjs", "nextjs", "svelte", "react", "qwik", "payload", "ghost", "nuxt", "astro", "remix", "solidjs", "analog", "medusa"},
	"go":     {"go-hello-world", "echo-go"},
	"python": {"python-hello-world", "django"},
	"elixir": {"elixir-hello-world", "phoenix", "elixir"},
	"php":    {"php-hello-world", "laravel", "symfony", "nette", "filament", "twill"},
	"java":   {"java-hello-world", "java-spring", "spring-boot"},
	"ruby":   {"ruby-hello-world", "rails"},
	"rust":   {"rust-hello-world"},
	"dotnet": {"dotnet-hello-world", "dotnet"},
	"deno":   {"deno-hello-world"},
	"gleam":  {"gleam-hello-world"},
	"static": {"nextjs", "svelte", "qwik", "astro", "angular", "solidjs", "react", "analog", "nuxt"},
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

// extractSnippet builds a maxLen-byte excerpt whose window best covers the query
// terms. Every occurrence of every matched query word is a candidate anchor; the
// window is scored by how many DISTINCT query words it contains, so a term first
// mentioned in a title or boilerplate lead does not bury the useful later cluster
// (anchoring on the earliest single match made a "typesense" search return a
// choose-search excerpt that listed only the three engines named in the headline
// and truncated before "typesense" — the agent then read it as "not a managed
// service"). The byte-budget window is clamped to UTF-8 rune boundaries so the
// excerpt is always valid UTF-8. Tie-break: tighter matched-term span -> rarer
// anchor word -> longer anchor word -> earlier position.
func extractSnippet(content, query string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	lower := strings.ToLower(content)

	// Distinct query words that occur in the doc, with every occurrence position.
	var words []string
	positions := map[string][]int{}
	for word := range strings.FieldsSeq(strings.ToLower(query)) {
		if _, seen := positions[word]; seen {
			continue
		}
		var ps []int
		for from := 0; from <= len(lower); {
			i := strings.Index(lower[from:], word)
			if i < 0 {
				break
			}
			ps = append(ps, from+i)
			from += i + len(word)
		}
		if len(ps) > 0 {
			words = append(words, word)
			positions[word] = ps
		}
	}

	if len(words) == 0 {
		lines := strings.SplitN(content, "\n", 3)
		if len(lines) >= 3 {
			return truncate(lines[2], maxLen)
		}
		return truncate(content, maxLen)
	}

	// Score a lead-biased window around every occurrence of every matched word;
	// keep the best by coverage, then cluster tightness, then anchor specificity.
	bestPos, bestDistinct, bestSpan, bestCount, bestLen := 0, -1, 0, 0, 0
	bestSet := false
	for _, w := range words {
		for _, pos := range positions[w] {
			start := clampRuneStart(content, max(pos-maxLen/3, 0))
			end := clampRuneStart(content, min(start+maxLen, len(content)))
			win := lower[start:end]
			distinct, spanLo, spanHi := 0, -1, -1
			for _, ww := range words {
				lo := strings.Index(win, ww)
				if lo < 0 {
					continue
				}
				distinct++
				if spanLo < 0 || lo < spanLo {
					spanLo = lo
				}
				if hi := strings.LastIndex(win, ww) + len(ww); hi > spanHi {
					spanHi = hi
				}
			}
			span := spanHi - spanLo
			count, wlen := len(positions[w]), len(w)
			better := func() bool {
				switch {
				case distinct != bestDistinct:
					return distinct > bestDistinct
				case span != bestSpan:
					return span < bestSpan
				case count != bestCount:
					return count < bestCount
				case wlen != bestLen:
					return wlen > bestLen
				default:
					return pos < bestPos
				}
			}
			if !bestSet || better() {
				bestPos, bestDistinct, bestSpan, bestCount, bestLen = pos, distinct, span, count, wlen
				bestSet = true
			}
		}
	}

	start := clampRuneStart(content, max(bestPos-maxLen/3, 0))
	end := clampRuneStart(content, min(start+maxLen, len(content)))
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

// clampRuneStart moves i down to the nearest index that begins a UTF-8 rune
// (or 0/len(s)). Byte-budget windows use it so a slice never cuts a multi-byte
// rune, keeping every emitted snippet valid UTF-8.
func clampRuneStart(s string, i int) int {
	if i <= 0 {
		return 0
	}
	if i >= len(s) {
		return len(s)
	}
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:clampRuneStart(s, maxLen)] + "..."
}
