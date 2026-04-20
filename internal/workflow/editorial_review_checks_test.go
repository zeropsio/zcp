package workflow

import (
	"strings"
	"testing"
)

// TestEditorialReviewChecks_TableDriven exercises each §16a predicate
// against a panel of payload shapes — one row per (predicate × pass/fail)
// leaf. Each row asserts the check's status + a substring of its Detail
// that pins the failure mode. The full battery is additionally
// re-verified under EditorialReviewChecks to confirm the 7-row order
// is stable.
func TestEditorialReviewChecks_TableDriven(t *testing.T) {
	t.Parallel()

	baseline := func() EditorialReviewReturn {
		return EditorialReviewReturn{
			SurfacesWalked: []string{"/var/www/README.md"},
			CitationCoverage: CitationCoverage{
				Numerator:   0,
				Denominator: 0,
			},
		}
	}
	withCrit := func(n int) EditorialReviewReturn {
		r := baseline()
		r.FindingsBySeverity.Crit = n
		return r
	}
	withWrong := func(n int) EditorialReviewReturn {
		r := baseline()
		r.FindingsBySeverity.Wrong = n
		return r
	}
	withReclassMismatch := func() EditorialReviewReturn {
		r := baseline()
		r.ReclassificationDeltaTable = []ReclassificationDeltaRow{
			{ItemPath: "apidev/README.md#g1", WriterSaid: "platform-trap", ReviewerSaid: "scaffold-decision", Final: "platform-trap"},
		}
		return r
	}
	withFabricatedCrit := func() EditorialReviewReturn {
		r := baseline()
		r.FindingsBySeverity.Crit = 1
		r.PerSurfaceFindings = []EditorialReviewFinding{
			{SurfacePath: "apidev/README.md#g1", Severity: "CRIT", Description: "execOnce burn", Tags: []string{"fabricated"}},
		}
		return r
	}
	withUncitedCoverage := func() EditorialReviewReturn {
		r := baseline()
		r.CitationCoverage = CitationCoverage{Numerator: 3, Denominator: 5, Percent: 60}
		return r
	}
	withDuplicateLedger := func() EditorialReviewReturn {
		r := baseline()
		r.CrossSurfaceLedger = []CrossSurfaceLedgerRow{
			{Fact: "DB_PASSWORD rotation", Surfaces: []string{"apidev/README.md", "CLAUDE.md"}, Severity: "duplicate"},
		}
		return r
	}

	tests := []struct {
		name       string
		in         *EditorialReviewReturn
		checkName  string
		wantStatus string
		wantDetail string
	}{
		{"dispatched_ok", ptrOf(baseline()), "editorial_review_dispatched", "pass", "surface(s) walked"},
		{"dispatched_missing", nil, "editorial_review_dispatched", "fail", "surfaces_walked is empty"},
		{"dispatched_empty_surfaces", ptrOf(EditorialReviewReturn{}), "editorial_review_dispatched", "fail", "surfaces_walked is empty"},
		{"crit_zero_passes", ptrOf(baseline()), "editorial_review_no_wrong_surface_crit", "pass", ""},
		{"crit_nonzero_fails", ptrOf(withCrit(2)), "editorial_review_no_wrong_surface_crit", "fail", "2 CRIT"},
		{"reclass_clean_passes", ptrOf(baseline()), "editorial_review_reclassification_delta", "pass", ""},
		{"reclass_mismatch_fails", ptrOf(withReclassMismatch()), "editorial_review_reclassification_delta", "fail", "reclassification disagreement"},
		{"fabricated_none_passes", ptrOf(withCrit(1)), "editorial_review_no_fabricated_mechanism", "pass", ""},
		{"fabricated_tag_fails", ptrOf(withFabricatedCrit()), "editorial_review_no_fabricated_mechanism", "fail", "fabricated-mechanism"},
		{"citation_zero_denominator_passes", ptrOf(baseline()), "editorial_review_citation_coverage", "pass", "denominator=0"},
		{"citation_gap_fails", ptrOf(withUncitedCoverage()), "editorial_review_citation_coverage", "fail", "lack a zerops_knowledge citation"},
		{"crossdup_clean_passes", ptrOf(baseline()), "editorial_review_cross_surface_duplication", "pass", ""},
		{"crossdup_flagged_fails", ptrOf(withDuplicateLedger()), "editorial_review_cross_surface_duplication", "fail", "cross-surface duplicate"},
		{"wrong_at_ceiling_passes", ptrOf(withWrong(1)), "editorial_review_wrong_count", "pass", "1 WRONG"},
		{"wrong_above_ceiling_fails", ptrOf(withWrong(2)), "editorial_review_wrong_count", "fail", "ceiling is 1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rows := EditorialReviewChecks(tt.in)
			found := false
			for _, row := range rows {
				if row.Name != tt.checkName {
					continue
				}
				found = true
				if row.Status != tt.wantStatus {
					t.Fatalf("%s status=%s want=%s detail=%q", tt.checkName, row.Status, tt.wantStatus, row.Detail)
				}
				if tt.wantDetail != "" && !strings.Contains(row.Detail, tt.wantDetail) {
					t.Fatalf("%s detail missing %q; got %q", tt.checkName, tt.wantDetail, row.Detail)
				}
			}
			if !found {
				t.Fatalf("battery missing check %q", tt.checkName)
			}
		})
	}
}

