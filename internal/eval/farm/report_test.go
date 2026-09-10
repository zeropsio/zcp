package farm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile is a small test helper: write content to dir/rel, creating parent
// directories as needed.
func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// minimalRunBundle builds the smallest run bundle report.go's tree-digest
// recompute needs: a results/ dir and a capture/ dir (with a manifest and
// provider.jsonl content it does not need to parse for these tests, since
// they cover the digest/pin/done.json gate before any BuildBehavioralReport
// call would run). done.json is written last, matching FM-3/FM-4/FM-5.
func minimalRunBundle(t *testing.T, runDir string) {
	t.Helper()
	writeFile(t, runDir, "results/a.txt", "result-a")
	writeFile(t, runDir, "capture/b.txt", "capture-b")
}

func writeDoneJSON(t *testing.T, runDir, runID string, evaluatorSHA, candidateSHA string, resultsDigest, captureDigest string) {
	t.Helper()
	doc := `{"runId":"` + runID + `","scenarioId":"acceptance-node-postgres-record",` +
		`"parts":{"results":{"treeDigest":"` + resultsDigest + `"},"capture":{"treeDigest":"` + captureDigest + `"}},` +
		`"evaluatorSha256":"` + evaluatorSHA + `","candidateSha256":"` + candidateSHA + `"}`
	writeFile(t, runDir, "done.json", doc)
}

// TestFarmReport_MissingDoneJSON_BlockedNoBundle pins FM-3: a run project
// that shows no done.json at all is "blocked: no bundle", not a crash and
// not silently skipped.
func TestFarmReport_MissingDoneJSON_BlockedNoBundle(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	minimalRunBundle(t, runDir)
	// no done.json written

	outcome := ReportRun(runDir, "")
	if outcome.Verdict != VerdictBlocked {
		t.Fatalf("Verdict = %q, want %q", outcome.Verdict, VerdictBlocked)
	}
	if !strings.Contains(outcome.Reason, "no bundle") {
		t.Fatalf("Reason = %q, want it to mention %q", outcome.Reason, "no bundle")
	}
}

// TestFarmReport_PartDigestMismatch_Blocked pins FM-5/FM-38: a part whose
// recomputed tree digest disagrees with done.json's claim is blocked, not
// failed — the evidence, not the run's own outcome, is in question.
func TestFarmReport_PartDigestMismatch_Blocked(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	minimalRunBundle(t, runDir)
	resultsDigest, err := TreeDigest(filepath.Join(runDir, "results"))
	if err != nil {
		t.Fatalf("TreeDigest(results): %v", err)
	}
	captureDigest, err := TreeDigest(filepath.Join(runDir, "capture"))
	if err != nil {
		t.Fatalf("TreeDigest(capture): %v", err)
	}
	writeDoneJSON(t, runDir, "run-mismatch", "eval-sha", "cand-sha", resultsDigest, captureDigest)

	// Corrupt one byte of an evidence file AFTER done.json was written
	// (FM-5: write order is never trusted — the report recomputes).
	writeFile(t, runDir, "results/a.txt", "tampered!")

	outcome := ReportRun(runDir, "")
	if outcome.Verdict != VerdictBlocked {
		t.Fatalf("Verdict = %q, want %q", outcome.Verdict, VerdictBlocked)
	}
	if !strings.Contains(outcome.Reason, "digest") {
		t.Fatalf("Reason = %q, want it to mention a digest mismatch", outcome.Reason)
	}
}

// TestFarmReport_ForeignEvaluator_Blocked pins FM-35: a bundle whose
// evaluatorSha256 differs from the farm's current pin is blocked in report,
// never silently graded under the wrong evaluator and never failed.
func TestFarmReport_ForeignEvaluator_Blocked(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	minimalRunBundle(t, runDir)
	resultsDigest, err := TreeDigest(filepath.Join(runDir, "results"))
	if err != nil {
		t.Fatalf("TreeDigest(results): %v", err)
	}
	captureDigest, err := TreeDigest(filepath.Join(runDir, "capture"))
	if err != nil {
		t.Fatalf("TreeDigest(capture): %v", err)
	}
	writeDoneJSON(t, runDir, "run-foreign", "evaluator-abc", "cand-sha", resultsDigest, captureDigest)

	outcome := ReportRun(runDir, "evaluator-DIFFERENT")
	if outcome.Verdict != VerdictBlocked {
		t.Fatalf("Verdict = %q, want %q", outcome.Verdict, VerdictBlocked)
	}
	if !strings.Contains(outcome.Reason, "evaluator") {
		t.Fatalf("Reason = %q, want it to mention the evaluator pin", outcome.Reason)
	}
}

