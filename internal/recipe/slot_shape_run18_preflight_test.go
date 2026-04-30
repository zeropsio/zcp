package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// slot_shape_run18_preflight_test.go — Run-18 §5.4 validator calibration.
//
// Two-axis calibration following docs/zcprecipator3/content-research.md:
//
//  1. Reference recipes (laravel-jetstream-app human-authored,
//     laravel-showcase-app early-flow output) are the readability +
//     mechanism-density floors. The validator must fire ZERO refusals
//     on their KB. Any FP indicates the regex is too coarse.
//
//  2. The run-17 corpus is the known-leaky bed — bullets that survived
//     to publication despite brief teaching. The validator must fire
//     refusals across the §3.1 check categories (self-inflicted, slug
//     citation, UI noun, scaffold path) above calibrated minimums.
//
// The test does NOT hardcode per-bullet outcomes — that's framework-
// fragile. It asserts: "zero on the bar, ≥ N on the leaks, categorized."
//
// SKIPS when corpora are absent.

const (
	run17Root           = "../../docs/zcprecipator3/runs/17"
	laravelJetstreamApp = "/Users/fxck/www/laravel-jetstream-app"
	laravelShowcaseApp  = "/Users/fxck/www/laravel-showcase-app"
)

// TestRun18Validator_ReferenceRecipes_ZeroFalsePositives — the
// laravel-jetstream + laravel-showcase apps-repo READMEs are the
// readability + density floor. The validator must not fire on them.
func TestRun18Validator_ReferenceRecipes_ZeroFalsePositives(t *testing.T) {
	t.Parallel()

	for _, ref := range []struct {
		name string
		path string
	}{
		{"laravel-jetstream-app", filepath.Join(laravelJetstreamApp, "README.md")},
		{"laravel-showcase-app", filepath.Join(laravelShowcaseApp, "README.md")},
	} {
		t.Run(ref.name, func(t *testing.T) {
			t.Parallel()
			if _, err := os.Stat(ref.path); err != nil {
				t.Skipf("reference %s absent (%v)", ref.name, err)
			}
			bullets, err := loadKBBullets(ref.path)
			if err != nil {
				t.Fatalf("load %s: %v", ref.name, err)
			}
			if len(bullets) == 0 {
				// Reference recipes may use an alternate KB structure
				// (laravel-jetstream uses ### H3 + paragraph form,
				// laravel-showcase uses bullets). Both are valid per
				// content-research.md. Skip when no bullets — the
				// bullet-shape validator simply doesn't apply.
				t.Skipf("%s KB uses non-bullet structure; bullet validator out of scope", ref.name)
			}
			t.Logf("loaded %d bullets from %s", len(bullets), ref.name)
			for i, b := range bullets {
				refusals := kbBulletAuthoringRefusals(b.stem, b.body)
				if len(refusals) > 0 {
					t.Errorf("[%s bullet #%d] reference recipe should not fire any validator refusal but did:\n  stem: %s\n  body[:200]: %s\n  refusals: %v",
						ref.name, i+1, b.stem, truncForLog(b.body, 200), refusals)
				}
			}
		})
	}
}

