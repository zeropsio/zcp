package workflow

import (
	"fmt"
	"slices"
	"strings"
)

// Seven dispatch-runnable checks for close.editorial-review complete,
// per check-rewrite.md §16a. Each predicate consumes the already-parsed
// EditorialReviewReturn payload and emits one StepCheck row (pass or
// fail). The row set is composed by EditorialReviewChecks below and
// surfaced through the substep validator's SubStepValidationResult.Checks.
//
// Unlike §16 author-runnable checks, these have no shell-shim equivalent
// — editorial quality cannot be graded by regex or byte diff; the
// reviewer sub-agent dispatch IS the runner. Every check's Detail
// includes the EditorialReviewPreAttestNote so downstream consumers can
// distinguish §16a rows from §16 shell-runnable rows without parsing
// the check name.

// EditorialReviewPreAttestNote is the exported marker string every
// §16a check emits in its Detail field where §16 checks emit a
// runnable command. Exported so the substep validator's aggregate
// guidance message can include it consistently.
const EditorialReviewPreAttestNote = "dispatched via close.editorial-review (no author-side equivalent; reviewer IS the runner)"

// editorialReviewWrongCountCeiling is the v35 bar for WRONG findings
// surviving inline-fix. One WRONG may remain when the inline-fix
// boundary requires author judgment; more than one means multiple
// editorial defects shipped past the reviewer.
const editorialReviewWrongCountCeiling = 1

// editorialReviewFabricatedTags are subclass tags the reviewer attaches
// to a per-surface finding when the root cause is a fabricated platform
// mechanism (v23 execOnce-burn, v28 folk-doctrine). Description text
// is scanned as a fallback when the tag list is absent.
var editorialReviewFabricatedTags = []string{
	"fabricated",
	"fabricated-mechanism",
	"folk-doctrine",
	"invented-mechanism",
}

// EditorialReviewChecks runs every §16a predicate against the parsed
// return payload and returns the aggregated rows in a stable order.
// `dispatched` is always first so a missing/unparseable payload
// surfaces at the top of the check list; the six detail checks follow
// in §16a table order.
func EditorialReviewChecks(ret *EditorialReviewReturn) []StepCheck {
	return []StepCheck{
		checkEditorialReviewDispatched(ret),
		checkEditorialReviewNoWrongSurfaceCRIT(ret),
		checkEditorialReviewReclassificationDelta(ret),
		checkEditorialReviewNoFabricatedMechanism(ret),
		checkEditorialReviewCitationCoverage(ret),
		checkEditorialReviewCrossSurfaceDuplication(ret),
		checkEditorialReviewWrongCount(ret),
	}
}

func checkEditorialReviewDispatched(ret *EditorialReviewReturn) StepCheck {
	if ret == nil || len(ret.SurfacesWalked) == 0 {
		return StepCheck{
			Name:   "editorial_review_dispatched",
			Status: stepCheckStatusFail,
			Detail: "editorial-review return payload missing or surfaces_walked is empty — dispatch the reviewer with the composed brief before attesting this substep. " + EditorialReviewPreAttestNote,
		}
	}
	return StepCheck{
		Name:   "editorial_review_dispatched",
		Status: stepCheckStatusPass,
		Detail: fmt.Sprintf("%d surface(s) walked", len(ret.SurfacesWalked)),
	}
}

func checkEditorialReviewNoWrongSurfaceCRIT(ret *EditorialReviewReturn) StepCheck {
	if ret == nil {
		return StepCheck{
			Name: "editorial_review_no_wrong_surface_crit", Status: stepCheckStatusFail,
			Detail: "editorial-review return payload missing — cannot grade CRIT count. " + EditorialReviewPreAttestNote,
		}
	}
	if ret.FindingsBySeverity.Crit == 0 {
		return StepCheck{
			Name: "editorial_review_no_wrong_surface_crit", Status: stepCheckStatusPass,
		}
	}
	return StepCheck{
		Name:   "editorial_review_no_wrong_surface_crit",
		Status: stepCheckStatusFail,
		Detail: fmt.Sprintf("%d CRIT finding(s) survived inline-fix; every CRIT must be fixed in-mount before attesting. %s", ret.FindingsBySeverity.Crit, EditorialReviewPreAttestNote),
	}
}

func checkEditorialReviewReclassificationDelta(ret *EditorialReviewReturn) StepCheck {
	if ret == nil {
		return StepCheck{
			Name: "editorial_review_reclassification_delta", Status: stepCheckStatusFail,
			Detail: "editorial-review return payload missing — cannot grade reclassification delta. " + EditorialReviewPreAttestNote,
		}
	}
	var unresolved []string
	for _, row := range ret.ReclassificationDeltaTable {
		if row.WriterSaid == row.ReviewerSaid {
			continue
		}
		if row.Final == row.ReviewerSaid {
			continue
		}
		unresolved = append(unresolved, fmt.Sprintf("%s (writer=%s reviewer=%s final=%s)", row.ItemPath, row.WriterSaid, row.ReviewerSaid, row.Final))
	}
	if len(unresolved) == 0 {
		return StepCheck{
			Name: "editorial_review_reclassification_delta", Status: stepCheckStatusPass,
		}
	}
	return StepCheck{
		Name:   "editorial_review_reclassification_delta",
		Status: stepCheckStatusFail,
		Detail: fmt.Sprintf("%d reclassification disagreement(s) unresolved: %s. %s", len(unresolved), strings.Join(unresolved, "; "), EditorialReviewPreAttestNote),
	}
}

