package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
)

// writeManifest is a test helper: writes a content manifest JSON file at
// projectRoot/ZCP_CONTENT_MANIFEST.json with the given payload.
func writeManifest(t *testing.T, projectRoot string, payload any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(projectRoot, "ZCP_CONTENT_MANIFEST.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

// writeManifestRaw writes raw bytes to the manifest path — used to plant
// malformed JSON for the Sub-check A failure path.
func writeManifestRaw(t *testing.T, projectRoot string, raw string) {
	t.Helper()
	path := filepath.Join(projectRoot, "ZCP_CONTENT_MANIFEST.json")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("write manifest raw: %v", err)
	}
}

// writeFactsLog writes a JSONL facts log with the given FactRecord titles.
// Each entry uses a real Type so ops.AppendFact's validation passes.
func writeFactsLog(t *testing.T, path string, titles []string) {
	t.Helper()
	for _, title := range titles {
		if err := ops.AppendFact(path, ops.FactRecord{
			Type:  ops.FactTypeGotchaCandidate,
			Title: title,
		}); err != nil {
			t.Fatalf("append fact %q: %v", title, err)
		}
	}
}

// runManifestCheck is the shared test harness: invokes
// checkWriterContentManifest with the given inputs and returns the check
// slice as []workflowStepCheckShim for consistent assertion helpers.
func runManifestCheck(t *testing.T, projectRoot string, readmesByHost map[string]string, factsLogPath string) []workflowStepCheckShim {
	t.Helper()
	checks := checkWriterContentManifest(t.Context(), projectRoot, readmesByHost, factsLogPath)
	out := make([]workflowStepCheckShim, 0, len(checks))
	for _, c := range checks {
		out = append(out, workflowStepCheckShim{Name: c.Name, Status: c.Status, Detail: c.Detail})
	}
	return out
}

// ── Sub-check A: presence + parse ───────────────────────────────────────

func TestContentManifest_MissingFile_Fails(t *testing.T) {
	t.Parallel()
	got := runManifestCheck(t, t.TempDir(), nil, "")
	c := findCheckByName(got, "writer_content_manifest_exists")
	if c == nil || c.Status != "fail" {
		t.Fatalf("expected fail for missing file, got %+v", c)
	}
	if !strings.Contains(c.Detail, "content manifest missing") {
		t.Errorf("expected 'content manifest missing' in detail: %s", c.Detail)
	}
}

func TestContentManifest_MalformedJSON_Fails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeManifestRaw(t, dir, "{not valid json")
	got := runManifestCheck(t, dir, nil, "")
	c := findCheckByName(got, "writer_content_manifest_valid")
	if c == nil || c.Status != "fail" {
		t.Fatalf("expected fail for malformed JSON, got %+v", c)
	}
}

func TestContentManifest_ValidMinimal_Passes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeManifest(t, dir, map[string]any{"version": 1, "facts": []any{}})
	got := runManifestCheck(t, dir, nil, "")
	for _, name := range []string{"writer_content_manifest_exists", "writer_content_manifest_valid"} {
		c := findCheckByName(got, name)
		if c == nil || c.Status != "pass" {
			t.Errorf("%s: expected pass, got %+v", name, c)
		}
	}
}

// ── Sub-check B: classification consistency ─────────────────────────────

func manifestWith(facts []map[string]string) map[string]any {
	return map[string]any{"version": 1, "facts": facts}
}

func TestContentManifest_DiscardClassRoutedToGotcha_FailsWithoutReason(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeManifest(t, dir, manifestWith([]map[string]string{{
		"fact_title":      "healthCheck path must have a GET handler",
		"classification":  "framework-quirk",
		"routed_to":       "apidev-gotcha",
		"override_reason": "",
	}}))
	got := runManifestCheck(t, dir, nil, "")
	c := findCheckByName(got, "writer_discard_classification_consistency")
	if c == nil || c.Status != "fail" {
		t.Fatalf("expected fail, got %+v", c)
	}
	if !strings.Contains(c.Detail, "framework-quirk") {
		t.Errorf("expected detail to name the classification, got: %s", c.Detail)
	}
}

func TestContentManifest_DiscardClassRoutedToGotcha_PassesWithReason(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeManifest(t, dir, manifestWith([]map[string]string{{
		"fact_title":      "healthCheck path must have a GET handler",
		"classification":  "framework-quirk",
		"routed_to":       "apidev-gotcha",
		"override_reason": "reframed from scaffold-internal concern to porter-facing symptom",
	}}))
	got := runManifestCheck(t, dir, nil, "")
	c := findCheckByName(got, "writer_discard_classification_consistency")
	if c == nil || c.Status != "pass" {
		t.Fatalf("expected pass with override_reason, got %+v", c)
	}
}