// TestRun18Validator_Run17Corpus_FiresOnLeaks — the run-17 corpus has
// known anti-pattern leaks per docs/zcprecipator3/plans/run-18-prep.md
// §1.2. The validator must fire across the §3.1 check categories
// above calibrated minimums.
//
// Minimums are read from the published READMEs; tightening them is a
// signal that the validator broadened or the corpus changed. Loosening
// them is a signal that the validator stopped catching a class.
func TestRun18Validator_Run17Corpus_FiresOnLeaks(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat(filepath.Join(run17Root, "apidev/README.md")); err != nil {
		t.Skipf("run-17 corpus absent (%v) — skipping", err)
	}

	counts := map[string]int{}
	totalBullets := 0
	for _, cb := range []string{"apidev", "appdev", "workerdev"} {
		bullets, err := loadKBBullets(filepath.Join(run17Root, cb, "README.md"))
		if err != nil {
			t.Fatalf("load %s: %v", cb, err)
		}
		totalBullets += len(bullets)
		for _, b := range bullets {
			for _, msg := range kbBulletAuthoringRefusals(b.stem, b.body) {
				counts[categorizeKBRefusal(msg)]++
			}
		}
	}
	t.Logf("run-17 KB bullets scanned: %d", totalBullets)
	t.Logf("refusal counts by category: %v", counts)

	// Calibrated minimums per inspection of run-17 KB sections only
	// (the IG-body slug-trailing instances are caught by IG checks,
	// not these counts). Adjustments to these numbers are signal of
	// validator drift, not test maintenance:
	//   self-inflicted-defense ≥ 2 (appdev /health, workerdev run.ports)
	//   self-inflicted-deployfiles ≥ 1 (appdev Self-deploy wipes)
	//   scaffold-ui-noun ≥ 2 (apidev cache-demo + queue panel,
	//                          appdev tab + Panels)
	//   scaffold-sveltekit ≥ 2 (appdev +server.js variants)
	//   scaffold-wildcard-proxy ≥ 1 (appdev /api/[...path])
	//   slug-trailing ≥ 2 (KB-only — appdev #4 + #6)
	//   slug-backtick ≥ 5 (KB-only known-slug backticks)
	for class, min := range map[string]int{
		"self-inflicted-defense":     2,
		"self-inflicted-deployfiles": 1,
		"scaffold-ui-noun":           2,
		"scaffold-sveltekit":         2,
		"scaffold-wildcard-proxy":    1,
		"slug-trailing":              2,
		"slug-backtick":              5,
	} {
		if got := counts[class]; got < min {
			t.Errorf("validator under-fires on %q: got %d, expected ≥ %d (run-17 corpus inspection)",
				class, got, min)
		}
	}
}

// TestRun18Validator_IGFusion_OnRun17 — the IG fusion check is plan-
// scoped (needs the managed-service hostname set). Asserts that AT
// LEAST one IG slot in the run-17 corpus fires the fusion refusal,
// with the matched body containing multiple managed-service hostnames.
func TestRun18Validator_IGFusion_OnRun17(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat(filepath.Join(run17Root, "environments/plan.json")); err != nil {
		t.Skipf("run-17 plan.json absent (%v) — skipping", err)
	}

	plan, err := ReadPlan(filepath.Join(run17Root, "environments"))
	if err != nil {
		t.Fatalf("ReadPlan: %v", err)
	}
	hostnames := managedServiceHostnames(plan)
	if len(hostnames) < 2 {
		t.Fatalf("expected ≥ 2 managed-service hostnames in plan, got %d (%v)", len(hostnames), hostnames)
	}

	totalFusionFires := 0
	totalIGItems := 0
	for _, cb := range []string{"apidev", "appdev", "workerdev"} {
		body, err := os.ReadFile(filepath.Join(run17Root, cb, "README.md"))
		if err != nil {
			t.Fatalf("read %s: %v", cb, err)
		}
		items := extractAllIGItemBodies(string(body))
		totalIGItems += len(items)
		for i, ig := range items {
			refusals := igSlotAuthoringRefusals(ig, hostnames)
			for _, r := range refusals {
				if strings.Contains(r, "fuses") {
					t.Logf("%s IG #%d fusion refusal: %s", cb, i+1, r)
					totalFusionFires++
				}
			}
		}
	}
	t.Logf("run-17 IG items scanned: %d", totalIGItems)
	t.Logf("fusion refusals fired: %d", totalFusionFires)

	if totalFusionFires < 1 {
		t.Errorf("expected ≥ 1 IG fusion refusal across run-17, got 0")
	}
}

