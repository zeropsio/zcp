package observer

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
)

// ObservationFormat1 and ObservationFormat2 are the formatVersions of the
// stored observation document (§7.5): the observer writes format 2, every
// reader reads both.
const (
	ObservationFormat1 = "zcp-farm-observation-1"
	ObservationFormat2 = "zcp-farm-observation-2"
)

// Assessment outcomes (§7.5) — derived, never model-authored.
const (
	OutcomeOK           = "ok"
	OutcomeProblem      = "problem"
	OutcomeInconclusive = "inconclusive"
)

// Session endings (§7.5 story.ending).
const (
	EndingFinished     = "finished"
	EndingGaveUp       = "gave-up"
	EndingSessionLimit = "session-limit"
	EndingTurnLimit    = "turn-limit"
	EndingTimeout      = "timeout"
	EndingCrashed      = "crashed"
)

// Observation sources (§7.5 source).
const (
	SourceWorker = "worker"
	SourceAction = "action"
	SourceLocal  = "local"
)

// Error kinds (§7.5 errorKind), set with status "error".
const (
	ErrorKindCredential = "credential"
	ErrorKindTimeout    = "timeout"
	ErrorKindKilled     = "killed"
	ErrorKindBundle     = "bundle"
	ErrorKindModel      = "model"
	ErrorKindOther      = "other"
)

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

	// Source is "worker", "action" or "local" (§7.5); empty on format 1.
	Source string `json:"source,omitempty"`

	// Status is "ok", "unparsed", or "error" (§7.5).
	Status    string `json:"status"`
	ErrorKind string `json:"errorKind,omitempty"`
	Error     string `json:"error,omitempty"`
	Raw       string `json:"raw,omitempty"`

	// Warnings lists every deterministic repair applied to the model's
	// answer, one plain sentence each (§7.5).
	Warnings []string `json:"warnings,omitempty"`

	// Outcome is derived (§7.5): "ok", "problem" or "inconclusive".
	Outcome    string     `json:"outcome,omitempty"`
	Headline   string     `json:"headline,omitempty"`
	Story      *Story     `json:"story,omitempty"`
	Goal       Goal       `json:"goal"`
	Checks     Checks     `json:"checks"`
	Findings   []Finding  `json:"findings,omitempty"`
	SelfReview SelfReview `json:"selfReview"`
}

// Story is the session in five short fields (§7.5); nil on format 1.
type Story struct {
	Task     string `json:"task"`
	Expected string `json:"expected"`
	Did      string `json:"did"`
	Stuck    *Stuck `json:"stuck"`
	Ending   string `json:"ending"`
}

// Stuck is where the run got stuck: a step range and what blocked it.
// From and To are 0 when no range is known.
type Stuck struct {
	From int    `json:"from"`
	To   int    `json:"to"`
	What string `json:"what"`

	// fromText marks a stuck the model wrote as a sentence instead of an
	// object; repairAnswer warns about it. Never serialized.
	fromText bool
}

// stuckRangePrefix matches a sentence's leading step range: "Steps 18-36:",
// "step #4 to #9 —", "Steps 12–54.".
var stuckRangePrefix = regexp.MustCompile(`(?i)^\s*steps?\s*#?(\d+)\s*(?:-|–|—|to)\s*#?(\d+)\s*[:.,;—–-]?\s*`)

// UnmarshalJSON reads the specified {from, to, what} object, and also a
// plain sentence (§7.5 repair): a leading "Steps a-b" becomes the range and
// the rest is what, so one mistyped field never costs the whole observation.
func (s *Stuck) UnmarshalJSON(b []byte) error {
	var text string
	if err := json.Unmarshal(b, &text); err == nil {
		*s = Stuck{What: strings.TrimSpace(text), fromText: true}
		if m := stuckRangePrefix.FindStringSubmatch(text); m != nil {
			s.From, _ = strconv.Atoi(m[1])
			s.To, _ = strconv.Atoi(m[2])
			s.What = strings.TrimSpace(text[len(m[0]):])
		}
		return nil
	}
	type plain Stuck
	var obj plain
	if err := json.Unmarshal(b, &obj); err != nil {
		return fmt.Errorf("story.stuck: %w", err)
	}
	*s = Stuck(obj)
	return nil
}

