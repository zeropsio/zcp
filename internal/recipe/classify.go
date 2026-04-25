package recipe

import (
	"fmt"
	"regexp"
	"strings"
)

// Classification is one of the seven categories from
// docs/spec-content-surfaces.md §"Fact classification taxonomy". Each
// fact lands in exactly one; three are DISCARD classes (framework-quirk,
// library-metadata, self-inflicted) that never reach any surface.
//
// Plan §2.C: sub-agents apply the taxonomy in-phase when recording
// facts; the engine-side classifier here is a safety net that catches
// mis-tagged records before finalize stitches them into published
// content — "facts tagged framework-quirk / self-inflicted /
// library-metadata never reach the stitched output."
type Classification string

const (
	ClassPlatformInvariant Classification = "platform-invariant"
	ClassIntersection      Classification = "intersection"
	ClassFrameworkQuirk    Classification = "framework-quirk"
	ClassLibraryMetadata   Classification = "library-metadata"
	ClassScaffoldDecision  Classification = "scaffold-decision"
	ClassOperational       Classification = "operational"
	ClassSelfInflicted     Classification = "self-inflicted"
)

// publishableClasses are the four that route to a surface body.
// Everything else drops silently during assembly.
var publishableClasses = map[Classification]bool{
	ClassPlatformInvariant: true,
	ClassIntersection:      true,
	ClassScaffoldDecision:  true,
	ClassOperational:       true,
}

// IsPublishable reports whether a class routes to any surface. DISCARD
// classes (framework-quirk, library-metadata, self-inflicted) return
// false — the content belongs in framework docs / dep manifests / the
// scaffold bug's own commit, not in the recipe.
func IsPublishable(c Classification) bool {
	return publishableClasses[c]
}

// ClassifyResult bundles a fact's classification with the
// zerops_knowledge guide id it should cite (if any). Validators walk
// the result at finalize — Workstream D.
type ClassifyResult struct {
	Class Classification
	Guide string
}

// Classify applies the spec taxonomy to one fact record and returns
// the category. Rule order mirrors docs/spec-content-surfaces.md §"How
// to classify — concrete rules":
//
//  1. Surface hint is the strongest signal the author gave — respect it
//     when it maps cleanly to a DISCARD or non-discard class.
//  2. For platform-trap and porter-change hints, a citation that hits
//     the engine's CitationMap means the fact IS about a topic Zerops
//     documents → platform-invariant. Otherwise intersection (a
//     platform × framework trap that's genuinely new content).
func Classify(r FactRecord) Classification {
	// V-1 (run-11) — auto-detect self-inflicted from fixApplied +
	// failureMode shape regardless of the agent's surfaceHint. Spec
	// rule 4 ("our code did X, we fixed it to do Y → discard") is
	// structurally unreliable when self-graded; the override applies
	// only when the agent already labeled the fact as publishable
	// (otherwise an explicit framework-quirk / library-metadata stays).
	if IsLikelySelfInflicted(r) {
		return ClassSelfInflicted
	}
	switch r.SurfaceHint {
	case "framework-quirk":
		return ClassFrameworkQuirk
	case "self-inflicted":
		return ClassSelfInflicted
	case "tooling-metadata", "library-metadata":
		return ClassLibraryMetadata
	case "operational":
		return ClassOperational
	case "scaffold-decision":
		return ClassScaffoldDecision
	case "browser-verification":
		// Browser-walk verifications are operational signals recorded by
		// the feature sub-agent after a `zerops_browser` tool call.
		// Publishable — land under the operational class.
		return ClassOperational
	case "platform-trap", "porter-change":
		if GuideForTopic(r.Citation) != "" || GuideForTopic(r.Topic) != "" {
			return ClassPlatformInvariant
		}
		return ClassIntersection
	case "root-overview", "tier-promotion", "tier-decision":
		// These are env/root content hints from the surface registry;
		// treat them as scaffold-decision for publishing purposes.
		return ClassScaffoldDecision
	}
	// Unknown hint — route conservatively to scaffold-decision so the
	// fact isn't silently dropped. Sub-agent author gets to re-classify.
	return ClassScaffoldDecision
}

