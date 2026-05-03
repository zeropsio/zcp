package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordFragmentCall is a test-side fixture for batch-dispatching
// record-fragment calls. The Class field carries the run-19-required
// Classification for KB/IG fragmentIDs (other surfaces leave it empty).
type recordFragmentCall struct {
	ID    string
	Body  string
	Class string
}

// initStyleClaudeMD returns a /init-shape CLAUDE.md body suitable for
// fixtures that need a valid run-16 claudemd-author output. Big enough
// to clear the validateCodebaseCLAUDE 200-byte minimum and shape-correct
// (2 H2 sections, no Zerops content) to pass slot-shape refusal.
func initStyleClaudeMD(host string) string {
	return fmt.Sprintf(`# %s

NestJS REST application that owns the showcase domain logic and exposes a HTTP API for the SPA. Framework-canonical layout with module + controller + service trios under src/.

## Build & run

- npm install — install dependencies (npm ci on CI)
- npm run start:dev — local dev with hot reload
- npm test — unit tests via jest
- npm run build — compile to dist/ for production

## Architecture

- src/main.ts — application bootstrap
- src/app.module.ts — root NestJS module
- src/items/ — items REST controller + service
- src/health/ — health endpoint module
`, host)
}

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

// Run-16 §6.4 — fill-fact-slot wires Session.FillFactSlot through the
// action dispatch path. The agent uses this after consulting
// zerops_knowledge to fill engine-pre-seeded shells (§7.2).
func TestDispatch_FillFactSlot_MergesSlotIntoEngineEmittedFact(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	outputRoot := filepath.Join(dir, "run")

	res := dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outputRoot,
	})
	if !res.OK {
		t.Fatalf("start: %+v", res)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	// Seed an engine-emitted shell directly (tranche 2 wires this from
	// emittedFactsForCodebase; tranche 1 just verifies the merge action).
	shell := FactRecord{
		Topic:            "apidev-connect-db",
		Kind:             FactKindPorterChange,
		EngineEmitted:    true,
		CandidateClass:   "intersection",
		CandidateSurface: "CODEBASE_IG",
		CitationGuide:    "managed-services-postgresql",
	}
	if err := sess.FactsLog.Append(shell); err != nil {
		t.Fatalf("Append shell: %v", err)
	}

	// Agent fills Why + CandidateHeading + Library after consulting the
	// per-managed-service knowledge atom.
	res = dispatch(t.Context(), store, RecipeInput{
		Action: "fill-fact-slot", Slug: "synth-showcase",
		Fact: &FactRecord{
			Topic:            "apidev-connect-db",
			Why:              "Postgres credentials live on db_hostname / db_user / db_password aliases via own-key projection.",
			CandidateHeading: "Connect to PostgreSQL via own-key aliases",
			Library:          "@nestjs/typeorm",
		},
	})
	if !res.OK {
		t.Fatalf("fill-fact-slot: %+v", res)
	}

	got, err := sess.FactsLog.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (merge replaces, not appends)", len(got))
	}
	if got[0].Why == "" || got[0].CandidateHeading == "" || got[0].Library == "" {
		t.Errorf("merged fact missing slot values: %+v", got[0])
	}
	if got[0].EngineEmitted {
		t.Error("EngineEmitted should flip to false on fill")
	}
}

func TestDispatch_FillFactSlot_RejectsMissingTopic(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	outputRoot := filepath.Join(dir, "run")

	res := dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outputRoot,
	})
	if !res.OK {
		t.Fatalf("start: %+v", res)
	}

	// No fact payload — must error.
	res = dispatch(t.Context(), store, RecipeInput{
		Action: "fill-fact-slot", Slug: "synth-showcase",
	})
	if res.OK {
		t.Error("fill-fact-slot without fact payload should error")
	}

	// Topic that doesn't exist in the log — must error.
	res = dispatch(t.Context(), store, RecipeInput{
		Action: "fill-fact-slot", Slug: "synth-showcase",
		Fact: &FactRecord{Topic: "does-not-exist", Why: "..."},
	})
	if res.OK {
		t.Error("fill-fact-slot for unknown topic should error")
	}
}

// Run-16 §8.1 — record-fragment slot-shape refusals reach the agent
// through the dispatch path (closes R-15-3 and R-15-4 at record time).
func TestDispatch_RecordFragment_RefusesNestedExtractMarkers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	outputRoot := filepath.Join(dir, "run")

	res := dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outputRoot,
	})
	if !res.OK {
		t.Fatalf("start: %+v", res)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	body := "Tier 0 intro\n<!-- #ZEROPS_EXTRACT_START -->\nbleed"
	res = dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "env/0/intro", Fragment: body,
	})
	if res.OK {
		t.Error("env/<N>/intro with #ZEROPS_EXTRACT_ marker should be refused (R-15-3)")
	}
	if !strings.Contains(res.Error, "R-15-3") {
		t.Errorf("error should name R-15-3: %q", res.Error)
	}
}

func TestDispatch_RecordFragment_RefusesMultiHeadingInIGSlot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	outputRoot := filepath.Join(dir, "run")

	res := dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outputRoot,
	})
	if !res.OK {
		t.Fatalf("start: %+v", res)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	body := "### 2. Trust the L7\nbody\n### 3. Drain on SIGTERM\nbody"
	res = dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID:     "codebase/api/integration-guide/2",
		Fragment:       body,
		Classification: "platform-invariant",
	})
	if res.OK {
		t.Error("multi-heading slotted IG should be refused (R-15-5)")
	}
}

