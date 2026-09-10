package farm

import (
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
