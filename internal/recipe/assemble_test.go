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

// TestAssemble_FragmentBodyWith_JSTemplateLiteral_NotFlagged — fragment
// bodies routinely contain JS template-literal syntax like
// fetch(`${API_URL}/items`). The post-render token scanner must not flag
// these as unbound engine tokens. Workstream A1 (run-9-readiness).
func TestAssemble_FragmentBodyWith_JSTemplateLiteral_NotFlagged(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	plan.Slug = "synth"
	plan.Framework = "nest"
	plan.Fragments = map[string]string{
		"codebase/api/intro":             "api",
		"codebase/api/integration-guide": "1. Configure `zerops.yaml` so the runtime calls `fetch(`${API_URL}/items`)` at boot — avoids hardcoding the origin.",
		"codebase/api/knowledge-base":    "- **404 on x** — because Y",
		"codebase/api/claude-md":         "api",
	}

	_, _, err := AssembleCodebaseREADME(plan, "api")
	if err != nil {
		t.Fatalf("JS template literal in fragment body should not trip token scan: %v", err)
	}
}

// TestAssemble_FragmentBodyWith_BareCurlyToken_NotFlagged — Svelte /
// Handlebars / Go html/template bodies carry `{FILENAME}` or
// `{{ .Name }}` literals. Workstream A1.
func TestAssemble_FragmentBodyWith_BareCurlyToken_NotFlagged(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	plan.Slug = "synth"
	plan.Framework = "svelte"
	plan.Fragments = map[string]string{
		"codebase/api/intro":             "`{FILENAME}` routes to `src/routes/{FILENAME}.svelte` at build time.",
		"codebase/api/integration-guide": "1. Configure `zerops.yaml`. Use `{#if cond}…{/if}` guards as needed.",
		"codebase/api/knowledge-base":    "- **404 on x** — because Y",
		"codebase/api/claude-md":         "api",
	}

	body, _, err := AssembleCodebaseREADME(plan, "api")
	if err != nil {
		t.Fatalf("bare {UPPER} in fragment body should not trip token scan: %v", err)
	}
	if !strings.Contains(body, "{FILENAME}") {
		t.Errorf("fragment body {FILENAME} token dropped from rendered output")
	}
}

// TestStitchContent_FencedBlockTokenAllowed pins Cluster B.2 (R-13-19):
// fragment bodies routinely demonstrate platform-injected env-var
// substitution by writing literal `${HOSTNAME}` inside a fenced code
// block. The pre-processor's engine-token check must skip occurrences
// inside ` ``` `-fenced blocks AND backtick-inline spans so the
// example renders without rejection.
func TestStitchContent_FencedBlockTokenAllowed(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	plan.Slug = "synth"
	plan.Framework = "nest"
	plan.Fragments = map[string]string{
		"codebase/api/intro":             "Worker example below.",
		"codebase/api/integration-guide": "1. Configure `zerops.yaml`.\n\n```\nworker-${HOSTNAME}-${pid}\n```\n",
		"codebase/api/knowledge-base":    "- **404 on x** — because Y, set `${HOSTNAME}` from env.",
		"codebase/api/claude-md":         "api",
	}

	if _, _, err := AssembleCodebaseREADME(plan, "api"); err != nil {
		t.Errorf("fenced-block ${HOSTNAME} literal should be allowed; got: %v", err)
	}
}

// TestStitchContent_UnfencedTokenErrorIncludesFragmentID pins the
// safety side of Cluster B.2: an unbound engine token OUTSIDE a fenced
// block still rejects, and the error names the offending fragment id
// so the author can navigate without spelunking the rendered surface.
func TestStitchContent_UnfencedTokenErrorIncludesFragmentID(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	plan.Slug = "synth"
	plan.Framework = "nest"
	plan.Fragments = map[string]string{
		"codebase/api/intro":             "Bare ${HOSTNAME} reference outside any fence.",
		"codebase/api/integration-guide": "1. Configure `zerops.yaml`.",
		"codebase/api/knowledge-base":    "- **404 on x** — because Y",
		"codebase/api/claude-md":         "api",
	}

	_, _, err := AssembleCodebaseREADME(plan, "api")
	if err == nil {
		t.Fatal("expected unfenced ${HOSTNAME} reference to be flagged")
	}
	if !strings.Contains(err.Error(), "codebase/api/intro") {
		t.Errorf("error should name the offending fragment id; got: %v", err)
	}
}

