package observer

import (
	"testing"
	"time"
)

// TestObservation_ParseFirstJSONObject pins §7.5: "the first top-level JSON
// object in its final text" — prose before/after is ignored, and braces
// inside a string don't end the object early.
func TestObservation_ParseFirstJSONObject(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "prose before and after",
			in:   "Here is my answer:\n" + `{"a":1}` + "\nThanks!",
			want: `{"a":1}`,
		},
		{
			name: "nested braces inside strings",
			in:   `{"a":"{not a brace}","b":2}` + " trailing",
			want: `{"a":"{not a brace}","b":2}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := firstJSONObject(tc.in)
			if !ok {
				t.Fatalf("firstJSONObject(%q) not found, want found", tc.in)
			}
			if got != tc.want {
				t.Errorf("firstJSONObject(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func validModelAnswerJSON() string {
	return `{
		"headline": "clean run",
		"goal": {"reached": "yes", "why": "service is healthy"},
		"checks": {"agree": true, "why": ""},
		"findings": [],
		"selfReview": {"accurate": "yes", "note": ""}
	}`
}

// TestObservation_InvalidAnswerIsUnparsedWithRaw pins §7.5: an answer that
// doesn't parse or doesn't validate (bad enum, more than 5 findings, a
// finding with no evidence, or no JSON at all) fails ParseAndValidate.
func TestObservation_InvalidAnswerIsUnparsedWithRaw(t *testing.T) {
	oneFinding := `{"severity":"high","owner":"agent","title":"t","what":"w","evidence":[{"step":1,"quote":"q"}],"lookAt":"l","fix":""}`

	cases := []struct {
		name string
		raw  string
	}{
		{"bad enum", `{"headline":"x","goal":{"reached":"maybe","why":"w"},"checks":{"agree":true,"why":""},"findings":[],"selfReview":{"accurate":"yes","note":""}}`},
		{"more than 5 findings", `{"headline":"x","goal":{"reached":"yes","why":"w"},"checks":{"agree":true,"why":""},"findings":[` +
			oneFinding + "," + oneFinding + "," + oneFinding + "," + oneFinding + "," + oneFinding + "," + oneFinding +
			`],"selfReview":{"accurate":"yes","note":""}}`},
		{"finding without evidence", `{"headline":"x","goal":{"reached":"yes","why":"w"},"checks":{"agree":true,"why":""},"findings":[{"severity":"high","owner":"agent","title":"t","what":"w","evidence":[],"lookAt":"l","fix":""}],"selfReview":{"accurate":"yes","note":""}}`},
		{"no JSON", "the agent did fine, nothing to report"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := ParseAndValidate(tc.raw)
			if ok {
				t.Errorf("ParseAndValidate(%q) succeeded, want failure (unparsed)", tc.raw)
			}
		})
	}

	// Sanity: the valid counterpart succeeds, proving the failures above
	// are due to the specific defect, not a validator that rejects
	// everything.
	if _, ok := ParseAndValidate(validModelAnswerJSON()); !ok {
		t.Fatalf("ParseAndValidate(valid answer) failed, want success")
	}
}

// TestObservation_QuoteCheck pins §7.5 FM-46: an evidence entry is verified
// iff its quote, whitespace-collapsed, is a substring of the cited step's
// full untruncated text (a tool step's text is its input JSON plus its
// result), collapsed the same way — trying both the plain text and its
// JSON-string-escapes-decoded form (a tool step's text is raw JSON, so a
// quoted value's `"`/`<`/newline can appear as `\"`/`<`/`\n` in the
// step; the model quotes the decoded characters — live-verified against
// real final1 bundles, e.g. cross-deploy step 12 `service \"appdev\" there`,
// recover step 65 `/var/www/<hostname>/`). Step 0 cites the
// rendered CHECKS section instead of a step. A step number outside 0..N is
// unverified.
func TestObservation_QuoteCheck(t *testing.T) {
	steps := []Step{
		{N: 1, Kind: StepUser, Text: "fix the  api\nservice"},
		{N: 2, Kind: StepTool, ToolName: "zerops_import", ToolInputJSON: `{"override":true,"tag":"<b>&x</b>"}`, ToolHasResult: true, ToolResultText: "DIAGNOSIS_REQUIRED"},
		{N: 3, Kind: StepTool, ToolName: "zerops_discover", ToolHasResult: true, ToolResultText: `{"note":"service \"appdev\" there","path":"/var/www/\u003chostname\u003e/","log":"line1\nline2"}`},
	}
	checksBody := `liveness/api/marker failed expected="body contains python" observed="marker not found" source=HTTP`

	cases := []struct {
		name  string
		step  int
		quote string
		want  bool
	}{
		{"verbatim", 1, "fix the  api\nservice", true},
		{"differs only in whitespace", 1, "fix the api service", true},
		{"not in step", 1, "delete the project", false},
		{"step past end", 99, "fix", false},
		{"quote spans a tool's input JSON", 2, `"override":true,"tag"`, true},
		{"input containing < > & matches unescaped", 2, `<b>&x</b>`, true},
		{"decoded escaped quote matches the real quote mark", 3, `"appdev"`, true},
		{"decoded \\u unicode escape matches the real angle brackets", 3, "<hostname>", true},
		{"decoded \\n in the step matches a plain space in the quote", 3, "line1 line2", true},
		{"step 0 quote present in CHECKS is verified", 0, `expected="body contains python"`, true},
		{"step 0 quote absent from CHECKS is unverified", 0, "totally unrelated text", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := verifyQuote(steps, checksBody, tc.step, tc.quote)
			if got != tc.want {
				t.Errorf("verifyQuote(steps, checksBody, %d, %q) = %v, want %v", tc.step, tc.quote, got, tc.want)
			}
		})
	}
}

