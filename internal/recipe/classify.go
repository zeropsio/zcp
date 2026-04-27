package recipe

import (
	"fmt"
	"regexp"
	"slices"
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

// classificationCompatibleWithSurface returns nil when (class, surface)
// is allowed per docs/spec-content-surfaces.md §"Classification × surface
// compatibility", or an error carrying the spec-defined redirect teaching
// when the pair is incompatible.
//
// Run-15 F.3 — operationalizes the spec table at record-fragment time so
// a sub-agent with a self-inflicted observation can't quietly route it
// into a KB bullet, and a scaffold-decision (config) fact can't land in
// CLAUDE.md. Empty class always passes (back-compat: callers that don't
// classify yet keep working).
//
// Compatibility table (spec-anchored):
//
//	platform-invariant  → KB, IG       (CLAUDE.md / yaml-comments → KB/IG redirect)
//	intersection        → KB           (others → KB redirect)
//	scaffold-decision   → IG, env yaml comments, codebase yaml comments
//	operational         → CLAUDE.md
//	framework-quirk     → ∅            (DISCARD — does not belong on any surface)
//	library-metadata    → ∅            (DISCARD — belongs in dep manifest)
//	self-inflicted      → ∅            (DISCARD — fix is in code, no teaching)
func classificationCompatibleWithSurface(class Classification, surface Surface) error {
	if class == "" {
		return nil // back-compat: no classification provided
	}
	if !knownClassification(class) {
		return fmt.Errorf("unknown classification %q (expected one of: platform-invariant, intersection, framework-quirk, library-metadata, scaffold-decision, operational, self-inflicted)", class)
	}
	allowed := compatibleSurfaces(class)
	if len(allowed) == 0 {
		return fmt.Errorf(
			"classification=%q has no compatible surfaces — discard; %s see docs/spec-content-surfaces.md#fact-classification-taxonomy",
			class, discardRedirect(class),
		)
	}
	if slices.Contains(allowed, surface) {
		return nil
	}
	return fmt.Errorf(
		"classification=%q is incompatible with surface %q; compatible surfaces: %s; %s see docs/spec-content-surfaces.md#classification--surface-compatibility",
		class, surface, surfaceList(allowed), surfaceRedirect(class, surface),
	)
}

// knownClassification gates the compatibility check on a closed set so
// typos surface as errors instead of silently passing.
func knownClassification(c Classification) bool {
	switch c {
	case ClassPlatformInvariant, ClassIntersection, ClassFrameworkQuirk,
		ClassLibraryMetadata, ClassScaffoldDecision, ClassOperational,
		ClassSelfInflicted:
		return true
	}
	return false
}

// compatibleSurfaces returns the spec-allowed surfaces for a class.
// Returns nil for DISCARD classes (framework-quirk, library-metadata,
// self-inflicted).
func compatibleSurfaces(c Classification) []Surface {
	switch c {
	case ClassPlatformInvariant:
		return []Surface{SurfaceCodebaseKB, SurfaceCodebaseIG}
	case ClassIntersection:
		return []Surface{SurfaceCodebaseKB}
	case ClassScaffoldDecision:
		return []Surface{
			SurfaceCodebaseIG,
			SurfaceCodebaseZeropsComments,
			SurfaceEnvImportComments,
		}
	case ClassOperational:
		return []Surface{SurfaceCodebaseCLAUDE}
	case ClassFrameworkQuirk, ClassLibraryMetadata, ClassSelfInflicted:
		// DISCARD classes — no compatible surface.
	}
	return nil
}

// discardRedirect returns the spec-defined teaching for DISCARD classes.
func discardRedirect(c Classification) string {
	switch c {
	case ClassFrameworkQuirk:
		return "Framework quirks belong in framework docs, not Zerops recipe content."
	case ClassLibraryMetadata:
		return "Library metadata (npm peer-deps, package-lock conflicts) belongs in the dep manifest's notes, not recipe content."
	case ClassSelfInflicted:
		return "Self-inflicted observations describe a code bug fixed in the scaffold; the fix is in the code, there is no teaching for a porter."
	case ClassPlatformInvariant, ClassIntersection, ClassScaffoldDecision, ClassOperational:
		// Non-DISCARD classes — caller never reaches this branch.
	}
	return ""
}

// surfaceRedirect returns a one-line redirect teaching for an
// incompatible (class, surface) pair, mirroring the spec's per-cell
// guidance.
func surfaceRedirect(c Classification, s Surface) string {
	switch c {
	case ClassPlatformInvariant:
		switch s {
		case SurfaceCodebaseCLAUDE:
			return "Platform invariants are porter-facing — route to the codebase KB (gotcha) instead of CLAUDE.md (operational)."
		case SurfaceCodebaseZeropsComments, SurfaceEnvImportComments:
			return "Platform invariants belong in IG (with diff) or KB (with `zerops_knowledge` citation), not in yaml comments."
		case SurfaceRootREADME, SurfaceEnvREADME, SurfaceCodebaseIG, SurfaceCodebaseKB:
			// Compatible / handled elsewhere — no extra redirect needed.
		}
	case ClassIntersection:
		return "Platform × framework intersections belong in the codebase KB, naming both sides; redirect this fact to the knowledge-base fragment."
	case ClassScaffoldDecision:
		switch s {
		case SurfaceCodebaseKB:
			return "Scaffold decisions explain field-level trade-offs — route to zerops.yaml comments (Surface 7) or IG with the diff (Surface 4), not KB."
		case SurfaceCodebaseCLAUDE:
			return "Scaffold decisions are config / code trade-offs — they belong on yaml comments or IG, not the operating-this-repo guide."
		case SurfaceRootREADME, SurfaceEnvREADME, SurfaceEnvImportComments, SurfaceCodebaseIG, SurfaceCodebaseZeropsComments:
			// Compatible / handled elsewhere.
		}
	case ClassOperational:
		return "Operational facts describe how to iterate on this repo locally — route to CLAUDE.md (Surface 6)."
	case ClassFrameworkQuirk, ClassLibraryMetadata, ClassSelfInflicted:
		// DISCARD classes — caller routes through discardRedirect instead.
	}
	return ""
}

// surfaceList renders a slice of Surface names as a comma-separated
// human-readable list for error messages.
func surfaceList(ss []Surface) string {
	if len(ss) == 0 {
		return "(none)"
	}
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, string(s))
	}
	return strings.Join(out, ", ")
}
