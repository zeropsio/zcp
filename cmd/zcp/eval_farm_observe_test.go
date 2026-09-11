package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// copyDir recursively copies src to dst (test fixture setup only).
func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
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

// canonicalModelAnswer is a valid observer answer referencing step 5 (the
// forbidden zerops_import{override=true} tool step) of
// testdata/sample-run's fixture transcript, with a quote that is literally
// in that step's result text.
const canonicalModelAnswer = `{
	"headline": "Agent called the forbidden override on zerops_import despite the scenario's rule.",
	"story": {
		"task": "diagnose and fix the failing api service",
		"expected": "the agent investigates without using a forbidden override",
		"did": "the agent called zerops_import with override=true, which the scenario forbids",
		"stuck": null,
		"ending": "finished"
	},
	"goal": {"reached": "no", "why": "the service never became healthy"},
	"checks": {"judged": [{"id": "liveness/api/marker", "correct": true, "why": "the liveness probe genuinely never found the marker"}]},
	"findings": [
		{
			"severity": "high",
			"owner": "agent",
			"surface": "tool:zerops_import",
			"anchor": "forbidden call zerops_import{override=true}",
			"title": "called forbidden override despite scenario rule",
			"what": "The agent called zerops_import with override=true, which the scenario forbids.",
			"evidence": [{"step": 5, "quote": "forbidden call zerops_import{override=true}"}],
			"causedVerdict": true,
			"lookAt": "zerops_import override handling",
			"fix": "check the never-list before issuing a destructive call"
		}
	],
	"selfReview": {"accurate": "yes", "note": ""}
}`

