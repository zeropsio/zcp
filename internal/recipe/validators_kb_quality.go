package recipe

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/zeropsio/zcp/internal/knowledge"
)

// V-2/V-3/V-4 KB-quality validators (run-11). validators_codebase.go's
// validateCodebaseKB calls into these; keeping them in their own file
// stays under the 350-line cap and groups the gap-V content-discipline
// surface together.

// guideKnowledgeSources maps each Citation map guide-id to one or
// more authoritative embedded knowledge URIs that document the topic.
// The Citation map's guide IDs are conceptual labels (`env-var-model`,
// `http-support`) that don't have a 1:1 file mapping in the embedded
// corpus — this map names the canonical source(s) explicitly so V-2
// can load them via knowledge.Store.Get and Jaccard against the
// bullet body. Update when a new Citation map id is introduced.
var guideKnowledgeSources = map[string][]string{
	"env-var-model": {
		"zerops://guides/environment-variables",
		"zerops://themes/core",
	},
	"init-commands": {
		"zerops://themes/core",
		"zerops://themes/model",
	},
	"rolling-deploys": {
		"zerops://guides/scaling",
		"zerops://guides/deployment-lifecycle",
	},
	"object-storage": {
		"zerops://guides/object-storage-integration",
	},
	"http-support": {
		"zerops://guides/public-access",
		"zerops://guides/networking",
	},
	"deploy-files": {
		"zerops://guides/zerops-yaml-advanced",
	},
	"readiness-health-checks": {
		"zerops://guides/deployment-lifecycle",
		"zerops://guides/production-checklist",
	},
}

// kbStopWords filters filler tokens that would inflate overlap. Small
// English-glue list; domain terms (subdomain, container, etc.) stay in.
var kbStopWords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "are": true,
	"was": true, "were": true, "be": true, "of": true, "to": true,
	"in": true, "on": true, "for": true, "and": true, "or": true,
	"but": true, "this": true, "that": true, "it": true, "its": true,
	"with": true, "by": true, "as": true, "at": true, "from": true,
	"if": true, "so": true, "do": true, "does": true, "did": true,
	"have": true, "has": true, "had": true, "not": true, "no": true,
	"yes": true, "any": true, "all": true, "via": true, "per": true,
	"into": true, "onto": true, "over": true, "than": true, "then": true,
	"can": true, "you": true, "your": true, "use": true, "using": true,
	"see": true, "may": true, "will": true, "would": true, "should": true,
	"these": true, "those": true, "such": true, "only": true, "also": true,
	"when": true, "where": true, "what": true, "which": true, "how": true,
	"each": true, "between": true, "across": true, "while": true, "out": true,
	"up": true, "down": true, "off": true, "one": true, "two": true,
	"more": true, "most": true, "some": true, "many": true, "few": true,
}

// guideKeywordTopN — number of top-frequency tokens kept from a
// guide's body to represent its "key phrases". V-2 computes
// containment of the bullet's token set within this keyword set.
const guideKeywordTopN = 100

// paraphraseContainmentThreshold — V-2 flags when the fraction of the
// bullet's tokens that fall inside the cited guide's top-N keyword
// set exceeds this. Containment (not symmetric Jaccard) is the
// natural metric for asymmetric set sizes — a bullet has ~20 tokens,
// the guide ~thousands, so symmetric Jaccard would never approach 0.5.
const paraphraseContainmentThreshold = 0.5

var (
	guideKeywordsOnce sync.Map // guideID -> map[string]bool (lazy)
	kbWordRE          = regexp.MustCompile(`[A-Za-z0-9-]+`)
	kbBacktickIDRE    = regexp.MustCompile("`([a-z][a-z0-9-]+)`")
	kbBulletStartRE   = regexp.MustCompile(`(?m)^\s*-\s+`)
)

// tokenizeForJaccard lower-cases, splits on word boundaries, drops
// stop-words and length<2 tokens. Returns a presence set.
func tokenizeForJaccard(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range kbWordRE.FindAllString(strings.ToLower(s), -1) {
		w = strings.Trim(w, "-")
		if len(w) < 2 || kbStopWords[w] {
			continue
		}
		out[w] = true
	}
	return out
}

