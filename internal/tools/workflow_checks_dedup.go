package tools

import (
	"context"
	"fmt"
	"strings"

	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
	"github.com/zeropsio/zcp/internal/workflow"
)

// checkCrossReadmeGotchaUniqueness — tool-layer thin wrapper (post-C-7d)
// around opschecks.CheckCrossReadmeGotchaUniqueness. The predicate body
// + hostname collection + pairwise comparison moved into the ops/checks
// package.
func checkCrossReadmeGotchaUniqueness(ctx context.Context, readmes map[string]string) []workflow.StepCheck {
	return opschecks.CheckCrossReadmeGotchaUniqueness(ctx, readmes)
}

// extractIntegrationGuideHeadings returns the H3 headings inside the
// integration-guide fragment, stripping leading numeric enumeration like
// "2. ". Used by checkGotchaRestatesGuide to correlate guide items with
// gotcha stems in the same README.
//
// Callers are responsible for filtering domain-specific boilerplate
// headings. The zerops.yaml heading is always present in every recipe
// README (the block is mandatory), so it is not a "code change the user
// must make" and should not count as an IG item for restatement
// purposes — the check itself filters it out before comparison.
func extractIntegrationGuideHeadings(ig string) []string {
	var headings []string
	for line := range strings.SplitSeq(ig, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "### ") {
			continue
		}
		h := strings.TrimPrefix(trimmed, "### ")
		// Strip enumeration "N. " prefix. Walk leading digits, require a
		// dot+space, and drop everything up to and including the space.
		digits := 0
		for digits < len(h) && h[digits] >= '0' && h[digits] <= '9' {
			digits++
		}
		if digits > 0 && digits+1 < len(h) && h[digits] == '.' && h[digits+1] == ' ' {
			h = h[digits+2:]
		}
		h = strings.TrimSpace(h)
		if h != "" {
			headings = append(headings, h)
		}
	}
	return headings
}

// checkGotchaRestatesGuide enforces that a gotcha bullet in the knowledge-
// base fragment must teach something the integration-guide does not. v15's
// appdev README had three gotchas ("Vite allowedHosts blocks Zerops
// subdomain", "VITE_API_URL undefined in dev mode", "Static deploy missing
// tilde suffix") whose normalized tokens were >= 67% identical to three
// integration-guide H3 headings immediately above. Restating a guide item
// as a gotcha doubles the publication surface area with no new content —
// readers see two bullets teaching the same fact in different tones.
//
// The rule: for each gotcha stem, if its normalized token set matches any
// integration-guide heading (excluding the boilerplate "zerops.yaml"
// section), the gotcha is a restatement and must be rewritten or deleted.
// Rewrites should focus on the failure symptom (error message, HTTP
// status, observable misbehavior) rather than the topic — a gotcha whose
// stem is "Blocked request HTTP 200 + blank browser" carries a distinct
// symptom from an IG item "Add .zerops.app to allowedHosts".
//
// Skipped when either fragment is empty or when the only IG heading is
// the zerops.yaml boilerplate.
func checkGotchaRestatesGuide(hostname, content string) []workflow.StepCheck {
	ig := extractFragmentContent(content, "integration-guide")
	kb := extractFragmentContent(content, "knowledge-base")
	if ig == "" || kb == "" {
		return nil
	}
	rawHeadings := extractIntegrationGuideHeadings(ig)
	type headingNorm struct {
		raw  string
		norm []string
	}
	var headings []headingNorm
	for _, h := range rawHeadings {
		lower := strings.ToLower(h)
		// Skip the boilerplate zerops.yaml block — every recipe has it,
		// it is not a "code change a user must make", so no gotcha is
		// a restatement of it. Matching on "zerops.yaml" anywhere in
		// the heading catches all common forms: "### 1. Adding
		// `zerops.yaml`", "### zerops.yaml", "### The `zerops.yaml` file".
		if strings.Contains(lower, "zerops.yaml") || strings.Contains(lower, "zerops yaml") {
			continue
		}
		norm := workflow.NormalizeStem(h)
		if len(norm) == 0 {
			continue
		}
		headings = append(headings, headingNorm{raw: h, norm: norm})
	}
	if len(headings) == 0 {
		return nil
	}

	stems := workflow.ExtractGotchaStems(kb)
	if len(stems) == 0 {
		return nil
	}

	var violations []string
	for _, stem := range stems {
		sNorm := workflow.NormalizeStem(stem)
		if len(sNorm) == 0 {
			continue
		}
		for _, h := range headings {
			if workflow.StemsMatch(sNorm, h.norm) {
				violations = append(violations, fmt.Sprintf("%q restates IG item %q", stem, h.raw))
				break
			}
		}
	}

	checkName := hostname + "_gotcha_distinct_from_guide"
	if len(violations) == 0 {
		return []workflow.StepCheck{{Name: checkName, Status: statusPass}}
	}
	return []workflow.StepCheck{{
		Name:   checkName,
		Status: statusFail,
		// No §18 shim for this check (kept-local per check-rewrite.md §17).
		// The PreAttestCmd slot is left empty; authors re-run via the
		// deploy.readmes substep after editing.
		Detail: fmt.Sprintf(
			"%s/README.md has gotchas that restate integration-guide items. Rewrite each to name the failure symptom (exact error message, HTTP status, observable misbehavior — 'Blocked request 200 + blank browser') instead of the topic; if the symptom is already in the integration-guide, delete the gotcha and use the slot for something the guide does NOT cover. Rewording changes the stem token set, which may flip cross_readme_gotcha_uniqueness — cross-check other codebases' knowledge-base stems before re-running. Violations: %s.",
			hostname, strings.Join(violations, "; "),
		),
	}}
}