// TestAssemble_UnreplacedEngineToken_IsFlagged — a real defect: the
// scanner still catches an engine-bound token that the template forgot
// to supply. Simulated by seeding a fragment body with {SLUG} and
// clearing plan.Slug so the binder leaves the literal in place.
// Workstream A1.
func TestAssemble_UnreplacedEngineToken_IsFlagged(t *testing.T) {
	t.Parallel()

	// Inject an engine-bound token into a fragment body; since replaceTokens
	// runs on the template BEFORE substituteFragmentMarkers, the token in
	// the fragment body is not substituted. Still, this test's point is the
	// error-path shape — we assert that when an engine token IS present
	// after render, it gets flagged.
	plan := syntheticShowcasePlan()
	plan.Slug = "synth"
	plan.Framework = "nest"
	plan.Fragments = map[string]string{
		"codebase/api/intro":             "{SLUG} placeholder leaked into fragment body",
		"codebase/api/integration-guide": "1. Configure `zerops.yaml` here.",
		"codebase/api/knowledge-base":    "- **404 on x** — because Y",
		"codebase/api/claude-md":         "api",
	}

	_, _, err := AssembleCodebaseREADME(plan, "api")
	if err == nil {
		t.Fatalf("expected unbound engine token {SLUG} in fragment body to be flagged")
	}
	if !strings.Contains(err.Error(), "{SLUG}") {
		t.Errorf("error should name the unbound token; got: %v", err)
	}
	if !strings.Contains(err.Error(), "codebase/api") {
		t.Errorf("error should name the surface; got: %v", err)
	}
}

// TestAssemble_ErrorNamesSurface — the wrapped error must carry the
// surface identifier so the author can navigate without spelunking.
// Workstream A1.
func TestAssemble_ErrorNamesSurface(t *testing.T) {
	t.Parallel()

	// Env surface error path: plan.Framework contains `{FRAMEWORK}` (!)
	// would normally self-resolve, so we test by poisoning a fragment id.
	plan := syntheticShowcasePlan()
	plan.Slug = "synth"
	plan.Framework = "nest"
	plan.Fragments = map[string]string{
		"env/0/intro": "{FRAMEWORK} literal still here",
	}

	_, _, err := AssembleEnvREADME(plan, 0)
	if err == nil {
		t.Fatalf("expected unbound engine token in fragment body to be flagged")
	}
	if !strings.Contains(err.Error(), "env/0") {
		t.Errorf("error should name the env surface; got: %v", err)
	}
}