// TestDispatch_RecordFragment_AcceptsClaudeMDZeropsContent — Run-21 R2-5.
//
// Pre-R2-5 the engine refused CLAUDE.md fragments containing `## Zerops`
// headings or hostname mentions. R2-5 dropped those guards: the brief
// at `briefs/claudemd-author/zerops_free_prohibition.md` is the
// authoring contract; engine-side word-blacklisting added false-
// positive friction (4× rejection cycle in run-21 around common
// English tokens). The dispatch now passes content the prior guard
// refused; structural-only checks remain.
func TestDispatch_RecordFragment_AcceptsClaudeMDZeropsContent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	outputRoot := filepath.Join(dir, "run")

	res := dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outputRoot,
	})
	if !res.OK {
		t.Fatalf("start: %+v", res)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	body := "## Build & run\n- npm test\n## Zerops service facts\n- port 3000\n## Architecture\n- src/"
	res = dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "codebase/api/claude-md", Fragment: body,
	})
	if !res.OK {
		t.Errorf("R2-5: claude-md with `## Zerops` heading must pass record-fragment now; got %+v", res)
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

// TestStitch_NonAbsSourceRoot_HardFails — run-11 gap M-1. Run 10 closed
// with cb.SourceRoot carrying the bare hostname ("api", "app", "worker")
// at finalize stitch time, causing README/CLAUDE to land at cwd-relative
// paths nothing else reads. Stitch must refuse non-absolute SourceRoot
// loud and name the offending codebase.
func TestStitch_NonAbsSourceRoot_HardFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run")
	store := NewStore(dir)
	if _, err := store.OpenOrCreate("synth-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()
	// The bug shape: bare hostname, relative.
	sess.Plan.Codebases[0].SourceRoot = "api"

	res := dispatch(t.Context(), store, RecipeInput{
		Action: "stitch-content", Slug: "synth-showcase",
	})
	if res.OK {
		t.Fatalf("expected stitch refusal on non-abs SourceRoot, got OK: %+v", res)
	}
	if !strings.Contains(res.Error, "api") {
		t.Errorf("error must name the codebase, got: %s", res.Error)
	}
	if !strings.Contains(res.Error, "absolute") {
		t.Errorf("error must mention absolute path requirement, got: %s", res.Error)
	}
}

// TestStitch_NonDevSuffixedSourceRoot_HardFails — M-1 second guard.
// `cb.SourceRoot = "/var/www/api"` (absolute but no `dev` suffix) is
// the synthetic location run-10 stitched into; nothing reads from
// there. Stitch must refuse.
func TestStitch_NonDevSuffixedSourceRoot_HardFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run")
	store := NewStore(dir)
	if _, err := store.OpenOrCreate("synth-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()
	sess.Plan.Codebases[0].SourceRoot = "/var/www/api"

	res := dispatch(t.Context(), store, RecipeInput{
		Action: "stitch-content", Slug: "synth-showcase",
	})
	if res.OK {
		t.Fatalf("expected stitch refusal on non-dev-suffixed SourceRoot, got OK: %+v", res)
	}
	if !strings.Contains(res.Error, "dev") {
		t.Errorf("error must name the dev-suffix requirement, got: %s", res.Error)
	}
}

// TestStitch_WritesCodebaseReadmeToSourceRoot — run-10-readiness §L.
// Apps-repo content (README + CLAUDE.md) lands at `<cb.SourceRoot>/`, the
// same tree as source, matching the reference apps-repo shape at
// /Users/fxck/www/laravel-showcase-app/. The invented intermediate
// directory `<outputRoot>/codebases/<h>/` is gone.
func TestStitch_WritesCodebaseReadmeToSourceRoot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run")
	store := NewStore(dir)
	if _, err := store.OpenOrCreate("synth-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	// Stage the scaffold-authored yaml at each SourceRoot — after L, the
	// same tree receives README + CLAUDE.md.
	for i, cb := range sess.Plan.Codebases {
		srcDir := filepath.Join(dir, "workspace", cb.Hostname+"dev")
		if err := os.MkdirAll(srcDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		body := "# " + cb.Hostname + " — commented inline, verbatim\nzerops: []\n"
		if err := os.WriteFile(filepath.Join(srcDir, "zerops.yaml"), []byte(body), 0o600); err != nil {
			t.Fatalf("write yaml: %v", err)
		}
		sess.Plan.Codebases[i].SourceRoot = srcDir
	}

	if err := fillAllFragments(store, sess.Plan); err != nil {
		t.Fatalf("fill fragments: %v", err)
	}
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "stitch-content", Slug: "synth-showcase",
	})
	if !res.OK {
		t.Fatalf("stitch: %+v", res)
	}

	for _, cb := range sess.Plan.Codebases {
		readmePath := filepath.Join(cb.SourceRoot, "README.md")
		if _, err := os.Stat(readmePath); err != nil {
			t.Errorf("README at SourceRoot missing for %s: %v", cb.Hostname, err)
		}
		claudePath := filepath.Join(cb.SourceRoot, "CLAUDE.md")
		if _, err := os.Stat(claudePath); err != nil {
			t.Errorf("CLAUDE.md at SourceRoot missing for %s: %v", cb.Hostname, err)
		}
		// The scaffold yaml stays where scaffold wrote it — no copy.
		yamlPath := filepath.Join(cb.SourceRoot, "zerops.yaml")
		if _, err := os.Stat(yamlPath); err != nil {
			t.Errorf("zerops.yaml at SourceRoot missing for %s: %v", cb.Hostname, err)
		}
	}
}

