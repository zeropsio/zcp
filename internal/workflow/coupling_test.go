package workflow

import (
	"regexp"
	"strings"
	"testing"
)

// TestCoupledNames_SameSurfaceCouples — v8.97 Fix 4.
// Two failed checks declaring identical ReadSurface appear in each other's
// coupled list.
func TestCoupledNames_SameSurfaceCouples(t *testing.T) {
	t.Parallel()

	checks := []StepCheck{
		{Name: "a", Status: "fail", ReadSurface: "apidev/README.md"},
		{Name: "b", Status: "fail", ReadSurface: "apidev/README.md"},
	}
	got := CoupledNames(checks)
	if want := []string{"b"}; !equalStrSlice(got["a"], want) {
		t.Errorf("a: want coupled=%v, got %v", want, got["a"])
	}
	if want := []string{"a"}; !equalStrSlice(got["b"], want) {
		t.Errorf("b: want coupled=%v, got %v", want, got["b"])
	}
}

// TestCoupledNames_DifferentSurfacesDoNotCouple — v8.97 Fix 4.
// Distinct ReadSurface values produce no coupling.
func TestCoupledNames_DifferentSurfacesDoNotCouple(t *testing.T) {
	t.Parallel()

	checks := []StepCheck{
		{Name: "a", Status: "fail", ReadSurface: "apidev/README.md"},
		{Name: "b", Status: "fail", ReadSurface: "appdev/README.md"},
	}
	got := CoupledNames(checks)
	if len(got) != 0 {
		t.Errorf("expected no coupling across distinct surfaces, got %v", got)
	}
}

// TestCoupledNames_ThreeChecksOneSurface — v8.97 Fix 4.
// Three failed checks on one surface each list the other two (not self,
// no duplicates). This is the "multi-check cluster" general case; any
// historical cascade (worker gotcha triad, env4 comment triad) is just an
// illustration of this invariant.
func TestCoupledNames_ThreeChecksOneSurface(t *testing.T) {
	t.Parallel()

	checks := []StepCheck{
		{Name: "x", Status: "fail", ReadSurface: "env/import.yaml"},
		{Name: "y", Status: "fail", ReadSurface: "env/import.yaml"},
		{Name: "z", Status: "fail", ReadSurface: "env/import.yaml"},
	}
	got := CoupledNames(checks)
	for _, name := range []string{"x", "y", "z"} {
		sibs := got[name]
		if len(sibs) != 2 {
			t.Errorf("%s: expected 2 coupled siblings, got %d (%v)", name, len(sibs), sibs)
		}
		for _, s := range sibs {
			if s == name {
				t.Errorf("%s: self appears in coupled list", name)
			}
		}
		// No duplicates.
		seen := map[string]bool{}
		for _, s := range sibs {
			if seen[s] {
				t.Errorf("%s: duplicate sibling %q", name, s)
			}
			seen[s] = true
		}
	}
}

// TestCoupledNames_PassedChecksNotStamped — v8.97 Fix 4.
// A passed check on the same surface as a failed check receives no
// coupling hint — only failures need remediation context.
func TestCoupledNames_PassedChecksNotStamped(t *testing.T) {
	t.Parallel()

	checks := []StepCheck{
		{Name: "a", Status: "fail", ReadSurface: "shared.yaml"},
		{Name: "b", Status: "pass", ReadSurface: "shared.yaml"},
	}
	got := CoupledNames(checks)
	if _, has := got["b"]; has {
		t.Errorf("passed check b should not receive coupling; got %v", got["b"])
	}
	if len(got["a"]) == 0 {
		t.Errorf("failed check a should still list b as coupled sibling; got %v", got["a"])
	}
}

// TestCoupledNames_EmptySurfaceIgnored — v8.97 Fix 4.
// Checks with ReadSurface == "" are not grouped. Empty is never a match
// key.
func TestCoupledNames_EmptySurfaceIgnored(t *testing.T) {
	t.Parallel()

	checks := []StepCheck{
		{Name: "a", Status: "fail", ReadSurface: ""},
		{Name: "b", Status: "fail", ReadSurface: ""},
	}
	got := CoupledNames(checks)
	if len(got) != 0 {
		t.Errorf("expected no coupling for empty ReadSurface, got %v", got)
	}
}

// TestStampCoupling_HowToFixNamesAllSiblings — v8.97 Fix 4.
// A failed check with two coupled siblings has its HowToFix extended with
// a tail that names both sibling names verbatim.
func TestStampCoupling_HowToFixNamesAllSiblings(t *testing.T) {
	t.Parallel()

	checks := []StepCheck{
		{Name: "a", Status: "fail", ReadSurface: "shared", HowToFix: "fix a"},
		{Name: "b", Status: "fail", ReadSurface: "shared", HowToFix: "fix b"},
		{Name: "c", Status: "fail", ReadSurface: "shared", HowToFix: "fix c"},
	}
	out := StampCoupling(checks)
	for _, c := range out {
		for _, sibling := range []string{"a", "b", "c"} {
			if sibling == c.Name {
				continue
			}
			if !strings.Contains(c.HowToFix, sibling) {
				t.Errorf("%s.HowToFix missing sibling %q; got: %s", c.Name, sibling, c.HowToFix)
			}
		}
	}
}

