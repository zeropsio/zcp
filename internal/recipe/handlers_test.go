package recipe

import (
	"os"
	"path/filepath"
	"strings"
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

// TestEnterPhase_Scaffold_PopulatesSourceRoot — Workstream A2.
// At `enter-phase scaffold`, any codebase whose SourceRoot is empty
// gets the convention-based `/var/www/<hostname>dev` path populated.
func TestEnterPhase_Scaffold_PopulatesSourceRoot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	outputRoot := filepath.Join(dir, "run")
	dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outputRoot,
	})
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()
	// All SourceRoots empty on live run.
	for i := range sess.Plan.Codebases {
		sess.Plan.Codebases[i].SourceRoot = ""
	}
	// Mark research + provision complete so enter-phase scaffold is
	// adjacent-forward (bypasses gate evaluation for this unit test).
	sess.Completed[PhaseResearch] = true
	sess.Completed[PhaseProvision] = true
	sess.Current = PhaseProvision

	res := dispatch(t.Context(), store, RecipeInput{
		Action: "enter-phase", Slug: "synth-showcase", Phase: "scaffold",
	})
	if !res.OK {
		t.Fatalf("enter-phase scaffold: %+v", res)
	}

	for _, cb := range sess.Plan.Codebases {
		want := "/var/www/" + cb.Hostname + "dev"
		if cb.SourceRoot != want {
			t.Errorf("codebase %q SourceRoot = %q, want %q",
				cb.Hostname, cb.SourceRoot, want)
		}
	}
}

// TestEnterPhase_Scaffold_DoesNotOverrideExplicitSourceRoot — explicit
// values set by chain resolver or update-plan are preserved.
func TestEnterPhase_Scaffold_DoesNotOverrideExplicitSourceRoot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	outputRoot := filepath.Join(dir, "run")
	dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outputRoot,
	})
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()
	customRoot := "/custom/path/api"
	for i := range sess.Plan.Codebases {
		if sess.Plan.Codebases[i].Hostname == "api" {
			sess.Plan.Codebases[i].SourceRoot = customRoot
		} else {
			sess.Plan.Codebases[i].SourceRoot = ""
		}
	}
	sess.Completed[PhaseResearch] = true
	sess.Completed[PhaseProvision] = true
	sess.Current = PhaseProvision

	res := dispatch(t.Context(), store, RecipeInput{
		Action: "enter-phase", Slug: "synth-showcase", Phase: "scaffold",
	})
	if !res.OK {
		t.Fatalf("enter-phase scaffold: %+v", res)
	}

	for _, cb := range sess.Plan.Codebases {
		if cb.Hostname == "api" {
			if cb.SourceRoot != customRoot {
				t.Errorf("explicit SourceRoot overwritten: got %q, want %q",
					cb.SourceRoot, customRoot)
			}
		} else {
			want := "/var/www/" + cb.Hostname + "dev"
			if cb.SourceRoot != want {
				t.Errorf("codebase %q SourceRoot = %q, want %q",
					cb.Hostname, cb.SourceRoot, want)
			}
		}
	}
}

// TestCopyCommittedYAML_MissingFileReturnsHardError — after A2, an empty
// SourceRoot or a missing source yaml is a hard error (not a silent
// soft-fail). Signals scaffold didn't run or didn't author the yaml.
func TestCopyCommittedYAML_MissingFileReturnsHardError(t *testing.T) {
	t.Parallel()

	// Case 1: SourceRoot empty.
	cb := Codebase{Hostname: "api", SourceRoot: ""}
	err := copyCommittedYAML(cb, t.TempDir())
	if err == nil {
		t.Fatal("expected hard error when SourceRoot empty")
	}
	if !strings.Contains(err.Error(), "scaffold did not run") {
		t.Errorf("error message should name root cause; got: %v", err)
	}

	// Case 2: SourceRoot points at real dir but yaml is missing.
	srcDir := t.TempDir()
	cb = Codebase{Hostname: "api", SourceRoot: srcDir}
	err = copyCommittedYAML(cb, t.TempDir())
	if err == nil {
		t.Fatal("expected hard error when zerops.yaml missing")
	}
	if !strings.Contains(err.Error(), "scaffold did not author") {
		t.Errorf("error message should name root cause; got: %v", err)
	}
}