// TestAssemble_FragmentBody_CodeTokens_E2E — run-9-readiness §R.
// Fixture covers every code-block token shape a fragment body is
// likely to carry. Landing this pins the A1 invariant: fragment bodies
// with legitimate `${UPPER}` or `{UPPER}` or `{{ ... }}` don't trip
// the post-render token scanner.
func TestAssemble_FragmentBody_CodeTokens_E2E(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	plan.Slug = "synth-showcase"
	plan.Framework = "synth"
	// Every shape of fragment-body code token that a real run has
	// produced or is likely to produce.
	codeBodies := strings.Join([]string{
		"JS template literal: ``fetch(`${API_URL}/items`)``",
		"Handlebars curly: `{FILENAME}` maps to `src/{FILENAME}.svelte`",
		"Double-brace: `{{template}}` is Vue syntax",
		"Svelte slot: `<slot />`",
		"Svelte conditional: `{#if cond}...{/if}`",
		"Go html/template: `{{ .FieldName }}`",
		"Backtick code: `` `${PLACEHOLDER}` ``",
	}, "\n\n")
	plan.Fragments = map[string]string{
		"root/intro":  "root intro with `${ROOT_TOKEN}` literal",
		"env/0/intro": "env-0 intro with `{TIER_LOCAL}` literal",
		"env/1/intro": "env-1 intro with `{{ .Tier }}` literal",
		"env/2/intro": "env-2 intro",
		"env/3/intro": "env-3 intro",
		"env/4/intro": "env-4 intro",
		"env/5/intro": "env-5 intro",
	}
	// Every codebase fragment id takes a body sample from codeBodies so
	// the round-trip exercises every surface.
	for _, cb := range plan.Codebases {
		base := "codebase/" + cb.Hostname
		plan.Fragments[base+"/intro"] = codeBodies
		plan.Fragments[base+"/integration-guide"] = "1. Configure `zerops.yaml`.\n\n" + codeBodies
		plan.Fragments[base+"/knowledge-base"] = "- **404 on x** — because Y\n\n" + codeBodies
		plan.Fragments[base+"/claude-md"] = codeBodies
	}

	// Root README.
	body, missing, err := AssembleRootREADME(plan)
	if err != nil {
		t.Fatalf("root: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("root missing: %v", missing)
	}
	if !strings.Contains(body, "${ROOT_TOKEN}") {
		t.Errorf("root body dropped fragment code token")
	}

	// Env READMEs for every tier.
	for i := range Tiers() {
		body, _, err := AssembleEnvREADME(plan, i)
		if err != nil {
			t.Errorf("env/%d: %v", i, err)
		}
		if body == "" {
			t.Errorf("env/%d: empty body", i)
		}
	}

	// Per-codebase README + CLAUDE.md.
	for _, cb := range plan.Codebases {
		rbody, _, err := AssembleCodebaseREADME(plan, cb.Hostname)
		if err != nil {
			t.Errorf("codebase/%s README: %v", cb.Hostname, err)
		}
		for _, sample := range []string{
			"${API_URL}", "{FILENAME}", "{{template}}",
			"<slot />", "{#if cond}", "{{ .FieldName }}", "${PLACEHOLDER}",
		} {
			if !strings.Contains(rbody, sample) {
				t.Errorf("codebase/%s README: %q missing from rendered body",
					cb.Hostname, sample)
			}
		}
		cbody, _, err := AssembleCodebaseClaudeMD(plan, cb.Hostname)
		if err != nil {
			t.Errorf("codebase/%s CLAUDE: %v", cb.Hostname, err)
		}
		if !strings.Contains(cbody, "${API_URL}") {
			t.Errorf("codebase/%s CLAUDE.md: code token dropped", cb.Hostname)
		}
	}
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
		FragmentID:     "codebase/api/integration-guide",
		Fragment:       "scaffold body",
		Classification: "platform-invariant",
	})
	if !res.OK {
		t.Fatalf("record scaffold IG: %+v", res)
	}
	res = dispatch(t.Context(), store, RecipeInput{
		Action: "record-fragment", Slug: "synth-showcase",
		FragmentID:     "codebase/api/integration-guide",
		Fragment:       "feature body",
		Classification: "platform-invariant",
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

// TestReadCodebaseYAMLForHost_MissingYaml_ReturnsError — run-11 gap M-2.
// Soft-fail-to-empty-string was the reason injectIGItem1 silently no-op'd
// in run 10. With non-empty SourceRoot, missing yaml is a hard error.
func TestReadCodebaseYAMLForHost_MissingYaml_ReturnsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sourceRoot := filepath.Join(dir, "apidev")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "api", SourceRoot: sourceRoot}},
	}
	_, err := readCodebaseYAMLForHost(plan, "api")
	if err == nil {
		t.Fatal("expected error when zerops.yaml is missing under non-empty SourceRoot")
	}
	if !strings.Contains(err.Error(), "zerops.yaml") {
		t.Errorf("error should name the missing file, got: %v", err)
	}
}