// TestEditorialReviewChecks_SevenRowsInStableOrder pins the battery's
// row count + order. Adding a check or renaming one without updating
// this test is a regression surface.
func TestEditorialReviewChecks_SevenRowsInStableOrder(t *testing.T) {
	t.Parallel()
	rows := EditorialReviewChecks(nil)
	want := []string{
		"editorial_review_dispatched",
		"editorial_review_no_wrong_surface_crit",
		"editorial_review_reclassification_delta",
		"editorial_review_no_fabricated_mechanism",
		"editorial_review_citation_coverage",
		"editorial_review_cross_surface_duplication",
		"editorial_review_wrong_count",
	}
	if len(rows) != len(want) {
		t.Fatalf("row count=%d want=%d", len(rows), len(want))
	}
	for i, name := range want {
		if rows[i].Name != name {
			t.Errorf("rows[%d]=%q want %q", i, rows[i].Name, name)
		}
	}
}

// TestEditorialReviewChecks_EveryFailDetailNamesPreAttestNote confirms
// the `dispatched via close.editorial-review (no author-side equivalent
// ...)` marker is present on every failing detail so downstream
// consumers can filter §16a rows without parsing the check name.
func TestEditorialReviewChecks_EveryFailDetailNamesPreAttestNote(t *testing.T) {
	t.Parallel()
	rows := EditorialReviewChecks(nil)
	for _, row := range rows {
		if row.Status == "pass" {
			continue
		}
		if !strings.Contains(row.Detail, EditorialReviewPreAttestNote) {
			t.Errorf("fail row %q detail missing pre-attest note; got %q", row.Name, row.Detail)
		}
	}
}

// TestParseEditorialReviewReturn_Shapes exercises the parser on the
// canonical attestation shapes: empty, non-JSON, invalid JSON, valid
// JSON. Each row asserts the typed error prefix or success condition.
func TestParseEditorialReviewReturn_Shapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		errSubstr string
	}{
		{"empty", "", true, "is empty"},
		{"non_json_text", "review complete", true, "not JSON"},
		{"malformed_json", `{"surfaces_walked":`, true, "JSON parse"},
		{"valid_minimal", validEditorialReviewPayload(), false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseEditorialReviewReturn(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.errSubstr) {
				t.Fatalf("err=%q missing %q", err.Error(), tt.errSubstr)
			}
		})
	}
}

// ptrOf returns a pointer to the value — test convenience.
func ptrOf[T any](v T) *T {
	return &v
}
