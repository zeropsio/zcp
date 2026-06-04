package recipe

import (
	"fmt"
	"regexp"
	"strings"
)

// RefinementSuspect names one engine-pre-flagged fragment the refinement
// sub-agent should investigate. Class is a short tag the rule-walk uses to
// route ACT/HOLD reasoning ("kb-author-claim-stem", "missing-citation",
// "single-sided-tradeoff"); FragmentID identifies the suspect; Reason is
// a one-line prose hint naming the rule that flagged it.
//
// Run-23 F-24 — refinement brief stops being "read everything, find
// anything wrong" and becomes "investigate THESE specific suspects,
// ACT or HOLD with reasons." The list is engine-pre-collected from
// notices + cheap rubric regex passes; the agent retains the rubric
// and may still find issues outside the list.
type RefinementSuspect struct {
	Class      string
	FragmentID string
	Reason     string
}

// kbAuthorClaimStemPattern catches KB stems that name a directive
// without a symptom signal — the bullet sentence that opens with a
// `**bold**` author-claim like "use X" / "set Y" / "configure Z" /
// "pin W" without an HTTP code, quoted error, failure verb, or
// observable wrong-state phrase. The match is necessarily heuristic
// (the KB-shape rule walk is multi-signal); this is the cheap
// pre-scan that surfaces likely candidates for the agent's own rule-walk.
var kbAuthorClaimStemPattern = regexp.MustCompile(`(?m)^- \*\*(?:Use|Set|Pin|Configure|Define|Add|Enable|Disable|Replace|Always|Never)\b[^*]*\*\* — `)

// kbSymptomSignalPattern matches the symptom-signals the KB-shape rule
// (KB1 in `derived_rules.md`) rewards: HTTP code, quoted error string,
// failure verb, observable-wrong-state phrase. A KB bullet that carries
// any of these in its first sentence is NOT a suspect.
var kbSymptomSignalPattern = regexp.MustCompile(`(?i)\b(?:[1-5]\d{2}|fails|crashes|corrupts|deadlocks?|silently exits?|returns null|breaks|drops|rejects|hangs|times? out|panics|missing|wrong|empty body|null where|404 on|undefined)\b`)

