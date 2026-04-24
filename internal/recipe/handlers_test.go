package recipe

import (
	"path/filepath"
	"testing"
)

func TestStore_OpenOrCreate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir) // empty mountRoot — no parent ever

	sess, err := store.OpenOrCreate("synth-minimal", filepath.Join(dir, "run"))
	if err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	if sess.Slug != "synth-minimal" {
		t.Errorf("Slug = %q", sess.Slug)
	}
	// Second call returns same session.
	sess2, err := store.OpenOrCreate("synth-minimal", filepath.Join(dir, "run"))
	if err != nil {
		t.Fatal(err)
	}
	if sess != sess2 {
		t.Error("OpenOrCreate returned a different session on second call")
	}
}

func TestDispatch_UnknownAction(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir())
	res := dispatch(t.Context(), store, RecipeInput{Action: "bogus", Slug: "x"})
	if res.OK {
		t.Error("expected OK=false for unknown action")
	}
	if res.Error == "" {
		t.Error("expected Error to be set")
	}
}

func TestDispatch_StartStatusRecordFactEmitYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	outputRoot := filepath.Join(dir, "run")

	// start
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outputRoot,
	})
	if !res.OK {
		t.Fatalf("start: %+v", res)
	}

	// Populate the session's plan directly (handlers would mutate via
	// future actions).
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	// status
	res = dispatch(t.Context(), store, RecipeInput{Action: "status", Slug: "synth-showcase"})
	if !res.OK || res.Status == nil || res.Status.Codebases != 3 {
		t.Errorf("status: %+v", res)
	}

	// record-fact (valid)
	res = dispatch(t.Context(), store, RecipeInput{
		Action: "record-fact", Slug: "synth-showcase",
		Fact: &FactRecord{
			Topic: "x", Symptom: "y", Mechanism: "z",
			SurfaceHint: "platform-trap", Citation: "env-var-model",
		},
	})
	if !res.OK {
		t.Errorf("record-fact valid: %+v", res)
	}

	// record-fact (missing citation)
	res = dispatch(t.Context(), store, RecipeInput{
		Action: "record-fact", Slug: "synth-showcase",
		Fact: &FactRecord{
			Topic: "x", Symptom: "y", Mechanism: "z",
			SurfaceHint: "platform-trap",
		},
	})
	if res.OK {
		t.Error("record-fact missing citation should be rejected")
	}

	// emit-yaml
	res = dispatch(t.Context(), store, RecipeInput{
		Action: "emit-yaml", Slug: "synth-showcase", TierIndex: 0,
	})
	if !res.OK || res.YAML == "" {
		t.Errorf("emit-yaml: %+v", res)
	}
}

// TestStore_CurrentSingleSession covers the cross-tool routing primitive v3
// uses when a v2-shaped tool (zerops_record_fact, zerops_workspace_manifest)
// is called under a recipe-only context — one open session lets the tool
// infer its slug + paths; zero or multiple sessions must return ok=false so
// the caller errors instead of writing to a guessed location.
func TestStore_CurrentSingleSession(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)

	// Zero sessions → ok=false.
	if _, _, _, ok := store.CurrentSingleSession(); ok {
		t.Fatal("want ok=false when no sessions are open")
	}

	// Exactly one session → ok=true with slug + paths under outputRoot.
	outputRoot := filepath.Join(dir, "run-a")
	if _, err := store.OpenOrCreate("alpha-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	slug, legacyPath, manifestPath, ok := store.CurrentSingleSession()
	if !ok {
		t.Fatal("want ok=true with one session open")
	}
	if slug != "alpha-showcase" {
		t.Errorf("slug = %q, want alpha-showcase", slug)
	}
	wantLegacy := filepath.Join(outputRoot, "legacy-facts.jsonl")
	if legacyPath != wantLegacy {
		t.Errorf("legacyFactsPath = %q, want %q", legacyPath, wantLegacy)
	}
	wantManifest := filepath.Join(outputRoot, "workspace-manifest.json")
	if manifestPath != wantManifest {
		t.Errorf("manifestPath = %q, want %q", manifestPath, wantManifest)
	}

	// A second session makes the resolver ambiguous → ok=false.
	if _, err := store.OpenOrCreate("beta-showcase", filepath.Join(dir, "run-b")); err != nil {
		t.Fatalf("OpenOrCreate second: %v", err)
	}
	if _, _, _, ok := store.CurrentSingleSession(); ok {
		t.Error("want ok=false with two sessions open — caller must pass slug explicitly")
	}
}

func TestDispatch_BuildBrief(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	outputRoot := filepath.Join(dir, "run")
	dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outputRoot,
	})
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	res := dispatch(t.Context(), store, RecipeInput{
		Action: "build-brief", Slug: "synth-showcase",
		BriefKind: "scaffold", Codebase: "api",
	})
	if !res.OK {
		t.Fatalf("build-brief scaffold: %+v", res)
	}
	if res.Brief == nil || res.Brief.Bytes == 0 {
		t.Error("brief body empty")
	}
}