// Span is the step range a finding stretched over.
type Span struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// JudgedCheck is the observer's judgement of one failed or blocked check.
type JudgedCheck struct {
	ID      string `json:"id"`
	Correct bool   `json:"correct"`
	Why     string `json:"why"`
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
	Verdict string        `json:"verdict,omitempty"` // passed|failed|blocked — never model-authored
	Agree   bool          `json:"agree"`             // derived on format 2 (§7.5)
	Why     string        `json:"why,omitempty"`     // format 1
	Judged  []JudgedCheck `json:"judged,omitempty"`  // format 2
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
	Severity      string     `json:"severity"` // high|medium|low
	Owner         string     `json:"owner"`
	Surface       string     `json:"surface,omitempty"` // format 2: <kind>:<name> (§7.5)
	Anchor        string     `json:"anchor,omitempty"`  // format 2: verbatim ZCP text or error code
	Title         string     `json:"title"`
	What          string     `json:"what"`
	Evidence      []Evidence `json:"evidence"`
	Span          *Span      `json:"span,omitempty"`
	CausedVerdict bool       `json:"causedVerdict,omitempty"`
	LookAt        string     `json:"lookAt"`
	Fix           string     `json:"fix"`
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
	Story      *Story     `json:"story"`
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
	validEnding = map[string]bool{
		EndingFinished: true, EndingGaveUp: true, EndingSessionLimit: true,
		EndingTurnLimit: true, EndingTimeout: true, EndingCrashed: true,
	}
)

// maxFindings is §7.5's finding cap: "at most three findings, most
// important first" — findings beyond this are dropped by repairAnswer and
// warned about, never a reason to mark the whole answer unparsed.
const maxFindings = 3

// ownerEvaluator is the finding owner that says a deterministic check is
// wrong or missing (§7.5); DeriveAgree treats it specially.
const ownerEvaluator = "evaluator"

// verdictFailed is one of checks.verdict's three values (§7.5) —
// warnUnexplainedFailure's own scope check.
const verdictFailed = "failed"

// RunFacts carries the run's own record-derived facts a finding's
// surface/anchor/span and a judged check's id are validated and repaired
// against (§7.5 "Parse, validate, repair") — read once from the run's own
// steps, checks and resolved verdict, never invented.
type RunFacts struct {
	// Steps and ChecksBody are the run's own numbered record and rendered
	// CHECKS section (§7.3), for the anchor check (FM-46 normalization,
	// step 0 citing CHECKS).
	Steps      []Step
	ChecksBody string
	// ToolNames is every tool the run actually called, display-name form
	// (mcp__ prefix stripped, §7.5).
	ToolNames map[string]bool
	// CheckIDs is every check id of the run (verification.json rows).
	CheckIDs map[string]bool
	// FailedOrBlockedCheckIDs is the subset of CheckIDs a judged entry may
	// legitimately reference (§7.5: "checks.judged — one entry per failed
	// or blocked check of the run").
	FailedOrBlockedCheckIDs map[string]bool
	// Verdict is the run's own resolved verdict (§7.5 checks.verdict) —
	// never the model's opinion — needed by warnUnexplainedFailure.
	Verdict string
}

// ParseAndValidate extracts the first top-level JSON object from raw,
// validates it against §7.5's structural rules, and — once valid — repairs
// it deterministically against facts (§7.5 "Parse, validate, repair"),
// returning one warning sentence per repair applied. ok is false when raw
// carries no JSON object, the object doesn't unmarshal, or it breaks the
// structure (unknown enum, a finding without title or evidence, a missing
// story or story.ending) — the caller stores status "unparsed" with raw
// capped at 20,000 chars in that case, and warnings is always nil.
func ParseAndValidate(raw string, facts RunFacts) (ModelAnswer, []string, bool) {
	obj, found := firstJSONObject(raw)
	if !found {
		return ModelAnswer{}, nil, false
	}
	var ans ModelAnswer
	if err := json.Unmarshal([]byte(obj), &ans); err != nil {
		return ModelAnswer{}, nil, false
	}
	ans, ownerWarnings := repairOwners(ans)
	if !validateAnswer(ans) {
		return ModelAnswer{}, nil, false
	}
	repaired, warnings := repairAnswer(ans, facts)
	return repaired, append(ownerWarnings, warnings...), true
}

// ownerBySurfaceKind maps a surface kind a model wrote as a finding's owner
// to the owner that kind implies: ZCP's recipes are its guidance, its tools
// are its tools, a check is the evaluator's (§7.5 repair).
var ownerBySurfaceKind = map[string]string{"recipe": "zcp-guidance", "tool": "zcp-tool", "check": ownerEvaluator}

// repairOwners fixes findings whose owner is not one of the six (§7.5): an
// owner that is a surface kind becomes the owner that kind implies; any
// other unknown owner drops its finding. One unknown value never costs the
// whole observation.
func repairOwners(ans ModelAnswer) (ModelAnswer, []string) {
	var warnings []string
	kept := ans.Findings[:0:0]
	for _, f := range ans.Findings {
		if validOwner[f.Owner] {
			kept = append(kept, f)
			continue
		}
		if owner, ok := ownerBySurfaceKind[f.Owner]; ok {
			warnings = append(warnings, fmt.Sprintf("finding %q had owner %q (a surface kind); read as %q", f.Title, f.Owner, owner))
			f.Owner = owner
			kept = append(kept, f)
			continue
		}
		warnings = append(warnings, fmt.Sprintf("dropped finding %q: unknown owner %q", f.Title, f.Owner))
	}
	ans.Findings = kept
	return ans, warnings
}

