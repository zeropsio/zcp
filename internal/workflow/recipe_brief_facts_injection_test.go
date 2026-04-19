package workflow

import (
	"os"
	"strings"
	"testing"
	"time"
)

func removeIfExists(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// TestSubStepGuide_FeatureSubagent_PrependsPriorDiscoveries — v8.96
// integration: when buildSubStepGuide resolves topic "subagent-brief"
// (which has IncludePriorDiscoveries=true), the returned guide MUST
// carry the "Prior discoveries" block sourced from the session's
// downstream-scoped facts.
//
// Sets sessionID via factLogPathLocal seam: the test writes the facts
// log under os.TempDir() with the same naming the runtime uses, then
// calls buildSubStepGuide with that sessionID. We re-use writeTestFacts
// and target the canonical TempDir path so factLogPathLocal resolves to
// the seeded file.
func TestSubStepGuide_FeatureSubagent_PrependsPriorDiscoveries(t *testing.T) {
	// Not parallel — this test writes to os.TempDir() under a fixed
	// session-ID-derived path that BuildPriorDiscoveriesBlock resolves
	// via factLogPathLocal. Concurrent runs of other tests with the
	// same sessionID would collide; the unique sessionID makes that
	// unlikely, but t.Parallel adds no value here.
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	rs := &RecipeState{Plan: plan, Tier: RecipeTierShowcase}

	sessionID := "v8-96-injection-test-" + t.Name()
	logPath := factLogPathLocal(sessionID)
	t.Cleanup(func() { _ = removeIfExists(logPath) })

	now := time.Now().UTC()
	recs := []testFactRecord{
		{
			Timestamp: now.Add(-2 * time.Minute).Format(time.RFC3339Nano),
			Substep:   SubStepScaffold,
			Type:      "platform_observation",
			Title:     "Meilisearch v0.57 renamed class from MeiliSearch to Meilisearch",
			Mechanism: "Meilisearch SDK v0.57",
			Scope:     factScopeDownstream,
		},
		{
			Timestamp: now.Add(-1 * time.Minute).Format(time.RFC3339Nano),
			Substep:   SubStepScaffold,
			Type:      "gotcha_candidate",
			Title:     "L7 balancer terminates SSL — services bind 0.0.0.0",
			Scope:     factScopeContent, // content-only, must NOT appear
		},
	}
	dir := strings.TrimSuffix(logPath, "/zcp-facts-"+sessionID+".jsonl")
	writeTestFacts(t, dir, sessionID, recs)

	got := rs.buildSubStepGuide(RecipeStepDeploy, SubStepSubagent, sessionID)
	if got == "" {
		t.Fatal("expected non-empty subagent-brief guide")
	}
	if !strings.Contains(got, "Prior discoveries") {
		t.Error("subagent-brief MUST be prefixed with 'Prior discoveries' header when IncludePriorDiscoveries=true and downstream facts exist")
	}
	if !strings.Contains(got, "Meilisearch v0.57") {
		t.Error("downstream-scoped fact missing from prepended block")
	}
	if strings.Contains(got, "L7 balancer terminates SSL") {
		t.Error("content-scoped fact must NOT leak into the dispatch brief")
	}
	// The original brief content must still be present after the prepend.
	if !strings.Contains(got, "feature sub-agent") {
		t.Error("original subagent-brief content missing after prior-discoveries prepend")
	}
}

// TestSubStepGuide_NoOptIn_DoesNotPrepend — readme-fragments has
// IncludePriorDiscoveries=false; even with a seeded facts log, the
// returned guide must NOT carry the prior-discoveries block. The writer
// reads the facts log directly via the v8.95 manifest contract; a
// duplicate prepend would read as authoritative content.
func TestSubStepGuide_NoOptIn_DoesNotPrepend(t *testing.T) {
	plan := fixtureForShape(ShapeBackendMinimal)
	rs := &RecipeState{Plan: plan, Tier: RecipeTierMinimal}

	sessionID := "v8-96-noopt-test-" + t.Name()
	logPath := factLogPathLocal(sessionID)
	t.Cleanup(func() { _ = removeIfExists(logPath) })
	dir := strings.TrimSuffix(logPath, "/zcp-facts-"+sessionID+".jsonl")
	writeTestFacts(t, dir, sessionID, []testFactRecord{{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Substep:   SubStepScaffold,
		Type:      "platform_observation",
		Title:     "should-not-appear",
		Scope:     factScopeDownstream,
	}})

	got := rs.buildSubStepGuide(RecipeStepDeploy, SubStepReadmes, sessionID)
	if got == "" {
		t.Fatal("expected non-empty readmes guide")
	}
	if strings.Contains(got, "Prior discoveries") {
		t.Error("readme-fragments topic must NOT prepend prior-discoveries block (IncludePriorDiscoveries=false)")
	}
	if strings.Contains(got, "should-not-appear") {
		t.Error("downstream fact leaked into a topic that did not opt in")
	}
}