func checkEditorialReviewNoFabricatedMechanism(ret *EditorialReviewReturn) StepCheck {
	if ret == nil {
		return StepCheck{
			Name: "editorial_review_no_fabricated_mechanism", Status: stepCheckStatusFail,
			Detail: "editorial-review return payload missing — cannot grade fabricated-mechanism count. " + EditorialReviewPreAttestNote,
		}
	}
	var offenders []string
	for _, f := range ret.PerSurfaceFindings {
		if !strings.EqualFold(f.Severity, "CRIT") {
			continue
		}
		if !findingFlagsFabricatedMechanism(f) {
			continue
		}
		offenders = append(offenders, fmt.Sprintf("%s: %s", f.SurfacePath, f.Description))
	}
	if len(offenders) == 0 {
		return StepCheck{
			Name: "editorial_review_no_fabricated_mechanism", Status: stepCheckStatusPass,
		}
	}
	return StepCheck{
		Name:   "editorial_review_no_fabricated_mechanism",
		Status: stepCheckStatusFail,
		Detail: fmt.Sprintf("%d fabricated-mechanism CRIT finding(s): %s. %s", len(offenders), strings.Join(offenders, "; "), EditorialReviewPreAttestNote),
	}
}

// findingFlagsFabricatedMechanism matches either the finding's tag list
// (case-insensitive equality) OR the description text (case-insensitive
// substring) against editorialReviewFabricatedTags — the reviewer may
// report either shape.
func findingFlagsFabricatedMechanism(f EditorialReviewFinding) bool {
	for _, tag := range f.Tags {
		low := strings.ToLower(tag)
		if slices.Contains(editorialReviewFabricatedTags, low) {
			return true
		}
	}
	descLower := strings.ToLower(f.Description)
	for _, needle := range editorialReviewFabricatedTags {
		if strings.Contains(descLower, needle) {
			return true
		}
	}
	return false
}

func checkEditorialReviewCitationCoverage(ret *EditorialReviewReturn) StepCheck {
	if ret == nil {
		return StepCheck{
			Name: "editorial_review_citation_coverage", Status: stepCheckStatusFail,
			Detail: "editorial-review return payload missing — cannot grade citation coverage. " + EditorialReviewPreAttestNote,
		}
	}
	cc := ret.CitationCoverage
	if cc.Denominator == 0 {
		return StepCheck{
			Name: "editorial_review_citation_coverage", Status: stepCheckStatusPass,
			Detail: "no matching-topic gotchas to cover (denominator=0)",
		}
	}
	uncited := cc.Denominator - cc.Numerator
	if uncited <= 0 {
		return StepCheck{
			Name:   "editorial_review_citation_coverage",
			Status: stepCheckStatusPass,
			Detail: fmt.Sprintf("%d of %d matching-topic gotchas cited (%.0f%%)", cc.Numerator, cc.Denominator, cc.Percent),
		}
	}
	return StepCheck{
		Name:   "editorial_review_citation_coverage",
		Status: stepCheckStatusFail,
		Detail: fmt.Sprintf("%d matching-topic gotcha(s) lack a zerops_knowledge citation (%d of %d cited). %s", uncited, cc.Numerator, cc.Denominator, EditorialReviewPreAttestNote),
	}
}

func checkEditorialReviewCrossSurfaceDuplication(ret *EditorialReviewReturn) StepCheck {
	if ret == nil {
		return StepCheck{
			Name: "editorial_review_cross_surface_duplication", Status: stepCheckStatusFail,
			Detail: "editorial-review return payload missing — cannot grade cross-surface ledger. " + EditorialReviewPreAttestNote,
		}
	}
	var duplicates []string
	for _, row := range ret.CrossSurfaceLedger {
		if strings.EqualFold(row.Severity, "duplicate") {
			duplicates = append(duplicates, fmt.Sprintf("%s across [%s]", row.Fact, strings.Join(row.Surfaces, ", ")))
		}
	}
	if len(duplicates) == 0 {
		return StepCheck{
			Name: "editorial_review_cross_surface_duplication", Status: stepCheckStatusPass,
		}
	}
	return StepCheck{
		Name:   "editorial_review_cross_surface_duplication",
		Status: stepCheckStatusFail,
		Detail: fmt.Sprintf("%d cross-surface duplicate(s): %s. %s", len(duplicates), strings.Join(duplicates, "; "), EditorialReviewPreAttestNote),
	}
}

func checkEditorialReviewWrongCount(ret *EditorialReviewReturn) StepCheck {
	if ret == nil {
		return StepCheck{
			Name: "editorial_review_wrong_count", Status: stepCheckStatusFail,
			Detail: "editorial-review return payload missing — cannot grade WRONG count. " + EditorialReviewPreAttestNote,
		}
	}
	if ret.FindingsBySeverity.Wrong <= editorialReviewWrongCountCeiling {
		return StepCheck{
			Name:   "editorial_review_wrong_count",
			Status: stepCheckStatusPass,
			Detail: fmt.Sprintf("%d WRONG (ceiling %d)", ret.FindingsBySeverity.Wrong, editorialReviewWrongCountCeiling),
		}
	}
	return StepCheck{
		Name:   "editorial_review_wrong_count",
		Status: stepCheckStatusFail,
		Detail: fmt.Sprintf("%d WRONG finding(s) survived inline-fix; ceiling is %d — reduce below ceiling before attesting (inline-fix what you can; escalate the rest with user-facing disposition). %s", ret.FindingsBySeverity.Wrong, editorialReviewWrongCountCeiling, EditorialReviewPreAttestNote),
	}
}
