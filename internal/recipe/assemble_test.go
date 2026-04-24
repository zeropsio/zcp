package recipe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAssemble_TemplateRendersStructuralData — Workstream A1: when a
// surface's structural template is rendered with a Plan, the output
// carries plan-derived tokens (title, slug, framework, tier list) even
// if no fragment bodies are attached yet. The assembler returns the
// rendered file + a list of missing fragment ids so the caller can gate.
func TestAssemble_TemplateRendersStructuralData(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	plan.Framework = "laravel"
	plan.Slug = "laravel-showcase"

	out, missing, err := AssembleRootREADME(plan)
	if err != nil {
		t.Fatalf("AssembleRootREADME: %v", err)
	}
	if !strings.Contains(out, "# laravel-showcase") {
		t.Errorf("title not rendered; got:\n%s", out)
	}
	if !strings.Contains(out, "https://app.zerops.io/recipes/laravel-showcase?environment=small-production") {
		t.Error("deploy-button URL not rendered with slug")
	}
	// 6 tier links, one per tier.
	for _, tier := range Tiers() {
		if !strings.Contains(out, tier.Label) {
			t.Errorf("tier label %q missing from rendered README", tier.Label)
		}
	}
	// Missing fragment is reported back, not silently filled.
	if len(missing) == 0 {
		t.Error("missing-fragments list empty, want root/intro unset")
	}
}

// TestAssemble_FragmentSubstitution — when Plan.Fragments contains a body
// for a template's marker, the marker block's inner content becomes the
// fragment body. No other template region is touched.
func TestAssemble_FragmentSubstitution(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	plan.Slug = "laravel-showcase"
	plan.Framework = "laravel"
	plan.Fragments = map[string]string{
		"root/intro": "A full-featured Laravel app demonstrating all Zerops integrations.",
	}

	out, missing, err := AssembleRootREADME(plan)
	if err != nil {
		t.Fatalf("AssembleRootREADME: %v", err)
	}
	for _, id := range missing {
		if id == "root/intro" {
			t.Fatalf("root/intro was provided but still reported missing; missing=%v", missing)
		}
	}
	if !strings.Contains(out, "A full-featured Laravel app demonstrating all Zerops integrations.") {
		t.Errorf("fragment body not substituted into marker block:\n%s", out)
	}
	if !strings.Contains(out, "<!-- #ZEROPS_EXTRACT_START:intro# -->") ||
		!strings.Contains(out, "<!-- #ZEROPS_EXTRACT_END:intro# -->") {
		t.Error("surrounding markers dropped during substitution")
	}
}

// TestAssemble_MissingFragmentFailsGate — the stitch-content gate must
// treat missing fragments as a gate failure, not an empty marker block.
// The scan happens against the rendered output so callers see the exact
// list of missing ids to record.
func TestAssemble_MissingFragmentFailsGate(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	plan.Slug = "x"
	plan.Framework = "x"
	// No Plan.Fragments — root/intro is unset.
	_, missing, err := AssembleRootREADME(plan)
	if err != nil {
		t.Fatalf("AssembleRootREADME: %v", err)
	}
	var sawIntro bool
	for _, id := range missing {
		if id == "root/intro" {
			sawIntro = true
		}
	}
	if !sawIntro {
		t.Errorf("expected root/intro in missing list; missing=%v", missing)
	}
}

// TestAssemble_NoUnreplacedTokens — after rendering, no leftover
// `{TOKEN}` placeholders should remain. Unbound tokens are a gate
// failure; catching them here keeps broken templates out of the
// stitched output.
func TestAssemble_NoUnreplacedTokens(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	plan.Slug = "laravel-showcase"
	plan.Framework = "laravel"
	plan.Fragments = map[string]string{"root/intro": "x"}

	out, _, err := AssembleRootREADME(plan)
	if err != nil {
		t.Fatalf("AssembleRootREADME: %v", err)
	}
	// Regex-like scan: any `{UPPERCASE_UNDERSCORE}` sequence means a
	// template token wasn't bound. False positives (e.g. JSON in
	// fragment bodies) are acceptable and rare at A1 scope — tokens
	// in fragment bodies would come from main/sub-agent text.
	if strings.ContainsAny(out, "{") {
		// Find any { followed by ALL CAPS + _ and then }.
		for i := 0; i < len(out)-2; i++ {
			if out[i] != '{' {
				continue
			}
			end := strings.IndexByte(out[i:], '}')
			if end <= 0 {
				continue
			}
			token := out[i+1 : i+end]
			if isAllUpperSnake(token) {
				t.Errorf("unreplaced token %q in rendered root README", token)
			}
		}
	}
}

func isAllUpperSnake(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || r == '_' {
			continue
		}
		return false
	}
	return true
}