func TestContentManifest_DiscardClassRoutedToDiscarded_Passes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeManifest(t, dir, manifestWith([]map[string]string{{
		"fact_title":      "Multer rejects uploads with FormData Content-Type injection",
		"classification":  "framework-quirk",
		"routed_to":       "discarded",
		"override_reason": "",
	}}))
	got := runManifestCheck(t, dir, nil, "")
	c := findCheckByName(got, "writer_discard_classification_consistency")
	if c == nil || c.Status != "pass" {
		t.Fatalf("expected pass when routed to discarded, got %+v", c)
	}
}

func TestContentManifest_NonDiscardClass_NoEnforcement(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// invariant routed to apidev-gotcha without override_reason is fine —
	// the consistency check only fires on framework-quirk/library-meta/
	// self-inflicted classifications.
	writeManifest(t, dir, manifestWith([]map[string]string{{
		"fact_title":      "execOnce fires once per Zerops service, not once per container",
		"classification":  "invariant",
		"routed_to":       "apidev-gotcha",
		"override_reason": "",
	}}))
	got := runManifestCheck(t, dir, nil, "")
	c := findCheckByName(got, "writer_discard_classification_consistency")
	if c == nil || c.Status != "pass" {
		t.Fatalf("expected pass for non-discard class, got %+v", c)
	}
}

// ── Sub-check C: manifest honesty (Jaccard gotcha-stem match) ──────────

const kbReadmeWithHealthCheckGotcha = `# apidev

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->

## Gotchas

- **Feature-sweep rejects the recipe if the plan.healthCheck path lacks a bare GET** — the sweep issues ` + "`curl -X GET <path>`" + ` and any non-2xx response fails the recipe.
- **Second gotcha stem** — about something else entirely.

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`

func TestContentManifest_Honesty_DiscardedButGotchaShipped_Fails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeManifest(t, dir, manifestWith([]map[string]string{{
		"fact_title":      "plan.healthCheck path must have a GET handler — feature sweep rejects 4xx",
		"classification":  "framework-quirk",
		"routed_to":       "discarded",
		"override_reason": "",
	}}))
	got := runManifestCheck(t, dir, map[string]string{"apidev": kbReadmeWithHealthCheckGotcha}, "")
	c := findCheckByName(got, "writer_manifest_honesty_discarded_as_gotcha")
	if c == nil || c.Status != "fail" {
		t.Fatalf("expected fail — fact marked discarded but similar gotcha shipped: %+v", c)
	}
	if !strings.Contains(c.Detail, "apidev") {
		t.Errorf("expected detail to name the host, got: %s", c.Detail)
	}
}

func TestContentManifest_Honesty_DiscardedAndNoMatch_Passes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeManifest(t, dir, manifestWith([]map[string]string{{
		"fact_title":      "completely unrelated vocabulary here",
		"classification":  "framework-quirk",
		"routed_to":       "discarded",
		"override_reason": "",
	}}))
	got := runManifestCheck(t, dir, map[string]string{"apidev": kbReadmeWithHealthCheckGotcha}, "")
	c := findCheckByName(got, "writer_manifest_honesty_discarded_as_gotcha")
	if c == nil || c.Status != "pass" {
		t.Fatalf("expected pass — no stem overlap, got %+v", c)
	}
}

// TestContentManifest_Honesty_v29HealthCheck pins the exact Jaccard case
// from the v29 audit: writer claims DISCARD, published README ships the same
// concept with a reframed stem. Jaccard similarity on stop-word-stripped
// tokens should fire at the 0.3 threshold.
func TestContentManifest_Honesty_v29HealthCheck(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeManifest(t, dir, manifestWith([]map[string]string{{
		"fact_title":      "plan.healthCheck path must have a GET handler — feature sweep rejects 4xx",
		"classification":  "framework-quirk",
		"routed_to":       "discarded",
		"override_reason": "",
	}}))
	got := runManifestCheck(t, dir, map[string]string{"apidev": kbReadmeWithHealthCheckGotcha}, "")
	c := findCheckByName(got, "writer_manifest_honesty_discarded_as_gotcha")
	if c == nil || c.Status != "fail" {
		t.Fatalf("expected v29 healthCheck case to fail Jaccard threshold: %+v", c)
	}
}

