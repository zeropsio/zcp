package workflow

import (
	"regexp"
	"strings"
)

// v8.81 §4.1 — post-writer content-fix dispatch gate.
//
// Context (from the v22 post-mortem): the v8.80 writer-subagent dispatch
// gate forces the README writer to fire at the `readmes` sub-step. But
// full-step content checks (content_reality, gotcha_causal_anchor,
// gotcha_distinct_from_guide, claude_readme_consistency, scaffold_hygiene,
// service_coverage, ig_per_item_standalone, cross_readme_gotcha_uniqueness)
// fire AFTER the sub-step completes, at `complete step=deploy` time. When
// any of these fail, the step stays in progress and the agent iterates.
//
// v22's agent absorbed the iteration into main context: 11 Edits on
// workerdev/README.md + 8 on apidev/README.md + 5 on workerdev/CLAUDE.md
// in a Read-once / Edit-many pattern spanning 16 minutes. That cost was
// the single largest driver of the v22 wall-clock regression.
//
// This gate ensures the retry of `complete step=deploy` references a
// content-fix sub-agent dispatch. The agent fetches the
// `content-fix-subagent-brief` topic, dispatches via the Agent tool, and
// only then retries the step. The sub-agent absorbs the content edits
// that would otherwise bloat main context.
//
// The gate is deliberately lenient on WHICH checks trigger it — any of
// the named content-check families counts. It's strict on the retry
// shape: the attestation must name the dispatch.

// contentCheckFailPrefixes identifies v8.78/v8.79/v8.80 content-quality
// check families. When any check whose name starts with one of these
// prefixes (or whose name is one of the hostnameless ones) fails, the
// agent must dispatch a content-fix sub-agent on retry instead of
// iterating inline.
var contentCheckFailNames = map[string]bool{
	"cross_readme_gotcha_uniqueness":     true,
	"recipe_architecture_narrative":      true,
	"knowledge_base_exceeds_predecessor": true,
}

var contentCheckFailSuffixes = []string{
	"_content_reality",
	"_gotcha_causal_anchor",
	"_gotcha_distinct_from_guide",
	"_claude_readme_consistency",
	"_scaffold_hygiene",
	"_service_coverage",
	"_ig_per_item_standalone",
	"_knowledge_base_authenticity",
}

// isContentCheck returns true when the check name belongs to a content-
// quality family that should trigger the content-fix dispatch gate on
// retry.
func isContentCheck(name string) bool {
	if contentCheckFailNames[name] {
		return true
	}
	for _, suffix := range contentCheckFailSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// contentFixAttestationRe matches attestation lines that reference a
// content-fix sub-agent dispatch. Intentionally permissive — the gate's
// purpose is friction against silent in-main iteration, not exact-phrasing
// enforcement. Accepted shapes:
//
//   - "content-fix sub-agent" / "content-fix subagent" / "content fix agent"
//   - "fix sub-agent" dispatched for content / readmes / gotchas
//   - "dispatched ... to fix ... README" / "dispatched ... to fix ... content"
//   - "inline-fix acknowledged" (explicit deviation marker, for edge cases
//     where dispatch is genuinely impossible)
var contentFixAttestationRe = regexp.MustCompile(`(?i)(content[\s\-]?fix[\s\-]?(sub[\s\-]?)?agent|fix[\s\-]?sub[\s\-]?agent.*?(readme|content|gotcha)|dispatch.*?fix.*?(readme|content|gotcha)|inline[\s\-]?fix[\s\-]?acknowledged)`)

// contentFixDispatchGate returns a non-nil failing StepCheckResult when
// the step is being retried after prior content-check failures AND the
// attestation does not reference a content-fix dispatch. Returns nil
// (gate passes) when no prior fails exist, the step isn't in the
// gate-enabled set, or the attestation satisfies the dispatch rule.
func contentFixDispatchGate(rs *RecipeState, step, attestation string) *StepCheckResult {
	if rs == nil || rs.PriorStepCheckFails == nil {
		return nil
	}
	// Only the deploy step carries the content-check battery that matters
	// for this gate. Extending to other steps would require they also
	// have content-flavored step checkers.
	if step != RecipeStepDeploy {
		return nil
	}
	priorFails := rs.PriorStepCheckFails[step]
	if len(priorFails) == 0 {
		return nil
	}
	if contentFixAttestationRe.MatchString(attestation) {
		return nil
	}
	return &StepCheckResult{
		Passed: false,
		Checks: []StepCheck{{
			Name:   "content_fix_dispatch_required",
			Status: "fail",
			Detail: buildContentFixGateDetail(priorFails),
		}},
		Summary: "content-fix sub-agent dispatch required on retry",
	}
}

func buildContentFixGateDetail(priorFails []string) string {
	var sb strings.Builder
	sb.WriteString("The previous `complete step=deploy` attempt failed on ")
	sb.WriteString(int2str(len(priorFails)))
	sb.WriteString(" content-quality check(s): ")
	sb.WriteString(strings.Join(priorFails, ", "))
	sb.WriteString(". On the v22 showcase run, this exact pattern triggered 11 in-main Edits on workerdev/README.md + 8 on apidev/README.md + 5 on workerdev/CLAUDE.md — ~15 minutes of wall-clock spent iterating content inside main context.\n\n")
	sb.WriteString("Instead, dispatch a content-fix sub-agent:\n\n")
	sb.WriteString("1. Fetch the brief: `zerops_guidance topic=\"content-fix-subagent-brief\"`.\n")
	sb.WriteString("2. Dispatch via the Agent tool with a description that matches `fix README content-check failures` (or similar). Include the exact list of failing checks in the prompt so the sub-agent can target them.\n")
	sb.WriteString("3. After the sub-agent returns, retry `complete step=deploy` with an attestation that references the dispatch — for example:\n")
	sb.WriteString("   \"Dispatched content-fix sub-agent for workerdev_content_reality + workerdev_gotcha_causal_anchor fails; sub-agent rewrote workerdev/README.md + CLAUDE.md gotchas to load-bearing shape.\"\n\n")
	sb.WriteString("If you have a principled reason to inline (e.g. a single trivial fix, blocked on sub-agent dispatch), include `inline-fix acknowledged` in the attestation to pass this gate explicitly. Every such deviation is recorded against the run's workflow-discipline grade.\n")
	return sb.String()
}

// recordContentCheckFails walks the checker result's Checks list and
// appends the names of content-check-flavored failures to the state's
// per-step record. Idempotent: duplicates are filtered.
func recordContentCheckFails(rs *RecipeState, step string, result *StepCheckResult) {
	if rs == nil || result == nil {
		return
	}
	if rs.PriorStepCheckFails == nil {
		rs.PriorStepCheckFails = map[string][]string{}
	}
	existing := map[string]bool{}
	for _, n := range rs.PriorStepCheckFails[step] {
		existing[n] = true
	}
	for _, c := range result.Checks {
		if c.Status != "fail" {
			continue
		}
		if !isContentCheck(c.Name) {
			continue
		}
		if existing[c.Name] {
			continue
		}
		existing[c.Name] = true
		rs.PriorStepCheckFails[step] = append(rs.PriorStepCheckFails[step], c.Name)
	}
}

// int2str is a tiny non-fmt int→string for small counts, avoiding fmt
// import bloat in a helper file. Handles 0–99 which covers every
// realistic content-check fail list.
func int2str(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