// TestStitch_IGFirstItemIsEmbeddedYaml — run-10-readiness §M.
// The stitch auto-generates IG item #1 as a fenced yaml block containing
// `<cb.SourceRoot>/zerops.yaml` verbatim. Matches the reference
// laravel-showcase-app/README.md pattern where the Integration Guide
// opens with the full yaml + inline comments — the porter sees the
// shape at a glance.
func TestStitch_IGFirstItemIsEmbeddedYaml(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run")
	store := NewStore(dir)
	if _, err := store.OpenOrCreate("synth-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	yamlByHost := map[string]string{}
	for i, cb := range sess.Plan.Codebases {
		srcDir := filepath.Join(dir, "workspace", cb.Hostname+"dev")
		if err := os.MkdirAll(srcDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		// Run-20 E1 — scaffold-leaked indented `#` comments are stripped
		// before IG #1 stamps the yaml. The bare-yaml fixture matches
		// the post-strip shape so the byte-for-byte assertion below
		// holds across the strip-then-inject path.
		body := "zerops:\n  - setup: prod\n    run:\n      base: nodejs@22\n"
		if err := os.WriteFile(filepath.Join(srcDir, "zerops.yaml"), []byte(body), 0o600); err != nil {
			t.Fatalf("write yaml: %v", err)
		}
		sess.Plan.Codebases[i].SourceRoot = srcDir
		yamlByHost[cb.Hostname] = body
	}

	if err := fillAllFragments(store, sess.Plan); err != nil {
		t.Fatalf("fill fragments: %v", err)
	}
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "stitch-content", Slug: "synth-showcase",
	})
	if !res.OK {
		t.Fatalf("stitch: %+v", res)
	}

	for _, cb := range sess.Plan.Codebases {
		body, err := os.ReadFile(filepath.Join(cb.SourceRoot, "README.md"))
		if err != nil {
			t.Fatalf("read README for %s: %v", cb.Hostname, err)
		}
		s := string(body)
		if !strings.Contains(s, "### 1. Adding `zerops.yaml`") {
			t.Errorf("%s README: missing `### 1. Adding ` + zerops.yaml header", cb.Hostname)
		}
		// The fenced yaml block must contain the yaml byte-for-byte.
		want := yamlByHost[cb.Hostname]
		if !strings.Contains(s, "```yaml\n"+want+"```") {
			t.Errorf("%s README: fenced yaml block does not match source.\nwant contained:\n%s\n---\n, got:\n%s",
				cb.Hostname, want, s)
		}
	}
}

