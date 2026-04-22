package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// ManifestFileName is the fixed path (relative to the recipe project
// root) where the content-authoring subagent writes its classification
// manifest before returning. Fixed so every checker can find it
// without state-channel coordination.
const ManifestFileName = "ZCP_CONTENT_MANIFEST.json"

// ContentManifest is the JSON shape the writer subagent emits. `Version`
// is reserved for future evolution of the contract; checks enforce no
// version constraint but surface the field name in manifest-exists
// errors so the agent can tell a missing manifest from a wrong-shape
// one.
type ContentManifest struct {
	Version int                   `json:"version"`
	Facts   []ContentManifestFact `json:"facts"`
}

// ContentManifestFact is one per-fact classification+routing decision.
// Each distinct FactRecord.Title in the session's facts log must be
// represented by exactly one entry. Field tags are snake_case — the
// documented wire contract with the writer subagent; renaming them to
// camelCase would silently break the server-subagent handshake.
type ContentManifestFact struct {
	FactTitle      string             `json:"fact_title"` //nolint:tagliatelle // wire contract with writer subagent
	Classification string             `json:"classification"`
	RoutedTo       string             `json:"routed_to"`       //nolint:tagliatelle // wire contract with writer subagent
	OverrideReason string             `json:"override_reason"` //nolint:tagliatelle // wire contract with writer subagent
	Citations      []ManifestCitation `json:"citations,omitempty"`
}

// ManifestCitation is one guide-fetch record the writer declares per
// fact. v39 Commit 4 — for every fact routed to content_gotcha or
// content_ig, the writer must have called zerops_knowledge at least
// once during authoring and recorded the topic + timestamp here.
// Empty citations list on a content_gotcha / content_ig entry fails
// the readmes_citations_present check; the check turns "is this bullet
// folk-doctrine?" into "did the knowledge fetch happen before the
// bullet was written?" — a file-existence question with zero
// subjectivity.
//
// Topic MUST be one of the knowledge topic IDs that zerops_knowledge
// accepts (env-var-model, init-commands, rolling-deploys, etc. — see
// docs/spec-content-surfaces.md §8 citation map). GuideFetchedAt is
// the RFC3339 timestamp the writer recorded when it made the
// zerops_knowledge call; the check treats any non-empty value as
// passing (trust the writer; the presence of SOME timestamp proves
// the lookup happened).
type ManifestCitation struct {
	Topic          string `json:"topic"`
	GuideFetchedAt string `json:"guide_fetched_at"` //nolint:tagliatelle // wire contract with writer subagent
}

