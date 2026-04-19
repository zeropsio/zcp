package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// manifestFileName is the fixed path (relative to the recipe project root)
// where the content-authoring subagent must write its classification
// manifest before returning. Fixed so the server-side checker can find it
// without state-channel coordination.
const manifestFileName = "ZCP_CONTENT_MANIFEST.json"

// contentManifest is the JSON shape the writer subagent emits. `version` is
// reserved for future evolution of the contract — the check enforces no
// version constraint, but emits the field name in manifest-exists errors
// so the agent can tell a missing manifest from a wrong-shape one.
type contentManifest struct {
	Version int                   `json:"version"`
	Facts   []contentManifestFact `json:"facts"`
}

// contentManifestFact is one per-fact classification+routing decision. Each
// distinct FactRecord.Title in the session's facts log must be represented
// by exactly one entry. Field tags are snake_case — that's the documented
// contract written into the writer subagent brief in recipe.md; renaming
// them to camelCase would silently break the server-subagent handshake.
type contentManifestFact struct {
	FactTitle      string `json:"fact_title"` //nolint:tagliatelle // wire contract with writer subagent
	Classification string `json:"classification"`
	RoutedTo       string `json:"routed_to"`       //nolint:tagliatelle // wire contract with writer subagent
	OverrideReason string `json:"override_reason"` //nolint:tagliatelle // wire contract with writer subagent
}

// discardDefaultClassifications is the set of classification values whose
// default routing is "discarded". Routing them anywhere else requires a
// non-empty override_reason explaining why the default doesn't apply.
// Reframing a scaffold-internal bug into a porter-facing symptom with a
// concrete failure mode is the canonical override path.
var discardDefaultClassifications = map[string]bool{
	"framework-quirk": true,
	"library-meta":    true,
	"self-inflicted":  true,
}

// jaccardHonestyThreshold is the floor similarity at which a DISCARD-marked
// fact's title is considered to have "leaked" into a published gotcha
// stem. Calibrated from the v29 healthCheck-bare-GET case: Jaccard 0.58
// over stop-word-stripped tokens — the 0.3 threshold catches that class
// without false-positives on unrelated gotchas. Semantic reframings where
// vocabulary diverges entirely (e.g. v29's Multer-FormData → "400 Unexpected
// end of form") still slip through; Sub-check B (classification-consistency)
// remains the primary gate for that case.
const jaccardHonestyThreshold = 0.3

// checkWriterContentManifest is the deploy-step post-author enforcement for
// the writer subagent's content-classification contract. It runs four
// sub-checks:
//
//	A. Manifest presence + parse — file exists and is valid JSON.
//	B. Classification consistency — every fact classified framework-quirk /
//	   library-meta / self-inflicted with `routed_to != "discarded"` must
//	   carry a non-empty override_reason.
//	C. Manifest honesty — for each fact routed to "discarded", no published
//	   gotcha stem may Jaccard-match the fact title at or above the
//	   jaccardHonestyThreshold.
//	D. Manifest completeness — every distinct FactRecord.Title in the facts
//	   log must have exactly one manifest entry. Guards against the
//	   deceptive-empty-manifest attack (writer emits {"facts":[]} to bypass
//	   sub-checks B and C trivially).
//
// factsLogPath resolves to ops.FactLogPath(sessionID). The empty string
// indicates test context or a nil resolver — sub-check D passes with a
// skip note in that case.
func checkWriterContentManifest(projectRoot string, readmesByHost map[string]string, factsLogPath string) []workflow.StepCheck {
	path := filepath.Join(projectRoot, manifestFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return []workflow.StepCheck{{
			Name:   "writer_content_manifest_exists",
			Status: statusFail,
			Detail: fmt.Sprintf("content manifest missing at %s — the content-authoring subagent must Write ZCP_CONTENT_MANIFEST.json at the recipe root before returning (see recipe.md content-authoring-brief §'Return contract'). The manifest reports classification + routing for every recorded fact so the deploy-step checker can enforce DISCARD-class routing.", path),
		}}
	}
	var manifest contentManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return []workflow.StepCheck{
			{Name: "writer_content_manifest_exists", Status: statusPass},
			{
				Name:   "writer_content_manifest_valid",
				Status: statusFail,
				Detail: fmt.Sprintf("content manifest invalid JSON at %s: %v. Required shape: {\"version\":1,\"facts\":[{\"fact_title\":...,\"classification\":...,\"routed_to\":...,\"override_reason\":\"\"}]}.", path, err),
			},
		}
	}

	checks := make([]workflow.StepCheck, 0, 6)
	checks = append(checks,
		workflow.StepCheck{Name: "writer_content_manifest_exists", Status: statusPass},
		workflow.StepCheck{Name: "writer_content_manifest_valid", Status: statusPass},
	)
	checks = append(checks, checkManifestClassificationConsistency(manifest)...)
	checks = append(checks, checkManifestHonesty(manifest, readmesByHost)...)
	checks = append(checks, checkManifestCompleteness(manifest, factsLogPath)...)
	return checks
}

