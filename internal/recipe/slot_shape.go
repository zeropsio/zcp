package recipe

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// readFileBytes is a thin wrapper around os.ReadFile that lets
// slot-shape's structural-preservation check stay testable. Returns
// (nil, err) on any read error so callers can no-op rather than
// crashing on missing/unreadable files.
func readFileBytes(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// checkSlotShape enforces per-fragment-id structural constraints at
// record-fragment time (run-16 §8.1). Returns an empty slice when the
// fragment body passes; returns one or more refusal messages when the
// body violates the slot's contract. Run-17 §10 — KB and CLAUDE.md
// surfaces aggregate every offender in a single scan so the agent
// re-authors against the full list in one round-trip (R-17-C10:
// run-16 evidence showed scaffold-api hitting eight successive
// CLAUDE.md refusals naming one hostname each).
//
// Why record-time refusal beats finalize-validator refusal: same-
// context recovery. The agent that just wrote the fragment is still
// in the conversation that knows why it wrote what it wrote — a
// refusal at record time gives the agent a specific, mechanical
// reshape instruction (e.g. "split this multi-heading IG into per-
// slot integration-guide/1, integration-guide/2 fragments"). A
// refusal at finalize is cross-phase and the agent has to re-load
// context.
//
// Legacy fragment IDs (back-compat per §6.5) — `codebase/<h>/integration-guide`
// without the `/<n>` slot suffix, `codebase/<h>/claude-md/{service-facts,notes}`
// sub-slots — are NOT subject to the new constraints; they fall
// through to an empty slice.
func checkSlotShape(fragmentID, body string) []string {
	return checkSlotShapeWithPlan(fragmentID, body, nil)
}

// CheckSlotShapeForReplay is the exported entry point used by the
// `cmd/zcp-recipe-sim (validate subcommand)` tool to run the dispatcher against
// fragments authored offline. Identical to the internal
// `checkSlotShapeWithPlan` — exported only to let the throwaway replay
// CLI consume it without forking the recipe package.
func CheckSlotShapeForReplay(fragmentID, body string, plan *Plan) []string {
	return checkSlotShapeWithPlan(fragmentID, body, plan)
}

// checkSlotShapeWithPlan is the plan-aware dispatcher. The IG fusion
// check (run-18 §3.1 check 4) needs the plan's managed-service hostname
// set to detect multi-service fusion in one slotted IG body. Body-only
// authoring-discipline checks (run-18 checks 1/2/3) don't need plan
// context; tests can keep calling checkSlotShape with no plan and still
// exercise everything except Check 4.
func checkSlotShapeWithPlan(fragmentID, body string, plan *Plan) []string {
	switch {
	case fragmentID == fragmentIDRoot:
		return single(checkRootIntro(body))
	case envIntroRe.MatchString(fragmentID):
		return single(checkEnvIntro(body))
	case envImportCommentsRe.MatchString(fragmentID):
		out := single(checkEnvImportComments(body))
		out = append(out, commentSurfaceSlugCitationRefusals(body, "env/<N>/import-comments/<host>")...)
		return out
	case codebaseIntroRe.MatchString(fragmentID):
		return single(checkCodebaseIntro(body))
	case slottedIGRe.MatchString(fragmentID):
		out := single(checkSlottedIG(body))
		out = append(out, igSlotAuthoringRefusals(body, managedServiceHostnames(plan))...)
		return out
	case codebaseKBRe.MatchString(fragmentID):
		return checkCodebaseKBAll(body)
	case codebaseZeropsYamlRe.MatchString(fragmentID):
		return checkCodebaseZeropsYaml(fragmentID, body, plan)
	case singleSlotClaudeMDRe.MatchString(fragmentID):
		return checkClaudeMDAll(body)
	}
	return nil
}

// single wraps a possibly-empty single-violation string into the
// []string contract that checkSlotShape returns.
func single(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

var (
	envIntroRe            = regexp.MustCompile(`^env/[0-9]+/intro$`)
	envImportCommentsRe   = regexp.MustCompile(`^env/[0-9]+/import-comments/[A-Za-z0-9_-]+$`)
	codebaseIntroRe       = regexp.MustCompile(`^codebase/[A-Za-z0-9_-]+/intro$`)
	slottedIGRe           = regexp.MustCompile(`^codebase/[A-Za-z0-9_-]+/integration-guide/[0-9]+$`)
	codebaseKBRe          = regexp.MustCompile(`^codebase/[A-Za-z0-9_-]+/knowledge-base$`)
	codebaseZeropsYamlRe  = regexp.MustCompile(`^codebase/[A-Za-z0-9_-]+/zerops-yaml$`)
	singleSlotClaudeMDRe  = regexp.MustCompile(`^codebase/[A-Za-z0-9_-]+/claude-md$`)
	headingH2Re           = regexp.MustCompile(`(?m)^##\s+`)
	headingH3Re           = regexp.MustCompile(`(?m)^###\s+`)
	zeropsExtractMarkerRe = regexp.MustCompile(`<!--\s*#ZEROPS_EXTRACT_`)
	zeropsHeadingRe       = regexp.MustCompile(`(?m)^##\s+Zerops\b`)
	zeropsToolRe          = regexp.MustCompile(`\bzerops_[a-z_]+`)
	zscRe                 = regexp.MustCompile(`\bzsc\b`)
	zcpRe                 = regexp.MustCompile(`\bzcp\b`)
	zcliRe                = regexp.MustCompile(`\bzcli\b`)

	// Run-17 §7 — KB stem symptom-first heuristic. Any one of these
	// signals is sufficient: HTTP status code, backtick or double-
	// quoted token, failure verb, or observable wrong-state phrase.
	// Per implementation guide §7 Option (A): the heuristic is stem-
	// only and accepts the `synchronize: false` style false-positive
	// (the backtick regex matches both error strings and config keys).
	// Refinement at Tranche 4 catches the residual author-claim stems.
	kbStemBoldRE        = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	kbStemHTTPCodeRE    = regexp.MustCompile(`\b[1-5]\d{2}\b`)
	kbStemQuotedErrorRE = regexp.MustCompile("`[^`]+`|\"[^\"]+\"")
	kbStemFailureVerbRE = regexp.MustCompile(
		`(?i)\b(fails|crashes|corrupts|deadlocks|silently exits|silently stops|returns null|breaks|drops|rejects|missing|hangs|times out|panics|leaks|stalls|truncates|drained)\b`)
	kbStemObservableRE = regexp.MustCompile(
		`(?i)\b(empty body|wrong header|null where|404 on|502 on|empty response|stale data|zero rows|no rows|unbound|undefined|forbidden)\b`)
)

func checkRootIntro(body string) string {
	if len(body) > 500 {
		return fmt.Sprintf("root/intro is a 1-sentence string, %d chars > 500-cap. See spec §Surface 1.", len(body))
	}
	if headingH2Re.MatchString(body) || headingH3Re.MatchString(body) {
		return "root/intro is a 1-sentence string with no markdown headings. See spec §Surface 1."
	}
	return ""
}

func checkEnvIntro(body string) string {
	if len(body) > 350 {
		return fmt.Sprintf("env/<N>/intro is a 1-2 sentence string, %d chars > 350-cap. See spec §Surface 2.", len(body))
	}
	if headingH2Re.MatchString(body) {
		return "env/<N>/intro must not contain `## ` headings. See spec §Surface 2."
	}
	if zeropsExtractMarkerRe.MatchString(body) {
		// R-15-3 closure — duplicate extract markers in env intros came from
		// agents nesting #ZEROPS_EXTRACT_* tokens inside the slot.
		return "env/<N>/intro must not contain `<!-- #ZEROPS_EXTRACT_*` tokens (R-15-3); the extract markers are engine-stamped at stitch time."
	}
	return ""
}

func checkEnvImportComments(body string) string {
	lines := strings.Count(body, "\n")
	if !strings.HasSuffix(body, "\n") && body != "" {
		lines++ // trailing-newline-less body still counts the last line
	}
	if lines > 8 {
		return fmt.Sprintf("env/<N>/import-comments/<host> ≤ 8 lines; got %d. See spec §Surface 3.", lines)
	}
	return ""
}

func checkCodebaseIntro(body string) string {
	if len(body) > 500 {
		return fmt.Sprintf("codebase/<h>/intro is a 1-2 sentence string, %d chars > 500-cap. See spec §Surface 4.", len(body))
	}
	if headingH2Re.MatchString(body) {
		return "codebase/<h>/intro must not contain `## ` headings. See spec §Surface 4."
	}
	return ""
}

func checkSlottedIG(body string) string {
	headings := headingH3Re.FindAllStringIndex(body, -1)
	if len(headings) != 1 {
		// R-15-5 closure: per-slot IG fragments are exactly one item.
		// Multiple ### headings in one slot mean the agent collapsed
		// multiple items into one slot — refuse so they split.
		return fmt.Sprintf(
			"codebase/<h>/integration-guide/<n> is one item: exactly one `### ` heading per slot, got %d. Split into separate slots. See spec §Surface 4.",
			len(headings),
		)
	}
	// Body line cap (excluding the heading line itself).
	lines := strings.Split(body, "\n")
	if len(lines) > 30 {
		return fmt.Sprintf("codebase/<h>/integration-guide/<n> body ≤ 30 lines; got %d. See spec §Surface 4.", len(lines))
	}
	return ""
}

// kbStemMatchesSymptomFirst returns true when the stem text between
// the leading `**...**` carries a porter-searchable signal. Stem-only
// check per implementation guide §7 Option (A) — the heuristic ORs
// across multiple signal classes and accepts the backtick-config-key
// false-positive. Refinement (Tranche 4) catches the residual author-
// claim stems.
func kbStemMatchesSymptomFirst(stem string) bool {
	return kbStemHTTPCodeRE.MatchString(stem) ||
		kbStemQuotedErrorRE.MatchString(stem) ||
		kbStemFailureVerbRE.MatchString(stem) ||
		kbStemObservableRE.MatchString(stem)
}

// checkCodebaseKBAll walks every bullet collecting refusals; returns
// the full list so the agent can re-author against every offender in
// one round-trip. Run-17 §10. Bullet-shape and stem-shape failures
// are collected per bullet; the cap-violation (over 8 bullets)
// appends after the per-bullet pass so the agent sees both surface
// failures and the cap blocker in one response.
func checkCodebaseKBAll(body string) []string {
	var out []string
	bulletCount := 0
	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		bulletCount++
		rest := strings.TrimPrefix(trimmed, "- ")
		if !strings.HasPrefix(rest, "**") {
			out = append(out,
				"codebase/<h>/knowledge-base bullets must follow `- **Topic** — 2-4 sentences` shape (no leading `**` found). See spec §Surface 5.")
			continue
		}
		m := kbStemBoldRE.FindStringSubmatch(rest)
		stem := ""
		if len(m) >= 2 {
			stem = m[1]
			if !kbStemMatchesSymptomFirst(stem) {
				out = append(out, fmt.Sprintf(
					"codebase/<h>/knowledge-base stem `%s` is author-claim shape; KB stems are symptom-first or directive-tightly-mapped-to-observable-error. Reshape: name the HTTP status code, quoted error string, failure verb, or observable wrong-state phrase the porter would search for. See `briefs/refinement/reference_kb_shapes.md`.",
					stem))
			}
		}
		// Run-18 §3.1 — authoring-discipline body checks (self-inflicted,
		// scaffold-internal, slug citation). Bullet body is the whole
		// line past `- ` — the run-17 corpus shows KB bullets land as
		// single-line paragraphs, matching the existing line-iteration
		// assumption.
		out = append(out, kbBulletAuthoringRefusals(stem, rest)...)
	}
	if bulletCount > 8 {
		out = append(out, fmt.Sprintf("codebase/<h>/knowledge-base ≤ 8 bullets; got %d. See spec §Surface 5.", bulletCount))
	}
	return out
}

// checkClaudeMDAll walks the body collecting every Zerops-content
// leak and the cap/H2-shape violations in one pass. Run-17 §10 —
// run-16 scaffold-api hit eight successive single-violation refusals
// (one per managed-service hostname) before the agent gave up; this
// aggregator surfaces all of them together so the agent re-authors
// once.
func checkClaudeMDAll(body string) []string {
	var out []string
	if zeropsHeadingRe.MatchString(body) {
		out = append(out,
			"codebase/<h>/claude-md must not contain `## Zerops` headings (R-15-4); Zerops platform content belongs in IG/KB/zerops.yaml comments per spec §Surface 6.")
	}
	for _, hit := range []struct {
		re    *regexp.Regexp
		token string
	}{
		{zscRe, "zsc"},
		{zeropsToolRe, "zerops_*"},
		{zcpRe, "zcp"},
		{zcliRe, "zcli"},
	} {
		if hit.re.MatchString(body) {
			out = append(out, fmt.Sprintf(
				"codebase/<h>/claude-md must not contain `%s` tool references (R-15-4); CLAUDE.md is a Zerops-free codebase guide per spec §Surface 6.",
				hit.token))
		}
	}
	lines := strings.Count(body, "\n")
	if !strings.HasSuffix(body, "\n") && body != "" {
		lines++
	}
	if lines > 80 {
		out = append(out, fmt.Sprintf("codebase/<h>/claude-md ≤ 80 lines; got %d. See spec §Surface 6.", lines))
	}
	h2 := headingH2Re.FindAllStringIndex(body, -1)
	if len(h2) < 2 || len(h2) > 4 {
		out = append(out, fmt.Sprintf(
			"codebase/<h>/claude-md must have 2-4 `## ` sections (Build & run, Architecture, optional extras); got %d. See spec §Surface 6.",
			len(h2)))
	}
	return out
}

// checkCodebaseZeropsYaml runs whole-yaml refusals on a
// `codebase/<h>/zerops-yaml` fragment body. Run-21-prep — the agent
// records the entire commented zerops.yaml as one fragment; refusals
// here cover authoring-discipline (no doc-link punts), citation shape
// (cite-by-mechanism, not by slug), and structural preservation (the
// agent owns comments, NOT yaml structure).
//
// Schema-conformance is NOT checked here — it fires at codebase-content
// complete-phase via gateZeropsYamlSchema (which reads on-disk yaml).
// Slot-shape stays cheap; gate-time runs the heavier check.
func checkCodebaseZeropsYaml(fragmentID, body string, plan *Plan) []string {
	var out []string
	if strings.TrimSpace(body) == "" {
		return []string{"codebase/<h>/zerops-yaml: body is empty; record the whole commented zerops.yaml as one fragment"}
	}
	// Anti-pattern: doc-link punts. The fragment-shape audit (run-21-
	// prep) caught a regression at apidev/zerops.yaml:131 where a yaml
	// comment ended with `Read more about it here: https://...`. Inline
	// the rationale; never punt to a URL.
	for ln := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimSpace(ln)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		lower := strings.ToLower(trimmed)
		switch {
		case strings.Contains(lower, "read more about it here"),
			strings.Contains(lower, "more information at"),
			strings.Contains(lower, "more info at"),
			strings.Contains(lower, "see docs:"),
			strings.Contains(lower, "see also:"),
			strings.Contains(lower, "for more details, see"):
			out = append(out, fmt.Sprintf("codebase/<h>/zerops-yaml: yaml comment punts to a URL/docs reference (%q); inline the rationale instead — name the constraint, the symptom, and the fix",
				trimForMessage(trimmed)))
		}
	}
	// Cite-by-mechanism on every comment line. The legacy per-block
	// validator scoped this to comment fragments; whole-yaml inherits
	// the same rule across every `# ...` line in the body.
	out = append(out, commentSurfaceSlugCitationRefusals(body, "codebase/<h>/zerops-yaml")...)

	// Structural preservation: agent owns comments only; yaml shape is
	// scaffold's contract. Strip comments from the fragment body AND
	// from the current on-disk yaml; compare. If they differ, the agent
	// added/removed/reordered keys.
	if msg := checkZeropsYamlStructurePreserved(fragmentID, body, plan); msg != "" {
		out = append(out, msg)
	}
	return out
}