// TestFarmReport_LegacyBundle_PrintsUnpinnedNotBlocked pins FM-6: a bundle
// predating the digest fields is "unpinned", never blocked and never a
// fabricated pass.
func TestFarmReport_LegacyBundle_PrintsUnpinnedNotBlocked(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	minimalRunBundle(t, runDir)
	// Legacy done.json: no parts, no digest fields at all.
	writeFile(t, runDir, "done.json", `{"runId":"run-legacy","scenarioId":"acceptance-node-postgres-record"}`)

	outcome := ReportRun(runDir, "some-pin")
	if outcome.Verdict != VerdictUnpinned {
		t.Fatalf("Verdict = %q, want %q", outcome.Verdict, VerdictUnpinned)
	}
}

// TestBundle_NoKnownSecretValue pins docs/spec-eval-farm.md §1.3 FM-7's
// upload-side redaction promise on the bundle a pulled batch dir holds: no
// file under a run bundle contains any value the wrapper's own credential
// set carried. Proven both ways — over a clean bundle (no known secret
// value appears) and over a deliberately poisoned copy (the same check
// fails), so a scan that always reports "clean" cannot pass this test.
func TestBundle_NoKnownSecretValue(t *testing.T) {
	t.Parallel()
	knownSecrets := []string{"sk-ant-fake0123456789", "ghp_fakefakefakefakefakefakefakefake0000"}

	clean := t.TempDir()
	writeFile(t, clean, "results/a.txt", "ordinary evidence, no secret here")
	writeFile(t, clean, "capture/manifest.json", `{"status":"complete"}`)

	if err := BundleNoKnownSecretValue(clean, knownSecrets); err != nil {
		t.Fatalf("clean bundle: NoKnownSecretValue returned an error: %v", err)
	}

	poisoned := t.TempDir()
	writeFile(t, poisoned, "results/a.txt", "ordinary evidence")
	writeFile(t, poisoned, "capture/leak.txt", "Authorization: Bearer sk-ant-fake0123456789")

	if err := BundleNoKnownSecretValue(poisoned, knownSecrets); err == nil {
		t.Fatal("poisoned bundle: NoKnownSecretValue returned nil, want an error naming the leaked value")
	}
}

// TestFarmReport_Run5Golden_ByteIdentical pins docs/spec-eval-farm.md §5.2
// FM-37: report over the preserved (redacted/reduced) S5 run-5 bundle
// reproduces that run's known-good single-run report byte-for-byte. run5 has
// no done.json (it predates FM-4's digest fields — FM-6), so the bundle's
// own verdict is "unpinned"; report.golden.txt is the §10.5 report body the
// grading still produces underneath that label, verified byte-identical to
// the real run's report.txt by generating it from the same reduced capture
// window before this test existed (see the S6 report for how).
func TestFarmReport_Run5Golden_ByteIdentical(t *testing.T) {
	t.Parallel()
	outcome := ReportRun(filepath.Join("testdata", "run5"), "")
	if outcome.Verdict != VerdictUnpinned {
		t.Fatalf("Verdict = %q, want %q (run5 predates FM-4's digest fields)", outcome.Verdict, VerdictUnpinned)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "run5", "report.golden.txt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if outcome.ReportText != string(want) {
		t.Fatalf("report text does not match report.golden.txt byte-for-byte\ngot:\n%s\nwant:\n%s", outcome.ReportText, want)
	}
}