// validateAnswer checks the structural rules whose violation makes the
// whole answer unparsed (§7.5) — never repaired: enum fields, a story
// (and a valid story.ending), and every finding carrying a title and at
// least one evidence entry.
func validateAnswer(a ModelAnswer) bool {
	if !validReached[a.Goal.Reached] {
		return false
	}
	if !validReached[a.SelfReview.Accurate] {
		return false
	}
	if a.Story == nil || !validEnding[a.Story.Ending] {
		return false
	}
	for _, f := range a.Findings {
		if !validSeverity[f.Severity] {
			return false
		}
		if !validOwner[f.Owner] {
			return false
		}
		if strings.TrimSpace(f.Title) == "" {
			return false
		}
		if len(f.Evidence) == 0 {
			return false
		}
	}
	return true
}

// repairAnswer applies §7.5's deterministic repairs, in the order the spec
// lists them, to ans (already structurally validated) against facts —
// never rejecting, only clearing/dropping the offending piece and
// appending one plain warning sentence per repair.
func repairAnswer(ans ModelAnswer, facts RunFacts) (ModelAnswer, []string) {
	var warnings []string

	if len(ans.Findings) > maxFindings {
		warnings = append(warnings, fmt.Sprintf(
			"dropped %d finding(s) beyond the three-finding cap", len(ans.Findings)-maxFindings))
		ans.Findings = ans.Findings[:maxFindings]
	}

	for i := range ans.Findings {
		f := &ans.Findings[i]
		if f.Surface != "" && !validSurface(f.Surface, facts) {
			warnings = append(warnings, fmt.Sprintf(
				"cleared finding %q's surface %q: not a valid ZCP surface for this run", f.Title, f.Surface))
			f.Surface = ""
		}
		if f.Anchor != "" && !anchorVerified(f, facts) {
			warnings = append(warnings, fmt.Sprintf(
				"cleared finding %q's anchor: not found in its cited step(s)", f.Title))
			f.Anchor = ""
		}
		if f.Span != nil && !validSpan(*f.Span, facts) {
			warnings = append(warnings, fmt.Sprintf(
				"dropped finding %q's span: outside the run", f.Title))
			f.Span = nil
		}
	}

	if len(ans.Checks.Judged) > 0 {
		kept := make([]JudgedCheck, 0, len(ans.Checks.Judged))
		for _, j := range ans.Checks.Judged {
			if !facts.FailedOrBlockedCheckIDs[j.ID] {
				warnings = append(warnings, fmt.Sprintf(
					"dropped judged check %q: not a failed or blocked check of this run", j.ID))
				continue
			}
			kept = append(kept, j)
		}
		ans.Checks.Judged = kept
	}

	if ans.Story != nil && ans.Story.Stuck != nil {
		st := ans.Story.Stuck
		if (st.From != 0 || st.To != 0) && !validSpan(Span{From: st.From, To: st.To}, facts) {
			warnings = append(warnings, fmt.Sprintf(
				"dropped story.stuck's step range %d–%d: outside the run", st.From, st.To))
			st.From, st.To = 0, 0
		}
		if st.fromText {
			warnings = append(warnings, "story.stuck was a sentence, not {from, to, what}; kept its text")
		}
	}

	if len(strings.Fields(ans.Headline)) > 30 {
		warnings = append(warnings, "headline is over 30 words")
	}

	if len(ans.Findings) > 0 && strings.HasPrefix(strings.TrimSpace(ans.Headline), "OK") {
		warnings = append(warnings, fmt.Sprintf(
			"headline starts with OK although the observation has %d finding(s)", len(ans.Findings)))
	}

	if warning, warn := warnUnexplainedFailure(facts.Verdict, ans.Findings, ans.Checks.Judged); warn {
		warnings = append(warnings, warning)
	}

	return ans, warnings
}

// bareSurfaceKinds are the surface kinds with no ":<name>" suffix (§7.5).
var bareSurfaceKinds = map[string]bool{"scenario": true, "platform": true, "agent": true}

