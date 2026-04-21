package knowledge

import (
	"io/fs"
	"strings"

	"github.com/zeropsio/zcp/internal/content"
)

// Cx-KNOWLEDGE-INDEX-MANIFEST (v35 F-6 close) — canonical wire-contract
// atoms surfaced via explicit keyword synonyms so the main agent can
// recover them from `zerops_knowledge query=...` when its paraphrased
// understanding of a schema fails.
//
// v35 smoking gun at 08:45:58: main agent called
// `zerops_knowledge query="ZCP_CONTENT_MANIFEST.json schema
// writer_manifest_completeness"` after eight failed manifest-fix
// attempts. Top hit was `decisions/choose-queue` (score 1) —
// completely unrelated. The embedding-based text-match ranking scores
// poorly on short schema-keyword queries where the target atom is
// short prose with few keyword repetitions. The synonym index is
// additive: embedding hits stay; synonym hits are prepended and
// boosted so they always appear in the top-N.
//
// See docs/zcprecipator2/05-regression/defect-class-registry.md §16.6.

// wireContractAtom describes one atom exposed as a synonym-routable
// search result. A query containing any of `synonyms` as a case-
// insensitive substring routes this atom to the top of the search
// ranking.
type wireContractAtom struct {
	atomID   string
	uri      string
	filePath string
	title    string
	synonyms []string
}

// wireContractAtoms is the authoritative list of (atom → synonym
// keywords) mappings. Each atom's synonym set should cover both
// literal wire-contract tokens (JSON keys, file names, check names)
// AND conceptual phrasings the agent is likely to try after seeing a
// check failure message.
//
// Keep `synonyms` entries lowercased; `matchWireContractSynonyms`
// lowercases the query before the substring check.
var wireContractAtoms = []wireContractAtom{
	{
		atomID:   "briefs.writer.manifest-contract",
		uri:      "zerops://recipe-atom/briefs.writer.manifest-contract",
		filePath: "workflows/recipe/briefs/writer/manifest-contract.md",
		title:    "Writer content manifest contract (ZCP_CONTENT_MANIFEST.json)",
		synonyms: []string{
			"zcp_content_manifest.json",
			"zcp_content_manifest",
			"content_manifest.json",
			"content manifest",
			"manifest schema",
			"manifest contract",
			"fact_title",
			"routed_to",
			"override_reason",
			"writer_manifest_completeness",
			"writer_manifest_honesty",
			"classification-consistency",
		},
	},
	{
		atomID:   "briefs.writer.routing-matrix",
		uri:      "zerops://recipe-atom/briefs.writer.routing-matrix",
		filePath: "workflows/recipe/briefs/writer/routing-matrix.md",
		title:    "Writer routing matrix (routed_to values)",
		synonyms: []string{
			"routing matrix",
			"manifest routing",
			"routed_to values",
			"claude_md",
			"content_ig",
			"content_intro",
			"zerops_yaml_comment",
			"content_env_comment",
			"discarded",
		},
	},
	{
		atomID:   "briefs.writer.classification-taxonomy",
		uri:      "zerops://recipe-atom/briefs.writer.classification-taxonomy",
		filePath: "workflows/recipe/briefs/writer/classification-taxonomy.md",
		title:    "Writer classification taxonomy (gotcha vs ig vs intro vs discarded)",
		synonyms: []string{
			"classification taxonomy",
			"classify fact",
			"classification values",
			"gotcha_candidate",
			"ig_item_candidate",
			"platform_observation",
		},
	},
	{
		atomID:   "briefs.writer.content-surface-contracts",
		uri:      "zerops://recipe-atom/briefs.writer.content-surface-contracts",
		filePath: "workflows/recipe/briefs/writer/content-surface-contracts.md",
		title:    "Writer content-surface contracts (fragment markers + intro + IG + knowledge-base)",
		synonyms: []string{
			"zerops_extract_start",
			"zerops_extract_end",
			"fragment marker",
			"fragment markers",
			"knowledge-base fragment",
			"integration-guide fragment",
			"intro fragment",
		},
	},
	{
		atomID:   "briefs.writer.citation-map",
		uri:      "zerops://recipe-atom/briefs.writer.citation-map",
		filePath: "workflows/recipe/briefs/writer/citation-map.md",
		title:    "Writer citation map (which guide covers which fact)",
		synonyms: []string{
			"citation map",
			"guide citation",
			"knowledge-base cite",
			"mandatory citation",
		},
	},
}

// synonymBoostScore is the base score assigned to synonym hits so they
// always rank above embedding-based text-match hits in the Search
// result list. The highest-ever text-match score in practice is
// roughly `len(words) * (titleBoost + contentBoost) = N*3`; 100 is
// comfortably above any realistic text-match total for bounded query
// lengths.
const synonymBoostScore = 100.0

// matchWireContractSynonyms returns the wire-contract atoms whose
// synonym set contains any substring match against the (lowercased)
// query. Returned order preserves `wireContractAtoms` declaration
// order so concurrent-substring queries (e.g. matching both
// manifest-contract and routing-matrix) rank deterministically.
func matchWireContractSynonyms(query string) []*wireContractAtom {
	q := strings.ToLower(query)
	if q == "" {
		return nil
	}
	var hits []*wireContractAtom
	for i := range wireContractAtoms {
		atom := &wireContractAtoms[i]
		for _, syn := range atom.synonyms {
			if strings.Contains(q, syn) {
				hits = append(hits, atom)
				break
			}
		}
	}
	return hits
}

// loadWireContractAtomBody reads the atom's body from the embedded
// recipe content tree. Returns the full body on success; empty string
// + error on read failure (caller decides whether to surface or
// silently skip — search shouldn't fail hard on an atom tree refactor
// that renamed a file).
func loadWireContractAtomBody(atom *wireContractAtom) (string, error) {
	data, err := fs.ReadFile(content.RecipeAtomsFS, atom.filePath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// wireContractSearchResults builds SearchResult entries for every
// atom matched by `matchWireContractSynonyms`. Each result carries
// `synonymBoostScore` plus a small per-atom tie-breaker so deterministic
// ordering holds across map iteration.
//
// On a read error for an atom, that atom is skipped silently (the
// remaining synonym hits still apply + embedding hits below still
// surface). A hard failure here would break the overall search path
// for an agent that queried a renamed atom — not the right trade.
func wireContractSearchResults(query string) []SearchResult {
	hits := matchWireContractSynonyms(query)
	if len(hits) == 0 {
		return nil
	}
	results := make([]SearchResult, 0, len(hits))
	for i, atom := range hits {
		body, err := loadWireContractAtomBody(atom)
		if err != nil {
			continue
		}
		// Tie-breaker: first declared atom scores fractionally
		// higher than subsequent ones so the ordering of
		// wireContractAtoms acts as a stable precedence list.
		score := synonymBoostScore - 0.01*float64(i)
		results = append(results, SearchResult{
			URI:     atom.uri,
			Title:   atom.title,
			Score:   score,
			Snippet: extractSnippet(body, query, 300) + "\n\n[Retrieve full atom body via: zerops_workflow action=dispatch-brief-atom atomId=" + atom.atomID + "]",
		})
	}
	return results
}
