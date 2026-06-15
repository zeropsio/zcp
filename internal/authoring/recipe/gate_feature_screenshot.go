package recipe

import (
	"fmt"
	"path/filepath"
	"strings"
)

// gate_feature_screenshot.go — Run-31 Fix #4 closure.
//
// Run-30 surfaced that the showcase verification atoms use `snapshot
// -i -c` (accessibility tree) + `get text [selector]` assertions only
// — no pixel screenshot, no visual baseline. Visual defects (oval
// status-dots, missing card backgrounds, FOUC mid-render) pass through
// post-run analysis with no eyeball-diff artifact. The run-30 fix
// edited briefs/feature/integration_validator.md to teach the agent to
// capture a final-state screenshot via `zerops_browser commands
// ["screenshot", "--full", "<outputRoot>/screenshots/dashboard-close.png"]`
// and record a `browser_verification` fact carrying the path in
// `extra.screenshot`.
//
// Run-31 evidence (docs/zcprecipator3/runs/31/ANALYSIS.md):
// `runs/31/screenshots/` does not exist; all 6 browser_verification
// facts in `runs/31/environments/facts.jsonl` carry
// `"screenshot":"none-snapshot-only"`; the features-frontend agent
// issued only `snapshot -i -c` calls. The brief teaching read as soft
// instruction ("Capture", "Add it", "Substitute") with no MUST /
// required-fact gate, and the agent skipped it cleanly. The atom-pin
// (TestIntegrationValidatorAtom_TeachesScreenshotAtClose) only asserts
// the brief STRING is present, not that the agent acts on it.
//
// gateFeatureScreenshotCaptured closes the soft-teaching gap with an
// engine-side close-gate: complete-phase phase=feature refuses when no
// `browser_verification` fact in this run carries an `extra.screenshot`
// path string under `<outputRoot>/screenshots/`. The refusing-pattern
// is the system.md §4 catalog-drift-safe shape — positive presence-
// check ("at least one screenshot fact exists at close time"), not a
// content ban-list / regex over the brief text.
//
// Pinned by:
//   - TestGateFeatureScreenshot_RefusesWhenNoScreenshotFact
//   - TestGateFeatureScreenshot_RefusesWhenScreenshotFactIsPlaceholder
//   - TestGateFeatureScreenshot_PassesWithScreenshotPathFact
//   - TestGateFeatureScreenshot_RefusesWhenPathOutsideScreenshotsDir
//   - TestGateFeatureScreenshot_RegisteredInFeaturePhaseGates
//   - TestFeatureClose_RefusesWithoutScreenshotFact (end-to-end)

// gateFeatureScreenshotCaptured refuses feature complete-phase when no
// browser_verification fact carries a non-placeholder screenshot path
// under <outputRoot>/screenshots/. Returns a single blocking violation
// when the gate refuses; empty slice when at least one fact qualifies.
func gateFeatureScreenshotCaptured(ctx GateContext) []Violation {
	if ctx.FactsLog == nil {
		// No facts log = no enforcement signal available; let other
		// gates surface the missing-infrastructure problem.
		return nil
	}
	records, err := ctx.FactsLog.Read()
	if err != nil {
		// Read failure is the existing facts-read-failure surface from
		// citation/fact-validity gates; don't double-report here.
		return nil
	}
	want := filepath.Join(ctx.OutputRoot, "screenshots") + string(filepath.Separator)
	for _, f := range records {
		if f.Kind != FactKindBrowserVerification {
			continue
		}
		path := strings.TrimSpace(f.Extra["screenshot"])
		if path == "" {
			continue
		}
		// Run-30/31 placeholder — the agent emitted browser_verification
		// facts with the literal "none-snapshot-only" string in
		// extra.screenshot when it ran snapshot-only. Treat as missing.
		if path == "none-snapshot-only" {
			continue
		}
		// Positive presence-check — the path must point under
		// <outputRoot>/screenshots/. Off-tree artifact paths (e.g.
		// /tmp/random.png) don't satisfy the gate; post-run analysis
		// looks under the run's screenshots/ dir.
		if !strings.HasPrefix(path, want) {
			continue
		}
		return nil
	}
	return []Violation{{
		Code:     "feature-screenshot-not-captured",
		Severity: SeverityBlocking,
		Message: fmt.Sprintf(
			"feature phase requires at least one browser_verification fact carrying `extra.screenshot` set to an absolute path under %s (the agent-browser `screenshot --full` artifact captured at close). "+
				"Briefs/feature/integration_validator.md §\"Capture a final-state screenshot before close\" teaches the close-step `zerops_browser` batch with `[\"screenshot\", \"--full\", <path>]` as the LAST inner command. "+
				"Run-31 evidence: agents recorded browser_verification facts with `extra.screenshot:\"none-snapshot-only\"` instead of the captured path; post-run analysis loses the visual baseline.",
			want,
		),
	}}
}
