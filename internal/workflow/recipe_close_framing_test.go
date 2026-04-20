package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/content"
)

// TestCloseSection_BothSubStepsAlwaysAutonomous — v8.97 Fix 2 bar.
// The rewritten close section must contain the literal "always autonomous"
// so the agent cannot interpret either SUB-STEP as user-gated. It must
// NOT use the abandoned sub-step framings ("Group VERIFY" / "Group
// PUBLISH") that earlier drafts tried and v31/v32 reinterpreted.
//
// v8.103: "user-gated" is allowed in the close section because the
// post-workflow export + publish CLI commands ARE user-gated — that's
// a different scope than the two sub-steps. The Fix 2 concern was
// specifically sub-step ambiguity, not the phrase itself.
func TestCloseSection_BothSubStepsAlwaysAutonomous(t *testing.T) {
	t.Parallel()

	body := sectionContent(t, "close")
	if !strings.Contains(body, "always autonomous") {
		t.Errorf("close section must contain literal \"always autonomous\" (Fix 2 calibration guard)")
	}
	forbidden := []string{
		"Group VERIFY",
		"Group PUBLISH",
	}
	for _, phrase := range forbidden {
		if strings.Contains(body, phrase) {
			t.Errorf("close section must NOT contain %q — old sub-step framing that v31/v32 reinterpreted", phrase)
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

// TestCloseSection_SingleCanonicalOutputLocation — v8.103.
// Observed in the wild: after close, the agent created a parallel
// `/var/www/recipe-{slug}/` directory with paraphrased env folder
// names alongside the correct `/var/www/zcprecipator/{slug}/`. Nothing
// in recipe.md ever suggested the parallel dir — pure agent invention.
// Fix: close section explicitly declares the single canonical output
// location and forbids duplicates.
func TestCloseSection_SingleCanonicalOutputLocation(t *testing.T) {
	t.Parallel()

	body := sectionContent(t, "close")
	if !strings.Contains(body, "zcprecipator/{slug}/") {
		t.Error("close section must name the canonical output location `{projectRoot}/zcprecipator/{slug}/`")
	}
	if !strings.Contains(body, "Do NOT create a parallel directory") {
		t.Error("close section must forbid parallel output directories (v8.103 — agent-invented `recipe-{slug}/` duplicates observed in the wild)")
	}
}

// TestCloseSection_PostCompletionCommandsUserGated — v8.103.
// The close section must declare both export and publish as user-gated.
// v8.98 Fix B framed export as autonomous — user objected, v8.103
// reverted. Regression guard against "Run it autonomously" wording.
func TestCloseSection_PostCompletionCommandsUserGated(t *testing.T) {
	t.Parallel()

	body := sectionContent(t, "close")
	if strings.Contains(body, "Run it autonomously") {
		t.Error("close section must NOT instruct autonomous export (v8.103 regression guard)")
	}
	if !strings.Contains(body, "strictly user-gated") {
		t.Error("close section must declare post-workflow commands as strictly user-gated")
	}
}

// TestDetailedGuide_CloseStepUnambiguous — v8.97 Fix 2. The guidance string
// the agent actually receives at close-step transition must contain the
// "always autonomous" calibration for the close SUB-STEPS. (v8.103:
// "user-gated" is allowed in the section prose because it's used to
// describe the post-workflow export/publish commands, a different scope.)
func TestDetailedGuide_CloseStepUnambiguous(t *testing.T) {
	t.Parallel()

	body := sectionContent(t, "close")
	if !strings.Contains(body, "always autonomous") {
		t.Errorf("close-step detailedGuide must contain \"always autonomous\"")
	}
}