// TestObservation_VerdictFromSummaryElseMeta pins §7.5: checks.verdict is
// the run's row result in the batch summary when present, else
// meta.json.task.result — regardless of what the model itself said.
func TestObservation_VerdictFromSummaryElseMeta(t *testing.T) {
	summary := []byte(`{"batch":"final1","runs":[
		{"runId":"final1-a","result":"passed"},
		{"runId":"final1-b","result":"blocked"}
	]}`)

	result, found, err := FindSummaryResult(summary, "final1-b")
	if err != nil {
		t.Fatalf("FindSummaryResult: %v", err)
	}
	if !found || result != "blocked" {
		t.Errorf("FindSummaryResult(final1-b) = (%q,%v), want (blocked,true)", result, found)
	}
	if got := ResolveVerdict(result, found, "failed"); got != "blocked" {
		t.Errorf("ResolveVerdict with summary present = %q, want blocked (summary wins)", got)
	}

	_, found, err = FindSummaryResult(summary, "final1-missing")
	if err != nil {
		t.Fatalf("FindSummaryResult: %v", err)
	}
	if found {
		t.Errorf("FindSummaryResult(final1-missing) found=true, want false")
	}
	if got := ResolveVerdict("", false, "failed"); got != "failed" {
		t.Errorf("ResolveVerdict with no summary row = %q, want meta.json.task.result (failed)", got)
	}

	// No summary.json at all (nil bytes / not present) falls through to meta.
	if got := ResolveVerdict("", false, "passed"); got != "passed" {
		t.Errorf("ResolveVerdict with no summary = %q, want passed", got)
	}
}

// TestObservation_ObsIDHasMillisecondsAndModel pins §7.5: obsId =
// "<UTC YYYYMMDDTHHMMSSmmmZ>-<model>". Expected value is hand-built from a
// literal timestamp, not derived from the implementation's own formatting
// call.
func TestObservation_ObsIDHasMillisecondsAndModel(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 6*int(time.Millisecond), time.UTC)
	got := ObsID(ts, "claude-sonnet-5")
	want := "20260102T030405006Z-claude-sonnet-5"
	if got != want {
		t.Errorf("ObsID = %q, want %q", got, want)
	}
}