// TestReadCodebaseYAMLForHost_PresentYaml_ReturnsBody — happy path.
func TestReadCodebaseYAMLForHost_PresentYaml_ReturnsBody(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sourceRoot := filepath.Join(dir, "apidev")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	want := "zerops:\n  - setup: prod\n"
	if err := os.WriteFile(filepath.Join(sourceRoot, "zerops.yaml"), []byte(want), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	plan := &Plan{
		Codebases: []Codebase{{Hostname: "api", SourceRoot: sourceRoot}},
	}
	got, err := readCodebaseYAMLForHost(plan, "api")
	if err != nil {
		t.Fatalf("readCodebaseYAMLForHost: %v", err)
	}
	if got != want {
		t.Errorf("body mismatch: got %q, want %q", got, want)
	}
}

// TestReadCodebaseYAMLForHost_EmptySourceRoot_ReturnsEmpty — pre-scaffold
// path: SourceRoot hasn't been populated yet, no error fires (early-phase
// renders may legitimately call this before scaffold authors the yaml).
func TestReadCodebaseYAMLForHost_EmptySourceRoot_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	plan := &Plan{
		Codebases: []Codebase{{Hostname: "api", SourceRoot: ""}},
	}
	got, err := readCodebaseYAMLForHost(plan, "api")
	if err != nil {
		t.Errorf("expected no error for empty SourceRoot, got: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty body for empty SourceRoot, got %q", got)
	}
}

// TestAssemble_DeliverableSplit — stitchContent writes into two shapes.
// Recipes-repo shape at <outputRoot>/: root README + per-tier README +
// per-tier import.yaml. Apps-repo shape at <cb.SourceRoot>/: README +
// CLAUDE.md + zerops.yaml (the scaffold-authored yaml is left in place).
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
	// Point each codebase at a staged workspace — stitch's codebase-scoped
	// writes target this tree directly.
	for i, cb := range sess.Plan.Codebases {
		wsRoot := filepath.Join(dir, "workspace", cb.Hostname+"dev")
		if err := os.MkdirAll(wsRoot, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", wsRoot, err)
		}
		if err := os.WriteFile(filepath.Join(wsRoot, "zerops.yaml"),
			[]byte("# "+cb.Hostname+" scaffold yaml\nzerops: []\n"), 0o600); err != nil {
			t.Fatalf("write workspace yaml: %v", err)
		}
		sess.Plan.Codebases[i].SourceRoot = wsRoot
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

	// Apps-repo shape — per codebase at SourceRoot: README + CLAUDE.md +
	// zerops.yaml (scaffold-authored).
	for _, cb := range sess.Plan.Codebases {
		for _, want := range []string{"README.md", "CLAUDE.md", "zerops.yaml"} {
			abs := filepath.Join(cb.SourceRoot, want)
			if _, err := os.Stat(abs); err != nil {
				t.Errorf("apps-shape path missing %s/%s: %v",
					cb.SourceRoot, want, err)
			}
		}
	}
}