// TestHandler_RecordFragment_AppendVsOverwrite — scaffold-authored
// codebase fragments (IG, KB, CLAUDE.md) are append-on-extend so a
// feature sub-agent can add to scaffold's body without clobbering it.
// Root / env fragments are overwrite because only the main agent
// authors them in finalize.
func TestHandler_RecordFragment_AppendVsOverwrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	outputRoot := filepath.Join(dir, "run")
	if _, err := store.OpenOrCreate("synth-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	// Append semantics — codebase IG extended by a feature sub-agent.
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "codebase/api/integration-guide",
		Fragment:   "scaffold body",
	})
	if !res.OK {
		t.Fatalf("record scaffold IG: %+v", res)
	}
	res = dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "codebase/api/integration-guide",
		Fragment:   "feature body",
	})
	if !res.OK {
		t.Fatalf("record feature IG: %+v", res)
	}
	body := sess.Plan.Fragments["codebase/api/integration-guide"]
	if !strings.Contains(body, "scaffold body") || !strings.Contains(body, "feature body") {
		t.Errorf("append semantics: expected both bodies, got: %q", body)
	}

	// Overwrite semantics — root/intro replaced on re-record.
	dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "root/intro", Fragment: "first",
	})
	dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID: "root/intro", Fragment: "second",
	})
	if got := sess.Plan.Fragments["root/intro"]; got != "second" {
		t.Errorf("overwrite semantics: root/intro = %q, want %q", got, "second")
	}
}

// TestHandler_RecordFragment_RejectsUnknownID — the engine validates the
// fragment id against the registered schema (per-codebase + per-tier
// ids based on the plan). An unknown id means the caller typo'd an id;
// silently accepting would land bad structure into the assembled output.
func TestHandler_RecordFragment_RejectsUnknownID(t *testing.T) {
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
		FragmentID: "codebase/doesnotexist/integration-guide",
		Fragment:   "x",
	})
	if res.OK {
		t.Error("expected unknown-codebase fragment id to be rejected")
	}
}

