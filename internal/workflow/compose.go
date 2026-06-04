package workflow

import (
	"sort"
	"strings"
)

// ComposeBodyBudget is the body-bytes ceiling the budget composer keeps the
// synthesized guidance under. It sits below the 28 KB soft cap
// (corpus_coverage_test) and the 32 KB MCP tool-response cap, leaving room for
// the surrounding scaffold (phase / services / plan) RenderStatus adds. The cap
// is a runtime GUARANTEE here, not just a test assertion: a large real project
// (the historical standard-mode multi-service envelopes hit 40 KB) is demoted
// gracefully rather than truncated by the transport.
const ComposeBodyBudget = 24 * 1024

// ComposeUnderBudget keeps the rendered matched set within budget bytes by
// demoting the lowest-relevance atoms to a one-line head — the structural owner
// of "the payload never exceeds the cap by demotion, not by adding an axis"
// (R1). Demotion order is deterministic and total: highest Priority number
// (least important) first, ties broken by atom id descending. Reference atoms
// (already one-line stubs) and priority-0 framing atoms are never demoted. The
// head is the atom's already-substituted first meaningful paragraph, so it stays
// SELF-CONTAINED — no dead pull pointer (an inline atom's body carries
// {hostname} placeholders the substitution-free pull retrieval cannot resolve,
// so a "pull on demand" pointer would 404; the head carries the substituted
// decision lever instead, which is recoverable enough for a cap-overflow
// backstop). When the full set fits, the input is returned UNCHANGED — so on a
// corpus whose fixtures are all under the cap this is a no-op (no rendering
// change), and it only ever fires for a genuinely oversized live payload.
func ComposeUnderBudget(matches []MatchedRender, corpus []KnowledgeAtom, budget int) []MatchedRender {
	total := 0
	for _, m := range matches {
		total += len(m.Body)
	}
	if total <= budget || len(matches) == 0 {
		return matches
	}

	byID := make(map[string]KnowledgeAtom, len(corpus))
	for _, a := range corpus {
		byID[a.ID] = a
	}

	// Demotion candidates: everything except framing (priority 0), reference
	// stubs, and atoms whose id we can't resolve (defensive — keep them full).
	order := make([]int, 0, len(matches))
	for i, m := range matches {
		a, ok := byID[m.AtomID]
		if !ok || a.Reference || a.Priority <= 0 {
			continue
		}
		order = append(order, i)
	}
	sort.SliceStable(order, func(x, y int) bool {
		ax := byID[matches[order[x]].AtomID]
		ay := byID[matches[order[y]].AtomID]
		if ax.Priority != ay.Priority {
			return ax.Priority > ay.Priority // higher number = less important = demote first
		}
		return matches[order[x]].AtomID > matches[order[y]].AtomID
	})

	out := make([]MatchedRender, len(matches))
	copy(out, matches)
	for _, idx := range order {
		if total <= budget {
			break
		}
		a := byID[out[idx].AtomID]
		head := composedHead(a.Title, out[idx].Body)
		if len(head) >= len(out[idx].Body) {
			continue // already short — demoting wouldn't reclaim bytes
		}
		total -= len(out[idx].Body) - len(head)
		out[idx].Body = head
	}
	return out
}

// composedHead renders an atom's substituted body down to a one-line head: the
// title plus its first meaningful paragraph (skipping leading markdown headings),
// capped. The result is self-contained — it carries the decision lever, already
// substituted, with no follow-up fetch required.
func composedHead(title, body string) string {
	const maxLen = 240
	lever := firstMeaningfulParagraph(body)
	if len(lever) > maxLen {
		lever = strings.TrimSpace(lever[:maxLen]) + "…"
	}
	if title == "" {
		return lever
	}
	if lever == "" {
		return "**" + title + "**"
	}
	return "**" + title + "** — " + lever
}

// firstMeaningfulParagraph returns the first non-empty paragraph of body,
// skipping leading markdown heading lines (so the head carries prose, not a
// bare "## Title"). A paragraph ends at the first blank line. Inline newlines
// within the paragraph collapse to single spaces.
func firstMeaningfulParagraph(body string) string {
	lines := strings.Split(body, "\n")
	var para []string
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" {
			if len(para) > 0 {
				break
			}
			continue
		}
		if len(para) == 0 && strings.HasPrefix(t, "#") {
			continue // skip a leading markdown heading
		}
		para = append(para, t)
	}
	return strings.Join(para, " ")
}
