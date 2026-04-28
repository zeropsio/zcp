package recipe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScaffoldBrief_DispatchedToProductionAgent_CarriesReachableSlugList
// pins run-15 R-14-P-1 — the run-14 stealth regression.
//
// Run-14 CHANGELOG promised that BuildScaffoldBriefWithResolver emits a
// `## Recipe-knowledge slugs you may consult` section when a Resolver
// is present. The composer-level unit test passed; the production
// dispatch path called the legacy non-resolver entry point so
// dispatched scaffold briefs carried zero matches for the section
// header.
//
// This test simulates the production path end-to-end: Store +
// OpenOrCreate (mount-root-attached Session) → update-plan → dispatch
// composer (action=build-subagent-prompt). It asserts the dispatched
// bytes carry both the section header and at least one slug bullet.
//
// The §0 production-surface discipline this test pins: a unit test on
// the composer is not the same as an e2e test on the dispatched output.
// New brief / record-fragment-response / validator extensions must each
// have an e2e test that observes the production output.
func TestScaffoldBrief_DispatchedToProductionAgent_CarriesReachableSlugList(t *testing.T) {
	t.Parallel()

	// Set up a recipes mount with three reachable slugs (each carrying
	// import.yaml — that's the Resolver's gate) and one decoy directory
	// without import.yaml that must NOT appear in the slug list.
	mount := t.TempDir()
	for _, slug := range []string{"nestjs-hello-world", "nodejs-hello-world", "svelte-hello-world"} {
		if err := os.MkdirAll(filepath.Join(mount, slug), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", slug, err)
		}
		if err := os.WriteFile(filepath.Join(mount, slug, "import.yaml"), []byte("project: {}\n"), 0o600); err != nil {
			t.Fatalf("write import.yaml for %s: %v", slug, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(mount, "no-import-yaml"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Store + Session — the production wiring.
	outputRoot := t.TempDir()
	store := NewStore(mount)
	sess, err := store.OpenOrCreate("synthetic-showcase", outputRoot)
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}

	// Populate Plan via the dispatch update-plan action — same path the
	// porter takes. Use the synthetic showcase plan for fragment-shape
	// realism.
	plan := syntheticShowcasePlan()
	res := dispatch(context.Background(), store, RecipeInput{
		Action: "update-plan", Slug: "synthetic-showcase", Plan: plan,
	})
	if !res.OK {
		t.Fatalf("update-plan: %s", res.Error)
	}

	// Dispatch the build-subagent-prompt action with kind=scaffold.
	// This is the exact production path: handlers.go::dispatch →
	// buildSubagentPromptForPhase(plan, parent, in, currentPhase, mountRoot).
	res = dispatch(context.Background(), store, RecipeInput{
		Action:    "build-subagent-prompt",
		Slug:      "synthetic-showcase",
		BriefKind: "scaffold",
		Codebase:  sess.Plan.Codebases[0].Hostname,
	})
	if !res.OK {
		t.Fatalf("build-subagent-prompt: %s", res.Error)
	}

	prompt := res.Prompt
	if prompt == "" {
		t.Fatal("dispatched prompt is empty")
	}
	if !strings.Contains(prompt, "## Recipe-knowledge slugs you may consult") {
		t.Errorf("dispatched scaffold brief missing reachable-slug section header (R-14-P-1 stealth regression):\n%s", prompt)
	}
	atLeastOneSlug := false
	for _, slug := range []string{"nestjs-hello-world", "nodejs-hello-world", "svelte-hello-world"} {
		if strings.Contains(prompt, "- "+slug) {
			atLeastOneSlug = true
			break
		}
	}
	if !atLeastOneSlug {
		t.Error("dispatched scaffold brief carries no slug bullet — section header without content")
	}
	if strings.Contains(prompt, "- no-import-yaml") {
		t.Error("dispatched scaffold brief lists a directory without import.yaml")
	}
}

// Run-16 §10.4 #11 — every new brief composer has an e2e dispatch test
// that observes production output (the brief lands inside the
// dispatched prompt byte-identically). One per kind.

func TestCodebaseContentBrief_DispatchedToProductionAgent_CarriesAtoms(t *testing.T) {
	t.Parallel()
	outputRoot := t.TempDir()
	store := NewStore(t.TempDir())
	if _, err := store.OpenOrCreate("synth-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}

	plan := syntheticShowcasePlan()
	res := dispatch(context.Background(), store, RecipeInput{
		Action: "update-plan", Slug: "synth-showcase", Plan: plan,
	})
	if !res.OK {
		t.Fatalf("update-plan: %s", res.Error)
	}

	res = dispatch(context.Background(), store, RecipeInput{
		Action:    "build-subagent-prompt",
		Slug:      "synth-showcase",
		BriefKind: "codebase-content",
		Codebase:  plan.Codebases[0].Hostname,
	})
	if !res.OK {
		t.Fatalf("build-subagent-prompt codebase-content: %s", res.Error)
	}
	if !strings.Contains(res.Prompt, "Codebase-content phase") {
		t.Error("dispatched codebase-content prompt missing phase entry header")
	}
	if !strings.Contains(res.Prompt, plan.Codebases[0].Hostname) {
		t.Error("dispatched codebase-content prompt missing target hostname")
	}
}

func TestClaudeMDBrief_DispatchedToProductionAgent_HardProhibition(t *testing.T) {
	t.Parallel()
	outputRoot := t.TempDir()
	store := NewStore(t.TempDir())
	if _, err := store.OpenOrCreate("synth-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}

	plan := syntheticShowcasePlan()
	if res := dispatch(context.Background(), store, RecipeInput{
		Action: "update-plan", Slug: "synth-showcase", Plan: plan,
	}); !res.OK {
		t.Fatalf("update-plan: %s", res.Error)
	}

	res := dispatch(context.Background(), store, RecipeInput{
		Action:    "build-subagent-prompt",
		Slug:      "synth-showcase",
		BriefKind: "claudemd-author",
		Codebase:  plan.Codebases[0].Hostname,
	})
	if !res.OK {
		t.Fatalf("build-subagent-prompt claudemd-author: %s", res.Error)
	}
	if !strings.Contains(res.Prompt, "Hard prohibition") {
		t.Error("dispatched claudemd-author prompt missing the hard-prohibition block (R-15-4 closure)")
	}
}

func TestEnvContentBrief_DispatchedToProductionAgent_CarriesTierFacts(t *testing.T) {
	t.Parallel()
	outputRoot := t.TempDir()
	store := NewStore(t.TempDir())
	if _, err := store.OpenOrCreate("synth-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}

	plan := syntheticShowcasePlan()
	if res := dispatch(context.Background(), store, RecipeInput{
		Action: "update-plan", Slug: "synth-showcase", Plan: plan,
	}); !res.OK {
		t.Fatalf("update-plan: %s", res.Error)
	}

	res := dispatch(context.Background(), store, RecipeInput{
		Action:    "build-subagent-prompt",
		Slug:      "synth-showcase",
		BriefKind: "env-content",
	})
	if !res.OK {
		t.Fatalf("build-subagent-prompt env-content: %s", res.Error)
	}
	if !strings.Contains(res.Prompt, "Per-tier capability matrix") {
		t.Error("dispatched env-content prompt missing capability matrix")
	}
}
