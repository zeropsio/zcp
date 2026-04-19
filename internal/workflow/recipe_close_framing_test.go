package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/content"
)

// TestCloseSection_BothSubStepsAlwaysAutonomous — v8.97 Fix 2 bar.
// The rewritten close section must contain the literal "always autonomous"
// so the agent cannot interpret either sub-step as user-gated. It must NOT
// contain the abandoned framings ("user-gated", "Group VERIFY", "Group
// PUBLISH") that earlier drafts tried and v31/v32 reinterpreted in opposite
// directions.
func TestCloseSection_BothSubStepsAlwaysAutonomous(t *testing.T) {
	t.Parallel()

	body := sectionContent(t, "close")
	if !strings.Contains(body, "always autonomous") {
		t.Errorf("close section must contain literal \"always autonomous\" (Fix 2 calibration guard)")
	}
	forbidden := []string{
		"user-gated",
		"Group VERIFY",
		"Group PUBLISH",
	}
	for _, phrase := range forbidden {
		if strings.Contains(body, phrase) {
			t.Errorf("close section must NOT contain %q — old framing that v31/v32 reinterpreted", phrase)
		}
	}
}

// TestCloseSection_V32ForwardGuard — v8.97 Fix 2. The close section carries
// the v32-specific regression sentence ("v32 asked the user...") so a future
// rewrite cannot silently drop the lesson learned from v32's close-skip
// failure. If a future doc refactor wants to restructure the close section,
// the regression phrase must stay — or move to a clearly-labeled replacement.
func TestCloseSection_V32ForwardGuard(t *testing.T) {
	t.Parallel()

	body := sectionContent(t, "close")
	if !strings.Contains(body, "v32 asked the user") {
		t.Errorf("close section must carry the \"v32 asked the user\" regression phrase as a forward guard (Fix 2)")
	}
}

// TestCloseSection_NoPublishAsSubStep — v8.97 Fix 2. The numbered sub-step
// list in the close section header contains only code-review and
// close-browser-walk. Publish appears only in the trailing CLI-guidance
// paragraph (prose reference, not a workflow decision).
func TestCloseSection_NoPublishAsSubStep(t *testing.T) {
	t.Parallel()

	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}
	body := ExtractSection(md, "close")

	// Stop reading at the "### Constraints" heading so we only inspect the
	// numbered sub-step list, not the full section prose.
	header := body
	if before, _, ok := strings.Cut(body, "### Constraints"); ok {
		header = before
	}

	// The header must list exactly the two autonomous sub-steps. Any
	// literal reference to `publish` or `export` as a numbered sub-step is
	// a Fix 2 regression.
	hasCodeReview := strings.Contains(header, "**code-review**")
	hasBrowserWalk := strings.Contains(header, "**close-browser-walk**")
	if !hasCodeReview {
		t.Errorf("close header missing **code-review** sub-step item")
	}
	if !hasBrowserWalk {
		t.Errorf("close header missing **close-browser-walk** sub-step item")
	}
	for _, banned := range []string{"**publish**", "**export**"} {
		if strings.Contains(header, banned) {
			t.Errorf("close header must NOT list %q as a numbered sub-step (Fix 2 bar)", banned)
		}
	}
}

// TestDetailedGuide_CloseStepUnambiguous — v8.97 Fix 2. The guidance string
// the agent actually receives at close-step transition must contain the
// "always autonomous" calibration and must NOT contain the abandoned
// user-gated framing. This exercises the live extraction path (buildGuide
// → resolveRecipeGuidance → ExtractSection) to catch regressions where the
// section content is correct in source but the extractor mis-reads it.
func TestDetailedGuide_CloseStepUnambiguous(t *testing.T) {
	t.Parallel()

	body := sectionContent(t, "close")
	if !strings.Contains(body, "always autonomous") {
		t.Errorf("close-step detailedGuide must contain \"always autonomous\"")
	}
	if strings.Contains(body, "user-gated") {
		t.Errorf("close-step detailedGuide must NOT contain \"user-gated\" (Fix 2 bar)")
	}
}