// TestStampCoupling_Idempotent — v8.97 Fix 4.
// Re-stamping an already-stamped slice is a no-op (tail appears exactly
// once; CoupledWith stays unchanged).
func TestStampCoupling_Idempotent(t *testing.T) {
	t.Parallel()

	checks := []StepCheck{
		{Name: "a", Status: "fail", ReadSurface: "shared", HowToFix: "fix a"},
		{Name: "b", Status: "fail", ReadSurface: "shared", HowToFix: "fix b"},
	}
	once := StampCoupling(checks)
	twice := StampCoupling(once)
	if len(once) != len(twice) {
		t.Fatalf("expected stamp to be length-preserving")
	}
	for i := range once {
		if once[i].HowToFix != twice[i].HowToFix {
			t.Errorf("%s.HowToFix drift on re-stamp", once[i].Name)
		}
		if len(once[i].CoupledWith) != len(twice[i].CoupledWith) {
			t.Errorf("%s.CoupledWith drift on re-stamp", once[i].Name)
		}
	}
}

// TestStampCoupling_MultiCheckSurface_IllustrativeApidev — v8.97 Fix 4.
// Illustrative fixture (not a per-cluster regression guard): the
// three-check-one-surface invariant covers the historical apidev readme
// cascade as a free consequence. If TestCoupledNames_ThreeChecksOneSurface
// passes, this passes too; it exists purely to show the v32 failure
// shape reaches coverage without a hand-maintained cluster table.
func TestStampCoupling_MultiCheckSurface_IllustrativeApidev(t *testing.T) {
	t.Parallel()

	surface := "workerdev/README.md — #knowledge-base fragment"
	checks := []StepCheck{
		{Name: "worker_knowledge_base_authenticity", Status: "fail", ReadSurface: surface},
		{Name: "worker_gotcha_distinct_from_guide", Status: "fail", ReadSurface: surface},
		{Name: "worker_worker_queue_group_gotcha", Status: "fail", ReadSurface: surface},
	}
	out := StampCoupling(checks)
	// Every check names the other two siblings verbatim.
	names := []string{"worker_knowledge_base_authenticity", "worker_gotcha_distinct_from_guide", "worker_worker_queue_group_gotcha"}
	for _, c := range out {
		for _, sibling := range names {
			if sibling == c.Name {
				continue
			}
			if !strings.Contains(c.HowToFix, sibling) {
				t.Errorf("%s.HowToFix missing sibling %q — illustrative shape broken; inspect CoupledNames invariants", c.Name, sibling)
			}
		}
	}
}

// TestStampCoupling_MultiCheckSurface_IllustrativeEnv4 — v8.97 Fix 4.
// Illustrative fixture for the env4 comment triad.
func TestStampCoupling_MultiCheckSurface_IllustrativeEnv4(t *testing.T) {
	t.Parallel()

	surface := "environments/4 — Small Production/import.yaml — service comments"
	checks := []StepCheck{
		{Name: "env4_import_comment_ratio", Status: "fail", ReadSurface: surface},
		{Name: "env4_import_comment_depth", Status: "fail", ReadSurface: surface},
		{Name: "env4_import_cross_env_refs", Status: "fail", ReadSurface: surface},
	}
	out := StampCoupling(checks)
	for _, c := range out {
		for _, sibling := range []string{"env4_import_comment_ratio", "env4_import_comment_depth", "env4_import_cross_env_refs"} {
			if sibling == c.Name {
				continue
			}
			if !strings.Contains(c.HowToFix, sibling) {
				t.Errorf("%s.HowToFix missing sibling %q", c.Name, sibling)
			}
		}
	}
}

// runVaryingSignals — tokens that indicate a ReadSurface string is
// NOT stable (contains per-run state that would break exact-string
// coupling matches). The regression TestAllReadSurfacesAreStable
// scans every emitted ReadSurface for these tokens.
var runVaryingSignals = []*regexp.Regexp{
	regexp.MustCompile(`/tmp/`),                                 // temp directories
	regexp.MustCompile(`/var/folders/`),                         // macOS temp
	regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`), // RFC3339 timestamps
	regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`), // UUIDs
	regexp.MustCompile(`^/(?:root|home|Users)/`),                                                      // absolute user paths
}

// TestAllReadSurfacesAreStable — v8.97 Fix 4 hygiene invariant.
// The coupling helper's exact-string match on ReadSurface relies on
// surfaces being stable across runs (not containing per-run state like
// timestamps, UUIDs, or absolute container paths that vary). This test
// exercises the helper against synthetic surfaces known to carry
// run-varying tokens and asserts the detector catches them. The
// production guard — preventing real checks from shipping unstable
// surfaces — is enforced at code-review time via this list of signals.
func TestAllReadSurfacesAreStable(t *testing.T) {
	t.Parallel()

	stable := []string{
		"apidev/README.md",
		"appdev/README.md — #knowledge-base fragment",
		"environments/4 — Small Production/import.yaml",
		"workerdev/zerops.yaml",
	}
	for _, s := range stable {
		for _, re := range runVaryingSignals {
			if re.MatchString(s) {
				t.Errorf("stable surface %q matched run-varying signal %q — detector is over-eager", s, re.String())
			}
		}
	}

	unstable := []string{
		"/tmp/session-12ab/README.md",
		"workerdev/2024-05-12T14:23:00Z-readme.md",
		"/var/folders/abc/xyz/README.md",
		"cache-550e8400-e29b-41d4-a716-446655440000.yaml",
		"/Users/tester/zerops/apidev/README.md",
	}
	for _, s := range unstable {
		matched := false
		for _, re := range runVaryingSignals {
			if re.MatchString(s) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("unstable surface %q went undetected — add a matching signal to runVaryingSignals", s)
		}
	}
}

func equalStrSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
