package recipe

import (
	"strings"
	"testing"
)

// Run-29 Fix #5 — showcase_scenario atom: dev-loop teaching.
//
// Run-28 features-frontend agent dispatched 8 cross-deploys
// appdev→appstage debugging one card. The brief described the WHAT
// (cards + states + browser-walk) but not the HOW (iterate on appdev
// HMR; cross-deploy ONCE per feature-pass close). The new section pins
// the four-step loop + when-cross-deploy-IS-right + when-cross-deploy-
// is-WRONG lists.

func TestShowcaseScenarioAtom_DevLoopSection_Present(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/feature/showcase_scenario.md")
	if err != nil {
		t.Fatalf("read showcase_scenario.md: %v", err)
	}
	if !strings.Contains(body, "## Dev loop — appdev HMR first, cross-deploy last") {
		t.Errorf("showcase_scenario.md missing dev-loop section heading")
	}
}

func TestShowcaseScenarioAtom_FourStepLoop_AllStepsNamed(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/feature/showcase_scenario.md")
	if err != nil {
		t.Fatalf("read showcase_scenario.md: %v", err)
	}
	for _, want := range []string{
		"**Author the card on appdev.**",
		"**Browser-walk on appdev**",
		"**Iterate WITHIN appdev.**",
		"**Cross-deploy to appstage ONCE per feature-pass close.**",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("showcase_scenario.md dev-loop missing step anchor %q", want)
		}
	}
}

func TestShowcaseScenarioAtom_WhenCrossDeployRight_ListPresent(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/feature/showcase_scenario.md")
	if err != nil {
		t.Fatalf("read showcase_scenario.md: %v", err)
	}
	for _, want := range []string{
		"### When cross-deploy IS the right tool",
		"build-time env-var bake",
		"`VITE_API_URL`",
		"CORS / cross-origin / TLS",
		"feature-pass is closing",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("showcase_scenario.md when-cross-deploy-right list missing %q", want)
		}
	}
}

func TestShowcaseScenarioAtom_WhenCrossDeployWrong_ListPresent(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/feature/showcase_scenario.md")
	if err != nil {
		t.Fatalf("read showcase_scenario.md: %v", err)
	}
	for _, want := range []string{
		"### When cross-deploy is the WRONG tool",
		"The click handler doesn't fire",
		"A fetch returns wrong data",
		"A card renders incorrectly",
		"ANY in-bundle behavior",
		"cross-deployed the same source twice in a row",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("showcase_scenario.md when-cross-deploy-wrong list missing %q", want)
		}
	}
}

// Run-29 Fix #6 — list ordering newest-first contract.
//
// Run-28 storage card shipped with `ListObjectsV2Command` results
// (alphabetical-by-key) sliced via `slice(0, 5)`. With timestamp-
// suffixed upload keys, alphabetical = OLDEST-first; the just-uploaded
// item lands at position 6+, invisible without scrolling. Browser-walk's
// primary observable ("just-added item appears in the chip list") fails
// silently no matter how many times the click fires correctly.

func TestShowcaseScenarioAtom_ListOrderingSection_Present(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/feature/showcase_scenario.md")
	if err != nil {
		t.Fatalf("read showcase_scenario.md: %v", err)
	}
	if !strings.Contains(body, "## List ordering — newest-first across every card") {
		t.Errorf("showcase_scenario.md missing list-ordering section heading")
	}
}

func TestShowcaseScenarioAtom_ListOrdering_BackendContractsPresent(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/feature/showcase_scenario.md")
	if err != nil {
		t.Fatalf("read showcase_scenario.md: %v", err)
	}
	for _, want := range []string{
		"`ORDER BY created_at DESC`",
		"`order: { createdAt: 'DESC' }`",
		"`LPUSH`",
		"`LTRIM 0 N-1`",
		"`ListObjectsV2Command`",
		"DESC by `LastModified`",
		"Search results",
		"return by relevance",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("showcase_scenario.md list-ordering section missing %q", want)
		}
	}
}

// Run-29 Fix #6 — storage-upload three-shape resilience.
//
// Run-28 features-frontend agent burned ~13 click attempts on the
// storage-upload button under headless Chromium before recording a
// field_rationale. The zerops_browser tool surface has no
// setInputFiles primitive. Brief now teaches: card MUST expose both
// a real <input type="file"> selector (human porter path) AND a
// blob-fallback button (browser-walk path), with explicit early
// field_rationale escape hatch after 2 click failures.

func TestShowcaseScenarioAtom_StorageUploadResilientShape_Present(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/feature/showcase_scenario.md")
	if err != nil {
		t.Fatalf("read showcase_scenario.md: %v", err)
	}
	if !strings.Contains(body, "## Storage-upload card — resilient shape") {
		t.Errorf("showcase_scenario.md missing storage-upload resilient-shape section heading")
	}
}

func TestShowcaseScenarioAtom_StorageUpload_BothAffordancesNamed(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/feature/showcase_scenario.md")
	if err != nil {
		t.Fatalf("read showcase_scenario.md: %v", err)
	}
	for _, want := range []string{
		`<input type="file" data-feature="upload-file"`,
		`<button data-feature="upload">`,
		"A real file selector",
		"A blob-fallback button",
		"`zerops_browser`",
		"`setInputFiles`",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("showcase_scenario.md storage-upload section missing %q", want)
		}
	}
}

func TestShowcaseScenarioAtom_StorageUpload_EarlyEscapeHatchTwoAttempts(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/feature/showcase_scenario.md")
	if err != nil {
		t.Fatalf("read showcase_scenario.md: %v", err)
	}
	for _, want := range []string{
		"### Browser-walk fallback escape hatch",
		"after **2 attempts**",
		"do NOT loop",
		"storage-upload-click-headless-fragility",
		"kind=field_rationale",
		"Two click attempts is the cap",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("showcase_scenario.md storage-upload escape-hatch section missing %q", want)
		}
	}
}
