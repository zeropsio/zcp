package tools

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// checkClaudeMdNoBurnTrapFolk flags CLAUDE.md / README prose that uses
// the term "burn trap" (or "burned the lock", etc.) in proximity to
// `execOnce`. That phrasing is fictional folk-doctrine: the Zerops
// platform has no "burn" concept — `zsc execOnce ${appVersionId}` keys
// on the deploy version, and every new deploy gets a fresh appVersionId
// so the lock is NEVER pre-burned by a prior deploy. v23 shipped
// apidev/CLAUDE.md with a "burn trap" gotcha narrating this incorrectly;
// when that CLAUDE.md propagates to downstream agents, they inherit the
// incorrect mental model.
//
// Detection is proximity-based: if the lowercased content contains both
// `execOnce` and `burn` tokens within proximityWindow characters of
// each other, the check fails. Distant co-occurrences (e.g. CI fuel
// burned in one paragraph, execOnce discussed ten paragraphs later)
// don't trigger — those are unrelated.
//
// See §3.6a of the v8.86 implementation plan + the execOnce-semantics
// eager topic for the correct mental model.
const burnTrapProximityWindow = 100

var (
	execOnceRe = regexp.MustCompile(`(?i)execonce`)
	burnRe     = regexp.MustCompile(`(?i)\bburn(ed|s|-?trap)?\b`)
)

func checkClaudeMdNoBurnTrapFolk(content, hostname string) []workflow.StepCheck {
	name := hostname + "_claude_md_no_burn_trap_folk"
	if strings.TrimSpace(content) == "" {
		return nil
	}
	execMatches := execOnceRe.FindAllStringIndex(content, -1)
	burnMatches := burnRe.FindAllStringIndex(content, -1)
	if len(execMatches) == 0 || len(burnMatches) == 0 {
		return []workflow.StepCheck{{Name: name, Status: statusPass}}
	}

	var hits []string
	for _, em := range execMatches {
		for _, bm := range burnMatches {
			dist := absInt(em[0] - bm[0])
			if dist <= burnTrapProximityWindow {
				start := min(bm[0], em[0])
				end := max(bm[1], em[1])
				hits = append(hits, strings.TrimSpace(content[start:end]))
				break
			}
		}
		if len(hits) >= 3 {
			break
		}
	}
	if len(hits) == 0 {
		return []workflow.StepCheck{{Name: name, Status: statusPass}}
	}
	return []workflow.StepCheck{{
		Name:   name,
		Status: statusFail,
		Detail: fmt.Sprintf(
			"CLAUDE.md/README uses folk-doctrine phrasing near execOnce: %q. The term 'burn trap' is fictional — `zsc execOnce ${appVersionId}` keys on the deploy version, so a fresh appVersionId makes the lock unreachable by prior-deploy state. If your first-deploy initCommand silently no-ops, the cause is in your script (early exit, ts-node module resolution, stdout buffering) — NOT a burned key. Rewrite the gotcha to name the real mechanism (see topic=\"execOnce-semantics\").",
			strings.Join(hits, "; "),
		),
	}}
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