// TestStitch_IGItem1IntroDescribesYamlBehavior — run-10-readiness §M
// follow-up. The item-#1 intro sentence is derived from the yaml body:
// which setups are declared, whether initCommands run (migrations /
// seeding / scout-import), and whether health / readiness checks ship.
// A porter reading the IG learns what THIS yaml does without having to
// decode the code block.
func TestStitch_IGItem1IntroDescribesYamlBehavior(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	apiRoot := filepath.Join(dir, "workspace", "apidev")
	if err := os.MkdirAll(apiRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yaml := `zerops:
  - setup: dev
    run:
      base: nodejs@22
      start: zsc noop --silent

  - setup: prod
    build:
      base: nodejs@22
    deploy:
      readinessCheck:
        httpGet:
          port: 3000
          path: /health
    run:
      base: nodejs@22
      start: node dist/main.js
      initCommands:
        - zsc execOnce ${appVersionId} -- node scripts/migrate.js
        - zsc execOnce ${appVersionId} -- node scripts/seed.js
      healthCheck:
        httpGet:
          port: 3000
          path: /health
`
	if err := os.WriteFile(filepath.Join(apiRoot, "zerops.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	plan := syntheticShowcasePlan()
	for i := range plan.Codebases {
		if plan.Codebases[i].Hostname == "api" {
			plan.Codebases[i].SourceRoot = apiRoot
		}
	}
	body, _, err := AssembleCodebaseREADME(plan, "api")
	if err != nil {
		t.Fatalf("AssembleCodebaseREADME: %v", err)
	}
	// Extract the intro (prose between "### 1." heading and the yaml fence)
	// so phrase assertions check the generated sentence, not stanzas in the
	// embedded yaml body.
	headerIdx := strings.Index(body, "### 1. Adding `zerops.yaml`")
	fenceIdx := strings.Index(body, "```yaml")
	if headerIdx < 0 || fenceIdx <= headerIdx {
		t.Fatalf("IG structure malformed: header=%d fence=%d\n%s", headerIdx, fenceIdx, body)
	}
	intro := body[headerIdx:fenceIdx]
	// The intro sentence names the setups, init-command presence, and
	// readiness-check presence so a porter knows what this yaml does.
	for _, phrase := range []string{
		"dev",
		"prod",
		"migrations",
		"readiness",
	} {
		if !strings.Contains(strings.ToLower(intro), phrase) {
			t.Errorf("IG item #1 intro must mention %q; intro was:\n%s",
				phrase, intro)
		}
	}
}

// TestStitch_IGSubsequentItemsArePorterItems — run-10-readiness §M.
// After the engine-generated item #1, fragment-authored items appear —
// the sub-agent's fragment starts at "### 2." per the updated brief.
func TestStitch_IGSubsequentItemsArePorterItems(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run")
	store := NewStore(dir)
	if _, err := store.OpenOrCreate("synth-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()
	for i, cb := range sess.Plan.Codebases {
		srcDir := filepath.Join(dir, "workspace", cb.Hostname+"dev")
		if err := os.MkdirAll(srcDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(srcDir, "zerops.yaml"),
			[]byte("zerops: []\n"), 0o600); err != nil {
			t.Fatalf("write yaml: %v", err)
		}
		sess.Plan.Codebases[i].SourceRoot = srcDir
	}

	// Fragment authored by sub-agent starts at ### 2.
	authored := "### 2. Trust the reverse proxy\n\nSet `trust proxy` so the runtime reads `X-Forwarded-*` correctly."
	fragments := []recordFragmentCall{
		{ID: "root/intro", Body: "intro"},
		{ID: "env/0/intro", Body: "tier 0"},
		{ID: "env/1/intro", Body: "tier 1"},
		{ID: "env/2/intro", Body: "tier 2"},
		{ID: "env/3/intro", Body: "tier 3"},
		{ID: "env/4/intro", Body: "tier 4"},
		{ID: "env/5/intro", Body: "tier 5"},
		{ID: "codebase/api/intro", Body: "api"},
		{ID: "codebase/api/integration-guide", Body: authored, Class: "platform-invariant"},
		{ID: "codebase/api/knowledge-base", Body: "- **404 on Topic** — prose", Class: "platform-invariant"},
		{ID: "codebase/api/claude-md", Body: initStyleClaudeMD("api")},
		{ID: "codebase/app/intro", Body: "app"},
		{ID: "codebase/app/integration-guide", Body: authored, Class: "platform-invariant"},
		{ID: "codebase/app/knowledge-base", Body: "- **404 on Topic** — prose", Class: "platform-invariant"},
		{ID: "codebase/app/claude-md", Body: initStyleClaudeMD("app")},
		{ID: "codebase/worker/intro", Body: "worker"},
		{ID: "codebase/worker/integration-guide", Body: authored, Class: "platform-invariant"},
		{ID: "codebase/worker/knowledge-base", Body: "- **404 on Topic** — prose", Class: "platform-invariant"},
		{ID: "codebase/worker/claude-md", Body: initStyleClaudeMD("worker")},
	}
	for _, f := range fragments {
		res := dispatch(t.Context(), store, RecipeInput{
			Action: "record-fragment", Slug: "synth-showcase",
			FragmentID: f.ID, Fragment: f.Body, Classification: f.Class,
		})
		if !res.OK {
			t.Fatalf("record-fragment %s: %+v", f.ID, res)
		}
	}
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "stitch-content", Slug: "synth-showcase",
	})
	if !res.OK {
		t.Fatalf("stitch: %+v", res)
	}

	for _, cb := range sess.Plan.Codebases {
		body, err := os.ReadFile(filepath.Join(cb.SourceRoot, "README.md"))
		if err != nil {
			t.Fatalf("read README for %s: %v", cb.Hostname, err)
		}
		s := string(body)
		// Engine item #1 precedes the authored item #2.
		idx1 := strings.Index(s, "### 1. Adding `zerops.yaml`")
		idx2 := strings.Index(s, "### 2. Trust the reverse proxy")
		if idx1 < 0 || idx2 < 0 || idx1 >= idx2 {
			t.Errorf("%s README: engine item #1 must precede authored #2 (idx1=%d, idx2=%d)",
				cb.Hostname, idx1, idx2)
		}
	}
}

// TestStitch_IGWorksWithEmptyFragment — run-10-readiness §M.
// If the sub-agent never recorded the integration-guide fragment, the
// engine's item #1 still renders cleanly (the overall assemble gate
// still reports the missing fragment id, but the body isn't corrupt).
func TestStitch_IGWorksWithEmptyFragment(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	plan.Slug = "synth-showcase"
	plan.Framework = "synth"
	dir := t.TempDir()
	apiRoot := filepath.Join(dir, "workspace", "apidev")
	if err := os.MkdirAll(apiRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yaml := "zerops: []\n"
	if err := os.WriteFile(filepath.Join(apiRoot, "zerops.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	for i := range plan.Codebases {
		if plan.Codebases[i].Hostname == "api" {
			plan.Codebases[i].SourceRoot = apiRoot
		}
	}

	// Assemble without the IG fragment supplied.
	body, missing, err := AssembleCodebaseREADME(plan, "api")
	if err != nil {
		t.Fatalf("AssembleCodebaseREADME: %v", err)
	}
	// The IG fragment is reported missing — caller gates on this.
	foundMissing := false
	for _, id := range missing {
		if id == "codebase/api/integration-guide" {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Errorf("expected codebase/api/integration-guide in missing; got %v", missing)
	}
	// But the engine's item #1 is still present in the body — it does not
	// depend on the fragment body to render.
	if !strings.Contains(body, "### 1. Adding `zerops.yaml`") {
		t.Errorf("engine item #1 should render even when fragment missing; body:\n%s", body)
	}
	if !strings.Contains(body, "```yaml\n"+yaml+"```") {
		t.Errorf("engine-embedded yaml block should render; body:\n%s", body)
	}
}

// TestStitch_NoOutputRootCodebasesDirectory — run-10-readiness §L.
// The invented `<outputRoot>/codebases/` directory is not created by
// stitch. No published recipe has this directory; its only reader is the
// chain-resolver's silently-failing `loadParent`.
func TestStitch_NoOutputRootCodebasesDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run")
	store := NewStore(dir)
	if _, err := store.OpenOrCreate("synth-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()
	for i, cb := range sess.Plan.Codebases {
		srcDir := filepath.Join(dir, "workspace", cb.Hostname+"dev")
		if err := os.MkdirAll(srcDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(srcDir, "zerops.yaml"),
			[]byte("zerops: []\n"), 0o600); err != nil {
			t.Fatalf("write yaml: %v", err)
		}
		sess.Plan.Codebases[i].SourceRoot = srcDir
	}
	if err := fillAllFragments(store, sess.Plan); err != nil {
		t.Fatalf("fill fragments: %v", err)
	}
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "stitch-content", Slug: "synth-showcase",
	})
	if !res.OK {
		t.Fatalf("stitch: %+v", res)
	}

	invented := filepath.Join(outputRoot, "codebases")
	if _, err := os.Stat(invented); err == nil {
		t.Errorf("outputRoot/codebases/ must not be created; found %s", invented)
	}
}

// TestRecordFragment_RejectsSlotHostname — run-11 gap N-1. Slot
// hostnames (`appdev`, `apidev`, `workerdev`) are SSHFS mount names,
// NOT codebase hostnames. Run 10's scaffold-app recorded all 5
// fragments under `codebase/appdev/*` and the engine accepted them,
// causing a cleanup-sub-agent dispatch + 8 zerops_knowledge requeries.
// The error must name the Plan codebase list AND name the slot-vs-
// codebase distinction so the sub-agent retries with the correct id
// on the first try.
func TestRecordFragment_RejectsSlotHostname(t *testing.T) {
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
		FragmentID: "codebase/appdev/intro",
		Fragment:   "any body",
	})
	if res.OK {
		t.Fatalf("expected rejection on slot hostname, got OK: %+v", res)
	}
	if !strings.Contains(res.Error, "slot") || !strings.Contains(res.Error, "codebase") {
		t.Errorf("error must name slot-vs-codebase distinction, got: %q", res.Error)
	}
	for _, hostname := range []string{"api", "app", "worker"} {
		if !strings.Contains(res.Error, hostname) {
			t.Errorf("error must name plan codebase %q, got: %q", hostname, res.Error)
		}
	}
}

// TestRecordFragment_AcceptsCodebaseHostname — same Plan, correct id
// → ok:true. Pins that the rejection is specific to slot hostnames
// and doesn't over-trigger.
func TestRecordFragment_AcceptsCodebaseHostname(t *testing.T) {
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
		FragmentID: "codebase/app/intro",
		Fragment:   "scaffold-authored intro for app",
	})
	if !res.OK {
		t.Fatalf("record-fragment with codebase hostname should succeed, got: %+v", res)
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
		FragmentID:     "codebase/api/integration-guide",
		Fragment:       "scaffold body",
		Classification: "platform-invariant",
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

// TestRecordFragment_ResponseCarriesSurfaceContract pins run-15 F.2 —
// every record-fragment response carries the SurfaceContract for the
// resolved surface (reader + test + caps + FormatSpec) so the agent
// reads the per-surface authoring contract verbatim at decision time.
// Brief preface teaches surfaces once at boot; the contract delivered
// at record-time keeps the rule in working memory through every
// fragment authoring step.
func TestRecordFragment_ResponseCarriesSurfaceContract(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	outputRoot := filepath.Join(dir, "run")
	if _, err := store.OpenOrCreate("synth-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	cases := []struct {
		fragmentID string
		wantSurf   Surface
		class      string // empty for single-class surfaces; required for KB/IG
	}{
		{"root/intro", SurfaceRootREADME, ""},
		{"env/2/intro", SurfaceEnvREADME, ""},
		{"env/2/import-comments/api", SurfaceEnvImportComments, ""},
		{"codebase/api/integration-guide", SurfaceCodebaseIG, "platform-invariant"},
		{"codebase/api/knowledge-base", SurfaceCodebaseKB, "platform-invariant"},
		{"codebase/api/claude-md/notes", SurfaceCodebaseCLAUDE, ""},
	}
	for _, tc := range cases {
		res := dispatch(t.Context(), store, RecipeInput{
			Action: "record-fragment", Slug: "synth-showcase",
			FragmentID: tc.fragmentID, Fragment: "stub body",
			Classification: tc.class,
		})
		if !res.OK {
			t.Errorf("%q: dispatch failed: %+v", tc.fragmentID, res)
			continue
		}
		if res.SurfaceContract == nil {
			t.Errorf("%q: missing SurfaceContract on record-fragment response", tc.fragmentID)
			continue
		}
		if res.SurfaceContract.Name != tc.wantSurf {
			t.Errorf("%q: SurfaceContract.Name = %q, want %q", tc.fragmentID, res.SurfaceContract.Name, tc.wantSurf)
		}
		if res.SurfaceContract.Reader == "" {
			t.Errorf("%q: SurfaceContract.Reader empty", tc.fragmentID)
		}
		if res.SurfaceContract.Test == "" {
			t.Errorf("%q: SurfaceContract.Test empty", tc.fragmentID)
		}
		if res.SurfaceContract.FormatSpec == "" {
			t.Errorf("%q: SurfaceContract.FormatSpec empty", tc.fragmentID)
		}
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
		Classification: "platform-invariant",
	})
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "codebase/api/integration-guide", Fragment: "feature",
		Classification: "platform-invariant",
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

// TestRecordFragment_DefaultModeAppendsOnCodebaseID — run-12 §R. Default
// behavior on append-class ids stays append (so feature can extend
// scaffold). Mode field unspecified.
func TestRecordFragment_DefaultModeAppendsOnCodebaseID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	if _, err := store.OpenOrCreate("synth-showcase", filepath.Join(dir, "run")); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "codebase/api/integration-guide", Fragment: "first body",
		Classification: "platform-invariant",
	})
	dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "codebase/api/integration-guide", Fragment: "second body",
		Classification: "platform-invariant",
	})
	got := sess.Plan.Fragments["codebase/api/integration-guide"]
	if got != "first body\n\nsecond body" {
		t.Errorf("default mode should append; got %q", got)
	}
}