// CollectRefinementSuspects gathers suspects from (a) existing notices
// emitted at finalize / codebase-content phases and (b) a cheap
// rule-walk regex pre-scan over the per-codebase KB fragment bodies.
//
// The function is intentionally deterministic + fast (single pass over
// notices + regex over each codebase KB body); composer calls it at
// brief assembly time. Cross-recipe-corpus shingle similarity is
// deferred — initial cut just covers the two highest-yield classes
// (cross-surface duplication notices + KB stem author-claim shape).
func CollectRefinementSuspects(plan *Plan, notices []Violation) []RefinementSuspect {
	if plan == nil {
		return nil
	}
	var suspects []RefinementSuspect

	// (a) Pull every existing notice into the suspect list. The notice
	// surface (codebase-content + finalize) flags duplication, voice
	// drift, and other rule-adjacent concerns the agent should
	// investigate during refinement. Engine flags; agent decides.
	for _, n := range notices {
		if n.Severity != SeverityNotice {
			continue
		}
		suspects = append(suspects, RefinementSuspect{
			Class:      n.Code,
			FragmentID: n.Path,
			Reason:     n.Message,
		})
	}

	// (b) KB rule-walk pre-scan — flag fragments whose KB body opens
	// with a `**author-directive**` bold-prefix bullet that lacks a
	// symptom signal. The agent's own rule-walk against KB1 in
	// `derived_rules.md` is the load-bearing decision; this just says
	// "look here first."
	for _, cb := range plan.Codebases {
		fragID := "codebase/" + cb.Hostname + "/knowledge-base"
		body, ok := plan.Fragments[fragID]
		if !ok || strings.TrimSpace(body) == "" {
			continue
		}
		// Walk each `- **stem**` line; flag when stem matches the
		// author-claim shape AND the same line lacks a symptom signal.
		for line := range strings.SplitSeq(body, "\n") {
			if !strings.HasPrefix(line, "- **") {
				continue
			}
			if !kbAuthorClaimStemPattern.MatchString(line) {
				continue
			}
			if kbSymptomSignalPattern.MatchString(line) {
				// Author-claim wrapper but symptom signal in the body
				// opening — the directive-tightly-mapped 8.5 anchor
				// case. Not a suspect.
				continue
			}
			suspects = append(suspects, RefinementSuspect{
				Class:      "kb-author-claim-stem",
				FragmentID: fragID,
				Reason:     "stem opens with author-directive bold prefix without a symptom signal in the same line — walk KB1 (symptom-first stem shape) in `derived_rules.md`; deeper context in `zerops_knowledge uri=\"zerops://themes/refinement-references/kb_shapes\"`",
			})
			// Only one suspect per fragment for this class.
			break
		}
	}

	// (c) Run-29 Fix #4 — IG ↔ yaml-comment same-mechanism duplication
	// pre-scan (cross-surface duplication defense-in-depth; the
	// authoring-order teaching in synthesis_workflow.md is the primary
	// fix). For each
	// codebase, walk the IG fragment + zerops.yaml fragment; for each
	// canonical mechanism anchor, if BOTH fragments contain the anchor
	// AND share ≥10 consecutive non-whitespace bytes of context, emit
	// a suspect. Anchor list is hand-curated from run-K dogfood
	// evidence; expansion follows the same dogfood-evidence rule as
	// the Fix #2 Notice list (don't expand without explicit dogfood
	// justification — catalog-drift signature).
	for _, cb := range plan.Codebases {
		igID := "codebase/" + cb.Hostname + "/integration-guide"
		yamlID := "codebase/" + cb.Hostname + "/zerops-yaml"
		igBody := plan.Fragments[igID]
		yamlBody := plan.Fragments[yamlID]
		if strings.TrimSpace(igBody) == "" || strings.TrimSpace(yamlBody) == "" {
			continue
		}
		// Ignore IG #1's verbatim yaml block when comparing — that's
		// the engine-emit and the spec carves it out as the special-case
		// non-duplication. Strip the first ```yaml ... ``` block.
		igProse := stripFirstYamlFencedBlock(igBody)
		for _, anchor := range igYamlSameMechanismAnchors {
			if !strings.Contains(igProse, anchor) {
				continue
			}
			if !strings.Contains(yamlBody, anchor) {
				continue
			}
			if !sharesProseContext(igProse, yamlBody, anchor, 10) {
				continue
			}
			suspects = append(suspects, RefinementSuspect{
				Class:      "ig-yamlcomment-dup",
				FragmentID: igID,
				Reason: fmt.Sprintf(
					"IG and zerops.yaml comment for %q both teach the %q mechanism with overlapping prose context — Surface 4 owns the mechanism, Surface 7 owns the field-adjacent WHY-choice (see briefs/codebase-content/synthesis_workflow.md §Surface ownership). Edit one surface to a cross-reference of the other.",
					cb.Hostname, anchor),
			})
		}
	}
	return suspects
}

// igYamlSameMechanismAnchors enumerates the canonical Zerops mechanism
// tokens that, when present on BOTH the codebase's IG body AND its
// zerops.yaml comments with overlapping prose context, signal a Run-29
// Fix #4 surface-ownership violation. Frozen list — expansion would be
// the catalog-drift signature.
var igYamlSameMechanismAnchors = []string{
	"${db_hostname}",
	"${broker_",
	"${cache_",
	"forcePathStyle",
	"execOnce",
	"VITE_API_URL",
}

// stripFirstYamlFencedBlock removes the first ```yaml ... ``` block
// from `body`. Used by the IG ↔ yaml-comment pre-scan so the engine-
// stamped IG #1 yaml block (which legitimately contains the codebase's
// own zerops.yaml verbatim) doesn't tip the duplication predicate.
func stripFirstYamlFencedBlock(body string) string {
	const yamlOpen = "```yaml"
	const fenceClose = "```"
	i := strings.Index(body, yamlOpen)
	if i < 0 {
		return body
	}
	closeStart := i + len(yamlOpen)
	j := strings.Index(body[closeStart:], fenceClose)
	if j < 0 {
		return body[:i]
	}
	return body[:i] + body[closeStart+j+len(fenceClose):]
}