// TestRun18Validator_IGFusion_NoFalsePositive_OnReferenceRecipes — the
// laravel-showcase IG must not fire the fusion check. Reference IG
// items are one-mechanism-per-item by construction.
func TestRun18Validator_IGFusion_NoFalsePositive_OnReferenceRecipes(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat(laravelShowcaseApp); err != nil {
		t.Skipf("reference recipe absent (%v)", err)
	}

	// Laravel showcase managed-service set per its zerops.yaml.
	hostnames := []string{"db", "cache", "broker", "storage", "search", "mailpit"}
	body, err := os.ReadFile(filepath.Join(laravelShowcaseApp, "README.md"))
	if err != nil {
		t.Fatalf("read laravel-showcase: %v", err)
	}
	items := extractAllIGItemBodies(string(body))
	t.Logf("laravel-showcase IG items: %d", len(items))
	for i, ig := range items {
		refusals := igSlotAuthoringRefusals(ig, hostnames)
		for _, r := range refusals {
			if strings.Contains(r, "fuses") {
				t.Errorf("laravel-showcase IG #%d falsely fired fusion: %s", i+1, r)
			}
		}
	}
}

// kbBullet captures one parsed bullet from a published KB section.
type kbBullet struct {
	stem string
	body string
}

// loadKBBullets extracts bullets from the engine-stamped knowledge-base
// extract region of a published apps-repo README. Markers wrap the KB
// content — those are the authoritative boundaries regardless of
// internal heading structure.
func loadKBBullets(path string) ([]kbBullet, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(body)

	const startMarker = "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->"
	const endMarker = "<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->"
	_, after, ok := strings.Cut(text, startMarker)
	if !ok {
		return nil, nil
	}
	region := after
	if before, _, ok := strings.Cut(after, endMarker); ok {
		region = before
	}

	var out []kbBullet
	for ln := range strings.SplitSeq(region, "\n") {
		trim := strings.TrimLeft(ln, " \t")
		if !strings.HasPrefix(trim, "- **") {
			continue
		}
		rest := strings.TrimPrefix(trim, "- ")
		m := kbStemBoldRE.FindStringSubmatch(rest)
		stem := ""
		if len(m) >= 2 {
			stem = m[1]
		}
		out = append(out, kbBullet{stem: stem, body: rest})
	}
	return out, nil
}

// extractAllIGItemBodies returns the body of every "### N. ..." IG item
// in the README, one per item, in document order.
func extractAllIGItemBodies(text string) []string {
	lines := strings.Split(text, "\n")
	var starts []int
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "### ") {
			continue
		}
		rest := strings.TrimPrefix(t, "### ")
		if len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 {
		return nil
	}
	var out []string
	for i, start := range starts {
		end := len(lines)
		if i+1 < len(starts) {
			end = starts[i+1]
		} else {
			// Stop at the next H2/H3 (Gotchas / next section) past this
			// IG item.
			for j := start + 1; j < len(lines); j++ {
				t := strings.TrimSpace(lines[j])
				if strings.HasPrefix(t, "## ") {
					end = j
					break
				}
				if rest, ok := strings.CutPrefix(t, "### "); ok {
					if len(rest) == 0 || rest[0] < '0' || rest[0] > '9' {
						end = j
						break
					}
				}
			}
		}
		out = append(out, strings.Join(lines[start:end], "\n"))
	}
	return out
}

// categorizeKBRefusal maps a kbBulletAuthoringRefusals message to a
// stable category tag for count-based assertions. Substring-matching
// the refusal prose keeps the producer (slot_shape_authoring.go) and
// consumer (this test) decoupled — adding a new check kind is a
// one-line edit here, not a contract change.
func categorizeKBRefusal(msg string) string {
	switch {
	case strings.Contains(msg, "deployFiles narrowing"):
		return "self-inflicted-deployfiles"
	case strings.Contains(msg, "we chose X over Y"):
		return "self-inflicted-choice"
	case strings.Contains(msg, "design choice"):
		return "self-inflicted-defense"
	case strings.Contains(msg, "SvelteKit route"):
		return "scaffold-sveltekit"
	case strings.Contains(msg, "/api/[...path]"):
		return "scaffold-wildcard-proxy"
	case strings.Contains(msg, "UI element"):
		return "scaffold-ui-noun"
	case strings.Contains(msg, "trailing citation label"):
		return "slug-trailing"
	case strings.Contains(msg, "backticked"):
		return "slug-backtick"
	}
	return "uncategorized"
}

func truncForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