// checkZeropsYamlStructurePreserved verifies the agent's whole-yaml
// fragment body, when comments are stripped, matches the on-disk bare
// yaml byte-for-byte. The fragment may add `# ` comment lines wherever
// porter-value calls for one; it may not modify any non-comment line.
//
// On-disk yaml at record-fragment time:
//   - First record after scaffold → bare scaffold yaml (no comments).
//     stripYAMLComments(bare) == bare. Compare to stripYAMLComments(body).
//   - Re-record (refinement HOLD): on-disk = previously-committed
//     commented yaml. stripYAMLComments(on-disk) == bare again. Compare
//     to stripYAMLComments(body). Same bare, both ways.
//
// Skips the check if SourceRoot or on-disk yaml is missing — early-
// phase tests + chain-parent codebases don't have one.
func checkZeropsYamlStructurePreserved(fragmentID, body string, plan *Plan) string {
	if plan == nil {
		return ""
	}
	host := codebaseHostFromFragmentID(fragmentID)
	if host == "" {
		return ""
	}
	var srcRoot string
	for _, cb := range plan.Codebases {
		if cb.Hostname == host {
			srcRoot = cb.SourceRoot
			break
		}
	}
	if srcRoot == "" {
		return ""
	}
	yamlPath := srcRoot + "/zerops.yaml"
	raw, err := readFileBytes(yamlPath)
	if err != nil {
		return ""
	}
	expected := normalizeYAMLForCompare(stripYAMLComments(string(raw)))
	actual := normalizeYAMLForCompare(stripYAMLComments(body))
	if expected == actual {
		return ""
	}
	return fmt.Sprintf("codebase/<h>/zerops-yaml: structural changes to yaml refused — agent owns comments, not the yaml shape. Add `# ` lines wherever porter value calls for one; do not add/remove/reorder yaml keys. Diff (stripped):\n--- bare (on-disk)\n%s\n--- fragment\n%s",
		expected, actual)
}

