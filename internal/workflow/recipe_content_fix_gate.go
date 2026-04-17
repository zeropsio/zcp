package workflow

import (
	"regexp"
	"strings"
)

// v8.86 §3.3 — post-writer content-fix confirmation gate.
//
// Background: v8.81 introduced this gate as a dispatch gate — when the
// writer subagent shipped content that failed content checks, the gate
// forced the agent to dispatch a *second* content-fix subagent before
// retrying. v23 evidence (119-min run, 5 total content-writing rounds)
// showed that shape is anti-convergent: the writer never saw the check
// rules up front, so 58% of runs were forced into a multi-round fix
// loop (17 checks × ~95% per-check pass-rate ≈ 42% first-try clean).
//
// v8.86 inverts verification direction (see plan §2): writer briefs now
// include each active check's runnable validation command, so the writer
// self-verifies before returning. The gate becomes a confirmation-only
// backstop — it fires only when the writer subagent shipped content
// that still fails post-return, which now indicates a writer-brief bug
// (the brief lied about what would be checked) rather than a fixable
// workflow state. The gate's failure message names that explicitly and
// refuses to offer a dispatch-fix escape hatch.

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
	"_claude_md_no_burn_trap_folk",
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

// briefBugAckRe matches the explicit acknowledgment the operator writes
// when they've identified the writer-brief bug and need to retry
// end-to-end anyway (e.g. while a brief patch is in flight). Keeps the
// gate from becoming a permanent block in pathological edge cases.
var briefBugAckRe = regexp.MustCompile(`(?i)writer[\s\-]?brief[\s\-]?bug[\s\-]?acknowledged`)

// contentFixDispatchGate (name kept for API stability with recipeComplete
// callers) returns a non-nil failing StepCheckResult when the deploy step
// is being retried after prior content-check failures. The failure message
// frames this as a writer-brief bug — the writer subagent's brief should
// have included every check's runnable validation, and the writer should
// have iterated its self-validation loop until clean. A post-return fail
// means either (a) the writer skipped its validation, (b) the writer's
// validation disagreed with the gate-side check, or (c) a new check was
// added without updating the writer brief.
//
// Unlike the v8.81 shape, there is NO dispatch-fix escape hatch — the
// gate refuses to accept "dispatched content-fix subagent" attestations
// because that pattern papered over the real bug (the writer brief's
// gap). The only permissive acknowledgment is `writer-brief-bug
// acknowledged` for operator-controlled retry during brief patching.
func contentFixDispatchGate(rs *RecipeState, step, attestation string) *StepCheckResult {
	if rs == nil || rs.PriorStepCheckFails == nil {
		return nil
	}
	if step != RecipeStepDeploy {
		return nil
	}
	priorFails := rs.PriorStepCheckFails[step]
	if len(priorFails) == 0 {
		return nil
	}
	if briefBugAckRe.MatchString(attestation) {
		return nil
	}
	return &StepCheckResult{
		Passed: false,
		Checks: []StepCheck{{
			Name:   "writer_brief_bug",
			Status: "fail",
			Detail: buildWriterBriefBugDetail(priorFails),
		}},
		Summary: "writer-brief bug: writer subagent shipped content failing content checks",
	}
}

func buildWriterBriefBugDetail(priorFails []string) string {
	var sb strings.Builder
	sb.WriteString("WRITER BRIEF BUG: the writer sub-agent shipped content that failed ")
	sb.WriteString(int2str(len(priorFails)))
	sb.WriteString(" content check(s): ")
	sb.WriteString(strings.Join(priorFails, ", "))
	sb.WriteString(".\n\n")
	sb.WriteString("This should not happen — the writer brief is supposed to include every active content check's runnable validation command, and the writer is supposed to self-verify against each one (iterating until clean) BEFORE returning. A post-return fail means one of:\n\n")
	sb.WriteString("  (a) The writer sub-agent did not run its pre-return self-validation. Re-read the writer brief: the VALIDATION SECTION is mandatory.\n")
	sb.WriteString("  (b) The writer's validation reported pass but the gate-side check disagrees. This is a parity bug — file it by naming the failing check + the validation command the brief gave.\n")
	sb.WriteString("  (c) A new content check was added without updating the writer brief. This is a brief-completeness bug.\n\n")
	sb.WriteString("DO NOT dispatch a content-fix sub-agent (the v8.81 shape that caused v23's 5-round loop is removed). Fix the writer brief or the check-parity bug at the source. The deploy step will not advance until the writer sub-agent ships clean content.\n\n")
	sb.WriteString("If you are the operator patching the brief and need to retry end-to-end: include `writer-brief-bug acknowledged` in your attestation. Every such acknowledgment is logged against the run's workflow-discipline grade.\n")
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