// sharesProseContext reports whether `a` and `b` share at least
// `minRunNoWS` consecutive non-whitespace bytes of prose context
// drawn from a window around the named anchor in each. Used by the
// IG ↔ yaml-comment duplication predicate so an anchor mention that
// lives in disjoint surrounding prose (one teaches mechanism A,
// the other a different mechanism that happens to mention the same
// token) doesn't trip the suspect.
func sharesProseContext(a, b, anchor string, minRunNoWS int) bool {
	ctxA := contextWindow(a, anchor, 200)
	ctxB := contextWindow(b, anchor, 200)
	if ctxA == "" || ctxB == "" {
		return false
	}
	// Build a compacted (whitespace-collapsed) version of A and slide
	// a window of `minRunNoWS` consecutive non-whitespace bytes;
	// require any to land inside compacted B.
	compactA := stripWhitespace(ctxA)
	compactB := stripWhitespace(ctxB)
	if len(compactA) < minRunNoWS || len(compactB) < minRunNoWS {
		return false
	}
	for i := 0; i+minRunNoWS <= len(compactA); i++ {
		if strings.Contains(compactB, compactA[i:i+minRunNoWS]) {
			return true
		}
	}
	return false
}

// contextWindow returns a window of up to `radius` bytes on either
// side of the FIRST occurrence of `anchor` in `body`. Empty when
// anchor is absent.
func contextWindow(body, anchor string, radius int) string {
	i := strings.Index(body, anchor)
	if i < 0 {
		return ""
	}
	start := max(i-radius, 0)
	end := min(i+len(anchor)+radius, len(body))
	return body[start:end]
}

// stripWhitespace drops ASCII whitespace (space, tab, CR, LF) from s.
// Non-ASCII whitespace is kept as-is — the predicate is a heuristic,
// not a Unicode normalizer.
func stripWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// FactBelongsToCodebases reports whether a fact's `service` field maps
// to any codebase under review. Slot-name aliases (`apidev`,
// `apistage`) map to the bare codebase name (`api`); the bare name
// matches directly. Returns true when service is empty (run-wide fact
// with no slot binding — keep these in scope by default) or when the
// caller passes an empty codebase list (back-compat fallback —
// "include everything" is safer than dropping every fact).
//
// Run-23 F-24 — refinement brief filters facts to per-codebase scope
// where the agent will be reviewing per-codebase fragments; drops
// facts whose `service` field doesn't match any codebase under review.
// Without the filter, the brief shipped 75 facts × ~600 bytes each
// (~45 KB) into the refinement composer; per-codebase scoping
// typically halves that.
//
// Slot-suffix matching is closed-set: the function recognizes only the
// `dev` and `stage` slot suffixes. The slot taxonomy at run-23 is
// `<host>` (single-slot tier) and `<host>dev` / `<host>stage` (dev-pair
// tiers); any future slot name would require extending the matcher.
// `/runtime` / `/build` sub-suffixes are stripped before matching so
// the slot-name comparison is host-prefixed.
func FactBelongsToCodebases(fact FactRecord, codebases []Codebase) bool {
	svc := strings.TrimSpace(fact.Service)
	if svc == "" {
		return true
	}
	if len(codebases) == 0 {
		// Back-compat fallback — refinement composer on a plan with
		// zero codebases authored. "Include everything" is safer than
		// dropping every fact (which would leave the refinement brief
		// fact-empty for the entire run).
		return true
	}
	// Drop a `/runtime` / `/build` suffix the slot fields sometimes carry.
	if i := strings.IndexByte(svc, '/'); i > 0 {
		svc = svc[:i]
	}
	for _, cb := range codebases {
		host := cb.Hostname
		if svc == host || svc == host+"dev" || svc == host+"stage" {
			return true
		}
	}
	return false
}

// FormatRefinementSuspects renders the suspect list as a markdown
// section for the brief. Empty list returns empty string (composer
// skips the section header).
func FormatRefinementSuspects(suspects []RefinementSuspect) string {
	if len(suspects) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Engine-flagged suspects (investigate at minimum these)\n\n")
	b.WriteString("Each suspect names a fragment and the rule that flagged it. The list is the engine's pre-scan over notices + KB regex; it is NOT exhaustive — `derived_rules.md` remains your authority. ACT or HOLD with reasons; record a notice when you HOLD on a flagged class.\n\n")
	for _, s := range suspects {
		fmt.Fprintf(&b, "- **%s** — `%s`: %s\n", s.Class, s.FragmentID, s.Reason)
	}
	b.WriteByte('\n')
	return b.String()
}