// fillAllFragments populates every fragment id the synthetic plan
// declares so stitchContent runs without surfacing missing ids. Shared
// between tests that need a clean assemble.
//
// Run-19 prep: KB/IG fragmentIDs require Classification at record-time;
// we set "platform-invariant" by default since the placeholder bodies
// describe a generic platform trap.
func fillAllFragments(store *Store, plan *Plan) error {
	type frag struct {
		id, body, class string
	}
	tiers := Tiers()
	frags := make([]frag, 0, 1+len(tiers)+4*len(plan.Codebases))
	frags = append(frags, frag{id: "root/intro", body: "intro"})
	for i := range tiers {
		frags = append(frags, frag{id: fmt.Sprintf("env/%d/intro", i), body: fmt.Sprintf("tier %d", i)})
	}
	for _, cb := range plan.Codebases {
		base := "codebase/" + cb.Hostname
		frags = append(frags,
			frag{id: base + "/intro", body: "cb intro"},
			frag{id: base + "/integration-guide", body: "1. IG", class: "platform-invariant"},
			frag{id: base + "/knowledge-base", body: "- **404 on x** — because", class: "platform-invariant"},
			// Run-16 §6.7a — single-slot /init-shape body. Must clear
			// the validateCodebaseCLAUDE 200-byte minimum and the 2-4
			// `## ` section slot-shape refusal.
			frag{id: base + "/claude-md", body: "# " + cb.Hostname + "\n\nFramework scaffold for the showcase.\n\n## Build & run\n\n- npm install\n- npm test\n\n## Architecture\n\n- src/main.ts — bootstrap\n- src/items/ — items domain\n"},
		)
	}
	for _, f := range frags {
		res := dispatch(context.Background(), store, RecipeInput{
			Action: "record-fragment", Slug: plan.Slug,
			FragmentID: f.id, Fragment: f.body, Classification: f.class,
		})
		if !res.OK {
			return fmt.Errorf("record-fragment %s: %s", f.id, res.Error)
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
	// Stage scaffold-authored yamls so A2's hard-fail doesn't abort stitch.
	for i, cb := range sess.Plan.Codebases {
		wsRoot := filepath.Join(dir, "workspace", cb.Hostname+"dev")
		if err := os.MkdirAll(wsRoot, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(wsRoot, "zerops.yaml"),
			[]byte("# "+cb.Hostname+" — because test\nzerops: []\n"), 0o600); err != nil {
			t.Fatalf("write yaml: %v", err)
		}
		sess.Plan.Codebases[i].SourceRoot = wsRoot
	}
	sess.Plan.Fragments = map[string]string{
		"root/intro":                        "synth recipe intro",
		"env/0/intro":                       "agent-tier intro",
		"env/1/intro":                       "remote-tier intro",
		"env/2/intro":                       "local-tier intro",
		"env/3/intro":                       "stage-tier intro",
		"env/4/intro":                       "small-prod-tier intro",
		"env/5/intro":                       "ha-prod-tier intro",
		"codebase/api/intro":                "api intro",
		"codebase/api/integration-guide":    "1. Bind to 0.0.0.0",
		"codebase/api/knowledge-base":       "- **404 on x** — because Y",
		"codebase/api/claude-md":            "port 3000, hostname api",
		"codebase/app/intro":                "app intro",
		"codebase/app/integration-guide":    "1. Bind to 0.0.0.0",
		"codebase/app/knowledge-base":       "- **404 on x** — because Y",
		"codebase/app/claude-md":            "port 5173",
		"codebase/worker/intro":             "worker intro",
		"codebase/worker/integration-guide": "1. Bind to 0.0.0.0",
		"codebase/worker/knowledge-base":    "- **404 on x** — because Y",
		"codebase/worker/claude-md":         "queue group: jobs",
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

// TestAssembleCodebaseClaudeMD_NoTemplateInjectedDevLoop — run-16
// rewrite of run-13 §Q. The template no longer carries any hardcoded
// content (single extract marker only); the dedicated claudemd-author
// sub-agent owns the entire CLAUDE.md body. This test verifies the
// template stays empty of authoring-tool voice and the agent's body
// renders verbatim.
func TestAssembleCodebaseClaudeMD_NoTemplateInjectedDevLoop(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	agentBody := "# api\n\nNestJS REST API.\n\n## Build & run\n\n- npm run start:dev\n\n## Architecture\n\n- src/main.ts"
	plan.Fragments = map[string]string{
		"codebase/api/claude-md": agentBody,
	}
	body, _, err := AssembleCodebaseClaudeMD(plan, "api")
	if err != nil {
		t.Fatalf("AssembleCodebaseClaudeMD: %v", err)
	}
	for _, s := range []string{
		"zcli push",
		"zcli vpn",
		"Iterate with",
		"## Zerops dev loop",
	} {
		if strings.Contains(body, s) {
			t.Errorf("template-injected forbidden string %q in CLAUDE.md output:\n%s", s, body)
		}
	}
	if !strings.Contains(body, "npm run start:dev") {
		t.Errorf("agent-authored dev-loop bullet missing from rendered CLAUDE.md:\n%s", body)
	}
}
