package skillpacks

import (
	"fmt"
	"sort"
	"strings"
)

// violation is one missing dependency found in a caller-stated skill set: a
// catalog skill named in Missing is required (directly) by every name in
// RequiredBy, and Missing itself is not in the set. RequiredBy is
// deterministically sorted.
type violation struct {
	Missing    string
	RequiredBy []string
}

// skillIndex returns pack's Skills indexed by name, for Requires-edge
// lookups. A name absent from the index (never installable, or outside this
// pack's catalog) resolves to a zero-value CatalogSkill with no Requires —
// callers below treat that as a leaf, never a lookup error, so a caller-set
// name that is not itself a catalog skill only ever adds noise, never a
// panic or a silently wrong closure.
func skillIndex(pack Pack) map[string]CatalogSkill {
	idx := make(map[string]CatalogSkill, len(pack.Skills))
	for _, sk := range pack.Skills {
		idx[sk.Name] = sk
	}
	return idx
}

// closure returns the transitive closure of names over pack's declared
// Requires edges (spec-skill-packs.md §4.2): names plus every direct and
// transitive dependency reachable from them within pack's catalog, so the
// picker can show "checking a skill auto-includes its transitive Requires"
// (§4.2) and pack-set can normalize an opening selection to
// closure(installed ∩ catalog) (§3.1). The result is deduplicated and
// sorted for deterministic output. closure never expands beyond what names
// itself and their Requires edges reach — the caller decides what goes in.
func closure(pack Pack, names []string) []string {
	byName := skillIndex(pack)
	seen := make(map[string]bool, len(names))

	var visit func(name string)
	visit = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		for _, req := range byName[name].Requires {
			visit(req)
		}
	}
	for _, n := range names {
		visit(n)
	}

	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// violations reports every missing dependency in names against pack's
// Requires graph: for each skill in names that is itself a catalog skill,
// each of its direct Requires targets not also in names is a violation.
// Results are one violation per distinct missing skill — a dependency
// required by more than one selected skill (e.g. wayfinder and triage both
// requiring grilling) collapses into a single entry rather than one per
// requirer, so a caller can't double-count or double-render the same
// missing skill. violations is deterministically sorted by Missing name;
// RequiredBy within one violation is sorted too. A dependency-closed set
// (per closure) produces nil. violations never walks the Requires of a
// missing skill — only direct edges of skills actually present in names,
// matching the pack-status "installed selection... in-catalog skill
// present, its dependency absent" contract (spec-skill-packs.md §3.1).
func violations(pack Pack, names []string) []violation {
	byName := skillIndex(pack)
	present := make(map[string]bool, len(names))
	for _, n := range names {
		present[n] = true
	}

	requirers := map[string]map[string]bool{} // missing skill -> set of requiring skills
	for _, n := range names {
		sk, ok := byName[n]
		if !ok {
			continue
		}
		for _, req := range sk.Requires {
			if present[req] {
				continue
			}
			if requirers[req] == nil {
				requirers[req] = map[string]bool{}
			}
			requirers[req][n] = true
		}
	}
	if len(requirers) == 0 {
		return nil
	}

	violations := make([]violation, 0, len(requirers))
	for missing, reqSet := range requirers {
		reqNames := make([]string, 0, len(reqSet))
		for r := range reqSet {
			reqNames = append(reqNames, r)
		}
		sort.Strings(reqNames)
		violations = append(violations, violation{Missing: missing, RequiredBy: reqNames})
	}
	sort.Slice(violations, func(i, j int) bool { return violations[i].Missing < violations[j].Missing })
	return violations
}

// transitiveViolations reports EVERY dependency transitively missing from
// names — not merely the direct violations of names' own members — so one
// report names the complete gap instead of a caller discovering each further
// layer only after fixing the previous one (e.g. just "implement" must name
// tdd, code-review, AND code-review's own dependency
// setup-matt-pocock-skills in one message). It is the one shared semantic
// behind pack-set's unclosed-selection refusal and pack-status's non-closed
// warning (spec-skill-packs.md §7 proofs 14+16), composed as a fixed-point
// iteration over violations: each round re-checks a candidate set grown by
// every violation discovered so far, stopping once a round finds nothing
// new. It never reads CatalogSkill.Requires directly and never reimplements
// closure's graph walk — only violations' single-level check, repeatedly.
//
// A name can never reappear across rounds: violations only ever reports a
// name absent from the candidate set it was given, and every reported name
// is folded into that set before the next round — so accumulation needs no
// dedup, and the loop is bounded by the catalog's size. The result is
// sorted by missing-skill name for a deterministic message.
func transitiveViolations(pack Pack, names []string) []violation {
	current := append([]string(nil), names...)
	var all []violation
	for {
		round := violations(pack, current)
		if len(round) == 0 {
			break
		}
		all = append(all, round...)
		for _, v := range round {
			current = append(current, v.Missing)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Missing < all[j].Missing })
	return all
}

// formatViolations renders violations as the single shared wording used by
// both pack-set's unclosed-selection refusal and pack-status's non-closed
// warning (spec-skill-packs.md §7 proof 16) — the only place this message
// is built, so the two surfaces can never drift apart. Empty violations
// render as the empty string.
func formatViolations(violations []violation) string {
	if len(violations) == 0 {
		return ""
	}
	parts := make([]string, len(violations))
	for i, v := range violations {
		parts[i] = fmt.Sprintf("%s (required by %s)", v.Missing, strings.Join(v.RequiredBy, ", "))
	}
	return "selection is not dependency-closed: missing " + strings.Join(parts, ", ")
}
