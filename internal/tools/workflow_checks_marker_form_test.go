package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

// TestMarkerFormCheck_RejectsMissingTrailingHash is the Cx-MARKER-FORM-FIX
// RED→GREEN test. The v36 F-12 defect: writer atoms showed ZEROPS_EXTRACT
// markers WITHOUT the trailing `#` sentinel. Writer copied the shown
// form verbatim. Existing `fragment_<name>` checks fail with
// "missing fragment markers" — true but ambiguous: markers ARE
// present, they're just in the wrong form. The writer sees "missing",
// adds another broken marker, and loops.
//
// `fragment_marker_exact_form` is a separate sentinel that catches
// the broken form specifically and names the remediation exactly.
func TestMarkerFormCheck_RejectsMissingTrailingHash(t *testing.T) {
	t.Parallel()

	// Every key type in broken form, intermingled with correct form
	// (fix passes run one Edit at a time so healed + unhealed markers
	// coexist mid-fix).
	readme := "<!-- #ZEROPS_EXTRACT_START:intro -->\nsomething\n<!-- #ZEROPS_EXTRACT_END:intro -->\n\n" +
		"<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\n```yaml\n# note:\n```\n<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n\n" +
		"<!-- #ZEROPS_EXTRACT_START:knowledge-base -->\n### Gotchas\n- **x** — y\n<!-- #ZEROPS_EXTRACT_END:knowledge-base -->\n"

	checks := checkReadmeFragments(readme, "apidev")
	formCheck := findMarkerFormCheck(checks)
	if formCheck == nil {
		t.Fatalf("fragment_marker_exact_form missing; got names %v", markerFormCheckNames(checks))
	}
	if formCheck.Status != "fail" {
		t.Errorf("status=%s want=fail; detail=%q", formCheck.Status, formCheck.Detail)
	}
	// Detail must name the target form so the agent sees the fix.
	if !strings.Contains(formCheck.Detail, "# -->") {
		t.Errorf("detail must show target form `# -->`; got %q", formCheck.Detail)
	}
}

// TestMarkerFormCheck_PassesWhenAllExact guards the pass path.
func TestMarkerFormCheck_PassesWhenAllExact(t *testing.T) {
	t.Parallel()

	readme := "<!-- #ZEROPS_EXTRACT_START:intro# -->\nsomething\n<!-- #ZEROPS_EXTRACT_END:intro# -->\n\n" +
		"<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\nbody\n<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n\n" +
		"<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n### Gotchas\n- **x** — y\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n"

	checks := checkReadmeFragments(readme, "apidev")
	formCheck := findMarkerFormCheck(checks)
	if formCheck == nil {
		t.Fatalf("fragment_marker_exact_form missing; got names %v", markerFormCheckNames(checks))
	}
	if formCheck.Status != "pass" {
		t.Errorf("form-exact README status=%s want=pass; detail=%q", formCheck.Status, formCheck.Detail)
	}
}

func findMarkerFormCheck(checks []workflow.StepCheck) *workflow.StepCheck {
	for i := range checks {
		if checks[i].Name == "fragment_marker_exact_form" {
			return &checks[i]
		}
	}
	return nil
}

func markerFormCheckNames(checks []workflow.StepCheck) []string {
	out := make([]string, len(checks))
	for i, c := range checks {
		out[i] = c.Name
	}
	return out
}
