package recipe

import (
	"path/filepath"
	"strings"
	"testing"
)

// Run-31 Fix #4 closure — engine close-gate for feature-pass screenshot
// capture. The run-30 brief edit at integration_validator.md taught the
// agent to capture a final-state screenshot via `zerops_browser
// commands ["screenshot", "--full", <path>]` and record a
// `browser_verification` fact carrying the path in `extra.screenshot`.
// Run-31 evidence: all 6 browser_verification facts in
// runs/31/environments/facts.jsonl carry `"screenshot":
// "none-snapshot-only"`; `runs/31/screenshots/` does not exist; the
// agent cleanly skipped the soft brief teaching. The brief-text-only
// pin (TestIntegrationValidatorAtom_TeachesScreenshotAtClose) asserts
// the brief STRING is present, not that the agent acts on it.
//
// Engine-side gate closes the gap: complete-phase phase=feature refuses
// when no `browser_verification` fact in this run carries an
// `extra.screenshot` path string under `<outputRoot>/screenshots/`. The
// refusing-pattern is the system.md §4 catalog-drift-safe shape —
// positive presence-check ("at least one screenshot fact exists at
// close time"), not a content ban-list / regex-over-brief-text.
//
// Pinned at three levels:
//   - the gate function itself (gateFeatureScreenshotCaptured) refuses
//     the missing-fact case and passes the present-fact case;
//   - gatesForPhase(PhaseFeature) wires it into the feature gate set;
//   - completePhase phase=feature surfaces the refusal as a blocking
//     violation that prevents phase advance.

// TestGateFeatureScreenshot_RefusesWhenNoScreenshotFact — fact stream
// has zero browser_verification facts; the gate must refuse.
func TestGateFeatureScreenshot_RefusesWhenNoScreenshotFact(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	plan := &Plan{Slug: "synth-showcase"}
	log := newMemFactsLog(t, root)
	ctx := GateContext{Plan: plan, OutputRoot: root, FactsLog: log}
	violations := gateFeatureScreenshotCaptured(ctx)
	if len(violations) != 1 {
		t.Fatalf("want 1 violation, got %d (%+v)", len(violations), violations)
	}
	if violations[0].Code != "feature-screenshot-not-captured" {
		t.Errorf("want code feature-screenshot-not-captured, got %s", violations[0].Code)
	}
	if violations[0].Severity != SeverityBlocking {
		t.Errorf("want severity blocking, got %v", violations[0].Severity)
	}
	if !strings.Contains(violations[0].Message, "screenshots/") {
		t.Errorf("error should name the screenshots/ path convention; got %q", violations[0].Message)
	}
}

// TestGateFeatureScreenshot_RefusesWhenScreenshotFactIsPlaceholder — the
// run-30/31 failure shape. Browser_verification facts exist but every
// one carries `extra.screenshot:"none-snapshot-only"` (the agent ran
// `snapshot -i -c` only, no `screenshot --full`). Gate must refuse.
func TestGateFeatureScreenshot_RefusesWhenScreenshotFactIsPlaceholder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	plan := &Plan{Slug: "synth-showcase"}
	log := newMemFactsLog(t, root)
	for _, topic := range []string{"appdev-status-browser", "appdev-items-browser"} {
		if err := log.Append(FactRecord{
			Topic:   topic,
			Kind:    FactKindBrowserVerification,
			Subject: "panel",
			Service: "appstage",
			Why:     "walked panel",
			Extra:   map[string]string{"screenshot": "none-snapshot-only"},
		}); err != nil {
			t.Fatalf("append fact: %v", err)
		}
	}
	ctx := GateContext{Plan: plan, OutputRoot: root, FactsLog: log}
	violations := gateFeatureScreenshotCaptured(ctx)
	if len(violations) != 1 {
		t.Fatalf("want 1 violation, got %d (%+v)", len(violations), violations)
	}
	if violations[0].Code != "feature-screenshot-not-captured" {
		t.Errorf("want code feature-screenshot-not-captured, got %s", violations[0].Code)
	}
}

