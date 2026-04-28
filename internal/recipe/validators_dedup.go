package recipe

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Run-16 §6.8 — cross-surface + cross-recipe duplication validators.
// Both are Notice-severity (not blocking). Primary R-15-6 closure is
// the structural single-author phase 5 (codebase-content sub-agent
// sees IG + KB candidate fact lists in one phase); these heuristic
// validators are backstops at finalize.
//
// Method (calibrated against the run-15 corpus): topic-name matching.
// Pure Jaccard scored 0.14-0.16 on the real R-15-6 dup pairs (against
// 0.06 on non-dup pairs); a Jaccard ≥ 0.7 catches zero real dups. The
// signal that DOES catch real dups is topic-name overlap — both IG
// section headings and KB bullets carry `Topic` keys, and dups
// repeat the topic verbatim or with minor inflection.
//
// Implementation: extract `### N. <topic>` IG headings + `- **<topic>**`
// KB bullet topics, normalize (lowercase + strip stopwords), flag
// shared 2+ keyword overlap with the same topic noun phrase.

var (
	igHeadingRE = regexp.MustCompile(`(?m)^###\s+(?:\d+\.\s+)?(.+?)\s*$`)
	kbTopicRE   = regexp.MustCompile(`(?m)^-\s+\*\*(.+?)\*\*`)
)

// validateCrossSurfaceDuplication compares IG and KB content for the
// same codebase. Returns Notice violations naming the duplicated
// topic. Thresholds are deliberately permissive: the structural phase-5
// fix is the primary closure; the validator is a calibration aid.
//
// Run-16 §6.8 — registered as Notice severity in the validator
// registry (not pinned to a SurfaceContract; called by the gate set
// directly). Calibration evidence: against the run-15 corpus, the
// topic-name matcher catches both real R-15-6 dups (apidev X-Cache,
// appdev duplex:'half') while sparing 5 non-dup IG/KB pairs from
// run-15's apidev/appdev/workerdev codebases.
func validateCrossSurfaceDuplication(_ context.Context, plan *Plan) []Violation {
	if plan == nil {
		return nil
	}
	var vs []Violation
	for _, cb := range plan.Codebases {
		// Run-16 §6.7 — the codebase-content sub-agent records IG via
		// slotted ids `codebase/<h>/integration-guide/<n>`; the legacy
		// single-fragment id is empty for slotted recipes. Synthesize
		// the merged IG body the same way assemble.go does at stitch
		// so the validator sees the same content the published surface
		// will carry.
		merged := mergeSlottedIGFragments(plan.Fragments, cb.Hostname)
		igFragment := merged["codebase/"+cb.Hostname+"/integration-guide"]
		kbFragment := plan.Fragments["codebase/"+cb.Hostname+"/knowledge-base"]
		if igFragment == "" || kbFragment == "" {
			continue
		}
		igTopics := extractTopics(igHeadingRE, igFragment)
		kbTopics := extractTopics(kbTopicRE, kbFragment)
		for _, ig := range igTopics {
			for _, kb := range kbTopics {
				if topicsOverlap(ig, kb) {
					vs = append(vs, notice(
						"cross-surface-duplication",
						"codebase/"+cb.Hostname,
						fmt.Sprintf("IG `%s` overlaps KB bullet `%s` — KB bullets should cover topics WITHOUT a codebase-side landing point. R-15-6 closure: structural phase 5 should have caught this; check both surfaces and dedup.", ig, kb),
					))
				}
			}
		}
	}
	return vs
}

