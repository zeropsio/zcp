package skillpacks

import "testing"

// TestRequirements_ClosureAndViolations exercises the shared closure module
// (spec-skill-packs.md §4.2/§3.1) that pack-set's unclosed-selection refusal
// and pack-status's non-closed warning both must consume rather than
// reimplement. Oracle: sets hand-computed from spec-skill-packs.md §4.2's
// edge table, not recomputed via the module's own traversal.
func TestRequirements_ClosureAndViolations(t *testing.T) {
	t.Parallel()

	pack, ok := Lookup("matt-pocock-skills")
	if !ok {
		t.Fatal("matt-pocock-skills missing from catalog")
	}

	t.Run("direct expansion", func(t *testing.T) {
		t.Parallel()
		// grill-with-docs Requires grilling, domain-modeling (§4.2 table).
		got := Closure(pack, []string{"grill-with-docs"})
		want := []string{"domain-modeling", "grill-with-docs", "grilling"}
		assertSameSet(t, "closure(grill-with-docs)", got, want)
	})

	t.Run("transitive depth-2", func(t *testing.T) {
		t.Parallel()
		// implement -> tdd, code-review; code-review -> setup-matt-pocock-skills.
		got := Closure(pack, []string{"implement"})
		want := []string{"code-review", "implement", "setup-matt-pocock-skills", "tdd"}
		assertSameSet(t, "closure(implement)", got, want)
	})

	t.Run("closed set has no violations", func(t *testing.T) {
		t.Parallel()
		closed := Closure(pack, []string{"implement"})
		got := Violations(pack, closed)
		if len(got) != 0 {
			t.Errorf("Violations(closure(implement)) = %+v, want none", got)
		}
	})

	t.Run("empty set has empty closure", func(t *testing.T) {
		t.Parallel()
		got := Closure(pack, nil)
		if len(got) != 0 {
			t.Errorf("Closure(nil) = %v, want empty", got)
		}
	})

	t.Run("multi-parent missing dependency collapses to one violation", func(t *testing.T) {
		t.Parallel()
		// wayfinder and triage both Requires grilling (§4.2 table); neither
		// grilling nor their other dependencies are in the selected set.
		got := Violations(pack, []string{"wayfinder", "triage"})

		var grillingViolation *Violation
		count := 0
		for i := range got {
			if got[i].Missing == "grilling" {
				grillingViolation = &got[i]
				count++
			}
		}
		if grillingViolation == nil {
			t.Fatalf("Violations(wayfinder,triage) = %+v, want an entry for missing grilling", got)
		}
		if count != 1 {
			t.Errorf("grilling appears %d times in Violations, want exactly 1 (multi-parent must collapse)", count)
		}
		want := []string{"triage", "wayfinder"}
		if len(grillingViolation.RequiredBy) != len(want) {
			t.Fatalf("grilling RequiredBy = %v, want %v", grillingViolation.RequiredBy, want)
		}
		for i := range want {
			if grillingViolation.RequiredBy[i] != want[i] {
				t.Fatalf("grilling RequiredBy = %v, want %v", grillingViolation.RequiredBy, want)
			}
		}
	})

	t.Run("Violations sorted deterministically by missing skill", func(t *testing.T) {
		t.Parallel()
		got := Violations(pack, []string{"implement"})
		want := []string{"code-review", "tdd"} // implement -> tdd, code-review (§4.2), sorted
		if len(got) != len(want) {
			t.Fatalf("Violations(implement) = %+v, want missing %v", got, want)
		}
		for i, v := range got {
			if v.Missing != want[i] {
				t.Errorf("Violations(implement)[%d].Missing = %q, want %q (sorted)", i, v.Missing, want[i])
			}
			if len(v.RequiredBy) != 1 || v.RequiredBy[0] != "implement" {
				t.Errorf("Violations(implement)[%d].RequiredBy = %v, want [implement]", i, v.RequiredBy)
			}
		}
	})

	t.Run("FormatViolations renders the shared wording", func(t *testing.T) {
		t.Parallel()
		// Hand-built, independent of Violations()'s own traversal — pins
		// the exact rendering both pack-set and pack-status must share.
		violations := []Violation{
			{Missing: "code-review", RequiredBy: []string{"implement"}},
			{Missing: "setup-matt-pocock-skills", RequiredBy: []string{"code-review"}},
			{Missing: "tdd", RequiredBy: []string{"implement"}},
		}
		got := FormatViolations(violations)
		want := "selection is not dependency-closed: missing code-review (required by implement), " +
			"setup-matt-pocock-skills (required by code-review), tdd (required by implement)"
		if got != want {
			t.Errorf("FormatViolations = %q, want %q", got, want)
		}
	})

	t.Run("FormatViolations of no violations is empty", func(t *testing.T) {
		t.Parallel()
		if got := FormatViolations(nil); got != "" {
			t.Errorf("FormatViolations(nil) = %q, want empty string", got)
		}
	})
}
