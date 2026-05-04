package recipe

import (
	"fmt"
	"regexp"
	"strings"
)

// RefinementSuspect names one engine-pre-flagged fragment the refinement
// sub-agent should investigate. Class is a short tag the rubric uses to
// route ACT/HOLD reasoning ("kb-author-claim-stem", "missing-citation",
// "single-sided-tradeoff"); FragmentID identifies the suspect; Reason is
// a one-line prose hint naming the rubric anchor that flagged it.
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
// (the rubric's full scoring is multi-signal); this is the cheap
// pre-scan that surfaces likely candidates for the agent's own scoring.
var kbAuthorClaimStemPattern = regexp.MustCompile(`(?m)^- \*\*(?:Use|Set|Pin|Configure|Define|Add|Enable|Disable|Replace|Always|Never)\b[^*]*\*\* — `)

// kbSymptomSignalPattern matches the symptom-signals the rubric
// rewards: HTTP code, quoted error string, failure verb,
// observable-wrong-state phrase. A KB bullet that carries any of
// these in its first sentence is NOT a suspect.
var kbSymptomSignalPattern = regexp.MustCompile(`(?i)\b(?:[1-5]\d{2}|fails|crashes|corrupts|deadlocks?|silently exits?|returns null|breaks|drops|rejects|hangs|times? out|panics|missing|wrong|empty body|null where|404 on|undefined)\b`)

// CollectRefinementSuspects gathers suspects from (a) existing notices
// emitted at finalize / codebase-content phases and (b) a cheap rubric
// regex pre-scan over the per-codebase KB fragment bodies.
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
	// drift, and other rubric-adjacent concerns the agent should
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

	// (b) KB rubric pre-scan — flag fragments whose KB body opens
	// with a `**author-directive**` bold-prefix bullet that lacks a
	// symptom signal. The agent's own rubric scoring is the load-
	// bearing decision; this just says "look here first."
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
				Reason:     "stem opens with author-directive bold prefix without a symptom signal in the same line — score Criterion 1 against `zerops://themes/refinement-references/kb_shapes`",
			})
			// Only one suspect per fragment for this class.
			break
		}
	}
	return suspects
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
	b.WriteString("Each suspect names a fragment and the rubric anchor that flagged it. The list is the engine's pre-scan over notices + KB regex; it is NOT exhaustive — the rubric remains your authority. ACT or HOLD with reasons; record a notice when you HOLD on a flagged class.\n\n")
	for _, s := range suspects {
		fmt.Fprintf(&b, "- **%s** — `%s`: %s\n", s.Class, s.FragmentID, s.Reason)
	}
	b.WriteByte('\n')
	return b.String()
}