// normalizeYAMLForCompare canonicalizes yaml text for byte-equality
// comparison: strips a leading UTF-8 BOM, normalizes CRLF/CR line
// endings to LF, trims trailing whitespace from each line, and trims
// trailing blank lines. Without normalization the structural-
// preservation check false-refuses on agents that author with editor
// defaults (CRLF on Windows shells, BOM from some editors, trailing
// spaces). The check measures yaml SHAPE, not byte exactness.
func normalizeYAMLForCompare(s string) string {
	s = strings.TrimPrefix(s, "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimRight(ln, " \t")
	}
	out := strings.Join(lines, "\n")
	return strings.TrimRight(out, "\n")
}

// claudeMDFragmentRefusalForHostname extends checkClaudeMDAll with a
// plan-time hostname check: the handler walks plan.Services and calls
// this once per declared hostname. Static-token coverage in
// checkClaudeMDAll catches `zsc` / `zerops_*` / `zcp` / `zcli` /
// `## Zerops`; the per-host loop catches managed-service hostname
// leakage like `db`, `cache`, `search`, `meilisearch`, etc. that the
// static list can't enumerate.
func claudeMDFragmentRefusalForHostname(body, hostname string) string {
	if hostname == "" {
		return ""
	}
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(hostname) + `\b`)
	if re.MatchString(body) {
		return fmt.Sprintf("codebase/<h>/claude-md must not reference managed-service hostname `%s` (R-15-4); the Zerops integration belongs in IG/KB/zerops.yaml comments per spec §Surface 6.", hostname)
	}
	return ""
}