// TestStitch_PerCodebaseYamlCopied — with SourceRoot populated and the
// scaffold yaml on disk, stitch-content copies it byte-identical (including
// inline comments) to the apps-repo shape. A2 + A2's fail-loud refinement.
func TestStitch_PerCodebaseYamlCopied(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run")
	store := NewStore(dir)
	if _, err := store.OpenOrCreate("synth-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	// Stage scaffold-authored yaml for each codebase and point SourceRoot
	// at it so copyCommittedYAML succeeds.
	for i, cb := range sess.Plan.Codebases {
		srcDir := filepath.Join(dir, "workspace", cb.Hostname)
		if err := os.MkdirAll(srcDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		body := "# " + cb.Hostname + " — commented inline, verbatim\nzerops: []\n"
		if err := os.WriteFile(filepath.Join(srcDir, "zerops.yaml"), []byte(body), 0o600); err != nil {
			t.Fatalf("write yaml: %v", err)
		}
		sess.Plan.Codebases[i].SourceRoot = srcDir
	}

	if err := fillAllFragments(store, "synth-showcase", sess.Plan); err != nil {
		t.Fatalf("fill fragments: %v", err)
	}
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "stitch-content", Slug: "synth-showcase",
	})
	if !res.OK {
		t.Fatalf("stitch: %+v", res)
	}

	for _, cb := range sess.Plan.Codebases {
		copied, err := os.ReadFile(filepath.Join(outputRoot, "codebases", cb.Hostname, "zerops.yaml"))
		if err != nil {
			t.Fatalf("read copied yaml for %s: %v", cb.Hostname, err)
		}
		want := "# " + cb.Hostname + " — commented inline, verbatim\nzerops: []\n"
		if string(copied) != want {
			t.Errorf("codebase %s yaml differs\nwant:\n%s\ngot:\n%s",
				cb.Hostname, want, copied)
		}
	}
}

// TestRecordFragment_ResponseEchoesID — run-9-readiness §2.J. Response
// includes fragmentId + bodyBytes + appended so the author sees which
// fragment landed and whether append semantics fired.
func TestRecordFragment_ResponseEchoesID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	outputRoot := filepath.Join(dir, "run")
	if _, err := store.OpenOrCreate("synth-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	res := dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "codebase/api/integration-guide",
		Fragment:   "scaffold body",
	})
	if !res.OK {
		t.Fatalf("record-fragment: %+v", res)
	}
	if res.FragmentID != "codebase/api/integration-guide" {
		t.Errorf("FragmentID = %q", res.FragmentID)
	}
	if res.BodyBytes != len("scaffold body") {
		t.Errorf("BodyBytes = %d, want %d", res.BodyBytes, len("scaffold body"))
	}
	if res.Appended {
		t.Error("first record should have Appended=false")
	}
}

// TestRecordFragment_AppendSetsAppendedTrue — second call to an append-
// class id fires append semantics. BodyBytes = sum of both + 2 (`\n\n`).
func TestRecordFragment_AppendSetsAppendedTrue(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	outputRoot := filepath.Join(dir, "run")
	if _, err := store.OpenOrCreate("synth-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "codebase/api/integration-guide", Fragment: "scaffold",
	})
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "codebase/api/integration-guide", Fragment: "feature",
	})
	if !res.OK {
		t.Fatalf("append: %+v", res)
	}
	if !res.Appended {
		t.Error("second append-class record should have Appended=true")
	}
	wantBytes := len("scaffold") + len("\n\n") + len("feature")
	if res.BodyBytes != wantBytes {
		t.Errorf("BodyBytes = %d, want %d", res.BodyBytes, wantBytes)
	}
}

// TestRecordFragment_OverwriteSetsAppendedFalse — root/env ids
// overwrite. BodyBytes = size of the latest body.
func TestRecordFragment_OverwriteSetsAppendedFalse(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	outputRoot := filepath.Join(dir, "run")
	if _, err := store.OpenOrCreate("synth-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "root/intro", Fragment: "first",
	})
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "root/intro", Fragment: "second-longer",
	})
	if !res.OK {
		t.Fatalf("overwrite: %+v", res)
	}
	if res.Appended {
		t.Error("root/intro overwrite should have Appended=false")
	}
	if res.BodyBytes != len("second-longer") {
		t.Errorf("BodyBytes = %d, want %d", res.BodyBytes, len("second-longer"))
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
