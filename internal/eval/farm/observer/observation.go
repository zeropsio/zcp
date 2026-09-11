package observer

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
)

// ObservationFormat1 is the formatVersion of the stored observation
// document (§7.5).
const ObservationFormat1 = "zcp-farm-observation-1"

// statusError and statusUnparsed are two of Observation.Status's three
// values (§7.5; the third, "ok", is exempt from this by being two
// characters long) — named constants, rather than repeated literals, since
// each now appears at enough call sites (production and test) to trip
// goconst.
const (
	statusError    = "error"
	statusUnparsed = "unparsed"
)

// Observation is the stored observation document (§7.5):
// runs/<runId>/observer/<obsId>.json.
type Observation struct {
	FormatVersion string    `json:"formatVersion"`
	RunID         string    `json:"runId"`
	ObsID         string    `json:"obsId"`
	Model         string    `json:"model"`
	CreatedAt     time.Time `json:"createdAt"`
	DurationMs    int64     `json:"durationMs"`
	CostUsd       float64   `json:"costUsd"`
	PromptSha256  string    `json:"promptSha256"`
	DigestSha256  string    `json:"digestSha256"`

	// Status is "ok", "unparsed", or "error" (§7.5).
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Raw    string `json:"raw,omitempty"`

	Headline   string     `json:"headline,omitempty"`
	Goal       Goal       `json:"goal"`
	Checks     Checks     `json:"checks"`
	Findings   []Finding  `json:"findings,omitempty"`
	SelfReview SelfReview `json:"selfReview"`
}

// Goal is the model's answer to "did the agent reach the user's goal"
// (§7.5).
type Goal struct {
	Reached string `json:"reached"` // yes|partly|no
	Why     string `json:"why"`
}

// Checks carries both the model's own agreement with the deterministic
// checks and, once resolved by the caller (never the model — §7.5), the
// run's own verdict.
type Checks struct {
	Verdict string `json:"verdict,omitempty"` // passed|failed|blocked — never model-authored
	Agree   bool   `json:"agree"`
	Why     string `json:"why,omitempty"`
}

// Evidence is one finding's cited step + quote, with the quote-check
// verdict (§7.5 FM-46).
type Evidence struct {
	Step     int    `json:"step"`
	Quote    string `json:"quote"`
	Verified bool   `json:"verified"`
}

// Finding is one observed issue (§7.5).
type Finding struct {
	Severity string     `json:"severity"` // high|medium|low
	Owner    string     `json:"owner"`
	Title    string     `json:"title"`
	What     string     `json:"what"`
	Evidence []Evidence `json:"evidence"`
	LookAt   string     `json:"lookAt"`
	Fix      string     `json:"fix"`
}

// SelfReview is the model's judgment of the agent's own self-review (§7.5).
type SelfReview struct {
	Accurate string `json:"accurate"` // yes|partly|no
	Note     string `json:"note"`
}

// ModelAnswer is the shape ParseAndValidate expects from the model's final
// text — the same field types as Observation's, minus the metadata fields
// the store/caller (never the model) supplies.
type ModelAnswer struct {
	Headline   string     `json:"headline"`
	Goal       Goal       `json:"goal"`
	Checks     Checks     `json:"checks"`
	Findings   []Finding  `json:"findings"`
	SelfReview SelfReview `json:"selfReview"`
}

// Finding severities (§7.5).
const (
	SeverityHigh   = "high"
	SeverityMedium = "medium"
	SeverityLow    = "low"
)

var (
	validReached  = map[string]bool{"yes": true, "partly": true, "no": true}
	validSeverity = map[string]bool{SeverityHigh: true, SeverityMedium: true, SeverityLow: true}
	validOwner    = map[string]bool{
		"zcp-guidance": true, "zcp-tool": true, "platform": true,
		"agent": true, "scenario": true, "evaluator": true,
	}
)

const maxFindings = 5

// ParseAndValidate extracts the first top-level JSON object from raw and
// validates it against §7.5's schema (enums, at most 5 findings, each
// finding carrying at least one evidence entry). ok is false when raw
// carries no JSON object, the object doesn't unmarshal, or it fails
// validation — the caller stores status "unparsed" with raw capped at
// 20,000 chars in that case.
func ParseAndValidate(raw string) (ModelAnswer, bool) {
	obj, found := firstJSONObject(raw)
	if !found {
		return ModelAnswer{}, false
	}
	var ans ModelAnswer
	if err := json.Unmarshal([]byte(obj), &ans); err != nil {
		return ModelAnswer{}, false
	}
	if !validateAnswer(ans) {
		return ModelAnswer{}, false
	}
	return ans, true
}

