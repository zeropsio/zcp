package workflow

import (
	"context"
	"fmt"
	"strings"
)

// stepCheckStatusFail / stepCheckStatusPass are the StepCheck.Status
// values a failing / passing row carries. Mirror the unexported
// statusFail / statusPass in internal/tools; kept local here so
// workflow doesn't have to import tools to read its own validation
// rows.
const (
	stepCheckStatusFail = "fail"
	stepCheckStatusPass = "pass"
)

// validateEditorialReview is the SubStepValidator for
// close.editorial-review. It parses the attestation as an
// EditorialReviewReturn payload (verbatim JSON the reviewer returned),
// runs the seven §16a dispatch-runnable checks, and packs the results
// into the validation response. The validator lives under
// internal/workflow/ (not internal/tools/) because it must populate
// SubStepValidationResult.Checks — a field the engine reads at substep
// complete per C-7.5.
//
// Contract:
//   - Parse failure → one hard Issue naming the parse error + a
//     placeholder Checks slice of seven FAILs so the agent sees the
//     per-check surface even on a missing payload.
//   - Parse success + all-green → Passed=true, Checks populated with
//     seven passes, Issues empty.
//   - Parse success + any FAIL → Passed=false, Checks populated with
//     the mixed row set, Issues derived from the failing rows for
//     Phase C adaptive-retry context.
func validateEditorialReview(_ context.Context, _ *RecipePlan, _ *RecipeState, attestation string) *SubStepValidationResult {
	ret, parseErr := ParseEditorialReviewReturn(attestation)
	if parseErr != nil {
		// Even on parse failure we emit the 7-row battery so the retry
		// surface shows the full check list. Every row reads as the
		// same failure with the parse error in the detail prefix.
		checks := EditorialReviewChecks(nil)
		return &SubStepValidationResult{
			Passed:   false,
			Issues:   []string{fmt.Sprintf("editorial-review attestation parse: %v", parseErr)},
			Guidance: editorialReviewValidatorGuidance(parseErr.Error(), checks),
			Checks:   checks,
		}
	}
	checks := EditorialReviewChecks(ret)
	var failures []string
	for _, c := range checks {
		if c.Status == stepCheckStatusFail {
			failures = append(failures, fmt.Sprintf("%s: %s", c.Name, c.Detail))
		}
	}
	if len(failures) == 0 {
		return &SubStepValidationResult{
			Passed: true,
			Checks: checks,
		}
	}
	return &SubStepValidationResult{
		Passed:   false,
		Issues:   failures,
		Guidance: editorialReviewValidatorGuidance("", checks),
		Checks:   checks,
	}
}

// editorialReviewValidatorGuidance builds the prose the agent sees
// when the substep validator rejects the attestation. Lists each
// failing check with its detail, names the canonical payload shape
// the validator expects, and reminds the agent that the reviewer
// dispatch itself is the runnable form — you cannot re-run a shell
// shim to satisfy the check. parseErr is empty on predicate-level
// failures and non-empty on JSON-parse failures so the message
// distinguishes the two shapes.
func editorialReviewValidatorGuidance(parseErr string, checks []StepCheck) string {
	var b strings.Builder
	b.WriteString("## close.editorial-review substep validation\n\n")
	if parseErr != "" {
		b.WriteString("The attestation could not be parsed as an editorial-review return payload:\n\n")
		b.WriteString("> " + parseErr + "\n\n")
		b.WriteString("The substep expects the reviewer's return payload verbatim as JSON, per `briefs/editorial-review/completion-shape.md`. Re-dispatch the reviewer (or copy its return JSON from the Agent call output), then attest with that payload as the substep attestation value.\n\n")
	} else {
		b.WriteString("One or more §16a dispatch-runnable checks failed. Apply the fixes each check names, re-dispatch the reviewer if the inline-fix boundary requires it, and re-attest with the updated return payload.\n\n")
	}
	b.WriteString("### Failing checks\n\n")
	for _, c := range checks {
		if c.Status != stepCheckStatusFail {
			continue
		}
		b.WriteString("- **" + c.Name + "** — " + c.Detail + "\n")
	}
	b.WriteString("\n")
	b.WriteString("### Reminder\n\n")
	b.WriteString("These checks are NOT shell-runnable (" + EditorialReviewPreAttestNote + "). The dispatch IS the runnable form. Fix the deliverable, re-dispatch, and re-attest with the fresh return payload.\n")
	return b.String()
}