// TestRecordFragment_ModeReplaceOverwrites — run-12 §R. mode=replace
// overwrites prior body even on append-class ids; sub-agent uses this
// to correct a fragment after a complete-phase violation.
func TestRecordFragment_ModeReplaceOverwrites(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	if _, err := store.OpenOrCreate("synth-showcase", filepath.Join(dir, "run")); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "codebase/api/integration-guide", Fragment: "first body",
		Classification: "platform-invariant",
	})
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "codebase/api/integration-guide", Fragment: "corrected body",
		Mode:           "replace",
		Classification: "platform-invariant",
	})
	if !res.OK {
		t.Fatalf("replace dispatch: %+v", res)
	}
	if res.Appended {
		t.Error("mode=replace must not set Appended=true")
	}
	got := sess.Plan.Fragments["codebase/api/integration-guide"]
	if got != "corrected body" {
		t.Errorf("mode=replace should overwrite; got %q", got)
	}
}

// TestRecordFragment_ModeReplaceOnEnvIDIsEquivalentToOverwrite — run-12
// §R. env/<N>/intro is overwrite by default; mode=replace keeps that
// behavior, no error.
func TestRecordFragment_ModeReplaceOnEnvIDIsEquivalentToOverwrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	if _, err := store.OpenOrCreate("synth-showcase", filepath.Join(dir, "run")); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "env/0/intro", Fragment: "v1",
	})
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "env/0/intro", Fragment: "v2", Mode: "replace",
	})
	if !res.OK {
		t.Fatalf("replace on env id: %+v", res)
	}
	if got := sess.Plan.Fragments["env/0/intro"]; got != "v2" {
		t.Errorf("mode=replace on env id should overwrite; got %q", got)
	}
}

