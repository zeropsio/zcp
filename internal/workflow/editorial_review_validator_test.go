package workflow

import (
	"context"
	"strings"
	"testing"
)

// TestValidateEditorialReview_ParsesCleanPayloadAndPasses confirms the
// validator accepts the canonical minimum-viable return payload
// (surfaces walked, all counts zero) and populates the full 7-row
// Checks battery on pass.
func TestValidateEditorialReview_ParsesCleanPayloadAndPasses(t *testing.T) {
	t.Parallel()
	result := validateEditorialReview(context.Background(), nil, nil, validEditorialReviewPayload())
	if result == nil {
		t.Fatal("validator returned nil")
	}
	if !result.Passed {
		t.Fatalf("expected pass; got issues=%v", result.Issues)
	}
	if len(result.Checks) != 7 {
		t.Fatalf("expected 7 rows; got %d", len(result.Checks))
	}
	for _, c := range result.Checks {
		if c.Status != "pass" {
			t.Errorf("row %q must pass; got %s (%s)", c.Name, c.Status, c.Detail)
		}
	}
}

// TestValidateEditorialReview_EmptyAttestationFailsWithParseError fails
// the substep when no JSON was attached — every row emits FAIL, the
// guidance names the parse error, and Issues carries the parse error
// for Phase C adaptive-retry.
func TestValidateEditorialReview_EmptyAttestationFailsWithParseError(t *testing.T) {
	t.Parallel()
	result := validateEditorialReview(context.Background(), nil, nil, "")
	if result == nil {
		t.Fatal("validator returned nil")
	}
	if result.Passed {
		t.Fatal("expected fail on empty attestation; got pass")
	}
	if len(result.Checks) != 7 {
		t.Fatalf("expected 7 rows on parse failure; got %d", len(result.Checks))
	}
	for _, c := range result.Checks {
		if c.Status != "fail" {
			t.Errorf("row %q must fail on parse error; got %s", c.Name, c.Status)
		}
	}
	if len(result.Issues) == 0 || !strings.Contains(result.Issues[0], "parse") {
		t.Fatalf("expected parse issue; got %v", result.Issues)
	}
	if !strings.Contains(result.Guidance, "could not be parsed") {
		t.Errorf("guidance missing parse-error prose; got: %s", result.Guidance)
	}
}

// TestValidateEditorialReview_PredicateFailurePropagates shows a
// CRIT-bearing payload produces a mixed 7-row battery (pass + fail),
// Issues carries the FAILs' details, and the validator reports
// Passed=false so the engine records the failure pattern.
func TestValidateEditorialReview_PredicateFailurePropagates(t *testing.T) {
	t.Parallel()
	ret := EditorialReviewReturn{
		SurfacesWalked: []string{"/var/www/README.md"},
	}
	ret.FindingsBySeverity.Crit = 2
	payload := mustMarshalEditorialReviewReturn(t, ret)

	result := validateEditorialReview(context.Background(), nil, nil, payload)
	if result.Passed {
		t.Fatal("expected fail on CRIT>0 payload; got pass")
	}
	if len(result.Checks) != 7 {
		t.Fatalf("expected 7 rows; got %d", len(result.Checks))
	}
	var gotFailNames []string
	for _, c := range result.Checks {
		if c.Status == "fail" {
			gotFailNames = append(gotFailNames, c.Name)
		}
	}
	if len(gotFailNames) != 1 || gotFailNames[0] != "editorial_review_no_wrong_surface_crit" {
		t.Errorf("expected only editorial_review_no_wrong_surface_crit to fail; got %v", gotFailNames)
	}
	// Issues must include the failing row's detail for Phase C retry context.
	if len(result.Issues) == 0 || !strings.Contains(strings.Join(result.Issues, "; "), "CRIT finding(s)") {
		t.Errorf("issues must reference the CRIT failure detail; got %v", result.Issues)
	}
	// Guidance lists only the failing check, not all seven.
	if !strings.Contains(result.Guidance, "editorial_review_no_wrong_surface_crit") {
		t.Errorf("guidance must name the failing check; got: %s", result.Guidance)
	}
	if strings.Contains(result.Guidance, "editorial_review_citation_coverage") && !strings.Contains(result.Guidance, "matching-topic gotcha") {
		// Passing checks should NOT appear under Failing checks heading.
		t.Errorf("guidance must not list passing checks under Failing section; got: %s", result.Guidance)
	}
}

// TestValidateEditorialReview_GuidanceAlwaysNamesPreAttestNote confirms
// the reminder "dispatched via close.editorial-review (no author-side
// equivalent; reviewer IS the runner)" appears in every failure
// guidance so the agent doesn't look for a shell-runnable fix.
func TestValidateEditorialReview_GuidanceAlwaysNamesPreAttestNote(t *testing.T) {
	t.Parallel()
	result := validateEditorialReview(context.Background(), nil, nil, "")
	if !strings.Contains(result.Guidance, EditorialReviewPreAttestNote) {
		t.Errorf("guidance must include pre-attest note; got: %s", result.Guidance)
	}
}