// LoadContentManifest reads and parses ZCP_CONTENT_MANIFEST.json at
// projectRoot/ManifestFileName. Returns the parsed manifest and nil
// error when the file exists and is valid JSON. Returns nil + error
// on file-missing or parse failure; the caller decides how to surface
// those (as fails, or silent graceful passes in checks downstream).
func LoadContentManifest(projectRoot string) (*ContentManifest, error) {
	data, err := os.ReadFile(filepath.Join(projectRoot, ManifestFileName))
	if err != nil {
		return nil, err
	}
	var m ContentManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// jaccardHonestyThreshold is the floor similarity at which a
// DISCARD-marked fact's title is considered to have "leaked" into a
// published gotcha stem. Calibrated from the v29 healthCheck-bare-GET
// case (Jaccard 0.58 over stop-word-stripped tokens); 0.3 catches that
// class without false-positives on unrelated gotchas. Semantic
// reframings where vocabulary diverges entirely (e.g. v29's
// Multer-FormData → "400 Unexpected end of form") still slip through;
// the classification-consistency check remains the primary gate for
// that case.
const jaccardHonestyThreshold = 0.3

// honestyDimension describes one (routed_to × surface) pair the
// manifest-honesty check grades. Each dimension emits exactly one
// StepCheck row; C-8's expansion (per check-rewrite.md §12) fires one
// row per dimension so the agent sees per-pair pass/fail detail
// instead of a single aggregate.
type honestyDimension struct {
	// routeToMatch returns true when the fact's RoutedTo participates
	// in this dimension. For the 5 `X_as_gotcha` dimensions, the match
	// is equality against a single route value; for `any_as_intro`,
	// the match accepts any non-empty RoutedTo that isn't content_intro
	// (a content_intro-routed fact appearing in intro is correct).
	routeToMatch func(routedTo string) bool
	// surfaceExtractor returns the stem/phrase tokens from readme
	// content that the dimension compares fact titles against via
	// Jaccard similarity. For gotcha dimensions this is the bolded
	// stems from the knowledge-base fragment; for intro it's the
	// intro-fragment body split into comparable phrase-units.
	surfaceExtractor func(readme string) []string
	// checkName is the StepCheck.Name emitted for this dimension.
	checkName string
	// routedToLabel appears in fail details, naming the offending
	// routing value so the agent can locate the manifest entry.
	routedToLabel string
	// surfaceLabel appears in fail details, naming the leakage surface.
	surfaceLabel string
	// failGuidance is appended to each fail row's Detail as a short
	// actionable remedy.
	failGuidance string
}

// honestyDimensions enumerates the 6 (routed_to × surface) pairs
// C-8 grades. Order is stable so downstream consumers see rows in a
// consistent sequence across runs. Changing the order is a wire-contract
// change visible to the agent.
var honestyDimensions = []honestyDimension{
	{
		routeToMatch:     routedEquals("discarded"),
		surfaceExtractor: extractGotchaStems,
		checkName:        "writer_manifest_honesty_discarded_as_gotcha",
		routedToLabel:    "discarded",
		surfaceLabel:     "knowledge-base gotcha",
		failGuidance:     "either remove the gotcha or update the manifest entry with the correct routed_to + override_reason",
	},
	{
		routeToMatch:     routedEquals("claude_md"),
		surfaceExtractor: extractGotchaStems,
		checkName:        "writer_manifest_honesty_claude_md_as_gotcha",
		routedToLabel:    "claude_md",
		surfaceLabel:     "knowledge-base gotcha",
		failGuidance:     "claude_md-routed facts belong in the repo-local CLAUDE.md; remove the duplicate gotcha or reclassify",
	},
	{
		routeToMatch:     routedEquals("content_ig"),
		surfaceExtractor: extractGotchaStems,
		checkName:        "writer_manifest_honesty_integration_guide_as_gotcha",
		routedToLabel:    "content_ig",
		surfaceLabel:     "knowledge-base gotcha",
		failGuidance:     "content_ig-routed facts belong in the integration-guide section; remove the duplicate gotcha or reclassify",
	},
	{
		routeToMatch:     routedEquals("zerops_yaml_comment"),
		surfaceExtractor: extractGotchaStems,
		checkName:        "writer_manifest_honesty_zerops_yaml_comment_as_gotcha",
		routedToLabel:    "zerops_yaml_comment",
		surfaceLabel:     "knowledge-base gotcha",
		failGuidance:     "zerops_yaml_comment-routed facts belong inline in zerops.yaml comments; remove the duplicate gotcha or reclassify",
	},
	{
		routeToMatch:     routedEquals("content_env_comment"),
		surfaceExtractor: extractGotchaStems,
		checkName:        "writer_manifest_honesty_env_comment_as_gotcha",
		routedToLabel:    "content_env_comment",
		surfaceLabel:     "knowledge-base gotcha",
		failGuidance:     "content_env_comment-routed facts belong inline in environments/*/import.yaml comments; remove the duplicate gotcha or reclassify",
	},
	{
		routeToMatch:     routedAnyExceptIntroOrEmpty,
		surfaceExtractor: extractIntroPhrases,
		checkName:        "writer_manifest_honesty_any_as_intro",
		routedToLabel:    "non-intro",
		surfaceLabel:     "intro fragment",
		failGuidance:     "only content_intro-routed facts belong in the intro fragment; relocate the leaked concept to its declared surface",
	},
}

// routedEquals returns a match predicate for a single RoutedTo value.
func routedEquals(target string) func(string) bool {
	return func(routedTo string) bool {
		return routedTo == target
	}
}

// routedAnyExceptIntroOrEmpty matches facts routed to anything except
// content_intro (the correct destination for intro content) OR an
// empty string (legacy / unclassified facts; the manifest-route-to
// check enforces classification independently).
func routedAnyExceptIntroOrEmpty(routedTo string) bool {
	return routedTo != "" && routedTo != "content_intro"
}

// CheckManifestHonesty catches the "writer lies about routing" case
// across all 6 (routed_to × surface) dimensions per check-rewrite.md §12.
// For each dimension: if any fact whose RoutedTo matches the dimension
// has a Jaccard-similar stem appearing on the dimension's leakage
// surface, that dimension fails with the offending pair(s) named.
// Dimensions with no matching facts (or no leakage) pass.
//
// Emits one StepCheck row per dimension — always the full 6 rows so the
// agent sees a stable per-dimension surface across runs. False-negatives
// (semantic reframings where vocabulary diverges entirely) are accepted
// — the classification-consistency check is the primary enforcement for
// that class.
func CheckManifestHonesty(_ context.Context, m *ContentManifest, readmesByHost map[string]string) []workflow.StepCheck {
	if m == nil {
		return nil
	}
	rows := make([]workflow.StepCheck, 0, len(honestyDimensions))
	for _, dim := range honestyDimensions {
		rows = append(rows, evaluateHonestyDimension(dim, m, readmesByHost))
	}
	return rows
}

// evaluateHonestyDimension computes a single dimension's pass/fail
// verdict against the manifest + readme corpus. Kept separate so each
// dimension's logic is independently testable + the CheckManifestHonesty
// body stays a one-line loop over the declarative honestyDimensions
// table.
func evaluateHonestyDimension(dim honestyDimension, m *ContentManifest, readmesByHost map[string]string) workflow.StepCheck {
	var failures []string
	for _, entry := range m.Facts {
		if !dim.routeToMatch(entry.RoutedTo) {
			continue
		}
		for host, readme := range readmesByHost {
			stems := dim.surfaceExtractor(readme)
			for _, stem := range stems {
				sim := jaccardSimilarityNoStopwords(entry.FactTitle, stem)
				if sim >= jaccardHonestyThreshold {
					failures = append(failures, fmt.Sprintf(
						"fact %q marked %s but %s/README.md %s %q (Jaccard=%.2f)",
						entry.FactTitle, dim.routedToLabel, host, dim.surfaceLabel, stem, sim,
					))
				}
			}
		}
	}
	if len(failures) == 0 {
		return workflow.StepCheck{
			Name:   dim.checkName,
			Status: StatusPass,
		}
	}
	return workflow.StepCheck{
		Name:   dim.checkName,
		Status: StatusFail,
		Detail: fmt.Sprintf("manifest says %s but matching %s shipped: %s. %s.",
			dim.routedToLabel, dim.surfaceLabel, strings.Join(failures, "; "), dim.failGuidance),
	}
}

// CheckManifestCompleteness compares the set of distinct
// FactRecord.Title values in the facts log against the manifest's
// fact_title set. Any log-present / manifest-absent titles fail the
// check.
//
// Graceful skips: empty factsLogPath (test context / nil resolver) and
// an unreadable/missing log file both pass — a real run always produces
// a log, and a missing file in a synthetic test context shouldn't block
// the rest of the checks from running.
func CheckManifestCompleteness(_ context.Context, m *ContentManifest, factsLogPath string) []workflow.StepCheck {
	if m == nil {
		return nil
	}
	if factsLogPath == "" {
		return []workflow.StepCheck{{
			Name:   "writer_manifest_completeness",
			Status: StatusPass,
			Detail: "facts-log path not plumbed; completeness check skipped (test context)",
		}}
	}
	facts, err := ops.ReadFacts(factsLogPath)
	if err != nil {
		return []workflow.StepCheck{{
			Name:   "writer_manifest_completeness",
			Status: StatusPass,
			Detail: fmt.Sprintf("facts log unreadable at %s (%v); completeness check skipped", factsLogPath, err),
		}}
	}
	// v8.96 §6.3 — drop downstream-scoped facts before checking
	// completeness. The writer subagent's manifest covers content-lane
	// facts only; a Scope="downstream" fact is scratch knowledge for
	// the next subagent, not published content. Without this filter,
	// the completeness check would force the writer to manifest
	// entries it has no business reasoning about.
	facts = filterContentScoped(facts)
	if len(facts) == 0 {
		return []workflow.StepCheck{{
			Name:   "writer_manifest_completeness",
			Status: StatusPass,
		}}
	}
	logTitles := make(map[string]bool, len(facts))
	for _, f := range facts {
		if t := strings.TrimSpace(f.Title); t != "" {
			logTitles[t] = true
		}
	}
	manifestTitles := make(map[string]bool, len(m.Facts))
	for _, entry := range m.Facts {
		if t := strings.TrimSpace(entry.FactTitle); t != "" {
			manifestTitles[t] = true
		}
	}
	var missing []string
	for title := range logTitles {
		if !manifestTitles[title] {
			missing = append(missing, title)
		}
	}
	if len(missing) == 0 {
		return []workflow.StepCheck{{
			Name:   "writer_manifest_completeness",
			Status: StatusPass,
		}}
	}
	return []workflow.StepCheck{{
		Name:   "writer_manifest_completeness",
		Status: StatusFail,
		Detail: fmt.Sprintf(
			"manifest missing %d entries whose `fact_title` matches a `title` from the facts log: %s. Each facts-log entry (JSON key `title`) must have exactly one manifest entry in ZCP_CONTENT_MANIFEST.json whose `fact_title` equals that title, with `classification` + `routed_to` set. An under-populated manifest bypasses the classification-consistency and honesty sub-checks.",
			len(missing), strings.Join(missing, "; "),
		),
	}}
}

// filterContentScoped (v8.96 §6.3) keeps facts whose Scope is content,
// both, or unset (the legacy default). Drops Scope="downstream" —
// those are framework / tooling discoveries routed to dispatch briefs,
// not to the writer subagent's manifest.
func filterContentScoped(facts []ops.FactRecord) []ops.FactRecord {
	out := make([]ops.FactRecord, 0, len(facts))
	for _, f := range facts {
		if f.Scope == ops.FactScopeDownstream {
			continue
		}
		out = append(out, f)
	}
	return out
}

// jaccardStopWords are tokens filtered out before Jaccard similarity
// comparison. Keeps the signal on content tokens (platform terms,
// identifiers, failure modes) rather than grammatical glue.
var jaccardStopWords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "are": true, "was": true,
	"must": true, "may": true, "can": true, "should": true, "for": true,
	"of": true, "in": true, "on": true, "at": true, "to": true, "from": true,
	"have": true, "has": true, "had": true, "be": true, "been": true,
	"if": true, "when": true, "not": true, "no": true, "with": true,
	"by": true, "as": true,
}

