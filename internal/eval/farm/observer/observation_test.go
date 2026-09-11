package observer

import (
	"fmt"
	"strings"
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

// sampleFacts is a small, self-consistent RunFacts fixture reused across the
// repair tests below: a 3-step run (step 2 a zerops_deploy tool call with a
// PREFLIGHT_FAILED result), one failed check (liveness/api/marker), verdict
// "failed".
func sampleFacts() RunFacts {
	return RunFacts{
		Steps: []Step{
			{N: 1, Kind: StepUser, Text: "fix the api service"},
			{N: 2, Kind: StepTool, ToolName: "zerops_deploy", ToolInputJSON: "{}", ToolHasResult: true, ToolResultText: "PREFLIGHT_FAILED: zerops.yaml not found"},
			{N: 3, Kind: StepAgent, Text: "done"},
		},
		ChecksBody:              `liveness/api/marker failed expected="body contains python" observed="marker not found" source=HTTP`,
		ToolNames:               map[string]bool{"zerops_deploy": true},
		CheckIDs:                map[string]bool{"liveness/api/marker": true},
		FailedOrBlockedCheckIDs: map[string]bool{"liveness/api/marker": true},
		// Verdict is "passed" here so the surface/anchor/span/cap repair
		// tests below never trip warnUnexplainedFailure's noise; that
		// repair gets its own dedicated facts.Verdict = "failed" in
		// TestParseAndValidate_WarnsUnexplainedFailure.
		Verdict: "passed",
	}
}

// findingSpec builds one finding's JSON literal (findingJSON), filling
// sensible defaults for the fields a given test doesn't care about, so each
// repair test only spells out what it's actually exercising.
type findingSpec struct {
	Severity      string
	Owner         string
	Surface       string
	Anchor        string
	Title         string
	EvidenceStep  int
	Quote         string
	Span          *Span
	CausedVerdict bool
}

func findingJSON(s findingSpec) string {
	if s.Severity == "" {
		s.Severity = "high"
	}
	if s.Owner == "" {
		s.Owner = "agent"
	}
	if s.Title == "" {
		s.Title = "t"
	}
	if s.Quote == "" {
		s.Quote = "q"
	}
	spanJSON := "null"
	if s.Span != nil {
		spanJSON = fmt.Sprintf(`{"from":%d,"to":%d}`, s.Span.From, s.Span.To)
	}
	return fmt.Sprintf(
		`{"severity":%q,"owner":%q,"surface":%q,"anchor":%q,"title":%q,"what":"w","evidence":[{"step":%d,"quote":%q}],"span":%s,"causedVerdict":%t,"lookAt":"l","fix":""}`,
		s.Severity, s.Owner, s.Surface, s.Anchor, s.Title, s.EvidenceStep, s.Quote, spanJSON, s.CausedVerdict,
	)
}

// answerJSON builds a format-2 model answer with the given findings JSON
// (already comma-joined, may be empty) spliced in; goal/story/selfReview
// default to a clean, structurally-valid shell.
func answerJSON(findings string) string {
	return `{
		"headline": "deploy preflight looked for zerops.yaml in the wrong place",
		"story": {"task": "t", "expected": "e", "did": "d", "stuck": null, "ending": "finished"},
		"goal": {"reached": "yes", "why": "service is healthy"},
		"checks": {"judged": []},
		"findings": [` + findings + `],
		"selfReview": {"accurate": "yes", "note": ""}
	}`
}

// TestParseAndValidate_UnparsedTriggers pins §7.5's "Parse, validate,
// repair": each of these breaks the structure and makes ParseAndValidate
// report unparsed (ok=false) — never repaired.
func TestParseAndValidate_UnparsedTriggers(t *testing.T) {
	oneFinding := findingJSON(findingSpec{EvidenceStep: 1})

	cases := []struct {
		name string
		raw  string
	}{
		{"no JSON at all", "the agent did fine, nothing to report"},
		{"bad goal.reached enum", strings.Replace(answerJSON(""), `"reached": "yes"`, `"reached": "maybe"`, 1)},
		{"bad selfReview.accurate enum", strings.Replace(answerJSON(""), `"accurate": "yes"`, `"accurate": "maybe"`, 1)},
		{"missing story", strings.Replace(answerJSON(""), `"story": {"task": "t", "expected": "e", "did": "d", "stuck": null, "ending": "finished"},`, "", 1)},
		{"missing story.ending", strings.Replace(answerJSON(""), `"ending": "finished"`, `"ending": ""`, 1)},
		{"invalid story.ending enum", strings.Replace(answerJSON(""), `"ending": "finished"`, `"ending": "confused"`, 1)},
		{"finding without title", answerJSON(strings.Replace(oneFinding, `"title":"t"`, `"title":""`, 1))},
		{"finding without evidence", answerJSON(`{"severity":"high","owner":"agent","title":"t","what":"w","evidence":[],"span":null,"causedVerdict":false,"lookAt":"l","fix":""}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, ok := ParseAndValidate(tc.raw, sampleFacts())
			if ok {
				t.Errorf("ParseAndValidate(%q) succeeded, want unparsed", tc.raw)
			}
		})
	}

	// Sanity: the valid counterparts succeed, proving the failures above are
	// due to the specific defect, not a validator that rejects everything.
	if _, _, ok := ParseAndValidate(answerJSON(""), sampleFacts()); !ok {
		t.Fatalf("ParseAndValidate(valid answer, no findings) failed, want success")
	}
	if _, _, ok := ParseAndValidate(answerJSON(oneFinding), sampleFacts()); !ok {
		t.Fatalf("ParseAndValidate(valid answer, one finding) failed, want success")
	}
}

// TestParseAndValidate_DropsFindingsBeyondThreeCap pins §7.5: "at most three
// findings" is a repair (drop the rest + warn), never a reason to mark the
// whole answer unparsed — the old code rejected more than 5 outright.
func TestParseAndValidate_DropsFindingsBeyondThreeCap(t *testing.T) {
	var findings []string
	for i := 1; i <= 5; i++ {
		findings = append(findings, findingJSON(findingSpec{Title: fmt.Sprintf("finding %d", i), EvidenceStep: 1}))
	}
	ans, warnings, ok := ParseAndValidate(answerJSON(strings.Join(findings, ",")), sampleFacts())
	if !ok {
		t.Fatalf("ParseAndValidate: got unparsed, want success (repaired)")
	}
	if len(ans.Findings) != 3 {
		t.Errorf("len(ans.Findings) = %d, want 3", len(ans.Findings))
	}
	if ans.Findings[0].Title != "finding 1" || ans.Findings[2].Title != "finding 3" {
		t.Errorf("ans.Findings = %+v, want the first three kept in order", ans.Findings)
	}
	if !anyContains(warnings, "three") {
		t.Errorf("warnings = %v, want one naming the three-finding cap", warnings)
	}
}

// TestParseAndValidate_ClearsInvalidSurface pins §7.5: a surface not of the
// "<kind>:<name>" (or bare scenario/platform/agent) form, naming a tool the
// run never called, or a check id the run doesn't have, is cleared (with a
// warning) — everything else passes through untouched.
func TestParseAndValidate_ClearsInvalidSurface(t *testing.T) {
	cases := []struct {
		name        string
		surface     string
		wantKept    bool
		wantWarning bool
	}{
		{"valid tool surface kept", "tool:zerops_deploy", true, false},
		{"valid tool/action surface kept", "tool:zerops_deploy/import", true, false},
		{"tool never called cleared", "tool:zerops_ghost", false, true},
		{"known check id kept", "check:liveness/api/marker", true, false},
		{"unknown check id cleared", "check:no_such_check", false, true},
		{"bare agent kind kept", "agent", true, false},
		{"bare platform kind kept", "platform", true, false},
		{"recipe surface always kept", "recipe:nodejs-basic", true, false},
		{"malformed grammar cleared", "not-a-kind", false, true},
		{"empty surface untouched", "", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := findingJSON(findingSpec{Surface: tc.surface, EvidenceStep: 2})
			ans, warnings, ok := ParseAndValidate(answerJSON(f), sampleFacts())
			if !ok {
				t.Fatalf("ParseAndValidate: got unparsed, want success")
			}
			got := ans.Findings[0].Surface
			if tc.wantKept && got != tc.surface {
				t.Errorf("surface = %q, want kept as %q", got, tc.surface)
			}
			if !tc.wantKept && got != "" {
				t.Errorf("surface = %q, want cleared", got)
			}
			if tc.wantWarning && len(warnings) == 0 {
				t.Errorf("warnings = %v, want a warning about the cleared surface", warnings)
			}
			if !tc.wantWarning && len(warnings) != 0 {
				t.Errorf("warnings = %v, want none", warnings)
			}
		})
	}
}

// TestParseAndValidate_ClearsUnverifiedAnchor pins §7.5 FM-46: an anchor
// that does not occur, once normalized, in any step the finding's evidence
// cites is cleared — including a step-0 citation against the rendered
// CHECKS section.
func TestParseAndValidate_ClearsUnverifiedAnchor(t *testing.T) {
	cases := []struct {
		name       string
		anchor     string
		evStep     int
		wantKept   bool
		wantWarned bool
	}{
		{"anchor present in cited step kept", "PREFLIGHT_FAILED", 2, true, false},
		{"anchor absent from cited step cleared", "totally unrelated text", 2, false, true},
		{"empty anchor untouched", "", 2, true, false},
		{"anchor verified via step-0 CHECKS citation", "liveness/api/marker", 0, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := findingJSON(findingSpec{Anchor: tc.anchor, EvidenceStep: tc.evStep})
			ans, warnings, ok := ParseAndValidate(answerJSON(f), sampleFacts())
			if !ok {
				t.Fatalf("ParseAndValidate: got unparsed, want success")
			}
			got := ans.Findings[0].Anchor
			if tc.wantKept && got != tc.anchor {
				t.Errorf("anchor = %q, want kept as %q", got, tc.anchor)
			}
			if !tc.wantKept && got != "" {
				t.Errorf("anchor = %q, want cleared", got)
			}
			if tc.wantWarned && len(warnings) == 0 {
				t.Errorf("warnings = %v, want one about the cleared anchor", warnings)
			}
		})
	}
}

// TestParseAndValidate_DropsInvalidSpan pins §7.5: a span outside the run
// (steps are numbered 1..len(steps)) or with from > to is dropped.
func TestParseAndValidate_DropsInvalidSpan(t *testing.T) {
	cases := []struct {
		name     string
		span     *Span
		wantKept bool
	}{
		{"span within range kept", &Span{From: 1, To: 3}, true},
		{"inverted span dropped", &Span{From: 3, To: 1}, false},
		{"span past the run dropped", &Span{From: 1, To: 10}, false},
		{"span before step 1 dropped", &Span{From: 0, To: 2}, false},
		{"nil span untouched", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := findingJSON(findingSpec{EvidenceStep: 1, Span: tc.span})
			ans, warnings, ok := ParseAndValidate(answerJSON(f), sampleFacts())
			if !ok {
				t.Fatalf("ParseAndValidate: got unparsed, want success")
			}
			got := ans.Findings[0].Span
			if tc.wantKept {
				switch {
				case tc.span == nil:
					if got != nil {
						t.Errorf("span = %+v, want kept as nil", got)
					}
				case got == nil || *got != *tc.span:
					t.Errorf("span = %+v, want kept as %+v", got, tc.span)
				}
				if len(warnings) != 0 {
					t.Errorf("warnings = %v, want none", warnings)
				}
			} else {
				if got != nil {
					t.Errorf("span = %+v, want dropped (nil)", got)
				}
				if len(warnings) == 0 {
					t.Errorf("warnings = %v, want one about the dropped span", warnings)
				}
			}
		})
	}
}

// TestParseAndValidate_DropsJudgedNotFailedOrBlocked pins §7.5: a judged
// check id that is not a failed or blocked check of the run is dropped.
func TestParseAndValidate_DropsJudgedNotFailedOrBlocked(t *testing.T) {
	raw := `{
		"headline": "h", "story": {"task":"t","expected":"e","did":"d","stuck":null,"ending":"finished"},
		"goal": {"reached": "yes", "why": "y"},
		"checks": {"judged": [
			{"id": "liveness/api/marker", "correct": true, "why": "w"},
			{"id": "no_such_check", "correct": false, "why": "w"}
		]},
		"findings": [], "selfReview": {"accurate": "yes", "note": ""}
	}`
	ans, warnings, ok := ParseAndValidate(raw, sampleFacts())
	if !ok {
		t.Fatalf("ParseAndValidate: got unparsed, want success")
	}
	if len(ans.Checks.Judged) != 1 || ans.Checks.Judged[0].ID != "liveness/api/marker" {
		t.Errorf("ans.Checks.Judged = %+v, want only the run's own failed check", ans.Checks.Judged)
	}
	if !anyContains(warnings, "no_such_check") {
		t.Errorf("warnings = %v, want one naming the dropped check id", warnings)
	}
}

// answerWithHeadlineJSON builds a structurally-valid format-2 answer (no
// findings) carrying headline verbatim, %q-encoded so the word-count
// fixtures below never have to worry about JSON escaping.
func answerWithHeadlineJSON(headline string) string {
	return fmt.Sprintf(`{
		"headline": %q,
		"story": {"task": "t", "expected": "e", "did": "d", "stuck": null, "ending": "finished"},
		"goal": {"reached": "yes", "why": "y"},
		"checks": {"judged": []},
		"findings": [],
		"selfReview": {"accurate": "yes", "note": ""}
	}`, headline)
}

// TestParseAndValidate_WarnsHeadlineOver30Words pins §7.5: a headline over
// 30 words is kept in full and warned about — never truncated or rejected.
func TestParseAndValidate_WarnsHeadlineOver30Words(t *testing.T) {
	longHeadline := strings.TrimSpace(strings.Repeat("word ", 31))  // 31 words
	shortHeadline := strings.TrimSpace(strings.Repeat("word ", 30)) // 30 words

	cases := []struct {
		name     string
		headline string
		wantWarn bool
	}{
		{"over 30 words warns but keeps headline", longHeadline, true},
		{"30 words or fewer: no warning", shortHeadline, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ans, warnings, ok := ParseAndValidate(answerWithHeadlineJSON(tc.headline), sampleFacts())
			if !ok {
				t.Fatalf("ParseAndValidate: got unparsed, want success")
			}
			if ans.Headline != tc.headline {
				t.Errorf("ans.Headline = %q, want the full headline kept as %q", ans.Headline, tc.headline)
			}
			if got := anyContains(warnings, "30 word"); got != tc.wantWarn {
				t.Errorf("warnings = %v, want a 30-word warning: %v", warnings, tc.wantWarn)
			}
		})
	}
}

// TestParseAndValidate_WarnsUnexplainedFailure pins §7.5's last repair: a
// failed run whose findings and judged checks make no visible attempt to
// explain the failure is warned about; scoped to verdict "failed" only.
func TestParseAndValidate_WarnsUnexplainedFailure(t *testing.T) {
	judgedAllCorrect := `{"judged": [{"id": "liveness/api/marker", "correct": true, "why": "w"}]}`
	judgedOneWrong := `{"judged": [{"id": "liveness/api/marker", "correct": false, "why": "w"}]}`

	cases := []struct {
		name     string
		verdict  string
		findings string
		checks   string
		wantWarn bool
	}{
		{"failed, no explanation, judged all correct: warns", "failed", "", judgedAllCorrect, true},
		{"failed, a finding caused the verdict: no warning", "failed", findingJSON(findingSpec{EvidenceStep: 1, CausedVerdict: true}), judgedAllCorrect, false},
		{"failed, a judged check is wrong: no warning", "failed", "", judgedOneWrong, false},
		{"passed verdict: never warns", "passed", "", judgedAllCorrect, false},
		{"blocked verdict: never warns (spec scopes to failed only)", "blocked", "", judgedAllCorrect, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{
				"headline": "h", "story": {"task":"t","expected":"e","did":"d","stuck":null,"ending":"finished"},
				"goal": {"reached": "yes", "why": "y"},
				"checks": ` + tc.checks + `,
				"findings": [` + tc.findings + `], "selfReview": {"accurate": "yes", "note": ""}
			}`
			facts := sampleFacts()
			facts.Verdict = tc.verdict
			_, warnings, ok := ParseAndValidate(raw, facts)
			if !ok {
				t.Fatalf("ParseAndValidate: got unparsed, want success")
			}
			if got := anyContains(warnings, "no finding explains"); got != tc.wantWarn {
				t.Errorf("warnings = %v, want unexplained-failure warning: %v", warnings, tc.wantWarn)
			}
		})
	}
}

func anyContains(list []string, substr string) bool {
	for _, s := range list {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

// TestDeriveOutcome pins §7.5's outcome derivation: inconclusive when the
// session couldn't show whether ZCP works, else problem when there's at
// least one finding, else ok.
func TestDeriveOutcome(t *testing.T) {
	cases := []struct {
		name     string
		ending   string
		findings int
		want     string
	}{
		{"session-limit is always inconclusive", EndingSessionLimit, 2, OutcomeInconclusive},
		{"turn-limit is always inconclusive", EndingTurnLimit, 0, OutcomeInconclusive},
		{"timeout is always inconclusive", EndingTimeout, 0, OutcomeInconclusive},
		{"crashed is always inconclusive", EndingCrashed, 3, OutcomeInconclusive},
		{"finished with findings is problem", EndingFinished, 1, OutcomeProblem},
		{"finished with no findings is ok", EndingFinished, 0, OutcomeOK},
		{"gave-up with no findings is ok", EndingGaveUp, 0, OutcomeOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeriveOutcome(tc.ending, tc.findings); got != tc.want {
				t.Errorf("DeriveOutcome(%q, %d) = %q, want %q", tc.ending, tc.findings, got, tc.want)
			}
		})
	}
}

// TestDeriveAgree pins §7.5: checks.agree is false when any judged entry is
// marked incorrect or any finding is owned by evaluator, true otherwise —
// derived, never model-authored.
func TestDeriveAgree(t *testing.T) {
	cases := []struct {
		name     string
		judged   []JudgedCheck
		findings []Finding
		want     bool
	}{
		{"no judged, no findings: agree", nil, nil, true},
		{"all judged correct: agree", []JudgedCheck{{ID: "a", Correct: true}}, nil, true},
		{"a judged check is wrong: disagree", []JudgedCheck{{ID: "a", Correct: false}}, nil, false},
		{"an evaluator-owned finding: disagree", nil, []Finding{{Owner: "evaluator"}}, false},
		{"correct judged plus a non-evaluator finding: agree", []JudgedCheck{{ID: "a", Correct: true}}, []Finding{{Owner: "agent"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeriveAgree(tc.judged, tc.findings); got != tc.want {
				t.Errorf("DeriveAgree(%+v, %+v) = %v, want %v", tc.judged, tc.findings, got, tc.want)
			}
		})
	}
}

// TestObservation_EffectiveOutcome pins §7.5: a format-2 observation's
// already-stored outcome wins; a format-1 observation (no stored outcome)
// derives it on read by the same rule with "ending" unknown — so only ok or
// problem, never inconclusive; a non-"ok" status has no outcome at all.
func TestObservation_EffectiveOutcome(t *testing.T) {
	cases := []struct {
		name string
		obs  Observation
		want string
	}{
		{"format-2: stored outcome wins", Observation{Status: "ok", Outcome: OutcomeProblem}, OutcomeProblem},
		{"format-1: no findings derives ok", Observation{Status: "ok", Findings: nil}, OutcomeOK},
		{"format-1: findings derive problem", Observation{Status: "ok", Findings: []Finding{{}}}, OutcomeProblem},
		{"error status: no outcome", Observation{Status: "error"}, ""},
		{"unparsed status: no outcome", Observation{Status: "unparsed"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.obs.EffectiveOutcome(); got != tc.want {
				t.Errorf("EffectiveOutcome() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDisplayToolName pins §7.5: a finding's tool surface names a tool
// "exactly as called in the run" but without any mcp__…__ prefix — the
// observer's own facts (and the model's prompt) use the stripped form.
func TestDisplayToolName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"mcp__zcp__zerops_deploy", "zerops_deploy"},
		{"mcp__zcp__zerops_import", "zerops_import"},
		{"zerops_discover", "zerops_discover"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := displayToolName(tc.in); got != tc.want {
			t.Errorf("displayToolName(%q) = %q, want %q", tc.in, got, tc.want)
		}
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
		{N: 3, Kind: StepTool, ToolName: "zerops_discover", ToolHasResult: true, ToolResultText: `{"note":"service \"appdev\" there","path":"/var/www/<hostname>/","log":"line1\nline2"}`},
		{N: 4, Kind: StepAgent, Text: "Do **NOT** `override` a failed build without checking the diagnosis first."},
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
		{"empty quote is never verified", 1, "", false},
		{"whitespace-only quote is never verified", 1, "   \n\t", false},
		{"plain-text quote matches step's Markdown emphasis and inline-code source", 4, "Do NOT override", true},
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

// answerWithStuckJSON builds a clean format-2 answer whose story.stuck is the
// given raw JSON value (an object, a string, or null).
func answerWithStuckJSON(stuck string) string {
	return `{
		"headline": "OK — clean run",
		"story": {"task": "t", "expected": "e", "did": "d", "stuck": ` + stuck + `, "ending": "finished"},
		"goal": {"reached": "yes", "why": "y"},
		"checks": {"judged": []},
		"findings": [],
		"selfReview": {"accurate": "yes", "note": ""}
	}`
}

// TestParseAndValidate_StuckAsText pins §7.5's repair of story.stuck: a
// model that writes stuck as a sentence instead of {from, to, what} must
// not lose the whole observation — the text is kept as what, a leading
// "Steps a-b" becomes the range when it lies inside the run, and a warning
// says so. (Live: a nestjs run's otherwise sound answer went unparsed on
// "stuck": "Steps 18-36: agent tried twice…".)
func TestParseAndValidate_StuckAsText(t *testing.T) {
	cases := []struct {
		name             string
		stuck            string
		wantFrom, wantTo int
		wantWhat         string
		wantWarn         bool
	}{
		{"object as specified", `{"from": 1, "to": 2, "what": "w"}`, 1, 2, "w", false},
		{"text with a step range inside the run", `"Steps 2-3: agent retried the deploy"`, 2, 3, "agent retried the deploy", true},
		{"text with a range outside the run keeps only what", `"Steps 18-36: agent tried twice"`, 0, 0, "agent tried twice", true},
		{"text without a range", `"agent looped on the preflight"`, 0, 0, "agent looped on the preflight", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ans, warnings, ok := ParseAndValidate(answerWithStuckJSON(tc.stuck), sampleFacts())
			if !ok {
				t.Fatalf("ParseAndValidate: got unparsed, want success")
			}
			if ans.Story == nil || ans.Story.Stuck == nil {
				t.Fatalf("story.stuck = nil, want it kept")
			}
			st := ans.Story.Stuck
			if st.From != tc.wantFrom || st.To != tc.wantTo || st.What != tc.wantWhat {
				t.Errorf("stuck = %+v, want from=%d to=%d what=%q", *st, tc.wantFrom, tc.wantTo, tc.wantWhat)
			}
			if got := anyContains(warnings, "stuck"); got != tc.wantWarn {
				t.Errorf("warnings = %v, want a stuck warning: %v", warnings, tc.wantWarn)
			}
		})
	}
}

// TestParseAndValidate_WarnsOKHeadlineWithFindings pins §7.5: "OK —" is the
// headline of a run with no findings; an answer that says OK yet reports a
// finding is kept and warned about, so the page can show the contradiction.
func TestParseAndValidate_WarnsOKHeadlineWithFindings(t *testing.T) {
	withFinding := strings.Replace(answerJSON(findingJSON(findingSpec{EvidenceStep: 2, Quote: "PREFLIGHT_FAILED"})),
		`"headline": "deploy preflight looked for zerops.yaml in the wrong place"`, `"headline": "OK — reached the goal; one note"`, 1)
	_, warnings, ok := ParseAndValidate(withFinding, sampleFacts())
	if !ok {
		t.Fatalf("ParseAndValidate: got unparsed, want success")
	}
	if !anyContains(warnings, "starts with OK") {
		t.Errorf("warnings = %v, want one saying the headline starts with OK although there are findings", warnings)
	}
	_, clean, ok := ParseAndValidate(answerWithStuckJSON("null"), sampleFacts())
	if !ok || anyContains(clean, "starts with OK") {
		t.Errorf("clean OK answer: ok=%v warnings=%v, want parsed with no OK warning", ok, clean)
	}
}

// TestParseAndValidate_RepairsOwnerConfusedWithSurfaceKind pins §7.5: a
// finding whose owner is a surface kind (the model conflated the two
// fields) is kept under the owner that kind implies, and a finding with
// any other unknown owner is dropped — each with a warning — instead of
// the whole observation going unparsed. (Live: a nestjs run's sound answer
// was lost to one finding's "owner": "recipe".)
func TestParseAndValidate_RepairsOwnerConfusedWithSurfaceKind(t *testing.T) {
	cases := []struct {
		owner     string
		wantOwner string // "" = the finding is dropped
	}{
		{"recipe", "zcp-guidance"},
		{"tool", "zcp-tool"},
		{"check", "evaluator"},
		{"bogus", ""},
	}
	for _, tc := range cases {
		t.Run(tc.owner, func(t *testing.T) {
			ans, warnings, ok := ParseAndValidate(answerJSON(findingJSON(findingSpec{Owner: tc.owner, EvidenceStep: 2, Quote: "PREFLIGHT_FAILED"})), sampleFacts())
			if !ok {
				t.Fatalf("ParseAndValidate: got unparsed, want the finding repaired or dropped")
			}
			if tc.wantOwner == "" {
				if len(ans.Findings) != 0 {
					t.Errorf("findings = %+v, want the unknown-owner finding dropped", ans.Findings)
				}
			} else if len(ans.Findings) != 1 || ans.Findings[0].Owner != tc.wantOwner {
				t.Errorf("findings = %+v, want one finding with owner %q", ans.Findings, tc.wantOwner)
			}
			if !anyContains(warnings, "owner") {
				t.Errorf("warnings = %v, want one about the owner", warnings)
			}
		})
	}
}