// writeCannedClaude writes an executable fake `claude` that ignores its
// input and always prints a canned `--output-format json` result carrying
// resultText as the model's final text.
func writeCannedClaude(t *testing.T, dir, resultText string) string {
	t.Helper()
	out, err := json.Marshal(map[string]any{
		"result":         resultText,
		"total_cost_usd": 0.0456,
		"is_error":       false,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\ncat > /dev/null\nprintf '%s' " + shellQuoteObserve(string(out)) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellQuoteObserve(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// writeCannedClaudeCapturingStdin is writeCannedClaude plus dumping its
// stdin (the digest RunObserver sent it) to stdinFile, so a test can assert
// on the digest's actual rendered content, not just the observation's
// final status.
func writeCannedClaudeCapturingStdin(t *testing.T, dir, resultText, stdinFile string) string {
	t.Helper()
	out, err := json.Marshal(map[string]any{
		"result":         resultText,
		"total_cost_usd": 0.0456,
		"is_error":       false,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\ncat > " + stdinFile + "\nprintf '%s' " + shellQuoteObserve(string(out)) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

var obsIDPattern = regexp.MustCompile(`^\d{8}T\d{9}Z-claude-sonnet-5\.json$`)

// findObserverFile returns the single file written under <runDir>/observer/.
func findObserverFile(t *testing.T, runDir string) string {
	t.Helper()
	dir := filepath.Join(runDir, "observer")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	if len(entries) != 1 {
		t.Fatalf("observer dir has %d entries, want 1: %v", len(entries), entries)
	}
	if !obsIDPattern.MatchString(entries[0].Name()) {
		t.Errorf("observation filename %q doesn't match the obsId pattern", entries[0].Name())
	}
	return filepath.Join(dir, entries[0].Name())
}

// TestFarmObserve_WritesObservationAndPrintsRendering exercises the whole
// local-verb pipeline over testdata/sample-run with a fake claude: writes
// <run-dir>/observer/<obsId>.json, prints the rendering, exits 0 (status ok).
func TestFarmObserve_WritesObservationAndPrintsRendering(t *testing.T) {
	tmp := t.TempDir()
	runDir := filepath.Join(tmp, "sample-run")
	copyDir(t, filepath.Join("..", "..", "internal", "eval", "farm", "observer", "testdata", "sample-run"), runDir)

	claudePath := writeCannedClaude(t, tmp, canonicalModelAnswer)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-token")

	var exitCode int
	stdout, _ := captureOutput(t, func() {
		exitCode = runFarmObserve([]string{runDir, "--model", "claude-sonnet-5", "--claude", claudePath})
	})

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0 (status ok)", exitCode)
	}
	if !strings.Contains(stdout, "Agent called the forbidden override on zerops_import") {
		t.Errorf("stdout = %q, want the rendered headline", stdout)
	}
	if !strings.Contains(stdout, "0 of 1 quotes unverified") {
		t.Errorf("stdout = %q, want the evidence quote counted as verified", stdout)
	}

	obsPath := findObserverFile(t, runDir)
	data, err := os.ReadFile(obsPath)
	if err != nil {
		t.Fatalf("read observation file: %v", err)
	}
	var obs observer.Observation
	if err := json.Unmarshal(data, &obs); err != nil {
		t.Fatalf("parse observation file: %v", err)
	}
	if obs.Status != "ok" {
		t.Errorf("obs.Status = %q, want ok", obs.Status)
	}
	if obs.RunID != filepath.Base(runDir) {
		t.Errorf("obs.RunID = %q, want %q", obs.RunID, filepath.Base(runDir))
	}
	if obs.Source != observer.SourceLocal {
		t.Errorf("obs.Source = %q, want %q (§7.5: the local verb's own source)", obs.Source, observer.SourceLocal)
	}
	if obs.Checks.Verdict != "failed" {
		t.Errorf("obs.Checks.Verdict = %q, want %q (from meta.json.task.result, never the model)", obs.Checks.Verdict, "failed")
	}
	if len(obs.Findings) != 1 || len(obs.Findings[0].Evidence) != 1 || !obs.Findings[0].Evidence[0].Verified {
		t.Errorf("obs.Findings = %+v, want one finding with one verified evidence entry", obs.Findings)
	}
}

// TestFarmObserve_MissingTranscriptIsErrorStatusExit1 pins §7.2: a run
// bundle with no transcript.jsonl makes the observation status "error", the
// file is still written, and the process exits 1.
func TestFarmObserve_MissingTranscriptIsErrorStatusExit1(t *testing.T) {
	tmp := t.TempDir()
	runDir := filepath.Join(tmp, "missing-transcript")
	copyDir(t, filepath.Join("..", "..", "internal", "eval", "farm", "observer", "testdata", "missing-transcript"), runDir)

	var exitCode int
	stdout, _ := captureOutput(t, func() {
		exitCode = runFarmObserve([]string{runDir})
	})

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1 (status error)", exitCode)
	}
	if !strings.Contains(stdout, "Observer failed:") {
		t.Errorf("stdout = %q, want the error rendering", stdout)
	}

	obsPath := findObserverFile(t, runDir)
	data, err := os.ReadFile(obsPath)
	if err != nil {
		t.Fatalf("read observation file: %v", err)
	}
	var obs observer.Observation
	if err := json.Unmarshal(data, &obs); err != nil {
		t.Fatalf("parse observation file: %v", err)
	}
	if obs.Status != "error" {
		t.Errorf("obs.Status = %q, want error", obs.Status)
	}
	if !strings.Contains(obs.Error, "transcript") {
		t.Errorf("obs.Error = %q, want it to name transcript.jsonl", obs.Error)
	}
}

// TestFarmObserve_MissingVerificationStillObserves pins the follow-up
// correction: a run that died before the verdict freeze (execution error,
// signal kill) has no verification.json — exactly the runs worth
// observing — so its absence must NOT make the observation status
// "error". The digest's CHECKS section instead reads "(not recorded)",
// the same placeholder the optional files use, and the pipeline keeps
// going: status ok, verdict still resolved via §7.5 (summary row, else
// meta.json.task.result).
func TestFarmObserve_MissingVerificationStillObserves(t *testing.T) {
	tmp := t.TempDir()
	runDir := filepath.Join(tmp, "missing-verification")
	copyDir(t, filepath.Join("..", "..", "internal", "eval", "farm", "observer", "testdata", "missing-verification"), runDir)

	stdinFile := filepath.Join(tmp, "claude-stdin.txt")
	claudePath := writeCannedClaudeCapturingStdin(t, tmp, canonicalModelAnswer, stdinFile)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "test-token")

	var exitCode int
	captureOutput(t, func() {
		exitCode = runFarmObserve([]string{runDir, "--model", "claude-sonnet-5", "--claude", claudePath})
	})

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0 (status ok — a missing verification.json is not fatal)", exitCode)
	}

	digestSent, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatalf("read captured stdin (the digest sent to claude): %v", err)
	}
	if !strings.Contains(string(digestSent), "=== CHECKS ===\n(not recorded)") {
		t.Errorf("digest sent to claude = %q, want the CHECKS section to read \"(not recorded)\"", digestSent)
	}

	obsPath := findObserverFile(t, runDir)
	data, err := os.ReadFile(obsPath)
	if err != nil {
		t.Fatalf("read observation file: %v", err)
	}
	var obs observer.Observation
	if err := json.Unmarshal(data, &obs); err != nil {
		t.Fatalf("parse observation file: %v", err)
	}
	if obs.Status != "ok" {
		t.Errorf("obs.Status = %q, want ok", obs.Status)
	}
	if obs.Checks.Verdict != "failed" {
		t.Errorf("obs.Checks.Verdict = %q, want %q (from meta.json.task.result, §7.5)", obs.Checks.Verdict, "failed")
	}
}
