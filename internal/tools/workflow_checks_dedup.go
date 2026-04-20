package tools

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// checkCrossReadmeGotchaUniqueness is the cross-codebase duplicate-gotcha
// check. Each per-codebase README gets its own predecessor-floor and
// authenticity scoring in isolation, so the optimal agent strategy to hit
// both floors is to stamp the same 3-4 authentic gotchas into every
// README. v15's nestjs-showcase had the NATS-credential fact appearing in
// api + worker, SSHFS ownership in api + worker, and zsc-execOnce burn in
// api + worker — five total mentions of facts that belong in one place.
//
// The rule: each normalized gotcha stem may appear in at most one
// codebase's README. A fact that applies to multiple codebases (NATS
// client credentials, execOnce burn recovery, SSHFS uid fix) lives in
// exactly one README — by convention, the service that owns the primary
// integration — and the others cross-reference it.
//
// Normalization uses the same token-set intersection as the predecessor-
// floor check (workflow.NormalizeStem + workflow.StemsMatch), so lightly-
// reworded clones ("SSHFS ownership blocks npm install" vs "SSHFS
// ownership blocks npm install on fresh mount") still collide.
//
// The check is skipped when only one README has any gotchas — with fewer
// than two populated knowledge-base fragments there is nothing to
// deduplicate.
func checkCrossReadmeGotchaUniqueness(readmes map[string]string) []workflow.StepCheck {
	type stemLoc struct {
		hostname string
		raw      string
		norm     []string
	}
	// Collect all (hostname, stem) pairs in a deterministic order so the
	// failure detail is stable across runs. Map iteration is randomized;
	// sort the hostnames once up front.
	hostnames := make([]string, 0, len(readmes))
	for h := range readmes {
		hostnames = append(hostnames, h)
	}
	sort.Strings(hostnames)

	var all []stemLoc
	populatedHosts := 0
	for _, h := range hostnames {
		kb := extractFragmentContent(readmes[h], "knowledge-base")
		if kb == "" {
			continue
		}
		stems := workflow.ExtractGotchaStems(kb)
		if len(stems) == 0 {
			continue
		}
		populatedHosts++
		for _, s := range stems {
			norm := workflow.NormalizeStem(s)
			if len(norm) == 0 {
				continue
			}
			all = append(all, stemLoc{hostname: h, raw: s, norm: norm})
		}
	}
	// No cross-comparison is possible with fewer than two READMEs that
	// actually have gotchas. Return a pass so the result surface stays
	// consistent regardless of codebase count.
	if populatedHosts < 2 {
		return []workflow.StepCheck{{
			Name: "cross_readme_gotcha_uniqueness", Status: statusPass,
		}}
	}

	// Pairwise comparison across distinct hostnames. Each duplicate pair
	// is reported once — a second match on either stem is suppressed by
	// the `reportedPair` set to avoid N² noise in the failure detail.
	type pair struct{ i, j int }
	reported := map[pair]bool{}
	var dups []string
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[i].hostname == all[j].hostname {
				continue
			}
			if !workflow.StemsMatch(all[i].norm, all[j].norm) {
				continue
			}
			if reported[pair{i, j}] {
				continue
			}
			reported[pair{i, j}] = true
			dups = append(dups, fmt.Sprintf(
				"%s %q ≈ %s %q",
				all[i].hostname, all[i].raw,
				all[j].hostname, all[j].raw,
			))
		}
	}

	if len(dups) == 0 {
		return []workflow.StepCheck{{
			Name: "cross_readme_gotcha_uniqueness", Status: statusPass,
		}}
	}
	return []workflow.StepCheck{{
		Name:   "cross_readme_gotcha_uniqueness",
		Status: statusFail,
		Detail: fmt.Sprintf(
			"gotcha stems appear in multiple codebase READMEs — each fact must live in exactly ONE README. Pick the codebase most responsible for the fact (server for NATS client config, app for Vite allowedHosts), keep it there, delete it from the others, and replace in the others with a cross-reference: \"See apidev/README.md §Gotchas for the NATS credential format.\" README.md is PUBLISHED content extracted to zerops.io/recipes — readers read the recipe page top-to-bottom, so duplicate facts waste publication surface and train readers to skim. Duplicates: %s",
			strings.Join(dups, "; "),
		),
	}}
}

// extractIntegrationGuideHeadings returns the H3 headings inside the
// integration-guide fragment, stripping leading numeric enumeration like
// "2. ". Used by checkGotchaRestatesGuide to correlate guide items with
// gotcha stems in the same README.
//
// Callers are responsible for filtering domain-specific boilerplate
// headings. The zerops.yaml heading is always present in every recipe
// README (the block is mandatory), so it is not a "code change the user
// must make" and should not count as an IG item for restatement
// purposes — the check itself filters it out before comparison.
func extractIntegrationGuideHeadings(ig string) []string {
	var headings []string
	for line := range strings.SplitSeq(ig, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "### ") {
			continue
		}
		h := strings.TrimPrefix(trimmed, "### ")
		// Strip enumeration "N. " prefix. Walk leading digits, require a
		// dot+space, and drop everything up to and including the space.
		digits := 0
		for digits < len(h) && h[digits] >= '0' && h[digits] <= '9' {
			digits++
		}
		if digits > 0 && digits+1 < len(h) && h[digits] == '.' && h[digits+1] == ' ' {
			h = h[digits+2:]
		}
		h = strings.TrimSpace(h)
		if h != "" {
			headings = append(headings, h)
		}
	}
	return headings
}