// checkManifestClassificationConsistency (Sub-check B) enforces that facts
// with a default-discard classification (framework-quirk, library-meta,
// self-inflicted) either route to "discarded" OR carry a non-empty
// override_reason. A missing reason means the writer silently overrode
// the default without explaining why — this is how v29 shipped
// healthCheck-bare-GET as an apidev gotcha despite its framework-quirk
// classification.
func checkManifestClassificationConsistency(m contentManifest) []workflow.StepCheck {
	var failures []string
	for _, entry := range m.Facts {
		if !discardDefaultClassifications[entry.Classification] {
			continue
		}
		if entry.RoutedTo == "discarded" {
			continue
		}
		if strings.TrimSpace(entry.OverrideReason) != "" {
			continue
		}
		failures = append(failures, fmt.Sprintf(
			"fact %q classified %s but routed to %s without override_reason",
			entry.FactTitle, entry.Classification, entry.RoutedTo,
		))
	}
	if len(failures) == 0 {
		return []workflow.StepCheck{{
			Name: "writer_discard_classification_consistency", Status: statusPass,
		}}
	}
	return []workflow.StepCheck{{
		Name:   "writer_discard_classification_consistency",
		Status: statusFail,
		Detail: "manifest inconsistencies: " + strings.Join(failures, "; ") + ". Either route these facts to 'discarded' OR supply a non-empty override_reason explaining why the default classification doesn't apply (e.g. 'reframed from scaffold-internal bug to porter-facing symptom with concrete failure mode').",
	}}
}

// checkManifestHonesty (Sub-check C) catches the "writer lies about
// routing" case: fact marked discarded in the manifest but a Jaccard-
// similar stem appears in a published README. False-negatives (semantic
// reframings where vocabulary diverges entirely) are accepted — Sub-check
// B is the primary enforcement for that class.
func checkManifestHonesty(m contentManifest, readmesByHost map[string]string) []workflow.StepCheck {
	var failures []string
	for _, entry := range m.Facts {
		if entry.RoutedTo != "discarded" {
			continue
		}
		for host, readme := range readmesByHost {
			stems := extractGotchaStems(readme)
			for _, stem := range stems {
				sim := jaccardSimilarityNoStopwords(entry.FactTitle, stem)
				if sim >= jaccardHonestyThreshold {
					failures = append(failures, fmt.Sprintf(
						"fact %q marked discarded but %s/README.md ships gotcha %q (Jaccard=%.2f)",
						entry.FactTitle, host, stem, sim,
					))
				}
			}
		}
	}
	if len(failures) == 0 {
		return []workflow.StepCheck{{
			Name: "writer_manifest_honesty", Status: statusPass,
		}}
	}
	return []workflow.StepCheck{{
		Name:   "writer_manifest_honesty",
		Status: statusFail,
		Detail: "manifest says discarded but matching gotcha shipped: " + strings.Join(failures, "; ") + ". Either remove the gotcha or update the manifest entry with the correct routed_to + override_reason.",
	}}
}

// checkManifestCompleteness (Sub-check D) compares the set of distinct
// FactRecord.Title values in the facts log against the manifest's fact_title
// set. Any log-present / manifest-absent titles fail the check. Graceful
// skips: empty factsLogPath (test context / nil resolver) and an
// unreadable/missing log file both pass — a real run always produces a log,
// and a missing file in a synthetic test context shouldn't block the rest
// of the checks from running.
func checkManifestCompleteness(m contentManifest, factsLogPath string) []workflow.StepCheck {
	if factsLogPath == "" {
		return []workflow.StepCheck{{
			Name: "writer_manifest_completeness", Status: statusPass,
			Detail: "facts-log path not plumbed; completeness check skipped (test context)",
		}}
	}
	facts, err := ops.ReadFacts(factsLogPath)
	if err != nil {
		return []workflow.StepCheck{{
			Name: "writer_manifest_completeness", Status: statusPass,
			Detail: fmt.Sprintf("facts log unreadable at %s (%v); completeness check skipped", factsLogPath, err),
		}}
	}
	// v8.96 §6.3 — drop downstream-scoped facts before checking
	// completeness. The writer subagent's manifest covers content-lane
	// facts only; a Scope="downstream" fact is scratch knowledge for the
	// next subagent, not published content. Without this filter, the
	// completeness check would force the writer to manifest entries it
	// has no business reasoning about.
	facts = filterContentScoped(facts)
	if len(facts) == 0 {
		return []workflow.StepCheck{{
			Name: "writer_manifest_completeness", Status: statusPass,
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
			Name: "writer_manifest_completeness", Status: statusPass,
		}}
	}
	return []workflow.StepCheck{{
		Name:   "writer_manifest_completeness",
		Status: statusFail,
		Detail: fmt.Sprintf("manifest missing entries for %d distinct FactRecord.Title values that appear in the facts log: %s. Every recorded fact must have exactly one manifest entry with classification + routed_to. An under-populated manifest bypasses sub-checks B and C.", len(missing), strings.Join(missing, "; ")),
	}}
}

// filterContentScoped (v8.96 §6.3) keeps facts whose Scope is content,
// both, or unset (the legacy default). Drops Scope="downstream" — those
// are framework / tooling discoveries routed to dispatch briefs, not to
// the writer subagent's manifest.
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
// stop-word-stripped, lowercased, alphanumeric-only token sets of a and b.
// Both empty sets yield 0.
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

// extractGotchaStems returns the bolded "stem" text from each gotcha bullet
// inside the README's knowledge-base fragment. Expected shape per the
// knowledge-base contract:
//
//	<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
//	## Gotchas
//	- **<stem>** — <explanation>
//	<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
func extractGotchaStems(readme string) []string {
	var stems []string
	inKB := false
	for line := range strings.SplitSeq(readme, "\n") {
		if strings.Contains(line, "ZEROPS_EXTRACT_START:knowledge-base") {
			inKB = true
			continue
		}
		if strings.Contains(line, "ZEROPS_EXTRACT_END:knowledge-base") {
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