// writeDoneJSONWithExecution writes a done.json carrying a specific
// runnerDimensions.execution value (D9's signal-kill shape) alongside the
// usual pinned digests.
func writeDoneJSONWithExecution(t *testing.T, runDir, runID, execution string, evaluatorSHA, candidateSHA string, resultsDigest, captureDigest string) {
	t.Helper()
	doc := `{"runId":"` + runID + `","scenarioId":"acceptance-node-postgres-record",` +
		`"runnerDimensions":{"execution":"` + execution + `","task":"unknown","taskEnd":"unknown"},` +
		`"parts":{"results":{"treeDigest":"` + resultsDigest + `"},"capture":{"treeDigest":"` + captureDigest + `"}},` +
		`"evaluatorSha256":"` + evaluatorSHA + `","candidateSha256":"` + candidateSHA + `"}`
	writeFile(t, runDir, "done.json", doc)
}

// TestFarmReport_SignalKilledBundle_FailedNotBlocked pins D9: a bundle
// killed mid-run (results has whatever meta.json the child managed to
// write, capture has no capture/eval/<runId>/<scenarioId> directory yet —
// the evaluator never got that far) is graded "failed" straight from
// done.json's own runnerDimensions.execution, never "blocked" — the run's
// outcome is already known (it was killed), it is not evidence in
// question. Before this fix, ReportRun tried to build the single-run
// report anyway, failed to find capture/eval/*, and returned "blocked:
// could not build the single-run report: ...".
func TestFarmReport_SignalKilledBundle_FailedNotBlocked(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	writeFile(t, runDir, "results/meta.json", `{"scenarioId":"acceptance-node-postgres-record"}`)
	writeFile(t, runDir, "capture/manifest.json", `{"status":"complete"}`)
	writeFile(t, runDir, "capture/provider.jsonl", `{"line":"partial"}`)
	writeFile(t, runDir, "capture/lifecycle.jsonl", `{"line":"partial"}`)
	writeFile(t, runDir, "capture/mcp/zcp-1.jsonl", `{"line":"partial"}`)
	// Deliberately no capture/eval/** — the evaluator never reached its
	// own report-writing step before it was signal-killed.

	resultsDigest, err := TreeDigest(filepath.Join(runDir, "results"))
	if err != nil {
		t.Fatalf("TreeDigest(results): %v", err)
	}
	captureDigest, err := TreeDigest(filepath.Join(runDir, "capture"))
	if err != nil {
		t.Fatalf("TreeDigest(capture): %v", err)
	}
	writeDoneJSONWithExecution(t, runDir, "run-killed", "error: killed by signal 9", "eval-sha", "cand-sha", resultsDigest, captureDigest)

	outcome := ReportRun(runDir, "")
	if outcome.Verdict != VerdictFailed {
		t.Fatalf("Verdict = %q, want %q (Reason: %s)", outcome.Verdict, VerdictFailed, outcome.Reason)
	}
	if !strings.Contains(outcome.Reason, "killed by signal") {
		t.Errorf("Reason = %q, want it to name the signal-kill execution", outcome.Reason)
	}
	if outcome.Done == nil || outcome.Done.RunID != "run-killed" {
		t.Errorf("Done = %+v, want the parsed done.json carried through", outcome.Done)
	}
}

