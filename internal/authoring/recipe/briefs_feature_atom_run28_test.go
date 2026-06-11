package recipe

import (
	"strings"
	"testing"
)

// briefs_feature_atom_run28_test.go — Run-28 fix #3 atom assertions.
//
// Pins the features-frontend showcase scenario layout-spec changes
// that close the run-26/run-27 grid → tabs override. The agent had
// an escape hatch when browser-walk surfaced "click at element-center
// fails on below-fold panels" and chose to abandon the layout rather
// than scroll-into-view before click. Atom now pins the layout as
// normative and teaches scrollIntoView for below-fold elements.

// TestShowcaseScenarioAtom_LayoutSpecIsNormative — Run-28 fix #3.
// The features-frontend showcase scenario brief must pin the layout
// as normative ("do not switch" or equivalent prohibition language)
// so the agent doesn't override grid → tabs when browser-walk fails
// on below-fold panels.
func TestShowcaseScenarioAtom_LayoutSpecIsNormative(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/feature/showcase_scenario.md")
	if err != nil {
		t.Fatalf("read showcase_scenario.md: %v", err)
	}
	for _, want := range []string{
		"normative",
		"not switch to tabs",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("showcase_scenario.md missing layout-pin anchor %q", want)
		}
	}
}

// TestShowcaseScenarioAtom_ScrollIntoViewTeaching_Present — Run-28
// fix #3. Atom must teach the scrollIntoView fix for below-fold
// panels rather than letting the agent abandon the grid layout.
func TestShowcaseScenarioAtom_ScrollIntoViewTeaching_Present(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/feature/showcase_scenario.md")
	if err != nil {
		t.Fatalf("read showcase_scenario.md: %v", err)
	}
	for _, want := range []string{
		"scrollIntoView",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("showcase_scenario.md missing scrollIntoView teaching anchor %q", want)
		}
	}
}
