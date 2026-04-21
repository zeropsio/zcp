package knowledge

import (
	"slices"
	"strings"
	"testing"
)

// Cx-KNOWLEDGE-INDEX-MANIFEST regression tests (HANDOFF-to-I6,
// defect-class-registry §16.6 `v35-knowledge-manifest-schema-miss`).
//
// v35 evidence at 08:45:58: main agent called
// `zerops_knowledge query="ZCP_CONTENT_MANIFEST.json schema
// writer_manifest_completeness"` after eight failed manifest-fix
// attempts. Top hit was `decisions/choose-queue` (score 1) —
// completely unrelated. The synonym index prepends wire-contract atoms
// matched by canonical keyword substrings so the knowledge engine can
// rescue the agent when its paraphrased understanding of a wire shape
// fails.
//
// Calibration bar B-13: for each canonical wire-contract atom × each
// representative keyword query, the atom must appear in the top 3 of
// `zerops_knowledge` results.

// canonicalQueries pairs the queries from HANDOFF-to-I6's
// Cx-KNOWLEDGE-INDEX-MANIFEST test scenario with the atom ID they must
// surface in the top 3. These are the exact queries the v35 main agent
// tried (or would have tried) when its manifest-shape paraphrase failed.
var canonicalQueries = []struct {
	query  string
	atomID string
}{
	{"ZCP_CONTENT_MANIFEST.json schema", "briefs.writer.manifest-contract"},
	{"writer_manifest_completeness", "briefs.writer.manifest-contract"},
	{"fact_title format", "briefs.writer.manifest-contract"},
	{"manifest routing", "briefs.writer.routing-matrix"},
	{"routing matrix", "briefs.writer.routing-matrix"},
	{"classification taxonomy", "briefs.writer.classification-taxonomy"},
	{"fragment markers", "briefs.writer.content-surface-contracts"},
	{"citation map", "briefs.writer.citation-map"},
}

// TestWireContractSynonyms_CanonicalQueriesSurfaceTargetAtoms pins the
// calibration-bar B-13 assertion: every canonical wire-contract query
// must return its target atom in the top 3 results.
func TestWireContractSynonyms_CanonicalQueriesSurfaceTargetAtoms(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	for _, tt := range canonicalQueries {
		t.Run(tt.query, func(t *testing.T) {
			t.Parallel()
			results := store.Search(tt.query, 5)
			if len(results) == 0 {
				t.Fatalf("Search(%q) returned no results", tt.query)
			}
			top3 := results
			if len(top3) > 3 {
				top3 = top3[:3]
			}
			found := false
			uris := make([]string, 0, len(top3))
			for _, r := range top3 {
				uris = append(uris, r.URI)
				if strings.Contains(r.URI, tt.atomID) {
					found = true
				}
			}
			if !found {
				t.Errorf("Search(%q) top-3 URIs %v — did not include atom %q", tt.query, uris, tt.atomID)
			}
		})
	}
}

// TestWireContractSynonyms_HitsRankAboveEmbeddingHits verifies the score
// boost holds: a synonym-matched atom outranks every text-match hit from
// the embedded document corpus.
func TestWireContractSynonyms_HitsRankAboveEmbeddingHits(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	// "manifest schema" is a synonym for manifest-contract AND a phrase
	// that legitimately appears in some embedded docs (platform schema
	// guides). Both should surface; the synonym hit must rank first.
	results := store.Search("manifest schema", 5)
	if len(results) == 0 {
		t.Fatal("Search returned no results")
	}
	first := results[0]
	if !strings.Contains(first.URI, "briefs.writer.manifest-contract") {
		t.Errorf("top result URI %q; expected manifest-contract atom", first.URI)
	}
	if first.Score < synonymBoostScore-1.0 {
		t.Errorf("top result score %f; expected >= %f (synonym boost)", first.Score, synonymBoostScore-1.0)
	}
}

// TestWireContractSynonyms_NoMatch_PassesThrough guards that queries
// with no synonym match behave exactly as before — the text-match pass
// is the sole source of results, no synonym pollution.
func TestWireContractSynonyms_NoMatch_PassesThrough(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	// Pick a query with no overlap with any wire-contract synonym.
	results := store.Search("postgresql connection pooling", 5)
	for _, r := range results {
		if strings.Contains(r.URI, "zerops://recipe-atom/") {
			t.Errorf("unrelated query returned synonym hit %q — synonym index over-triggered", r.URI)
		}
	}
}

// TestWireContractSynonyms_SnippetIncludesRetrievalPointer confirms the
// snippet tells the main agent how to retrieve the full atom body via
// the Cx-BRIEF-OVERFLOW dispatch-brief-atom action — otherwise the
// agent has an atom-ID reference but no path to the content.
func TestWireContractSynonyms_SnippetIncludesRetrievalPointer(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	results := store.Search("ZCP_CONTENT_MANIFEST.json", 5)
	if len(results) == 0 {
		t.Fatal("Search returned no results")
	}
	first := results[0]
	if !strings.Contains(first.Snippet, "action=dispatch-brief-atom") {
		t.Errorf("snippet missing dispatch-brief-atom retrieval pointer; got: %s", first.Snippet)
	}
	if !strings.Contains(first.Snippet, "briefs.writer.manifest-contract") {
		t.Errorf("snippet missing atomId reference; got: %s", first.Snippet)
	}
}

// TestMatchWireContractSynonyms_CaseInsensitive verifies substring
// matching works regardless of query case.
func TestMatchWireContractSynonyms_CaseInsensitive(t *testing.T) {
	t.Parallel()
	variants := []string{
		"ZCP_CONTENT_MANIFEST.json",
		"zcp_content_manifest.json",
		"Zcp_Content_Manifest.json",
	}
	for _, q := range variants {
		hits := matchWireContractSynonyms(q)
		if len(hits) == 0 {
			t.Errorf("matchWireContractSynonyms(%q) returned no hits", q)
			continue
		}
		atomIDs := make([]string, 0, len(hits))
		for _, h := range hits {
			atomIDs = append(atomIDs, h.atomID)
		}
		if !slices.Contains(atomIDs, "briefs.writer.manifest-contract") {
			t.Errorf("matchWireContractSynonyms(%q) did not match manifest-contract; got: %v", q, atomIDs)
		}
	}
}

// TestLoadWireContractAtomBody_AllAtomsResolve is a build-time safety
// net: every atom registered in wireContractAtoms must resolve to a
// readable file in the embedded recipe tree. An atom file rename or
// delete fails this test at CI, before the synonym index silently
// misses at runtime.
func TestLoadWireContractAtomBody_AllAtomsResolve(t *testing.T) {
	t.Parallel()
	for i := range wireContractAtoms {
		atom := &wireContractAtoms[i]
		body, err := loadWireContractAtomBody(atom)
		if err != nil {
			t.Errorf("loadWireContractAtomBody(%q): %v", atom.atomID, err)
			continue
		}
		if len(strings.TrimSpace(body)) == 0 {
			t.Errorf("atom %q body is empty", atom.atomID)
		}
	}
}