// validSurface implements §7.5's surface grammar: "<kind>:<name>" for
// tool/recipe/check, or a bare kind (scenario, platform, agent). A tool
// surface must name a tool the run actually called, display-name form
// (mcp__ prefix stripped — §7.5: "a tool: name is exactly as called in the
// run … without any mcp__…__ prefix"), an optional "/<action or step>"
// suffix ignored for the purpose of this check; a check surface must name
// one of the run's own check ids. A recipe slug can't be checked against
// the run's own facts, so any non-empty one is accepted.
func validSurface(s string, facts RunFacts) bool {
	if bareSurfaceKinds[s] {
		return true
	}
	kind, name, ok := strings.Cut(s, ":")
	if !ok || name == "" {
		return false
	}
	switch kind {
	case "tool":
		toolName := name
		if before, _, found := strings.Cut(name, "/"); found {
			toolName = before
		}
		return facts.ToolNames[toolName]
	case "recipe":
		return true
	case "check":
		return facts.CheckIDs[name]
	default:
		return false
	}
}

// anchorVerified reports whether f.Anchor occurs, under FM-46's
// normalization, in the full text of at least one step f's evidence cites
// (§7.5: "an anchor that does not occur … in any step the finding cites is
// cleared") — step 0 citing the rendered CHECKS section, like a quote.
func anchorVerified(f *Finding, facts RunFacts) bool {
	for _, ev := range f.Evidence {
		if verifyQuote(facts.Steps, facts.ChecksBody, ev.Step, f.Anchor) {
			return true
		}
	}
	return false
}

// validSpan reports whether sp falls within the run's own step numbering
// (1..len(Steps)) and is not inverted (§7.5: "a span outside the run or
// with from > to is dropped").
func validSpan(sp Span, facts RunFacts) bool {
	if sp.From > sp.To {
		return false
	}
	return sp.From >= 1 && sp.To <= len(facts.Steps)
}

// warnUnexplainedFailure implements §7.5's last repair: a failed run whose
// own findings and judged checks make no visible attempt to explain the
// failure is worth flagging to a maintainer, even though nothing here is
// invalid enough to repair away. Scoped to a "failed" verdict only, per the
// spec's literal wording.
func warnUnexplainedFailure(verdict string, findings []Finding, judged []JudgedCheck) (warning string, warn bool) {
	if verdict != verdictFailed {
		return "", false
	}
	for _, f := range findings {
		if f.CausedVerdict {
			return "", false
		}
	}
	for _, j := range judged {
		if !j.Correct {
			return "", false
		}
	}
	return "no finding explains the failed verdict, and every judged check is marked correct", true
}

// displayToolName strips Claude Code's "mcp__<server>__" prefix from an MCP
// tool's raw transcript name, matching what the observer prompt tells the
// model to write in a finding's surface (§7.5: "a tool: name is exactly as
// called in the run, without any mcp__…__ prefix").
func displayToolName(raw string) string {
	rest, ok := strings.CutPrefix(raw, "mcp__")
	if !ok {
		return raw
	}
	if _, after, found := strings.Cut(rest, "__"); found {
		return after
	}
	return raw
}

// DeriveOutcome implements §7.5's outcome derivation for a parsed answer:
// inconclusive when the session couldn't show whether ZCP works, else
// problem when there is at least one finding, else ok.
func DeriveOutcome(ending string, findingsCount int) string {
	switch ending {
	case EndingSessionLimit, EndingTurnLimit, EndingTimeout, EndingCrashed:
		return OutcomeInconclusive
	}
	if findingsCount > 0 {
		return OutcomeProblem
	}
	return OutcomeOK
}

// DeriveAgree implements §7.5's checks.agree derivation (format 2, never
// model-authored): false when any judged entry is marked incorrect or any
// finding is owned by evaluator, true otherwise.
func DeriveAgree(judged []JudgedCheck, findings []Finding) bool {
	for _, j := range judged {
		if !j.Correct {
			return false
		}
	}
	for _, f := range findings {
		if f.Owner == ownerEvaluator {
			return false
		}
	}
	return true
}

// EffectiveOutcome returns o.Outcome when already derived and stored
// (format 2, set at observe time), or derives it on read for a format-1
// document that predates the field (§7.5: "outcome derived by the same
// rule with ending unknown" — so only ok or problem, never inconclusive).
// A non-"ok" status carries no outcome at all.
func (o *Observation) EffectiveOutcome() string {
	if o.Outcome != "" {
		return o.Outcome
	}
	if o.Status != "ok" {
		return ""
	}
	if len(o.Findings) > 0 {
		return OutcomeProblem
	}
	return OutcomeOK
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

// NormalizeText applies FM-46's full normalization to s: JSON escape
// sequences decoded, backticks and asterisks dropped, whitespace runs
// collapsed. The console clusters anchors across runs with it (§8.6), so a
// quote and the step it came from compare the way the quote check does.
func NormalizeText(s string) string {
	return collapseWhitespace(stripMarkdownPunctuation(decodeJSONEscapes(s)))
}

// DecodeJSONEscapes is decodeJSONEscapes for readers outside the package:
// the console shows tool results with their escapes decoded (§8.3).
func DecodeJSONEscapes(s string) string {
	return decodeJSONEscapes(s)
}