// TestRecordFragment_ReplaceReturnsPriorBody pins Cluster B.1 (R-13-3):
// mode=replace overwrites the entire fragment body, so the response
// carries the prior body. The agent extending an existing fragment can
// merge against priorBody verbatim instead of grep+reconstructing from
// the on-disk README — features-1 burned ~1m38s in run-13 doing exactly
// that after a replace clobbered five scaffold-authored IG sections.
func TestRecordFragment_ReplaceReturnsPriorBody(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	if _, err := store.OpenOrCreate("synth-showcase", filepath.Join(dir, "run")); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID:     "codebase/api/integration-guide",
		Fragment:       "ORIGINAL\n",
		Classification: "platform-invariant",
	})

	res := dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID:     "codebase/api/integration-guide",
		Fragment:       "REPLACED\n",
		Mode:           "replace",
		Classification: "platform-invariant",
	})
	if !res.OK {
		t.Fatalf("replace dispatch: %+v", res)
	}
	if res.PriorBody != "ORIGINAL\n" {
		t.Errorf("PriorBody = %q, want %q", res.PriorBody, "ORIGINAL\n")
	}
}

// TestRecordFragment_AppendDoesNotReturnPriorBody pins the negative
// half of Cluster B.1: append-class operations do not need priorBody
// in the response (the response already carries the post-append body
// size; the agent never lost prior content via append).
func TestRecordFragment_AppendDoesNotReturnPriorBody(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	if _, err := store.OpenOrCreate("synth-showcase", filepath.Join(dir, "run")); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "codebase/api/integration-guide", Fragment: "first",
		Classification: "platform-invariant",
	})
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "codebase/api/integration-guide", Fragment: "second",
		Classification: "platform-invariant",
	})
	if !res.OK {
		t.Fatalf("append: %+v", res)
	}
	if res.PriorBody != "" {
		t.Errorf("append should not populate PriorBody; got %q", res.PriorBody)
	}
}

