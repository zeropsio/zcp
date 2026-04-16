package tools

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// gotchaFloorByRole maps codebase role to its minimum gotcha count.
// Rationale: v7 gold (6/5/4) was the highest sustained baseline; v20
// peak (7/6/6) was aspirational. We undershoot v7 by 1 across the
// board to leave headroom for narrow-surface recipes while still
// creating upward pressure that the quality checks (causal-anchor,
// content-reality) alone did not supply. v21's content compressed 26%
// precisely because there was no floor; this check is the void filler.
var gotchaFloorByRole = map[string]int{
	"api":       5, // typically covers 5 managed-service categories
	"frontend":  3, // fewer failure modes — framework + platform-static
	"worker":    4, // queue-group + drain + migration-ownership + entity-parity
	"fullstack": 5, // single-codebase full-stack behaves like api
}

// checkGotchaDepthFloor enforces a per-role minimum gotcha count on a
// codebase's knowledge-base fragment. Returns a single event named
// `{hostname}_gotcha_depth_floor`. Unknown role → skip (caller
// classifies; we don't guess). Empty content → skip.
//
// The check complements the v8.78 quality checks instead of replacing
// them: causal-anchor + content-reality apply per-gotcha (downward
// pressure, "every gotcha must be real and load-bearing"); this floor
// applies per-codebase (upward pressure, "at least N gotchas
// required"). A recipe that lands exactly at the floor passed with
// the minimum; the intent is "no compression under the floor", not
// "hit the floor exactly".
func checkGotchaDepthFloor(kbContent, role, hostname string) []workflow.StepCheck {
	checkName := hostname + "_gotcha_depth_floor"
	if kbContent == "" {
		return nil
	}
	floor, ok := gotchaFloorByRole[role]
	if !ok {
		return nil
	}
	count := countGotchaBullets(kbContent)
	if count >= floor {
		return []workflow.StepCheck{{Name: checkName, Status: statusPass}}
	}
	return []workflow.StepCheck{{
		Name:   checkName,
		Status: statusFail,
		Detail: fmt.Sprintf(
			"%s knowledge-base has %d gotcha(s); role %q expects at least %d. Under-minimum counts signal compression-to-pass — the causal-anchor and content-reality checks create downward pressure; this floor creates upward pressure. Do NOT add decorative gotchas; narrate real ones from debug experience on THIS build. If the recipe truly has fewer failure modes than the floor (e.g. narrow single-service plan), name the exception in the intro fragment with a concrete reason.",
			hostname, count, role, floor,
		),
	}}
}

// countGotchaBullets counts top-level `- **` bullets inside a
// knowledge-base fragment. Matches the cross-readme uniqueness
// check's extraction shape so two checks can't disagree on "what is
// a gotcha".
func countGotchaBullets(kbContent string) int {
	n := 0
	for line := range strings.SplitSeq(kbContent, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- **") {
			n++
		}
	}
	return n
}