func validateAnswer(a ModelAnswer) bool {
	if !validReached[a.Goal.Reached] {
		return false
	}
	if len(a.Findings) > maxFindings {
		return false
	}
	for _, f := range a.Findings {
		if !validSeverity[f.Severity] {
			return false
		}
		if !validOwner[f.Owner] {
			return false
		}
		if len(f.Evidence) == 0 {
			return false
		}
	}
	return validReached[a.SelfReview.Accurate]
}

// firstJSONObject returns the first top-level (brace-balanced, string-aware)
// JSON object found in s, and whether one was found (§7.5: "the first
// top-level JSON object in its final text").
func firstJSONObject(s string) (string, bool) {
	for start := strings.IndexByte(s, '{'); start != -1; {
		if end, ok := balancedObjectEnd(s[start:]); ok {
			return s[start : start+end+1], true
		}
		next := strings.IndexByte(s[start+1:], '{')
		if next == -1 {
			return "", false
		}
		start += 1 + next
	}
	return "", false
}

// balancedObjectEnd scans s (which must start with '{') for the index of
// the matching closing brace, tracking JSON-string state so a brace inside
// a string is never mistaken for structure.
func balancedObjectEnd(s string) (int, bool) {
	depth := 0
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// verifyQuote implements §7.5 FM-46: quote (whitespace-collapsed) must be a
// substring of the cited step's full, untruncated text (collapsed the same
// way) — either as-is, or with both sides' JSON string escapes decoded
// first (a tool step's text is raw, undecoded JSON, so a quoted value's
// `"`/`<`/newline can appear in it as `\"`/`<`/`\n`; the model quotes
// the decoded characters — live-verified against real final1 bundles).
// Step 0 cites the rendered CHECKS section instead of a step (the model's
// only way to point at a deterministic check row). A step number outside
// 0..len(steps) is always unverified.
func verifyQuote(steps []Step, checksBody string, stepNum int, quote string) bool {
	if stepNum == 0 {
		return quoteMatches(checksBody, quote)
	}
	idx := stepNum - 1
	if idx < 0 || idx >= len(steps) {
		return false
	}
	step := steps[idx]
	full := step.Text
	if step.Kind == StepTool {
		full = step.ToolInputJSON + step.ToolResultText
	}
	return quoteMatches(full, quote)
}

// quoteMatches is the substring check verifyQuote applies to one (full,
// quote) pair, tried in order: whitespace-collapsed as-is; failing that,
// whitespace-collapsed after JSON-string-escape-decoding both sides;
// failing that, the same JSON-decoded-and-collapsed text with Markdown's
// backtick and asterisk delimiters dropped from both sides — a source step
// can carry Markdown source (e.g. a guidance sentence quoting
// "Do **NOT** `override`") while the model quotes its rendered plain
// reading ("Do NOT override"). An empty or whitespace-only quote is never
// verified: it is trivially a substring of any text, so without this check
// a model could satisfy the quote check by citing nothing at all.
func quoteMatches(full, quote string) bool {
	if collapseWhitespace(quote) == "" {
		return false
	}
	if strings.Contains(collapseWhitespace(full), collapseWhitespace(quote)) {
		return true
	}
	decodedFull, decodedQuote := collapseWhitespace(decodeJSONEscapes(full)), collapseWhitespace(decodeJSONEscapes(quote))
	if strings.Contains(decodedFull, decodedQuote) {
		return true
	}
	return strings.Contains(stripMarkdownPunctuation(decodedFull), stripMarkdownPunctuation(decodedQuote))
}

// stripMarkdownPunctuation drops every backtick and asterisk — Markdown's
// inline-code and emphasis delimiters (§7.5 FM-46's third quote-check
// normalization).
func stripMarkdownPunctuation(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '`' || r == '*' {
			return -1
		}
		return r
	}, s)
}