// TestRecordFragment_UnknownModeRejected — run-12 §R. Mode strings other
// than "" / "append" / "replace" are rejected with an actionable error.
func TestRecordFragment_UnknownModeRejected(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	if _, err := store.OpenOrCreate("synth-showcase", filepath.Join(dir, "run")); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	res := dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "env/0/intro", Fragment: "v1", Mode: "concat",
	})
	if res.OK {
		t.Errorf("expected error for mode=concat, got OK")
	}
	if !strings.Contains(res.Error, "mode must be") {
		t.Errorf("error should explain valid modes; got %q", res.Error)
	}
}

// TestVerifyDispatch_MatchingBriefAccepted — run-12 §D. Wrapper text
// around the brief (header before, context after) is allowed; only
// truncations/paraphrases are rejected. Run-13 §4 clarified that
// position is unconstrained — pre-pending also passes.
func TestVerifyDispatch_MatchingBriefAccepted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	if _, err := store.OpenOrCreate("synth-showcase", filepath.Join(dir, "run")); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	brief, err := BuildScaffoldBrief(sess.Plan, sess.Plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	cases := []struct {
		name       string
		dispatched string
	}{
		{name: "wrapper appended", dispatched: brief.Body + "\n\n## Wrapper notes\nx\n"},
		{name: "wrapper prepended", dispatched: "Sub-agent: scaffold api.\n\n" + brief.Body},
		{name: "wrapper both sides", dispatched: "Header.\n\n" + brief.Body + "\n\nFooter.\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := dispatch(t.Context(), store, RecipeInput{
				Action: "verify-subagent-dispatch", Slug: "synth-showcase",
				BriefKind: "scaffold", Codebase: "api",
				DispatchedPrompt: tc.dispatched,
			})
			if !res.OK {
				t.Errorf("matching dispatch rejected: %+v", res)
			}
		})
	}
}

// TestVerifyDispatch_TruncatedBriefRejected — run-12 §D. Run-11 main
// agent truncated scaffold-app brief 14582 → 9047 (62%); D's check
// catches this.
func TestVerifyDispatch_TruncatedBriefRejected(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	if _, err := store.OpenOrCreate("synth-showcase", filepath.Join(dir, "run")); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	brief, _ := BuildScaffoldBrief(sess.Plan, sess.Plan.Codebases[0], nil)
	truncated := brief.Body[:len(brief.Body)/2]
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "verify-subagent-dispatch", Slug: "synth-showcase",
		BriefKind: "scaffold", Codebase: "api",
		DispatchedPrompt: truncated,
	})
	if res.OK {
		t.Error("truncated dispatch accepted")
	}
	if !strings.Contains(res.Error, "dispatch missing engine brief body") {
		t.Errorf("error should explain mismatch; got %q", res.Error)
	}
}

// TestVerifyDispatch_ParaphrasedBriefRejected — run-12 §D. Wrapper that
// rewrites engine prose loses critical platform rules; rejected.
func TestVerifyDispatch_ParaphrasedBriefRejected(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	if _, err := store.OpenOrCreate("synth-showcase", filepath.Join(dir, "run")); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	paraphrased := "You are the api scaffold agent. Bind 0.0.0.0. Trust the L7 proxy. Done.\n"
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "verify-subagent-dispatch", Slug: "synth-showcase",
		BriefKind: "scaffold", Codebase: "api",
		DispatchedPrompt: paraphrased,
	})
	if res.OK {
		t.Error("paraphrased dispatch accepted")
	}
	if !strings.Contains(res.Error, "dispatch missing engine brief body") {
		t.Errorf("error should explain mismatch; got %q", res.Error)
	}
}

// TestDispatch_CompletePhaseScaffold_AutoStitchesCodebases — run-13
// §3. complete-phase scaffold materializes per-codebase fragments to
// <SourceRoot>/{README.md,CLAUDE.md} so codebase surface validators
// see them. Without auto-stitch, validators silently no-op on
// IsNotExist and the right-author-fixes-violations design from §G
// fails to fire.
func TestDispatch_CompletePhaseScaffold_AutoStitchesCodebases(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run")
	store := NewStore(dir)
	dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outputRoot,
	})
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()
	stageScaffoldYAMLs(t, dir, sess.Plan)
	sess.Current = PhaseScaffold

	dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID:     "codebase/api/integration-guide",
		Fragment:       "### 2. Bind 0.0.0.0\n\nLoopback is unreachable.\n",
		Classification: "platform-invariant",
	})

	dispatch(t.Context(), store, RecipeInput{
		Action: "complete-phase", Slug: "synth-showcase",
	})

	apiSourceRoot := sess.Plan.Codebases[0].SourceRoot
	for _, leaf := range []string{"README.md", "CLAUDE.md"} {
		p := filepath.Join(apiSourceRoot, leaf)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s after complete-phase scaffold: %v", p, err)
		}
	}
}

