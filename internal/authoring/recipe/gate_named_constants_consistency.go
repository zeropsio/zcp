package recipe

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Run-40 A1 — named-constants consistency gate.
//
// plan.NamedConstants is the canonical store for cross-codebase
// string constants (queue group names, cache prefixes, signing-key
// aliases). Run-39 surfaced the failure mode: source code used
// `'workers'` as the NATS queue group, but tier yaml import.yaml
// comments cited `'showcase-workers'`. The agent had no
// single-source-of-truth to read; the env-content composer surfaced
// the fact stream which carried both names under different topic
// strings (closed separately by ENG-4's latest-by-canonical-topic).
//
// gateNamedConstantsConsistency runs at env-content complete-phase.
// It scans tier yaml import.yaml comments (plan.EnvComments[].Service[*]
// + .Project) for each KEY in plan.NamedConstants and refuses close
// when a comment cites a backtick-quoted value that contradicts the
// canonical entry. The agent's recovery is to re-author the affected
// import-comment fragment via `record-fragment mode=replace` with
// the canonical value, or to update plan.NamedConstants if the
// canonical entry was wrong.
//
// Conservative by design: only refuses when a comment cites a
// non-canonical value AS the named constant. Free prose mentioning
// `'showcase-workers'` in a different context (historical note,
// alternative suggestion) doesn't trip the gate — the regex anchors
// on the named-constant KEY's textual proximity.
//
// Diagnosed in plans/run-40-evidence-grounded-plan.md §"S1-1" + §"A1".

// gateNamedConstantsConsistency emits a blocking violation for each
// tier-comment pairing where the comment cites a backtick-quoted
// value alongside a named-constant KEY but the cited value isn't
// the canonical one in plan.NamedConstants.
func gateNamedConstantsConsistency(ctx GateContext) []Violation {
	if ctx.Plan == nil || len(ctx.Plan.NamedConstants) == 0 {
		return nil
	}
	if len(ctx.Plan.EnvComments) == 0 {
		return nil
	}
	keys := sortedConstantKeys(ctx.Plan.NamedConstants)
	var out []Violation
	for tierKey, ec := range ctx.Plan.EnvComments {
		if ec.Project != "" {
			out = append(out, scanCommentForStaleConstants(ec.Project, ctx.Plan.NamedConstants, keys, tierKey, "project")...)
		}
		for host, body := range ec.Service {
			if body == "" {
				continue
			}
			out = append(out, scanCommentForStaleConstants(body, ctx.Plan.NamedConstants, keys, tierKey, host)...)
		}
	}
	return out
}

// scanCommentForStaleConstants looks for `KEY` mentions and adjacent
// backtick-quoted values; reports each pairing whose value doesn't
// match plan.NamedConstants[KEY]. Returns one violation per stale
// citation so the agent sees every drift in a single round-trip.
func scanCommentForStaleConstants(body string, canonical map[string]string, keys []string, tierKey, target string) []Violation {
	var out []Violation
	for _, key := range keys {
		canonValue := canonical[key]
		if canonValue == "" {
			continue
		}
		stale := staleConstantCitations(body, key, canonValue)
		for _, cite := range stale {
			out = append(out, Violation{
				Code:     "named-constant-stale-citation",
				Path:     fmt.Sprintf("env/%s/import-comments/%s", tierKey, target),
				Severity: SeverityBlocking,
				Message: fmt.Sprintf(
					"tier %s import-comment for `%s` cites named constant %q with stale value %q; plan.NamedConstants[%q] = %q. Either re-author the import-comment via `record-fragment mode=replace` to use the canonical value, or update plan.NamedConstants if the canonical entry was wrong.",
					tierKey, target, key, cite, key, canonValue),
			})
		}
	}
	return out
}

// staleConstantCitations returns the set of backtick-quoted values
// the body cites alongside the named-constant key, EXCLUDING the
// canonical value. Anchors on KEY-proximate citations to avoid
// flagging prose mentions of the same string in unrelated context.
//
// Heuristic: a citation is "near" the KEY when the backticked token
// appears within 120 characters of the KEY mention. Run-39's
// "queue group `showcase-workers`" pattern is well within this
// window; the bound is generous enough for multi-sentence prose.
func staleConstantCitations(body, key, canonValue string) []string {
	if !strings.Contains(body, key) {
		return nil
	}
	const proximityWindow = 120
	stale := map[string]struct{}{}
	for _, idx := range allIndexes(body, key) {
		start := max(idx-proximityWindow, 0)
		end := min(idx+len(key)+proximityWindow, len(body))
		window := body[start:end]
		for _, m := range backtickQuotedValueRe.FindAllStringSubmatch(window, -1) {
			if len(m) < 2 {
				continue
			}
			val := m[1]
			if val == "" || val == canonValue || val == key {
				continue
			}
			stale[val] = struct{}{}
		}
	}
	if len(stale) == 0 {
		return nil
	}
	out := make([]string, 0, len(stale))
	for v := range stale {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// backtickQuotedValueRe matches a backtick-quoted token whose content
// looks like a value literal — alphanumerics, hyphens, dots,
// underscores, slashes. Tokens with whitespace are filtered out
// because they're typically prose (e.g. "queue group `workers`" — we
// match `workers`, not the heading). Citation form in run-39's tier
// yamls: "queue group `showcase-workers`".
var backtickQuotedValueRe = regexp.MustCompile("`([A-Za-z0-9._/-]+)`")

// allIndexes returns the start offsets of every occurrence of sub
// in s. Used by the proximity-window scan in staleConstantCitations.
func allIndexes(s, sub string) []int {
	if sub == "" {
		return nil
	}
	var out []int
	for offset := 0; ; {
		i := strings.Index(s[offset:], sub)
		if i < 0 {
			return out
		}
		out = append(out, offset+i)
		offset += i + len(sub)
	}
}

// sortedConstantKeys returns plan.NamedConstants keys in stable
// order so the gate produces deterministic violation output across
// runs.
func sortedConstantKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
