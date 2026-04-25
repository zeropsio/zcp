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
		body := "zerops:\n  - setup: prod\n    # " + cb.Hostname + " — because reference style\n    run:\n      base: nodejs@22\n"
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
	fragmentIDs := map[string]string{
		"root/intro":                              "intro",
		"env/0/intro":                             "tier 0",
		"env/1/intro":                             "tier 1",
		"env/2/intro":                             "tier 2",
		"env/3/intro":                             "tier 3",
		"env/4/intro":                             "tier 4",
		"env/5/intro":                             "tier 5",
		"codebase/api/intro":                      "api",
		"codebase/api/integration-guide":          authored,
		"codebase/api/knowledge-base":             "- **Topic** — prose",
		"codebase/api/claude-md/service-facts":    "port 3000",
		"codebase/api/claude-md/notes":            "dev loop",
		"codebase/app/intro":                      "app",
		"codebase/app/integration-guide":          authored,
		"codebase/app/knowledge-base":             "- **Topic** — prose",
		"codebase/app/claude-md/service-facts":    "port 5173",
		"codebase/app/claude-md/notes":            "dev loop",
		"codebase/worker/intro":                   "worker",
		"codebase/worker/integration-guide":       authored,
		"codebase/worker/knowledge-base":          "- **Topic** — prose",
		"codebase/worker/claude-md/service-facts": "worker queue",
		"codebase/worker/claude-md/notes":         "dev loop",
	}
	for id, body := range fragmentIDs {
		res := dispatch(t.Context(), store, RecipeInput{
			Action: "record-fragment", Slug: "synth-showcase",
			FragmentID: id, Fragment: body,
		})
		if !res.OK {
			t.Fatalf("record-fragment %s: %+v", id, res)
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
	})
	dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "codebase/api/integration-guide", Fragment: "second body",
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
	})
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "codebase/api/integration-guide", Fragment: "corrected body",
		Mode: "replace",
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