// TestGateFeatureScreenshot_PassesWithScreenshotPathFact — happy path.
// At least one browser_verification fact carries an extra.screenshot
// path string under <outputRoot>/screenshots/. Gate passes.
func TestGateFeatureScreenshot_PassesWithScreenshotPathFact(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	plan := &Plan{Slug: "synth-showcase"}
	log := newMemFactsLog(t, root)
	if err := log.Append(FactRecord{
		Topic:   "dashboard-close",
		Kind:    FactKindBrowserVerification,
		Subject: "dashboard",
		Service: "appstage",
		Why:     "all cards populated; screenshot captured",
		Extra: map[string]string{
			"screenshot": filepath.Join(root, "screenshots", "dashboard-close.png"),
		},
	}); err != nil {
		t.Fatalf("append fact: %v", err)
	}
	ctx := GateContext{Plan: plan, OutputRoot: root, FactsLog: log}
	if v := gateFeatureScreenshotCaptured(ctx); len(v) != 0 {
		t.Errorf("want 0 violations, got %d (%+v)", len(v), v)
	}
}

// TestGateFeatureScreenshot_RefusesWhenPathOutsideScreenshotsDir — the
// fact carries SOME path but not under <outputRoot>/screenshots/. The
// gate's positive presence-check refuses to silently accept off-tree
// artifact locations (post-run analysis looks under screenshots/).
func TestGateFeatureScreenshot_RefusesWhenPathOutsideScreenshotsDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	plan := &Plan{Slug: "synth-showcase"}
	log := newMemFactsLog(t, root)
	if err := log.Append(FactRecord{
		Topic:   "dashboard-close",
		Kind:    FactKindBrowserVerification,
		Subject: "dashboard",
		Service: "appstage",
		Why:     "screenshot captured to /tmp",
		Extra:   map[string]string{"screenshot": "/tmp/random.png"},
	}); err != nil {
		t.Fatalf("append fact: %v", err)
	}
	ctx := GateContext{Plan: plan, OutputRoot: root, FactsLog: log}
	violations := gateFeatureScreenshotCaptured(ctx)
	if len(violations) != 1 {
		t.Fatalf("want 1 violation when path is outside screenshots/, got %d (%+v)", len(violations), violations)
	}
}

// TestGateFeatureScreenshot_RegisteredInFeaturePhaseGates — the gate
// must be wired into gatesForPhase(PhaseFeature) so it fires at
// complete-phase phase=feature. Catalog-drift-safe placement: feature
// is the phase the screenshot teaching belongs to (frontend close-out
// after the final cross-deploy).
func TestGateFeatureScreenshot_RegisteredInFeaturePhaseGates(t *testing.T) {
	t.Parallel()
	gates := gatesForPhase(PhaseFeature)
	mustHaveGate(t, gates, "feature-screenshot-captured")
}

// TestFeatureClose_RefusesWithoutScreenshotFact — end-to-end. A session
// at PhaseFeature with zero screenshot facts must surface the gate's
// blocking violation through completePhase, not advance the phase.
func TestFeatureClose_RefusesWithoutScreenshotFact(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	log := OpenFactsLog(filepath.Join(root, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, root, nil)
	for _, p := range []Phase{PhaseResearch, PhaseProvision, PhaseScaffold, PhaseFeature} {
		if err := sess.EnterPhase(p); err != nil {
			t.Fatalf("EnterPhase(%s): %v", p, err)
		}
		if p != PhaseFeature {
			sess.Completed[p] = true
		}
	}
	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseFeature)}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})
	if r.OK {
		t.Fatalf("expected refusal when no screenshot fact recorded; got OK=true (violations=%+v)", r.Violations)
	}
	var hit bool
	for _, v := range r.Violations {
		if v.Code == "feature-screenshot-not-captured" {
			hit = true
			break
		}
	}
	if !hit {
		t.Errorf("expected violation code feature-screenshot-not-captured among %+v", r.Violations)
	}
	if sess.Completed[PhaseFeature] {
		t.Error("phase=feature must not be marked complete when gate refuses")
	}
}
