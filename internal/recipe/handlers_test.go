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