// ClassifyDetailed returns the classification + the zerops_knowledge
// guide id the fact should cite (empty when the topic isn't in the
// CitationMap).
func ClassifyDetailed(r FactRecord) ClassifyResult {
	class := Classify(r)
	guide := GuideForTopic(r.Citation)
	if guide == "" {
		guide = GuideForTopic(r.Topic)
	}
	return ClassifyResult{Class: class, Guide: guide}
}

// V-1 (run-11) deterministic self-inflicted detection. Two regexes
// pattern-match recipe-source change phrasing in fixApplied; a small
// hand-curated platform-mechanism vocabulary list rules out genuine
// platform-side teaching when that vocabulary appears in failureMode.
//
// The list is intentionally narrow — bias toward false-negatives
// (override doesn't fire on a genuine self-inflicted record) over
// false-positives (override silences a true platform-trap). Other V
// validators catch leaks that V-1 misses.
var selfInflictedFixPatterns = []*regexp.Regexp{
	// "removed dist from .deployignore", "added X from package.json"
	regexp.MustCompile(`(?i)\b(removed|added|changed)\b.+\bfrom\b\s+\S+`),
	// "switched npx ts-node to node dist/migrate.js"
	regexp.MustCompile(`(?i)\bswitched\b.+\bto\b\s+\S+`),
}

// PlatformVocabulary is the single hand-curated list of platform-side
// mechanism terms. V-1 (classify, case-sensitive contains in
// failureMode) and V-3 (validators_kb_quality, case-insensitive
// contains in KB bullet body) both consume it. Alphabetized by lower-
// case form, deduped union of the two pre-merge lists.
//
// Add entries when a new platform-side mechanism becomes load-bearing
// in the recipe pipeline; do NOT promote this list into a "framework"
// or "ClassificationContext" struct — flat slice is the contract.
var PlatformVocabulary = []string{
	"${",
	"appVersionId",
	"balancer",
	"buildFromGit",
	"deployFiles",
	"envIsolation",
	"execOnce",
	"forcePathStyle",
	"httpSupport",
	"initCommands",
	"L7",
	"managed service",
	"minContainers",
	"preparecommands",
	"runtime card",
	"subdomain",
	"VXLAN",
	"Zerops",
	"zerops.yaml",
	"zeropsSubdomain",
	"zsc",
}

// IsLikelySelfInflicted applies V-1's deterministic shape check.
// Returns true when fixApplied looks like a recipe-source change AND
// failureMode lacks platform-side mechanism vocabulary. Both fields
// must be present — older facts (pre-U-2 schema) never trigger.
func IsLikelySelfInflicted(r FactRecord) bool {
	if r.FixApplied == "" || r.FailureMode == "" {
		return false
	}
	matched := false
	for _, p := range selfInflictedFixPatterns {
		if p.MatchString(r.FixApplied) {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	for _, kw := range PlatformVocabulary {
		if strings.Contains(r.FailureMode, kw) {
			return false
		}
	}
	return true
}

// ClassifyWithNotice returns Classify's result plus a one-line warning
// when V-1's auto-override fires. Empty notice means no override —
// caller should not surface anything to the agent.
//
// The notice names spec rule 4 (the self-inflicted litmus) so the
// sub-agent author has a hook to look up the exact rule that the
// fact failed on, and to either re-record with corrected shape or
// accept the discard.
func ClassifyWithNotice(r FactRecord) (Classification, string) {
	base := Classify(r)
	if base == ClassSelfInflicted && IsLikelySelfInflicted(r) && r.SurfaceHint != "self-inflicted" {
		notice := fmt.Sprintf(
			"auto-reclassified self-inflicted (spec rule 4): fixApplied %q describes a recipe-source change without platform-side mechanism in failureMode — discard, not KB. Original surfaceHint: %q.",
			r.FixApplied, r.SurfaceHint,
		)
		return base, notice
	}
	return base, ""
}

// ClassifyLog partitions a facts log into publishable and dropped
// slices per the taxonomy. Publishable facts route to their surfaces
// (plus metadata attached via ClassifyDetailed). Dropped facts are
// surfaced by count in status output so the author sees why their
// fact didn't land.
func ClassifyLog(records []FactRecord) (publishable, dropped []FactRecord) {
	for _, r := range records {
		if IsPublishable(Classify(r)) {
			publishable = append(publishable, r)
			continue
		}
		dropped = append(dropped, r)
	}
	return publishable, dropped
}