// TestFarmReport_RedactedBundle_ManifestConsistent_Grades pins the D8 fix
// end-to-end at the report layer: a bundle whose capture/manifest.json was
// rewritten (by the wrapper's update_capture_manifest, eval/farm/
// wrapper.sh) to match its post-redaction files, with done.json's part
// digests computed over that same post-redaction tree, must grade normally
// — never "blocked: part ... tree digest ... does not match" (FM-5/FM-38)
// and never a manifest-size-mismatch report-build failure (internal/
// capture/read_manifest_file.go's size check, the exact live failure D8
// names).
func TestFarmReport_RedactedBundle_ManifestConsistent_Grades(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	writeFile(t, runDir, "results/a.txt", "result-a")

	// Base this on the real run5 capture window (a valid, richly-structured
	// InspectSession-passing tree; a fabricated minimal one is too easy to
	// get subtly wrong) and apply one genuine, identity-safe redaction to
	// it: rewrite the session.start line's free-text "label" field only —
	// never sessionId or any other structural/identity field
	// InspectSession checks.
	copyDir(t, filepath.Join("testdata", "run5", "capture"), filepath.Join(runDir, "capture"))

	providerPath := filepath.Join(runDir, "capture", "provider.jsonl")
	original, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatalf("read provider.jsonl: %v", err)
	}
	lines := strings.SplitN(string(original), "\n", 2)
	if len(lines) != 2 || !strings.Contains(lines[0], `"kind":"session.start"`) {
		t.Fatalf("provider.jsonl first line = %q, want a session.start record (test fixture assumption broken)", lines[0])
	}
	if !strings.Contains(lines[0], "acceptance-node-postgres-record") {
		t.Fatalf("provider.jsonl first line = %q, want it to contain the scenario id this test redacts", lines[0])
	}
	redactedFirstLine := strings.ReplaceAll(lines[0], "acceptance-node-postgres-record", "<redacted>")
	redacted := redactedFirstLine + "\n" + lines[1]
	if redacted == string(original) {
		t.Fatalf("redaction was a no-op — test fixture assumption broken")
	}
	if err := os.WriteFile(providerPath, []byte(redacted), 0o600); err != nil {
		t.Fatalf("write redacted provider.jsonl: %v", err)
	}
	sum := sha256.Sum256([]byte(redacted))
	providerSHA := hex.EncodeToString(sum[:])

	// Patch capture/manifest.json's provider entry to the post-redaction
	// size/sha256 — exactly what the wrapper's update_capture_manifest
	// (eval/farm/wrapper.sh, D8) does.
	manifestPath := filepath.Join(runDir, "capture", "manifest.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest.json: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("parse manifest.json: %v", err)
	}
	files, _ := manifest["files"].([]any)
	patched := false
	for _, f := range files {
		entry, _ := f.(map[string]any)
		if entry["path"] == "provider.jsonl" {
			entry["sizeBytes"] = float64(len(redacted))
			entry["sha256"] = providerSHA
			patched = true
		}
	}
	if !patched {
		t.Fatalf("manifest.json has no files[] entry for provider.jsonl")
	}
	rewritten, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal patched manifest.json: %v", err)
	}
	if err := os.WriteFile(manifestPath, rewritten, 0o600); err != nil {
		t.Fatalf("write patched manifest.json: %v", err)
	}

	resultsDigest, err := TreeDigest(filepath.Join(runDir, "results"))
	if err != nil {
		t.Fatalf("TreeDigest(results): %v", err)
	}
	captureDigest, err := TreeDigest(filepath.Join(runDir, "capture"))
	if err != nil {
		t.Fatalf("TreeDigest(capture): %v", err)
	}
	writeDoneJSON(t, runDir, "run5-redacted", "eval-sha", "cand-sha", resultsDigest, captureDigest)

	outcome := ReportRun(runDir, "")
	if outcome.Verdict == VerdictBlocked {
		t.Fatalf("Verdict = %q (Reason: %s), want NOT blocked — the manifest was kept consistent with the redacted bytes", outcome.Verdict, outcome.Reason)
	}
}

// copyDir recursively copies src to dst (used to base a test fixture on the
// real run5 testdata tree instead of a hand-fabricated one).
func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatalf("copyDir(%s, %s): %v", src, dst, err)
	}
}

// TestFarmReport_AnthropicAPIKeyPresent_Blocked pins the owner decision
// (spec 79ced2cc, docs/spec-eval-farm.md FM-16): farm runs are OAuth-only,
// so a bundle whose meta.json records anthropicApiKeyPresent=true is
// blocked, regardless of whether it is otherwise pinned/legacy.
func TestFarmReport_AnthropicAPIKeyPresent_Blocked(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	writeFile(t, runDir, "capture/manifest.json", `{"status":"complete"}`)
	writeFile(t, runDir, "capture/eval/run1/scenario1/meta.json", `{"scenarioId":"scenario1","anthropicApiKeyPresent":true}`)
	writeFile(t, runDir, "done.json", `{"runId":"run-apikey","scenarioId":"scenario1"}`)

	outcome := ReportRun(runDir, "")
	if outcome.Verdict != VerdictBlocked {
		t.Fatalf("Verdict = %q, want %q", outcome.Verdict, VerdictBlocked)
	}
	if !strings.Contains(outcome.Reason, "oauth") {
		t.Fatalf("Reason = %q, want it to mention farm runs are oauth-only", outcome.Reason)
	}
}