// decodeJSONEscapes decodes JSON string escape sequences (`\"` `\\` `\/`
// `\b` `\f` `\n` `\r` `\t` `\uXXXX`, including surrogate pairs) found
// anywhere in s. Unlike json.Unmarshal, s need not be (and generally isn't)
// a complete, quoted JSON string — it is a step's raw JSON text, decoded
// best-effort in place: an unrecognized or truncated escape is copied
// through unchanged rather than erroring.
func decodeJSONEscapes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	n := len(s)
	for i := 0; i < n; {
		c := s[i]
		if c != '\\' || i+1 >= n {
			b.WriteByte(c)
			i++
			continue
		}
		switch s[i+1] {
		case '"':
			b.WriteByte('"')
			i += 2
		case '\\':
			b.WriteByte('\\')
			i += 2
		case '/':
			b.WriteByte('/')
			i += 2
		case 'b':
			b.WriteByte('\b')
			i += 2
		case 'f':
			b.WriteByte('\f')
			i += 2
		case 'n':
			b.WriteByte('\n')
			i += 2
		case 'r':
			b.WriteByte('\r')
			i += 2
		case 't':
			b.WriteByte('\t')
			i += 2
		case 'u':
			r, width, ok := decodeUnicodeEscape(s[i:])
			if !ok {
				b.WriteByte(c)
				i++
				continue
			}
			b.WriteRune(r)
			i += width
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// decodeUnicodeEscape decodes one `\uXXXX` escape at the start of s (and,
// when it is a UTF-16 high surrogate immediately followed by a matching
// `\uXXXX` low surrogate, both together), returning the decoded rune, the
// byte width consumed from s, and whether decoding succeeded.
func decodeUnicodeEscape(s string) (rune, int, bool) {
	if len(s) < 6 {
		return 0, 0, false
	}
	r1, ok := parseHex4(s[2:6])
	if !ok {
		return 0, 0, false
	}
	if utf16.IsSurrogate(r1) && len(s) >= 12 && s[6] == '\\' && s[7] == 'u' {
		if r2, ok := parseHex4(s[8:12]); ok {
			if combined := utf16.DecodeRune(r1, r2); combined != unicode.ReplacementChar {
				return combined, 12, true
			}
		}
	}
	return r1, 6, true
}

// parseHex4 parses a 4-hex-digit `\u` escape body into its rune value.
func parseHex4(s string) (rune, bool) {
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, false
	}
	return rune(v), true
}

// VerifyEvidence sets Verified on every finding's evidence entries, against
// steps (the run's own, untruncated record) and checksBody (the rendered
// CHECKS section, for a step-0 citation) — §7.5 FM-46. Mutates and returns
// findings for convenient chaining.
func VerifyEvidence(steps []Step, checksBody string, findings []Finding) []Finding {
	for fi := range findings {
		for ei := range findings[fi].Evidence {
			ev := &findings[fi].Evidence[ei]
			ev.Verified = verifyQuote(steps, checksBody, ev.Step, ev.Quote)
		}
	}
	return findings
}

// batchSummaryDoc is the subset of batches/<batch>/summary.json
// (docs/spec-eval-farm.md §1.4 FM-9) FindSummaryResult needs.
type batchSummaryDoc struct {
	Runs []struct {
		RunID  string `json:"runId"`
		Result string `json:"result"`
	} `json:"runs"`
}

// FindSummaryResult looks up runID's row in a batch summary.json's bytes,
// returning its result and whether a row was found.
func FindSummaryResult(summaryJSON []byte, runID string) (result string, found bool, err error) {
	var doc batchSummaryDoc
	if err := json.Unmarshal(summaryJSON, &doc); err != nil {
		return "", false, fmt.Errorf("parse summary.json: %w", err)
	}
	for _, row := range doc.Runs {
		if row.RunID == runID {
			return row.Result, true, nil
		}
	}
	return "", false, nil
}

// ResolveVerdict implements §7.5's checks.verdict rule: the batch summary's
// row result for this run when present, else meta.json.task.result. Never
// the model's own opinion.
func ResolveVerdict(summaryResult string, summaryFound bool, metaTaskResult string) string {
	if summaryFound {
		return summaryResult
	}
	return metaTaskResult
}

// ObsID builds "<UTC YYYYMMDDTHHMMSSmmmZ>-<model>" (§7.5).
func ObsID(t time.Time, model string) string {
	ts := t.UTC().Format("20060102T150405.000")
	ts = strings.Replace(ts, ".", "", 1)
	return ts + "Z-" + model
}