// tokenizeWithCounts is like tokenizeForJaccard but returns frequency
// counts. Used to pick the top-N most frequent tokens from a guide
// body.
func tokenizeWithCounts(s string) map[string]int {
	out := map[string]int{}
	for _, w := range kbWordRE.FindAllString(strings.ToLower(s), -1) {
		w = strings.Trim(w, "-")
		if len(w) < 2 || kbStopWords[w] {
			continue
		}
		out[w]++
	}
	return out
}

// guideKeywordSet returns the top-N most frequent meaningful tokens
// across all knowledge URIs for a guide id. Memoized — guide bodies
// are immutable for a given binary. Returns nil when the guide id
// has no source mapping or the embedded store is unavailable.
func guideKeywordSet(guideID string) map[string]bool {
	if cached, ok := guideKeywordsOnce.Load(guideID); ok {
		if set, ok := cached.(map[string]bool); ok {
			return set
		}
	}
	uris, ok := guideKnowledgeSources[guideID]
	if !ok {
		return nil
	}
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		return nil
	}
	counts := map[string]int{}
	for _, uri := range uris {
		doc, err := store.Get(uri)
		if err != nil || doc == nil {
			continue
		}
		for tok, c := range tokenizeWithCounts(doc.Content) {
			counts[tok] += c
		}
	}
	if len(counts) == 0 {
		return nil
	}
	type kv struct {
		k string
		v int
	}
	pairs := make([]kv, 0, len(counts))
	for k, v := range counts {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].v != pairs[j].v {
			return pairs[i].v > pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})
	n := min(guideKeywordTopN, len(pairs))
	out := make(map[string]bool, n)
	for _, p := range pairs[:n] {
		out[p.k] = true
	}
	guideKeywordsOnce.Store(guideID, out)
	return out
}

// containment returns |bullet ∩ keyword| / |bullet|. Asymmetric
// metric — what fraction of the bullet's vocabulary IS in the cited
// guide's top-frequency vocabulary.
func containment(bullet, keyword map[string]bool) float64 {
	if len(bullet) == 0 || len(keyword) == 0 {
		return 0
	}
	inter := 0
	for k := range bullet {
		if keyword[k] {
			inter++
		}
	}
	return float64(inter) / float64(len(bullet))
}

// kbBulletBlocks splits a KB body into per-bullet text blocks. Each
// bullet starts with `^- ` and continues until the next bullet or end.
func kbBulletBlocks(kb string) []string {
	idx := kbBulletStartRE.FindAllStringIndex(kb, -1)
	if len(idx) == 0 {
		return nil
	}
	out := make([]string, 0, len(idx))
	for i, m := range idx {
		end := len(kb)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		out = append(out, kb[m[0]:end])
	}
	return out
}

// platformMentionVocabBase — the static portion of V-3's platform
// vocabulary. Plan-derived hostnames are appended at validate-time so
// recipe-specific service names (e.g. `meilisearch1`) count as
// platform mentions too. Case-sensitive matches; case-insensitive
// containment is checked at lookup time.
var platformMentionVocabBase = []string{
	"Zerops", "L7", "balancer", "subdomain", "zerops.yaml",
	"zsc", "execOnce", "appVersionId", "VXLAN",
	"zeropsSubdomain", "httpSupport", "runtime card",
	"managed service", "${", "deployFiles", "initCommands",
	"envIsolation", "buildFromGit", "preparecommands",
	"forcePathStyle", "minContainers",
}

// kbBulletHasPlatformMention reports whether a bullet body names any
// platform-side mechanism. Used by V-3 to flag bullets with only
// framework concerns. Case-insensitive on substrings.
func kbBulletHasPlatformMention(bullet string, plan *Plan) bool {
	lo := strings.ToLower(bullet)
	for _, kw := range platformMentionVocabBase {
		if strings.Contains(lo, strings.ToLower(kw)) {
			return true
		}
	}
	if plan != nil {
		for _, c := range plan.Codebases {
			if c.Hostname == "" {
				continue
			}
			if strings.Contains(lo, strings.ToLower(c.Hostname)) {
				return true
			}
		}
		for _, s := range plan.Services {
			if s.Hostname == "" {
				continue
			}
			if strings.Contains(lo, strings.ToLower(s.Hostname)) {
				return true
			}
		}
	}
	return false
}