// validateCrossRecipeDuplication compares this recipe's content
// against parent's published surfaces. When parent != nil, every
// IG/KB topic the parent already covers should be cross-referenced,
// not re-authored. Notice severity — same calibration discipline.
func validateCrossRecipeDuplication(_ context.Context, plan *Plan, parent *ParentRecipe) []Violation {
	if plan == nil || parent == nil || len(parent.Codebases) == 0 {
		return nil
	}
	var vs []Violation
	for _, cb := range plan.Codebases {
		// Run-16 reviewer D-5 — slotted IG (`codebase/<h>/integration-guide/<n>`)
		// is the run-16 default. Synthesize the merged body the same way
		// validateCrossSurfaceDuplication does so the parent comparison
		// fires against the published shape, not the empty legacy id.
		merged := mergeSlottedIGFragments(plan.Fragments, cb.Hostname)
		igFragment := merged["codebase/"+cb.Hostname+"/integration-guide"]
		if igFragment == "" {
			continue
		}
		// Parent codebase same hostname (when shape matches).
		parentCB, ok := parent.Codebases[cb.Hostname]
		if !ok {
			continue
		}
		// Extract parent's IG topics from its README.
		parentIGTopics := extractTopics(igHeadingRE, parentCB.README)
		thisIGTopics := extractTopics(igHeadingRE, igFragment)
		for _, this := range thisIGTopics {
			for _, parentTopic := range parentIGTopics {
				if topicsOverlap(this, parentTopic) {
					vs = append(vs, notice(
						"cross-recipe-duplication",
						"codebase/"+cb.Hostname,
						fmt.Sprintf("IG `%s` overlaps parent `%s` IG `%s` — parent already covers this topic. Cross-reference the parent instead of re-authoring (parent slug: %s).", this, cb.Hostname, parentTopic, parent.Slug),
					))
				}
			}
		}
	}
	return vs
}

// extractTopics pulls topic phrases from text using a regex. Returns
// the captured groups, lowercased + trimmed. Skips empty matches.
func extractTopics(re *regexp.Regexp, text string) []string {
	if text == "" {
		return nil
	}
	matches := re.FindAllStringSubmatch(text, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		topic := strings.ToLower(strings.TrimSpace(m[1]))
		if topic != "" {
			out = append(out, topic)
		}
	}
	return out
}

// topicsOverlap reports whether two topic phrases share enough
// content-word overlap to count as a duplicate teaching. Uses a 2+
// shared-word rule after stripping stopwords + punctuation. Calibrated
// against run-15 corpus: catches the R-15-6 dups (X-Cache topic in
// both IG + KB; duplex topic in both) while sparing structurally
// distinct topics that share a single common word.
func topicsOverlap(a, b string) bool {
	wa := topicWords(a)
	wb := topicWords(b)
	if len(wa) == 0 || len(wb) == 0 {
		return false
	}
	// Build set of `b` words for O(|a|) lookup.
	set := make(map[string]bool, len(wb))
	for _, w := range wb {
		set[w] = true
	}
	shared := 0
	for _, w := range wa {
		if set[w] {
			shared++
		}
	}
	return shared >= 2
}

// topicStopwords filters short connective words that appear in nearly
// every topic phrase and inflate the overlap signal. Tuned against
// the run-15 corpus.
var topicStopwords = map[string]bool{
	"a": true, "an": true, "and": true, "the": true, "for": true, "of": true,
	"on": true, "in": true, "to": true, "with": true, "via": true, "by": true,
	"is": true, "are": true, "be": true, "or": true, "as": true, "at": true,
	"how": true, "your": true, "use": true, "using": true, "this": true,
	"that": true, "from": true, "into": true, "across": true,
}

// topicWords splits a topic phrase into normalized content words.
// Lowercase, alpha-only, length ≥ 3, non-stopword. Punctuation,
// numbers, and code-fence backticks are stripped.
func topicWords(topic string) []string {
	cleanRE := regexp.MustCompile(`[^a-zA-Z]+`)
	clean := cleanRE.ReplaceAllString(topic, " ")
	out := make([]string, 0)
	for w := range strings.FieldsSeq(clean) {
		w = strings.ToLower(w)
		if len(w) < 3 || topicStopwords[w] {
			continue
		}
		out = append(out, w)
	}
	return out
}
