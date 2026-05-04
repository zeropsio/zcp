package recipe

import (
	"strings"
	"testing"
)

// Run-23 F-24 / Fix 7 — direct unit coverage for the refinement-suspect
// helpers. The composer exercises them indirectly, but per-helper tests
// pin the contract so future refactors can't silently change the
// classification or codebase-matching shape.

// TestCollectRefinementSuspects_FromCrossSurfaceDuplicationNotices —
// notice-flagged duplications surface in the suspect list with the
// notice's Code carried through as the Class tag.
func TestCollectRefinementSuspects_FromCrossSurfaceDuplicationNotices(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "synth",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
		},
	}
	notices := []Violation{
		{
			Code:     "cross-surface-duplication",
			Path:     "codebase/api",
			Message:  "IG `cache hit ratio` overlaps KB bullet `cache hit ratio` — KB bullets should cover topics WITHOUT a codebase-side landing point.",
			Severity: SeverityNotice,
		},
		{
			// Blocking violations are not pulled into suspect list — the
			// agent is reviewing already-passing fragments at refinement.
			Code:     "blocking-shape",
			Path:     "codebase/api/integration-guide",
			Message:  "blocking failure",
			Severity: SeverityBlocking,
		},
	}
	got := CollectRefinementSuspects(plan, notices)

	var found bool
	for _, s := range got {
		if s.Class == "cross-surface-duplication" && s.FragmentID == "codebase/api" {
			found = true
			if !strings.Contains(s.Reason, "overlaps KB bullet") {
				t.Errorf("suspect Reason missing notice message context: %q", s.Reason)
			}
		}
		if s.Class == "blocking-shape" {
			t.Errorf("blocking violation leaked into suspect list (Class=%q)", s.Class)
		}
	}
	if !found {
		t.Errorf("cross-surface-duplication notice missing from suspect list (got=%+v)", got)
	}
}

// TestCollectRefinementSuspects_FromRubricPreScan — KB bullets opening
// with a `**author-directive**` bold-prefix WITHOUT a symptom signal in
// the same line are flagged as suspects under the kb-author-claim-stem
// class. Bullets that pair the directive with a symptom signal (HTTP
// code, "fails", "missing", etc.) are NOT flagged.
func TestCollectRefinementSuspects_FromRubricPreScan(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "synth",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
			{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22", IsWorker: true},
		},
		Fragments: map[string]string{
			// api KB has a directive-only stem — flag.
			"codebase/api/knowledge-base": "- **Always pin the runtime version** — the auto-detected runtime drifts.\n",
			// worker KB pairs directive with a symptom signal (the
			// regex matches "fails") — do NOT flag.
			"codebase/worker/knowledge-base": "- **Configure queue-group on every subscriber** — without it, the at-most-once delivery contract fails and downstream consumers see duplicate messages.\n",
		},
	}
	got := CollectRefinementSuspects(plan, nil)

	var apiFlagged, workerFlagged bool
	for _, s := range got {
		if s.Class == "kb-author-claim-stem" && s.FragmentID == "codebase/api/knowledge-base" {
			apiFlagged = true
		}
		if s.Class == "kb-author-claim-stem" && s.FragmentID == "codebase/worker/knowledge-base" {
			workerFlagged = true
		}
	}
	if !apiFlagged {
		t.Errorf("api KB directive-only stem not flagged as suspect (got=%+v)", got)
	}
	if workerFlagged {
		t.Errorf("worker KB stem with symptom signal incorrectly flagged as suspect (got=%+v)", got)
	}
}

// TestFactBelongsToCodebases_MatchesBareAndSlotHostnames — the slot-
// suffix matcher accepts `<host>`, `<host>dev`, `<host>stage`, and
// `<host>/runtime` (slot-name + sub-suffix shape). Service names that
// don't map to any codebase under review return false.
func TestFactBelongsToCodebases_MatchesBareAndSlotHostnames(t *testing.T) {
	t.Parallel()
	codebases := []Codebase{
		{Hostname: "api"},
		{Hostname: "worker"},
	}
	cases := []struct {
		name    string
		service string
		want    bool
	}{
		{"bare api matches api codebase", "api", true},
		{"apidev slot matches api codebase", "apidev", true},
		{"apistage slot matches api codebase", "apistage", true},
		{"apidev/runtime sub-suffix matches api codebase", "apidev/runtime", true},
		{"workerdev slot matches worker codebase", "workerdev", true},
		{"frontend does NOT match api codebase", "frontend", false},
		{"db does NOT match either codebase", "db", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fact := FactRecord{Service: tc.service}
			got := FactBelongsToCodebases(fact, codebases)
			if got != tc.want {
				t.Errorf("FactBelongsToCodebases(service=%q) = %v, want %v", tc.service, got, tc.want)
			}
		})
	}
}

// TestFactBelongsToCodebases_EmptyServiceReturnsTrue — back-compat path
// for run-wide facts (tier_decisions, project-scope porter_changes)
// that have no slot binding. Empty service ⇒ keep in scope.
func TestFactBelongsToCodebases_EmptyServiceReturnsTrue(t *testing.T) {
	t.Parallel()
	fact := FactRecord{Service: ""}
	codebases := []Codebase{{Hostname: "api"}}
	if !FactBelongsToCodebases(fact, codebases) {
		t.Error("empty service should remain in scope (run-wide fact); got dropped")
	}
}

// TestFactBelongsToCodebases_EmptyCodebases_ReturnsTrue — back-compat
// path. When the caller passes an empty codebase list (e.g. refinement
// composer on a plan with zero codebases authored), the per-codebase
// scoping has nothing to filter against; "include everything" is the
// safe default — dropping every fact would leave the refinement brief
// fact-empty for the entire run.
func TestFactBelongsToCodebases_EmptyCodebases_ReturnsTrue(t *testing.T) {
	t.Parallel()
	fact := FactRecord{Service: "apidev"}
	if !FactBelongsToCodebases(fact, nil) {
		t.Error("nil codebases should return true (include-everything fallback); got false")
	}
	if !FactBelongsToCodebases(fact, []Codebase{}) {
		t.Error("empty codebases slice should return true (include-everything fallback); got false")
	}
}

// TestFormatRefinementSuspects_EmptyListReturnsEmpty — the composer
// skips the section header when the suspect list is empty so the brief
// doesn't ship a section heading with no body.
func TestFormatRefinementSuspects_EmptyListReturnsEmpty(t *testing.T) {
	t.Parallel()
	if got := FormatRefinementSuspects(nil); got != "" {
		t.Errorf("expected empty string for nil suspects, got %q", got)
	}
	if got := FormatRefinementSuspects([]RefinementSuspect{}); got != "" {
		t.Errorf("expected empty string for empty slice, got %q", got)
	}
}