// jaccardSimilarityNoStopwords returns |A∩B| / |A∪B| over the
// stop-word-stripped, lowercased, alphanumeric-only token sets of a
// and b. Both empty sets yield 0.
func jaccardSimilarityNoStopwords(a, b string) float64 {
	ta := tokenizeForJaccard(a)
	tb := tokenizeForJaccard(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	setA := make(map[string]bool, len(ta))
	for _, t := range ta {
		setA[t] = true
	}
	setB := make(map[string]bool, len(tb))
	for _, t := range tb {
		setB[t] = true
	}
	intersect := 0
	for t := range setA {
		if setB[t] {
			intersect++
		}
	}
	union := len(setA) + len(setB) - intersect
	if union == 0 {
		return 0
	}
	return float64(intersect) / float64(union)
}

// tokenizeForJaccard lowercases, splits on any non-alphanumeric, drops
// stop words and empty tokens.
func tokenizeForJaccard(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		t := strings.ToLower(b.String())
		b.Reset()
		if jaccardStopWords[t] {
			return
		}
		out = append(out, t)
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// extractIntroPhrases returns comparable phrase units from the intro
// fragment body. The intro is prose (not bullet-stem-structured like
// the gotcha fragment), so "phrase units" are individual sentences —
// split by . / ! / ? — trimmed of whitespace. Short fragments (< 12
// runes) are discarded since they carry too few tokens for a
// meaningful Jaccard comparison. A fact title leaking into prose
// typically appears as a near-verbatim sentence; sentence-level
// Jaccard is the right granularity.
func extractIntroPhrases(readme string) []string {
	inIntro := false
	var body strings.Builder
	for line := range strings.SplitSeq(readme, "\n") {
		// Cx-MARKER-FORM-FIX: require the trailing `#` sentinel so
		// this helper stays consistent with the primary fragment
		// check (checkReadmeFragments uses the exact form). Broken
		// markers are reported by fragment_marker_exact_form; this
		// helper treats them as absent.
		if strings.Contains(line, "ZEROPS_EXTRACT_START:intro#") {
			inIntro = true
			continue
		}
		if strings.Contains(line, "ZEROPS_EXTRACT_END:intro#") {
			inIntro = false
			continue
		}
		if !inIntro {
			continue
		}
		body.WriteString(line)
		body.WriteByte('\n')
	}
	text := strings.TrimSpace(body.String())
	if text == "" {
		return nil
	}
	var phrases []string
	current := strings.Builder{}
	flush := func() {
		p := strings.TrimSpace(current.String())
		current.Reset()
		if len([]rune(p)) < 12 {
			return
		}
		phrases = append(phrases, p)
	}
	for _, r := range text {
		switch r {
		case '.', '!', '?':
			flush()
		case '\n', '\r':
			current.WriteRune(' ')
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return phrases
}

// extractGotchaStems returns the bolded "stem" text from each gotcha
// bullet inside the README's knowledge-base fragment. Expected shape
// per the knowledge-base contract:
//
//	<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
//	## Gotchas
//	- **<stem>** — <explanation>
//	<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
func extractGotchaStems(readme string) []string {
	var stems []string
	inKB := false
	for line := range strings.SplitSeq(readme, "\n") {
		// Cx-MARKER-FORM-FIX: require the trailing `#` sentinel. See
		// extractIntroPhrases for rationale.
		if strings.Contains(line, "ZEROPS_EXTRACT_START:knowledge-base#") {
			inKB = true
			continue
		}
		if strings.Contains(line, "ZEROPS_EXTRACT_END:knowledge-base#") {
			inKB = false
			continue
		}
		if !inKB || !strings.HasPrefix(strings.TrimSpace(line), "- **") {
			continue
		}
		_, rest, ok := strings.Cut(line, "**")
		if !ok {
			continue
		}
		stem, _, ok := strings.Cut(rest, "**")
		if !ok {
			continue
		}
		if stem != "" {
			stems = append(stems, stem)
		}
	}
	return stems
}