// kbCitedGuideBoilerplateRE flags the "Cited guide: <name>" tail
// pattern run 10 produced — at end-of-bullet (boilerplate), not in
// running prose. The literal phrase "Cited guide:" preceded or
// followed by backtick-quoted guide id.
var kbCitedGuideBoilerplateRE = regexp.MustCompile("(?i)\\*?\\*?Cited guide:\\s*`")

// validateKBCitedGuideBoilerplate runs O-2 (KB side): per-bullet
// regex flag for "Cited guide: <name>." tails. Citations belong in
// prose ("Per the http-support guide…") not as a tail.
func validateKBCitedGuideBoilerplate(path, kb string) []Violation {
	var vs []Violation
	for _, bullet := range kbBulletBlocks(kb) {
		if kbCitedGuideBoilerplateRE.MatchString(bullet) {
			vs = append(vs, notice("kb-cited-guide-boilerplate", path,
				"KB bullet ends with `Cited guide: <name>` boilerplate; citations belong in prose. Restate the rule in the bullet's own words; if you couldn't, the rule isn't yours to write. See spec §'Citation map' — citations are author-time signals, not render output"))
		}
	}
	return vs
}

// kbSelfInflictedVoiceRE flags bullets in first-person/recipe-author
// voice. Phrase signatures of "we tried X, it failed, switched to Y"
// debugging journeys — not porter-facing teaching. Spec rule 4.
var kbSelfInflictedVoiceRE = regexp.MustCompile(`(?i)\b(` +
	`we (chose|tried|fixed|discovered|added|switched|noticed|wrote|saw|hit)|` +
	`i (added|switched|noticed|chose|tried|fixed)|` +
	`the fix was|after running|` +
	`(my|our) (code|setup|scaffold)` +
	`)\b`)

// validateKBSelfInflictedShape runs V-4: regex-flag bullets in
// first-person voice — the "we tried X, the fix was Y" shape that
// reads as scaffold-debugging forensics. Belongs in a commit message
// or discarded; not in published KB.
func validateKBSelfInflictedShape(path, kb string) []Violation {
	var vs []Violation
	for _, bullet := range kbBulletBlocks(kb) {
		if kbSelfInflictedVoiceRE.MatchString(bullet) {
			vs = append(vs, notice("kb-bullet-self-inflicted-shape", path,
				"first-person/recipe-author voice in KB bullet — KB content speaks to porter, not from author. Move to commit message or discard. See spec §'How to classify' rule 4"))
		}
	}
	return vs
}

// validateKBNoPlatformMention runs V-3: each bullet must mention at
// least one platform-side mechanism term. Bullets covering only
// framework concerns (NestJS, Express, Svelte, library lifecycle
// without platform interaction) are framework-quirk per spec → flag.
func validateKBNoPlatformMention(path, kb string, plan *Plan) []Violation {
	var vs []Violation
	for _, bullet := range kbBulletBlocks(kb) {
		if kbBulletHasPlatformMention(bullet, plan) {
			continue
		}
		vs = append(vs, notice("kb-bullet-no-platform-mention", path,
			"KB bullet has zero platform-side vocabulary; framework-quirk content belongs in framework docs, not a Zerops recipe (spec rule 5)"))
	}
	return vs
}

// validateKBParaphrase runs V-2: per-bullet containment of the
// bullet's tokens within the cited guide's top-N most frequent
// keyword set. Bullets with backtick-quoted citation matching a
// known guide id are checked; bullets without a citation are
// untouched (V-3 catches platform-mention-less bullets).
func validateKBParaphrase(path, kb string) []Violation {
	var vs []Violation
	for _, bullet := range kbBulletBlocks(kb) {
		ids := kbBacktickIDRE.FindAllStringSubmatch(bullet, -1)
		bulletTokens := tokenizeForJaccard(bullet)
		seenForBullet := map[string]bool{}
		for _, m := range ids {
			id := m[1]
			if seenForBullet[id] {
				continue
			}
			seenForBullet[id] = true
			keywords := guideKeywordSet(id)
			if keywords == nil {
				continue
			}
			if containment(bulletTokens, keywords) > paraphraseContainmentThreshold {
				vs = append(vs, notice("kb-bullet-paraphrases-cited-guide", path,
					fmt.Sprintf("KB bullet's vocabulary is mostly already in the cited %q guide (containment > %.0f%%); add new content beyond the guide or omit the bullet — see spec rule 3",
						id, paraphraseContainmentThreshold*100)))
				break
			}
		}
	}
	return vs
}