// TestAssemble_CopiesCommittedYaml — per-codebase zerops.yaml lands
// verbatim in the apps-repo shape at outputRoot/codebases/<hostname>/zerops.yaml.
// The scaffold sub-agent authors that file (with inline comments) during
// scaffold; A2's stitch copies it without re-parsing or re-emitting, so
// comments survive byte-identical.
func TestAssemble_CopiesCommittedYaml(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run")
	store := NewStore(dir)
	if _, err := store.OpenOrCreate("synth-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	// Stage a scaffold-authored zerops.yaml with a distinct inline
	// comment so the round-trip check catches any re-emission regression.
	scaffoldRoot := filepath.Join(dir, "workspace", "api")
	if err := os.MkdirAll(scaffoldRoot, 0o755); err != nil {
		t.Fatalf("mkdir scaffold: %v", err)
	}
	committed := `# scaffold-authored yaml — inline comment must survive verbatim
zerops:
  - setup: dev
    build:
      base: nodejs@22
    run:
      # why: L7 balancer routes to 0.0.0.0
      base: nodejs@22
`
	yamlPath := filepath.Join(scaffoldRoot, "zerops.yaml")
	if err := os.WriteFile(yamlPath, []byte(committed), 0o600); err != nil {
		t.Fatalf("write scaffold yaml: %v", err)
	}
	for i, cb := range sess.Plan.Codebases {
		if cb.Hostname == "api" {
			sess.Plan.Codebases[i].SourceRoot = scaffoldRoot
		}
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

	copied, err := os.ReadFile(filepath.Join(outputRoot, "codebases", "api", "zerops.yaml"))
	if err != nil {
		t.Fatalf("read copied yaml: %v", err)
	}
	if string(copied) != committed {
		t.Errorf("copied yaml differs from committed source\nwant:\n%s\ngot:\n%s",
			committed, copied)
	}
}

// TestAssemble_DeliverableSplit — stitchContent writes into two shapes
// under outputRoot. Recipes-repo shape: root README + per-tier README
// + per-tier import.yaml. Apps-repo shape (per codebase): README +
// CLAUDE.md + zerops.yaml (copied).
func TestAssemble_DeliverableSplit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run")
	store := NewStore(dir)
	if _, err := store.OpenOrCreate("synth-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()
	// Point each codebase at a staged workspace so the yaml copy step
	// can pick up a valid source.
	for i, cb := range sess.Plan.Codebases {
		wsRoot := filepath.Join(dir, "workspace", cb.Hostname)
		if err := os.MkdirAll(wsRoot, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", wsRoot, err)
		}
		if err := os.WriteFile(filepath.Join(wsRoot, "zerops.yaml"),
			[]byte("# "+cb.Hostname+" scaffold yaml\nzerops: []\n"), 0o600); err != nil {
			t.Fatalf("write workspace yaml: %v", err)
		}
		sess.Plan.Codebases[i].SourceRoot = wsRoot
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

	// Recipes-repo shape — root + 6 tier dirs.
	tiers := Tiers()
	recipesShape := make([]string, 0, 1+2*len(tiers))
	recipesShape = append(recipesShape, "README.md")
	for _, tier := range tiers {
		recipesShape = append(recipesShape,
			filepath.Join(tier.Folder, "README.md"),
			filepath.Join(tier.Folder, "import.yaml"),
		)
	}
	for _, p := range recipesShape {
		abs := filepath.Join(outputRoot, p)
		if _, err := os.Stat(abs); err != nil {
			t.Errorf("recipes-shape path missing %s: %v", p, err)
		}
	}

	// Apps-repo shape — per codebase: README + CLAUDE.md + zerops.yaml.
	for _, cb := range sess.Plan.Codebases {
		for _, want := range []string{"README.md", "CLAUDE.md", "zerops.yaml"} {
			abs := filepath.Join(outputRoot, "codebases", cb.Hostname, want)
			if _, err := os.Stat(abs); err != nil {
				t.Errorf("apps-shape path missing codebases/%s/%s: %v",
					cb.Hostname, want, err)
			}
		}
	}
}

// fillAllFragments populates every fragment id the synthetic plan
// declares so stitchContent runs without surfacing missing ids. Shared
// between A2 tests that need a clean assemble.
func fillAllFragments(store *Store, slug string, plan *Plan) error {
	ids := map[string]string{
		"root/intro": "intro",
	}
	for i := range Tiers() {
		ids[fmt.Sprintf("env/%d/intro", i)] = fmt.Sprintf("tier %d", i)
	}
	for _, cb := range plan.Codebases {
		base := "codebase/" + cb.Hostname
		ids[base+"/intro"] = "cb intro"
		ids[base+"/integration-guide"] = "1. IG"
		ids[base+"/knowledge-base"] = "- **x** — because"
		ids[base+"/claude-md/service-facts"] = "port 3000"
		ids[base+"/claude-md/notes"] = "dev loop"
	}
	for id, body := range ids {
		res := dispatch(context.Background(), store, RecipeInput{
			Action: "record-fragment", Slug: slug,
			FragmentID: id, Fragment: body,
		})
		if !res.OK {
			return fmt.Errorf("record-fragment %s: %s", id, res.Error)
		}
	}
	return nil
}

// TestAssemble_StitchWritesFragmentsToDisk — stitchContent renders every
// canonical surface using Plan.Fragments + templates and writes them to
// the output tree. Root README + env READMEs + per-codebase README land
// at the documented paths.
func TestAssemble_StitchWritesFragmentsToDisk(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	outputRoot := filepath.Join(dir, "run")
	if _, err := store.OpenOrCreate("synth-showcase", outputRoot); err != nil {
		t.Fatalf("OpenOrCreate: %v", err)
	}
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()
	sess.Plan.Slug = "synth-showcase"
	sess.Plan.Framework = "synth"
	sess.Plan.Fragments = map[string]string{
		"root/intro":                              "synth recipe intro",
		"env/0/intro":                             "agent-tier intro",
		"env/1/intro":                             "remote-tier intro",
		"env/2/intro":                             "local-tier intro",
		"env/3/intro":                             "stage-tier intro",
		"env/4/intro":                             "small-prod-tier intro",
		"env/5/intro":                             "ha-prod-tier intro",
		"codebase/api/intro":                      "api intro",
		"codebase/api/integration-guide":          "1. Bind to 0.0.0.0",
		"codebase/api/knowledge-base":             "- **x** — because Y",
		"codebase/api/claude-md/service-facts":    "port 3000, hostname api",
		"codebase/api/claude-md/notes":            "dev loop: ssh api",
		"codebase/app/intro":                      "app intro",
		"codebase/app/integration-guide":          "1. Bind to 0.0.0.0",
		"codebase/app/knowledge-base":             "- **x** — because Y",
		"codebase/app/claude-md/service-facts":    "port 5173",
		"codebase/app/claude-md/notes":            "dev loop: ssh app",
		"codebase/worker/intro":                   "worker intro",
		"codebase/worker/integration-guide":       "1. Bind to 0.0.0.0",
		"codebase/worker/knowledge-base":          "- **x** — because Y",
		"codebase/worker/claude-md/service-facts": "queue group: jobs",
		"codebase/worker/claude-md/notes":         "dev loop: ssh worker",
	}

	// Pre-emit deliverable yamls so the env folders exist.
	for i := range Tiers() {
		if _, err := sess.EmitYAML(ShapeDeliverable, i); err != nil {
			t.Fatalf("emit tier %d: %v", i, err)
		}
	}

	missing, err := stitchContent(sess)
	if err != nil {
		t.Fatalf("stitchContent: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("want no missing fragments, got %v", missing)
	}

	// Root README.
	root, err := os.ReadFile(filepath.Join(outputRoot, "README.md"))
	if err != nil {
		t.Fatalf("read root README: %v", err)
	}
	if !strings.Contains(string(root), "synth recipe intro") {
		t.Errorf("root README missing intro fragment:\n%s", root)
	}

	// Env README for tier 0.
	tier0, _ := TierAt(0)
	envBody, err := os.ReadFile(filepath.Join(outputRoot, tier0.Folder, "README.md"))
	if err != nil {
		t.Fatalf("read env README: %v", err)
	}
	if !strings.Contains(string(envBody), "agent-tier intro") {
		t.Errorf("env README missing intro fragment:\n%s", envBody)
	}
}
