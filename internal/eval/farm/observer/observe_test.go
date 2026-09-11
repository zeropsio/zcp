package observer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	"headline": "Agent violated the never-override rule while diagnosing a failed build.",
	"goal": {"reached": "no", "why": "the service never became healthy"},
	"checks": {"agree": true, "why": ""},
	"findings": [
		{
			"severity": "high",
			"owner": "agent",
			"title": "called forbidden override despite scenario rule",
			"what": "The agent called zerops_import with override=true, which the scenario forbids.",
			"evidence": [{"step": 5, "quote": "forbidden call zerops_import{override=true}"}],
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
	if obs.Headline != "Agent violated the never-override rule while diagnosing a failed build." {
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
