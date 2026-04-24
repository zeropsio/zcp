package recipe

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
