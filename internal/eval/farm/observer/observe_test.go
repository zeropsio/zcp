package observer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// obsCanonicalModelAnswer is a valid observer answer referencing step 5 of
// testdata/sample-run's fixture transcript, with a quote that is literally
// in that step's result text — the same fixture cmd/zcp's observe tests use
// (cmd/zcp/eval_farm_observe_test.go's canonicalModelAnswer), so both
// surfaces are proven against the one independent oracle.
const obsCanonicalModelAnswer = `{
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

// obsWriteCannedClaude writes an executable fake `claude` that ignores its
// input and always prints a canned `--output-format json` result carrying
// resultText as the model's final text.
func obsWriteCannedClaude(t *testing.T, dir, resultText string) string {
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
	script := "#!/bin/sh\ncat > /dev/null\nprintf '%s' " + obsShellQuote(string(out)) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func obsShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// TestObserve_MatchesLocalVerbOutput pins that Observe (the pipeline the
// local verb now wraps) produces the same fields the pre-move local verb
// produced over the same fixture: status ok, the rendered headline, one
// finding with one verified evidence entry, and the verdict resolved from
// meta.json.task.result ("failed", independent oracle: the fixture's own
// meta.json, read directly above rather than recomputed by Observe's own
// logic).
func TestObserve_MatchesLocalVerbOutput(t *testing.T) {
	tmp := t.TempDir()
	claudePath := obsWriteCannedClaude(t, tmp, obsCanonicalModelAnswer)

	bundle := NewDirBundle("testdata/sample-run")
	obs := Observe(context.Background(), bundle, ObserveConfig{
		RunID:      "sample-run",
		Model:      "claude-sonnet-5",
		ClaudePath: claudePath,
		OAuthToken: "test-token",
		Timeout:    time.Minute,
		Environ:    os.Environ,
	})

	if obs.Status != "ok" {
		t.Fatalf("obs.Status = %q, want ok (obs.Error = %q)", obs.Status, obs.Error)
	}
	if obs.Headline != "Agent called the forbidden override on zerops_import despite the scenario's rule." {
		t.Errorf("obs.Headline = %q, want the canned headline", obs.Headline)
	}
	if obs.Checks.Verdict != "failed" {
		t.Errorf("obs.Checks.Verdict = %q, want %q (from meta.json.task.result, never the model)", obs.Checks.Verdict, "failed")
	}
	if len(obs.Findings) != 1 || len(obs.Findings[0].Evidence) != 1 || !obs.Findings[0].Evidence[0].Verified {
		t.Errorf("obs.Findings = %+v, want one finding with one verified evidence entry", obs.Findings)
	}
}

// TestObserve_SummaryJSONOverridesMetaVerdict pins §7.5's checks.verdict
// rule: when SummaryJSON carries a row for the run, its result wins over
// meta.json.task.result ("failed" in the fixture) — here "passed", an
// independent literal distinct from the fixture's own meta.json value, so
// the assertion can't pass by accident from reading meta.json alone.
func TestObserve_SummaryJSONOverridesMetaVerdict(t *testing.T) {
	tmp := t.TempDir()
	claudePath := obsWriteCannedClaude(t, tmp, obsCanonicalModelAnswer)

	summaryJSON := []byte(`{"batch":"b","finishedAt":"2026-09-11T12:00:00Z","endedBy":"settled","runs":[{"runId":"sample-run","scenario":"sample-scenario","result":"passed"}]}`)

	bundle := NewDirBundle("testdata/sample-run")
	obs := Observe(context.Background(), bundle, ObserveConfig{
		RunID:       "sample-run",
		Model:       "claude-sonnet-5",
		ClaudePath:  claudePath,
		OAuthToken:  "test-token",
		Timeout:     time.Minute,
		Environ:     os.Environ,
		SummaryJSON: summaryJSON,
	})

	if obs.Status != "ok" {
		t.Fatalf("obs.Status = %q, want ok (obs.Error = %q)", obs.Status, obs.Error)
	}
	if obs.Checks.Verdict != "passed" {
		t.Errorf("obs.Checks.Verdict = %q, want %q (summary.json row overrides meta.json.task.result)", obs.Checks.Verdict, "passed")
	}
}

// obsWriteClaudeIsError writes a fake claude that prints
// {"is_error":true,"result":resultText,...} and exits with exitCode — used
// to pin §7.4/§7.5: a claude call reporting is_error must always yield
// observation status "error" with resultText as the cause, whether or not
// the process itself exited non-zero, and never status "unparsed".
func obsWriteClaudeIsError(t *testing.T, dir, resultText string, exitCode int) string {
	t.Helper()
	out, err := json.Marshal(map[string]any{
		"result":         resultText,
		"total_cost_usd": 0.0,
		"is_error":       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\ncat > /dev/null\nprintf '%s' " + obsShellQuote(string(out)) + "\nexit " + strconv.Itoa(exitCode) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestObserve_ClaudeIsError_StatusErrorNeverUnparsed pins §7.4/§7.5: when
// claude's own JSON output reports is_error true, the observation is
// status "error" with the JSON result text as the cause — regardless of
// the process exit code, and never status "unparsed" (before the fix, an
// is_error-true-but-exit-0 answer fell through to ParseAndValidate, which
// rejected the plain error text as an invalid schema and rendered
// "unparsed" instead of surfacing the real cause).
func TestObserve_ClaudeIsError_StatusErrorNeverUnparsed(t *testing.T) {
	const wantText = "Invalid API key · Please run /login"
	cases := []struct {
		name     string
		exitCode int
	}{
		{"exit 1", 1},
		{"exit 0", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			claudePath := obsWriteClaudeIsError(t, tmp, wantText, tc.exitCode)

			bundle := NewDirBundle("testdata/sample-run")
			obs := Observe(context.Background(), bundle, ObserveConfig{
				RunID:      "sample-run",
				Model:      "claude-sonnet-5",
				ClaudePath: claudePath,
				OAuthToken: "test-token",
				Timeout:    time.Minute,
				Environ:    os.Environ,
			})

			if obs.Status != statusError {
				t.Fatalf("obs.Status = %q, want %q (obs.Error = %q, obs.Raw = %q)", obs.Status, statusError, obs.Error, obs.Raw)
			}
			if !strings.Contains(obs.Error, "Invalid API key") {
				t.Errorf("obs.Error = %q, want it to contain %q", obs.Error, "Invalid API key")
			}
			if obs.ErrorKind != ErrorKindCredential {
				t.Errorf("obs.ErrorKind = %q, want %q (an invalid/expired API key names a credential problem, §7.5)", obs.ErrorKind, ErrorKindCredential)
			}
		})
	}
}

// TestObserve_ErrorKind_OtherFromUnrelatedIsError pins §7.5: an is_error
// result whose text names no credential problem classifies as errorKind
// "other" — the catch-all, never "credential" by default.
func TestObserve_ErrorKind_OtherFromUnrelatedIsError(t *testing.T) {
	tmp := t.TempDir()
	claudePath := obsWriteClaudeIsError(t, tmp, "internal error: request failed", 1)

	bundle := NewDirBundle("testdata/sample-run")
	obs := Observe(context.Background(), bundle, ObserveConfig{
		RunID: "sample-run", Model: "claude-sonnet-5", ClaudePath: claudePath,
		OAuthToken: "test-token", Timeout: time.Minute, Environ: os.Environ,
	})
	if obs.Status != statusError {
		t.Fatalf("obs.Status = %q, want %q", obs.Status, statusError)
	}
	if obs.ErrorKind != ErrorKindOther {
		t.Errorf("obs.ErrorKind = %q, want %q", obs.ErrorKind, ErrorKindOther)
	}
}

// TestObserve_ErrorKind_Bundle pins §7.5: a missing required bundle file
// (transcript.jsonl) makes the observation status "error" with errorKind
// "bundle".
func TestObserve_ErrorKind_Bundle(t *testing.T) {
	bundle := NewDirBundle("testdata/missing-transcript")
	obs := Observe(context.Background(), bundle, ObserveConfig{
		RunID: "missing-transcript", Model: "claude-sonnet-5", ClaudePath: "claude",
		OAuthToken: "test-token", Timeout: time.Minute, Environ: os.Environ,
	})
	if obs.Status != statusError {
		t.Fatalf("obs.Status = %q, want %q (obs.Error = %q)", obs.Status, statusError, obs.Error)
	}
	if obs.ErrorKind != ErrorKindBundle {
		t.Errorf("obs.ErrorKind = %q, want %q", obs.ErrorKind, ErrorKindBundle)
	}
}

// TestObserve_ErrorKind_Credential_EmptyToken pins §7.5: an empty OAuth
// token refuses before any child starts, classified errorKind "credential".
func TestObserve_ErrorKind_Credential_EmptyToken(t *testing.T) {
	bundle := NewDirBundle("testdata/sample-run")
	obs := Observe(context.Background(), bundle, ObserveConfig{
		RunID: "sample-run", Model: "claude-sonnet-5", ClaudePath: "claude",
		OAuthToken: "", Timeout: time.Minute, Environ: os.Environ,
	})
	if obs.Status != statusError {
		t.Fatalf("obs.Status = %q, want %q (obs.Error = %q)", obs.Status, statusError, obs.Error)
	}
	if obs.ErrorKind != ErrorKindCredential {
		t.Errorf("obs.ErrorKind = %q, want %q", obs.ErrorKind, ErrorKindCredential)
	}
}

// TestObserve_ErrorKind_Timeout pins §7.5: a claude call that outlives
// cfg.Timeout is classified errorKind "timeout".
func TestObserve_ErrorKind_Timeout(t *testing.T) {
	tmp := t.TempDir()
	claudePath := filepath.Join(tmp, "claude")
	if err := os.WriteFile(claudePath, []byte("#!/bin/sh\nsleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	bundle := NewDirBundle("testdata/sample-run")
	obs := Observe(context.Background(), bundle, ObserveConfig{
		RunID: "sample-run", Model: "claude-sonnet-5", ClaudePath: claudePath,
		OAuthToken: "test-token", Timeout: 200 * time.Millisecond, Environ: os.Environ,
	})
	if obs.Status != statusError {
		t.Fatalf("obs.Status = %q, want %q (obs.Error = %q)", obs.Status, statusError, obs.Error)
	}
	if obs.ErrorKind != ErrorKindTimeout {
		t.Errorf("obs.ErrorKind = %q, want %q", obs.ErrorKind, ErrorKindTimeout)
	}
}

// TestObserve_ErrorKind_Killed pins §7.5: a claude process that dies on a
// signal is classified errorKind "killed".
func TestObserve_ErrorKind_Killed(t *testing.T) {
	tmp := t.TempDir()
	claudePath := filepath.Join(tmp, "claude")
	if err := os.WriteFile(claudePath, []byte("#!/bin/sh\nkill -TERM $$\nsleep 5\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	bundle := NewDirBundle("testdata/sample-run")
	obs := Observe(context.Background(), bundle, ObserveConfig{
		RunID: "sample-run", Model: "claude-sonnet-5", ClaudePath: claudePath,
		OAuthToken: "test-token", Timeout: time.Minute, Environ: os.Environ,
	})
	if obs.Status != statusError {
		t.Fatalf("obs.Status = %q, want %q (obs.Error = %q)", obs.Status, statusError, obs.Error)
	}
	if obs.ErrorKind != ErrorKindKilled {
		t.Errorf("obs.ErrorKind = %q, want %q", obs.ErrorKind, ErrorKindKilled)
	}
}

// TestObserve_ErrorKind_Model_EmptyReply pins §7.5: claude exiting cleanly
// (is_error false) with an empty result text is "no usable answer" —
// errorKind "model", never "unparsed".
func TestObserve_ErrorKind_Model_EmptyReply(t *testing.T) {
	tmp := t.TempDir()
	claudePath := obsWriteCannedClaude(t, tmp, "")

	bundle := NewDirBundle("testdata/sample-run")
	obs := Observe(context.Background(), bundle, ObserveConfig{
		RunID: "sample-run", Model: "claude-sonnet-5", ClaudePath: claudePath,
		OAuthToken: "test-token", Timeout: time.Minute, Environ: os.Environ,
	})
	if obs.Status != statusError {
		t.Fatalf("obs.Status = %q, want %q (obs.Error = %q)", obs.Status, statusError, obs.Error)
	}
	if obs.ErrorKind != ErrorKindModel {
		t.Errorf("obs.ErrorKind = %q, want %q", obs.ErrorKind, ErrorKindModel)
	}
}

// TestObserve_FormatVersion2AndSourceFromConfig pins §7.5: the observer
// always writes formatVersion zcp-farm-observation-2, and source comes from
// the caller's config (never guessed).
func TestObserve_FormatVersion2AndSourceFromConfig(t *testing.T) {
	tmp := t.TempDir()
	claudePath := obsWriteCannedClaude(t, tmp, obsCanonicalModelAnswer)

	bundle := NewDirBundle("testdata/sample-run")
	obs := Observe(context.Background(), bundle, ObserveConfig{
		RunID: "sample-run", Model: "claude-sonnet-5", ClaudePath: claudePath,
		OAuthToken: "test-token", Timeout: time.Minute, Environ: os.Environ,
		Source: SourceWorker,
	})
	if obs.FormatVersion != ObservationFormat2 {
		t.Errorf("obs.FormatVersion = %q, want %q", obs.FormatVersion, ObservationFormat2)
	}
	if obs.Source != SourceWorker {
		t.Errorf("obs.Source = %q, want %q", obs.Source, SourceWorker)
	}
}

// TestObserve_StatusOk_SetsStoryOutcomeAndDerivedChecks pins §7.5: a
// successfully parsed answer's story, outcome and checks.agree are stored
// as the pipeline derives/carries them — never left over from format 1.
func TestObserve_StatusOk_SetsStoryOutcomeAndDerivedChecks(t *testing.T) {
	tmp := t.TempDir()
	claudePath := obsWriteCannedClaude(t, tmp, obsCanonicalModelAnswer)

	bundle := NewDirBundle("testdata/sample-run")
	obs := Observe(context.Background(), bundle, ObserveConfig{
		RunID: "sample-run", Model: "claude-sonnet-5", ClaudePath: claudePath,
		OAuthToken: "test-token", Timeout: time.Minute, Environ: os.Environ,
	})
	if obs.Status != "ok" {
		t.Fatalf("obs.Status = %q, want ok (obs.Error = %q)", obs.Status, obs.Error)
	}
	if obs.Story == nil || obs.Story.Ending != EndingFinished {
		t.Errorf("obs.Story = %+v, want the parsed story with ending %q", obs.Story, EndingFinished)
	}
	if obs.Outcome != OutcomeProblem {
		t.Errorf("obs.Outcome = %q, want %q (one finding)", obs.Outcome, OutcomeProblem)
	}
	if !obs.Checks.Agree {
		t.Errorf("obs.Checks.Agree = %v, want true (judged check correct, no evaluator finding)", obs.Checks.Agree)
	}
	if len(obs.Checks.Judged) != 1 || obs.Checks.Judged[0].ID != "liveness/api/marker" {
		t.Errorf("obs.Checks.Judged = %+v, want the one judged check kept", obs.Checks.Judged)
	}
	if len(obs.Findings) != 1 || obs.Findings[0].Surface != "tool:zerops_import" || obs.Findings[0].Anchor == "" {
		t.Errorf("obs.Findings = %+v, want the surface/anchor kept (both verified against the run)", obs.Findings)
	}
}

// TestObserve_UnparsedAnswer_StoresRawUpTo20000Chars pins §7.5: raw keeps
// the unparsed answer's text capped at 20,000 chars, not render.go's
// separate 2,000-char display cap — a 6,000-char unparsed answer must be
// stored whole.
func TestObserve_UnparsedAnswer_StoresRawUpTo20000Chars(t *testing.T) {
	tmp := t.TempDir()
	unparsedText := strings.Repeat("x", 6000) // not JSON: never parses as a ModelAnswer
	claudePath := obsWriteCannedClaude(t, tmp, unparsedText)

	bundle := NewDirBundle("testdata/sample-run")
	obs := Observe(context.Background(), bundle, ObserveConfig{
		RunID:      "sample-run",
		Model:      "claude-sonnet-5",
		ClaudePath: claudePath,
		OAuthToken: "test-token",
		Timeout:    time.Minute,
		Environ:    os.Environ,
	})

	if obs.Status != statusUnparsed {
		t.Fatalf("obs.Status = %q, want %q (obs.Error = %q)", obs.Status, statusUnparsed, obs.Error)
	}
	if len(obs.Raw) != 6000 {
		t.Errorf("len(obs.Raw) = %d, want 6000 (the whole answer, not truncated to render's 2,000-char display cap)", len(obs.Raw))
	}
}