// ── Sub-check D: manifest completeness (facts log cross-check) ─────────

func TestContentManifest_Completeness_AllFactsPresent_Passes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	factsPath := filepath.Join(dir, "facts.jsonl")
	titles := []string{"fact A", "fact B", "fact C"}
	writeFactsLog(t, factsPath, titles)
	facts := make([]map[string]string, 0, len(titles))
	for _, title := range titles {
		facts = append(facts, map[string]string{
			"fact_title":      title,
			"classification":  "invariant",
			"routed_to":       "apidev-gotcha",
			"override_reason": "",
		})
	}
	writeManifest(t, dir, manifestWith(facts))
	got := runManifestCheck(t, dir, nil, factsPath)
	c := findCheckByName(got, "writer_manifest_completeness")
	if c == nil || c.Status != "pass" {
		t.Fatalf("expected pass when every fact has an entry, got %+v", c)
	}
}

func TestContentManifest_Completeness_EmptyManifestNonEmptyLog_Fails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	factsPath := filepath.Join(dir, "facts.jsonl")
	writeFactsLog(t, factsPath, []string{"fact A", "fact B"})
	writeManifest(t, dir, map[string]any{"version": 1, "facts": []any{}})
	got := runManifestCheck(t, dir, nil, factsPath)
	c := findCheckByName(got, "writer_manifest_completeness")
	if c == nil || c.Status != "fail" {
		t.Fatalf("expected fail for empty manifest with populated facts log (deceptive-empty attack), got %+v", c)
	}
	if !strings.Contains(c.Detail, "fact A") || !strings.Contains(c.Detail, "fact B") {
		t.Errorf("expected missing-title detail to list both titles, got: %s", c.Detail)
	}
}

func TestContentManifest_Completeness_PartialManifest_Fails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	factsPath := filepath.Join(dir, "facts.jsonl")
	writeFactsLog(t, factsPath, []string{"fact A", "fact B", "fact C"})
	writeManifest(t, dir, manifestWith([]map[string]string{
		{"fact_title": "fact A", "classification": "invariant", "routed_to": "apidev-gotcha"},
		// missing fact B and fact C
	}))
	got := runManifestCheck(t, dir, nil, factsPath)
	c := findCheckByName(got, "writer_manifest_completeness")
	if c == nil || c.Status != "fail" {
		t.Fatalf("expected fail for partial manifest, got %+v", c)
	}
	for _, want := range []string{"fact B", "fact C"} {
		if !strings.Contains(c.Detail, want) {
			t.Errorf("expected missing title %q in detail, got: %s", want, c.Detail)
		}
	}
}

func TestContentManifest_Completeness_FactsLogMissing_SkipsGracefully(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeManifest(t, dir, map[string]any{"version": 1, "facts": []any{}})
	got := runManifestCheck(t, dir, nil, filepath.Join(dir, "nonexistent-facts.jsonl"))
	c := findCheckByName(got, "writer_manifest_completeness")
	if c == nil || c.Status != "pass" {
		t.Fatalf("expected pass (graceful skip) when facts log is missing, got %+v", c)
	}
}

func TestContentManifest_Completeness_EmptyLog_Passes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	factsPath := filepath.Join(dir, "facts.jsonl")
	// touch an empty file
	if err := os.WriteFile(factsPath, nil, 0o644); err != nil {
		t.Fatalf("touch: %v", err)
	}
	writeManifest(t, dir, map[string]any{"version": 1, "facts": []any{}})
	got := runManifestCheck(t, dir, nil, factsPath)
	c := findCheckByName(got, "writer_manifest_completeness")
	if c == nil || c.Status != "pass" {
		t.Fatalf("expected pass for empty log + empty manifest, got %+v", c)
	}
}

// TestContentManifest_EmptyPath_SkipsCompleteness — when the caller plumbs an
// empty factsLogPath (test context, nil resolver), sub-check D must pass with
// a skip note rather than fail.
func TestContentManifest_EmptyPath_SkipsCompleteness(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeManifest(t, dir, map[string]any{"version": 1, "facts": []any{}})
	got := runManifestCheck(t, dir, nil, "")
	c := findCheckByName(got, "writer_manifest_completeness")
	if c == nil || c.Status != "pass" {
		t.Fatalf("expected pass with empty factsLogPath (test context), got %+v", c)
	}
}