// checkGotchaRestatesGuide enforces that a gotcha bullet in the knowledge-
// base fragment must teach something the integration-guide does not. v15's
// appdev README had three gotchas ("Vite allowedHosts blocks Zerops
// subdomain", "VITE_API_URL undefined in dev mode", "Static deploy missing
// tilde suffix") whose normalized tokens were >= 67% identical to three
// integration-guide H3 headings immediately above. Restating a guide item
// as a gotcha doubles the publication surface area with no new content —
// readers see two bullets teaching the same fact in different tones.
//
// The rule: for each gotcha stem, if its normalized token set matches any
// integration-guide heading (excluding the boilerplate "zerops.yaml"
// section), the gotcha is a restatement and must be rewritten or deleted.
// Rewrites should focus on the failure symptom (error message, HTTP
// status, observable misbehavior) rather than the topic — a gotcha whose
// stem is "Blocked request HTTP 200 + blank browser" carries a distinct
// symptom from an IG item "Add .zerops.app to allowedHosts".
//
// Skipped when either fragment is empty or when the only IG heading is
// the zerops.yaml boilerplate.
func checkGotchaRestatesGuide(hostname, content string) []workflow.StepCheck {
	ig := extractFragmentContent(content, "integration-guide")
	kb := extractFragmentContent(content, "knowledge-base")
	if ig == "" || kb == "" {
		return nil
	}
	rawHeadings := extractIntegrationGuideHeadings(ig)
	type headingNorm struct {
		raw  string
		norm []string
	}
	var headings []headingNorm
	for _, h := range rawHeadings {
		lower := strings.ToLower(h)
		// Skip the boilerplate zerops.yaml block — every recipe has it,
		// it is not a "code change a user must make", so no gotcha is
		// a restatement of it. Matching on "zerops.yaml" anywhere in
		// the heading catches all common forms: "### 1. Adding
		// `zerops.yaml`", "### zerops.yaml", "### The `zerops.yaml` file".
		if strings.Contains(lower, "zerops.yaml") || strings.Contains(lower, "zerops yaml") {
			continue
		}
		norm := workflow.NormalizeStem(h)
		if len(norm) == 0 {
			continue
		}
		headings = append(headings, headingNorm{raw: h, norm: norm})
	}
	if len(headings) == 0 {
		return nil
	}

	stems := workflow.ExtractGotchaStems(kb)
	if len(stems) == 0 {
		return nil
	}

	var violations []string
	for _, stem := range stems {
		sNorm := workflow.NormalizeStem(stem)
		if len(sNorm) == 0 {
			continue
		}
		for _, h := range headings {
			if workflow.StemsMatch(sNorm, h.norm) {
				violations = append(violations, fmt.Sprintf("%q restates IG item %q", stem, h.raw))
				break
			}
		}
	}

	checkName := hostname + "_gotcha_distinct_from_guide"
	if len(violations) == 0 {
		return []workflow.StepCheck{{Name: checkName, Status: statusPass}}
	}
	howToFix := fmt.Sprintf(
		"Rewrite each restated gotcha in %s/README.md to name the failure symptom (exact error message, HTTP status, observable misbehavior — 'Blocked request 200 + blank browser') instead of the topic. If the symptom is already in the integration-guide, delete the gotcha and use the slot for something the guide does NOT cover. Violations: %s.",
		hostname, strings.Join(violations, "; "),
	)
	// v8.104 — rewording a gotcha stem to pass this check changes its
	// token set, which can newly collide with a sibling codebase's
	// gotcha stem and flip cross_readme_gotcha_uniqueness from pass to
	// fail on the next round. Surface the coupling inline in HowToFix
	// so the author reviews sibling READMEs before reword, not after
	// the next failure round.
	perturbs := []string{"cross_readme_gotcha_uniqueness"}
	howToFix = howToFix + "\n\nPerturbsChecks (fixing this may flip): " +
		strings.Join(perturbs, ", ") +
		" — rewording changes the stem token set, which can newly collide with a sibling codebase's gotcha. Cross-check other codebases' knowledge-base stems before re-running."
	return []workflow.StepCheck{{
		Name:        checkName,
		Status:      statusFail,
		ReadSurface: fmt.Sprintf("%s/README.md — both #integration-guide H3 headings and #knowledge-base bolded gotcha stems", hostname),
		Required:    "no gotcha stem normalizes to the same token set as any integration-guide H3 heading",
		Actual:      fmt.Sprintf("%d restated gotcha(s)", len(violations)),
		HowToFix:    howToFix,
		Detail: fmt.Sprintf(
			"%s README has gotchas that restate integration-guide items. Violations: %s",
			hostname, strings.Join(violations, "; "),
		),
		PerturbsChecks: perturbs,
	}}
}