// TestDispatch_CompletePhase_CodebaseScoped_OnlyValidatesNamedCodebase
// — run-13 §G2. complete-phase with codebase=<host> runs the codebase-
// scoped surface validators against just that codebase. A peer
// codebase's violation does NOT surface; the named codebase's
// violation does. Closes the §G actor mismatch — sub-agent self-
// validates before terminating, sees only its own work.
//
// Run-17 §8 — scoped close at codebase-content phase runs the
// content-surface validators (IG/KB/CLAUDE) since scaffold/feature no
// longer own content authoring.
func TestDispatch_CompletePhase_CodebaseScoped_OnlyValidatesNamedCodebase(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run")
	store := NewStore(dir)
	dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outputRoot,
	})
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()
	stageScaffoldYAMLs(t, dir, sess.Plan)
	sess.Current = PhaseCodebaseContent

	// api gets a violating IG (plain ordered list — codebase-ig-plain-
	// ordered-list fires); app gets a clean IG.
	dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID:     "codebase/api/integration-guide",
		Fragment:       "1. plain ordered\n2. list shape\n",
		Classification: "platform-invariant",
	})
	dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID:     "codebase/app/integration-guide",
		Fragment:       "### 2. Bind 0.0.0.0\n\nLoopback is unreachable.\n### 3. Use `vite preview`\n\nDev mode is wrong for prod.\n### 4. Object-storage path style\n\nForce path style.\n### 5. Trust proxy\n\nBehind L7.\n",
		Classification: "platform-invariant",
	})

	apiResult := dispatch(t.Context(), store, RecipeInput{
		Action: "complete-phase", Slug: "synth-showcase", Codebase: "api",
	})
	if apiResult.OK {
		t.Errorf("expected api scoped close to fail with violations, got OK=true; violations=%+v", apiResult.Violations)
	}
	if !containsCode(apiResult.Violations, "codebase-ig-plain-ordered-list") {
		t.Errorf("expected codebase-ig-plain-ordered-list on api, got %+v", apiResult.Violations)
	}

	appResult := dispatch(t.Context(), store, RecipeInput{
		Action: "complete-phase", Slug: "synth-showcase", Codebase: "app",
	})
	if containsCode(appResult.Violations, "codebase-ig-plain-ordered-list") {
		t.Errorf("expected NO plain-ordered-list on app, got %+v", appResult.Violations)
	}
}

// TestDispatch_CompletePhase_CodebaseScoped_DoesNotAdvancePhase —
// run-13 §G2. Per-codebase complete-phase is a self-validate, NOT a
// state transition. Phase stays scaffold even after a clean per-
// codebase close. The phase-advance trigger is still the no-codebase
// form, which is main's responsibility once all sub-agents return.
func TestDispatch_CompletePhase_CodebaseScoped_DoesNotAdvancePhase(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run")
	store := NewStore(dir)
	dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outputRoot,
	})
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()
	stageScaffoldYAMLs(t, dir, sess.Plan)
	sess.Current = PhaseScaffold

	dispatch(t.Context(), store, RecipeInput{
		Action: "complete-phase", Slug: "synth-showcase", Codebase: "api",
	})
	if sess.Completed[PhaseScaffold] {
		t.Errorf("scoped close should not mark phase complete; Completed[Scaffold] = true")
	}
}

// TestDispatch_CompletePhase_UnknownCodebase_Errors — run-13 §G2.
// Misspelled codebase name should fail loudly, not silently no-op.
func TestDispatch_CompletePhase_UnknownCodebase_Errors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run")
	store := NewStore(dir)
	dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outputRoot,
	})
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()
	sess.Current = PhaseScaffold

	res := dispatch(t.Context(), store, RecipeInput{
		Action: "complete-phase", Slug: "synth-showcase", Codebase: "nonexistent",
	})
	if res.OK {
		t.Errorf("expected error for unknown codebase, got OK=true")
	}
	if !strings.Contains(res.Error, "nonexistent") {
		t.Errorf("error message does not name the bad codebase; got %q", res.Error)
	}
}

// TestDispatch_CompletePhase_NoCodebase_StillAdvancesOnClean — run-13
// §G2 regression guard. The phase-level (no-codebase) close still
// advances the phase when gates pass. Sub-agents call the scoped
// form; main calls the phase-level form.
func TestDispatch_CompletePhase_NoCodebase_StillAdvancesOnClean(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run")
	store := NewStore(dir)
	dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outputRoot,
	})
	sess, _ := store.Get("synth-showcase")
	// Plan that satisfies provision-phase gates (no managed-service set
	// requirements until showcase tier; provision has none) and lets
	// the phase-level complete-phase advance cleanly.
	sess.Plan = &Plan{
		Slug: "synth-showcase", Framework: "synth", Tier: tierMinimal,
		Research: ResearchResult{CodebaseShape: "1"},
		Codebases: []Codebase{
			{Hostname: "app", Role: RoleMonolith, BaseRuntime: "nodejs@22"},
		},
		Services: []Service{
			{Hostname: "db", Type: "postgresql@18", Kind: ServiceKindManaged, Priority: 10},
		},
	}
	sess.Current = PhaseProvision
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "complete-phase", Slug: "synth-showcase",
	})
	if !res.OK {
		t.Fatalf("complete-phase provision should succeed for clean plan; violations=%+v error=%q", res.Violations, res.Error)
	}
	if !sess.Completed[PhaseProvision] {
		t.Errorf("phase-level close on clean plan must mark phase complete")
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
