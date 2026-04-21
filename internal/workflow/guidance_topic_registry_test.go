package workflow

import (
	"slices"
	"strings"
	"testing"
)

// Cx-GUIDANCE-TOPIC-REGISTRY regression tests (HANDOFF-to-I6, defect-
// class-registry §16.5 `v35-guidance-unknown-topic`). v35 evidence at
// 07:29:50-51: three hallucinated guidance-topic lookups in a row
// (`dual-runtime-consumption`, `client-code-observable-failure`,
// `init-script-loud-failure`). First two returned bare
// "unknown guidance topic" errors; the third returned a zero-byte
// response — worse than the error because the main agent cannot tell
// "no additional guidance" from "lookup miss".

// TestAllTopicIDs_Sorted pins the AllTopicIDs contract: every registered
// topic ID appears in the returned slice, in alphabetical order, no
// duplicates. This is the closed universe the main agent caches from
// the recipe-start response.
func TestAllTopicIDs_Sorted(t *testing.T) {
	t.Parallel()
	ids := AllTopicIDs()
	if len(ids) == 0 {
		t.Fatal("AllTopicIDs returned empty — topic registry is not initialized")
	}
	if !slices.IsSorted(ids) {
		t.Errorf("AllTopicIDs must be sorted alphabetically; got: %v", ids)
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			t.Errorf("AllTopicIDs contains duplicate %q", id)
		}
		seen[id] = true
	}
	// Spot-check: a known registered topic (any topic) must be present.
	allTopics := append([]*GuidanceTopic(nil), recipeGenerateTopics...)
	allTopics = append(allTopics, recipeDeployTopics...)
	if len(allTopics) > 0 && !seen[allTopics[0].ID] {
		t.Errorf("AllTopicIDs missing known topic %q", allTopics[0].ID)
	}
}

// TestNearestTopicIDs_FindsTypoedTarget verifies that a query one edit
// away from a real topic surfaces that topic in the top-3. This is the
// primary use case: the main agent typo'd a known ID and the server
// should rescue it with the corrected form.
func TestNearestTopicIDs_FindsTypoedTarget(t *testing.T) {
	t.Parallel()
	ids := AllTopicIDs()
	if len(ids) == 0 {
		t.Skip("no registered topics")
	}
	target := ids[0]
	// Drop a middle character to create a 1-edit typo.
	if len(target) < 3 {
		t.Skipf("target topic %q too short for typo injection", target)
	}
	typo := target[:len(target)/2] + target[len(target)/2+1:]
	matches := NearestTopicIDs(typo, 3)
	if len(matches) == 0 {
		t.Fatalf("NearestTopicIDs(%q, 3) returned no matches", typo)
	}
	if !slices.Contains(matches, target) {
		t.Errorf("NearestTopicIDs(%q, 3) = %v; expected %q in top-3", typo, matches, target)
	}
}

// TestNearestTopicIDs_EmptyInput returns nil for empty query or k≤0 —
// guards against accidentally returning the full registry via an
// unbounded ranking.
func TestNearestTopicIDs_EmptyInput(t *testing.T) {
	t.Parallel()
	if got := NearestTopicIDs("", 3); got != nil {
		t.Errorf("NearestTopicIDs(\"\", 3) = %v; want nil", got)
	}
	if got := NearestTopicIDs("anything", 0); got != nil {
		t.Errorf("NearestTopicIDs(\"anything\", 0) = %v; want nil", got)
	}
	if got := NearestTopicIDs("anything", -1); got != nil {
		t.Errorf("NearestTopicIDs(\"anything\", -1) = %v; want nil", got)
	}
}

// TestNearestTopicIDs_CapK asserts that requesting more matches than
// registered topics returns every topic (not a panic or over-allocated
// slice).
func TestNearestTopicIDs_CapK(t *testing.T) {
	t.Parallel()
	ids := AllTopicIDs()
	got := NearestTopicIDs("xyz", len(ids)+100)
	if len(got) != len(ids) {
		t.Errorf("NearestTopicIDs with k > registry size returned %d matches; want %d", len(got), len(ids))
	}
}

// TestRecipeStart_IncludesGuidanceTopicIDs verifies that the recipe-
// start response carries the closed universe of valid topic IDs so the
// main agent references the registry instead of pattern-matching from
// its own reasoning.
func TestRecipeStart_IncludesGuidanceTopicIDs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	resp, err := eng.RecipeStart("proj-topic-ids", "guidance-list test", RecipeTierMinimal)
	if err != nil {
		t.Fatalf("RecipeStart: %v", err)
	}
	if len(resp.GuidanceTopicIDs) == 0 {
		t.Fatal("RecipeStart response missing GuidanceTopicIDs — main agent has no closed-universe reference")
	}
	if len(resp.GuidanceTopicIDs) != len(AllTopicIDs()) {
		t.Errorf("GuidanceTopicIDs length %d != AllTopicIDs length %d — list diverged from registry", len(resp.GuidanceTopicIDs), len(AllTopicIDs()))
	}
	// Spot-check: every returned ID resolves via LookupTopic.
	for _, id := range resp.GuidanceTopicIDs {
		if LookupTopic(id) == nil {
			t.Errorf("GuidanceTopicIDs contains %q but LookupTopic returns nil", id)
		}
	}
}

// TestResolveTopic_AllTopics_NonEmpty_UnderMatchingPredicate catches
// the v35 F-5 silent-empty class: every registered topic whose
// predicate matches at least one plan shape must resolve to non-empty
// content for that shape. An empty resolution under a matching
// predicate means the registry references blocks missing from
// recipe.md — a server-side bug the Cx-GUIDANCE-TOPIC-REGISTRY
// TOPIC_EMPTY guard exists to surface.
func TestResolveTopic_AllTopics_NonEmpty_UnderMatchingPredicate(t *testing.T) {
	t.Parallel()
	shapes := []struct {
		name string
		plan *RecipePlan
	}{
		{"hello-world", fixtureForShape(ShapeHelloWorld)},
		{"backend-minimal", fixtureForShape(ShapeBackendMinimal)},
		{"fullstack-showcase", fixtureForShape(ShapeFullStackShowcase)},
		{"dual-runtime-showcase", fixtureForShape(ShapeDualRuntimeShowcase)},
	}
	for _, id := range AllTopicIDs() {
		topic := LookupTopic(id)
		if topic == nil {
			t.Errorf("LookupTopic(%q) returned nil despite ID being in AllTopicIDs", id)
			continue
		}
		// Find at least one shape the predicate accepts.
		var matchingShape struct {
			name string
			plan *RecipePlan
		}
		anyMatch := false
		for _, s := range shapes {
			if topic.Predicate == nil || topic.Predicate(s.plan) {
				matchingShape = s
				anyMatch = true
				break
			}
		}
		if !anyMatch {
			// Predicate rejects every canonical shape — not a silent-
			// empty case (predicate-filtered empty is expected).
			continue
		}
		content, err := ResolveTopic(id, matchingShape.plan)
		if err != nil {
			t.Errorf("ResolveTopic(%q) on %s: %v", id, matchingShape.name, err)
			continue
		}
		if strings.TrimSpace(content) == "" {
			t.Errorf("ResolveTopic(%q) on %s returned empty despite predicate matching — block missing from recipe.md?", id, matchingShape.name)
		}
	}
}
