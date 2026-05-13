package recipe

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// readWriterContentSurfaceContracts reads the writer's
// `content-surface-contracts.md` substrate from the local checkout.
// The file lives under `internal/content/workflows/recipe/briefs/writer/`
// (the workflow-authoring atom tree); these tests run from the
// `internal/recipe` package, so the relative path traverses two
// directories up. Mirrors the resolution shape used by
// `internal/workflow/recipe_writer_kb_floor_g8_test.go`.
func readWriterContentSurfaceContracts() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	atomPath := filepath.Join(wd, "..", "content", "workflows", "recipe", "briefs", "writer", "content-surface-contracts.md")
	body, err := os.ReadFile(atomPath)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// Pillar A — content tests pinning every load-bearing local-substrate
// paragraph to the rendered brief.
//
// Run-44 ground truth (`docs/zcprecipator3/runs/44/SESSION_LOGS/subagents/
// agent-a900b4fac6504a81b.jsonl` for refinement-2 + `agent-a4e4ea3726f0da5cc.
// jsonl` for refinement-1) showed two render-pipeline silent-drop
// failures: derived_rules.md's KB-DEFENSIVE-FLOOR (line 62) rendered as
// blank in the refinement-1 brief, and phase_entry.md's findings schema
// classification field (line 34) was missing from the refinement-2 brief.
// Local substrate carried the text; the rendered brief shown to the
// sub-agent didn't. Root cause was stale-binary (go:embed snapshot
// pre-dating the substrate edits); the composers themselves are pure.
//
// These tests close the silent-drop failure mode: every named rule in
// derived_rules.md and every named defect class / single-question test
// in audit_checklist.md must appear verbatim in the rendered brief.
// Hard-fail on miss — no advisory/tolerance softening. The render-
// pipeline silent-drop is the failure mode this gate exists to close.

// derivedRulesNamedAnchors lists every load-bearing rule prefix in
// derived_rules.md. A rule prefix appears at the start of a bullet line
// like `- **V1 —` / `- **KB-DEFENSIVE-FLOOR —` / `- **F-XSURF-REF —`.
// Drop a name here when retiring a rule; add a name here when authoring
// a new one — the matching paragraph then becomes part of the rendered-
// brief contract.
var derivedRulesNamedAnchors = []string{
	// Universal voice rules.
	"V1", "V2", "V3", "V4", "V5", "V6",
	// Root README.
	"R1", "R2", "R3", "R4", "R5", "R6",
	// Tier README.
	"T1", "T2", "T3", "T4",
	// Tier import.yaml.
	"TY1", "TY2", "TY3", "TY4", "TY5",
	// Apps-repo IG.
	"IG1", "IG2", "IG3", "IG4", "IG5", "IG6",
	// Apps-repo KB.
	"KB1", "KB3", "KB4", "KB5", "KB6",
	"KB-DEFENSIVE-FLOOR",
	// Apps-repo zerops.yaml.
	"Y1", "Y2", "Y3", "Y4", "Y5", "Y6", "Y7", "Y8", "Y9", "Y10",
	"Y11", "Y12", "Y13", "Y15",
	// Gap-fillers.
	"TIER-1", "PREPROC-1",
	// Factuality guards.
	"F-SUBDOMAIN", "F-BOXDRAWING", "F-XSURF-REF",
	"F-FRIENDLY-AUTH", "F-EXECONCE-SEMANTICS",
}

// TestRefinementBrief_DerivedRulesNamedAnchorsRender — every named rule
// in derived_rules.md must surface verbatim in the rendered refinement-1
// brief. Closes the run-44 G7 silent-drop failure mode where a local
// substrate edit (KB-DEFENSIVE-FLOOR at line 62) didn't reach the
// rendered brief because the running binary was compiled pre-edit.
func TestRefinementBrief_DerivedRulesNamedAnchorsRender(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "pillar-a-pin",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinementBrief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	atom, err := readAtom("briefs/refinement/derived_rules.md")
	if err != nil {
		t.Fatalf("read derived_rules.md: %v", err)
	}

	for _, name := range derivedRulesNamedAnchors {
		// Pin two shapes: the bullet header form (`- **<NAME> —`) and a
		// looser per-rule substring search. The bullet-header form is
		// the canonical render shape; the looser form catches authoring
		// edits that retype the bullet header.
		anchor := "- **" + name + " —"
		if !strings.Contains(atom, anchor) {
			t.Fatalf("derivedRulesNamedAnchors lists %q but derived_rules.md does not carry bullet header %q; update derivedRulesNamedAnchors or restore the rule", name, anchor)
		}
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("rendered refinement-1 brief missing named rule anchor %q", anchor)
		}
	}
}

// TestRefinementBrief_DerivedRulesBulletCountStable — the number of
// load-bearing `- **<NAME> —` bullets in derived_rules.md must equal
// len(derivedRulesNamedAnchors). New rules force a deliberate update of
// the anchor list (which then forces the test above to pin the new
// rendered text). Closes the "rule added to substrate but never pinned"
// authoring gap.
func TestRefinementBrief_DerivedRulesBulletCountStable(t *testing.T) {
	t.Parallel()
	atom, err := readAtom("briefs/refinement/derived_rules.md")
	if err != nil {
		t.Fatalf("read derived_rules.md: %v", err)
	}
	// Count headers of the form `- **<TOKEN> —` where TOKEN matches the
	// rule-id grammar (uppercase + digits + hyphens, ≥2 chars).
	re := regexp.MustCompile(`(?m)^- \*\*[A-Z][A-Z0-9-]*[0-9A-Z] —`)
	matches := re.FindAllString(atom, -1)
	if len(matches) != len(derivedRulesNamedAnchors) {
		t.Fatalf("derived_rules.md has %d named-rule bullets but derivedRulesNamedAnchors lists %d; reconcile so every shipped rule is pinned in the rendered brief.\nfound bullets: %v", len(matches), len(derivedRulesNamedAnchors), matches)
	}
}

// auditChecklistRequiredSubstrings — load-bearing anchors in the
// rewritten (Pillar B) audit_checklist.md that MUST render in the
// refinement-2 brief body. Includes the per-surface single-question
// tests (verbatim from `content-surface-contracts.md`), the surface
// identifiers, the failure-mode emission shape, and the cross-surface
// uniqueness pass anchor.
var auditChecklistRequiredSubstrings = []string{
	// Surface IDs from the audit's seven-surface taxonomy.
	"S3 — Tier `import.yaml`",
	"S4 — Per-codebase Integration Guide",
	"S5 — Per-codebase Knowledge Base",
	"S6 — Per-codebase CLAUDE.md",
	"S7 — Per-codebase `zerops.yaml`",
	// Per-surface single-question tests — verbatim from
	// content-surface-contracts.md. The audit's test for each surface
	// MUST match the writer's test character-for-character; drift
	// between writer and auditor would re-open the Pillar B gap.
	"Would a porter who is NOT using this recipe as a template, but bringing their own code, need to copy THIS exact content into their own app?",
	"Would a developer who read the Zerops docs AND the relevant framework docs STILL be surprised by this?",
	"Is this useful for operating THIS repo specifically",
	"Does each service block explain a decision",
	"Does each comment explain a trade-off the reader couldn't infer from the field name?",
	// Failure-mode emission shape (DROP / MOVE-TO / REWRITE).
	"DROP",
	"MOVE-TO",
	"REWRITE",
	// Cross-surface uniqueness pass anchor.
	"cross-surface uniqueness",
	// Self-referential decoration prohibition.
	"Self-referential decoration",
}

// TestRefinement2Brief_AuditChecklistRequiredSubstringsRender — every
// load-bearing anchor in audit_checklist.md must surface in the rendered
// refinement-2 brief. Pillar A safety net for the Pillar B audit walk:
// any future substrate edit that omits one of these anchors fails the
// test before the binary ships.
func TestRefinement2Brief_AuditChecklistRequiredSubstringsRender(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "pillar-a-pin",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
			{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22", IsWorker: true},
		},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	for _, want := range auditChecklistRequiredSubstrings {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("rendered refinement-2 brief missing audit anchor %q", want)
		}
	}
}

// TestRefinement2Brief_PhaseEntryEmissionShapeRenders — the rewritten
// phase_entry.md emission contract anchors (slim findings shape) must
// render in the brief body. The slim shape is the engine-side-enrichment
// input contract; Pillar C's enrich-findings boundary depends on the
// sub-agent emitting THIS shape, not the legacy 13-defect-class shape.
func TestRefinement2Brief_PhaseEntryEmissionShapeRenders(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "pillar-a-pin",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	for _, want := range []string{
		// Slim findings emit shape — the engine enriches at the
		// `enrich-findings` boundary. fragmentId / classification /
		// suggestedReplacement are engine-filled and NOT in the slim
		// emit shape; pin only the agent-authored fields.
		`"surface":`,
		`"scope":`,
		`"surfaceTestFailureMode":`,
		`"itemReference":`,
		`"rationale":`,
		// Run-45 Issue 3 — factRef is the agent-authored
		// disambiguation slot that retires topic-substring matching at
		// the engine's classification lookup.
		`"factRef":`,
		"<topic>@<recordedAt>",
		// Cross-surface uniqueness pass appears as an explicit step.
		"cross-surface uniqueness",
		// Per-surface walk discipline.
		"For each surface",
		"for each item",
		// Engine-enrichment boundary is named explicitly.
		"enrich-findings",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("rendered refinement-2 brief missing phase-entry anchor %q", want)
		}
	}
}

// TestRefinement2_MainAgentTriageGuidance_NamesEnrichFindings — the
// main-agent triage notice (surfaced on `build-subagent-prompt
// briefKind=refinement2` response) MUST name `enrich-findings` so the
// main agent knows to call the boundary BEFORE running the per-finding
// triage. Without this, the run-40 → 44 cycle re-opens (sub-agent
// emits slim, main agent treats it as already-enriched, every ACT
// hits `classification is required` rejection).
func TestRefinement2_MainAgentTriageGuidance_NamesEnrichFindings(t *testing.T) {
	t.Parallel()
	guidance := refinement2MainAgentTriageGuidance(BriefRefinement2)
	for _, want := range []string{
		"enrich-findings",
		"fragmentId",
		"classification",
		"suggestedReplacement",
		"verbatim",
	} {
		if !strings.Contains(guidance, want) {
			t.Errorf("refinement-2 main-agent triage guidance missing anchor %q", want)
		}
	}
	// Notice is empty for non-refinement2 kinds.
	if got := refinement2MainAgentTriageGuidance(BriefRefinement); got != "" {
		t.Errorf("non-refinement2 guidance should be empty, got %q", got)
	}
}

// TestRefinement2Brief_CitationMapRenders — the citation map block must
// render in the brief body. Engine-side `enrich-findings` looks up
// suggestedReplacement from the same map; brief carries it so the
// sub-agent can identify the topic family without external state.
func TestRefinement2Brief_CitationMapRenders(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "pillar-a-pin",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	// The citation map's load-bearing topic rows.
	for _, want := range []string{
		"rolling-deploys",
		"init-commands",
		"object-storage",
		"env-var-model",
		"http-support",
		"deploy-files",
		"readiness-health-checks",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("rendered refinement-2 brief missing citation-map topic %q", want)
		}
	}
}

// TestRefinement2Brief_CitationMapScopingCoversIGAndKB — Issue 1 fix.
// Run-44 emitted 0 CODEBASE_IG findings against 10 cross-surface
// findings because the citation-map prose scoped "MUST cite" to KB
// bullets only. The audit_checklist `Citation requirement (S4 + S5)`
// section AND the writer `content-surface-contracts.md §S1/§S2` both
// require citations on IG items too. This pin hard-fails if the
// citation-map prose ever drops back to a single-surface scope.
//
// The asserted shape is surface-agnostic: the block MUST reference
// BOTH S5 KB and S4 IG (either by surface ID or by surface name) in
// the scoping paragraph, AND MUST NOT carry a "KB bullet" framing
// that excludes IG items from the requirement.
func TestRefinement2Brief_CitationMapScopingCoversIGAndKB(t *testing.T) {
	t.Parallel()
	block := citationMapBlock()

	// The scoping sentence must name BOTH surfaces. Pin by both ID and
	// human-readable name so the substrate can phrase it either way
	// (e.g. "S4 IG items + S5 KB bullets" or "any item on the
	// Integration Guide or Knowledge Base").
	mustContain := []string{
		// Both surface IDs present.
		"S4",
		"S5",
		// IG keyword present.
		"IG",
		// KB keyword present.
		"KB",
	}
	for _, want := range mustContain {
		if !strings.Contains(block, want) {
			t.Errorf("citationMapBlock() missing surface anchor %q — citation requirement must scope to BOTH S4 (IG) AND S5 (KB)", want)
		}
	}

	// Forbid the run-44-shipped KB-only scoping sentence. Any wording
	// that scopes the requirement to ONLY "a KB bullet" without
	// naming IG re-opens G2.
	forbidden := []string{
		"When a KB bullet covers one of these topics, the body MUST cite",
	}
	for _, no := range forbidden {
		if strings.Contains(block, no) {
			t.Errorf("citationMapBlock() carries run-44 KB-only scoping prose %q; rewrite to scope to BOTH KB AND IG", no)
		}
	}
}

// TestRefinement2Brief_CitationMapScopingRendersInBrief — Pillar A.
// Same surface-coverage assertion against the full rendered brief
// body, so a regression in either citationMapBlock() OR the
// BuildRefinement2Brief composer surfaces here.
func TestRefinement2Brief_CitationMapScopingRendersInBrief(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "pillar-a-pin",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	// Forbid the run-44-shipped KB-only scoping sentence reaching the
	// brief body.
	if strings.Contains(brief.Body, "When a KB bullet covers one of these topics, the body MUST cite") {
		t.Errorf("rendered refinement-2 brief carries run-44 KB-only scoping prose; rewrite citationMapBlock() to cover BOTH KB AND IG")
	}
	// Required IG-aware framing — name both surfaces explicitly so a
	// future rewrite can't accidentally narrow the scope.
	for _, want := range []string{"S4", "S5", "IG", "KB"} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("rendered refinement-2 brief missing citation-scope anchor %q", want)
		}
	}
}

// TestRefinement2Brief_GoRenderedProseInvariants — Pillar A pin coverage
// for EVERY Go-rendered prose block under briefs_refinement2.go.
// Markdown substrate files have anchor pins; Go-rendered prose did not
// until Issue 4 added these. Each prose function below must carry the
// design's load-bearing invariants verbatim.
func TestRefinement2Brief_GoRenderedProseInvariants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		body   string
		anchor []string
	}{
		{
			name: "citationMapBlock — surface-agnostic scoping + form-(a)/(b)/(c) shape",
			body: citationMapBlock(),
			anchor: []string{
				// Surface-agnostic citation scoping (Issue 1 fix).
				"S4",
				"S5",
				"IG",
				"KB",
				// Acceptable forms.
				"(a)",
				"(b)",
				"(c)",
				// Host+path match rule retains.
				"host+path match",
				// Closed-set escape clause references both surfaces.
				"KB bullet or IG item",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, want := range tc.anchor {
				if !strings.Contains(tc.body, want) {
					t.Errorf("%s: rendered prose missing invariant anchor %q", tc.name, want)
				}
			}
		})
	}
}

// refinement2InlineProseAnchors — Issue 4 (Run-45 G2 follow-up). Pillar
// A's "every load-bearing paragraph pinned, silent-drop impossible"
// promise applies to ALL prose surfaces, Go-rendered or markdown-
// substrate-rendered. citationMapBlock() is a standalone prose function
// covered above; the prose composed INLINE in BuildRefinement2Brief (the
// "Stitched output to audit" header + the **Root** / **Tier environments**
// / **Codebases** / **Per-codebase dependency manifests** subsection
// labels + the orient-the-reader sentence) is Go-rendered too but was
// not pinned until this list.
//
// Each entry is a verbatim substring of agent-visible prose authored at
// briefs_refinement2.go:86-109. A composer edit that removes/modifies
// any of these paragraphs forces a deliberate pin update — silent drop
// is impossible.
var refinement2InlineProseAnchors = []string{
	// Stitched-output pointer block — header + orient sentence.
	"## Stitched output to audit",
	"Read each path end-to-end before running cross-surface checks. Don't `Grep`; cross-surface defects need full surfaces held in working memory.",
	// Subsection labels — these route the sub-agent's working-memory
	// load across the four surface clusters. The S2+S3 / S4+S5+S6+S7
	// numbering inside the labels is the audit's seven-surface taxonomy;
	// drift here would mis-signal which surfaces feed which audit step.
	"**Root**",
	"**Tier environments (S2 + S3)**",
	"**Codebases (S4 + S5 + S6 + S7)**",
	"**Per-codebase dependency manifests (for aspirational-as-current)**",
	// Bullet-content suffixes — Fprintf-rendered prose tails that name
	// WHAT the sub-agent reads at each path. The prefix path varies with
	// runDir/Tier.Folder/Codebase.Hostname; the suffix prose is the
	// load-bearing instruction text and would silently drop if removed.
	"` — root README (S1)",
	"` — fragment store (canonical content for every fragmentId)",
	"` — recorded facts (audit citation source)",
	"/README.md` + `import.yaml`",
	"` — IG (S4), KB (S5), CLAUDE.md (S6), yaml comments (S7)",
	"{package.json, composer.json, pyproject.toml, requirements.txt}` — read whichever exists",
}

// TestRefinement2Brief_InlineProseAnchorsRender — Pillar A pin sweep for
// the inline Go-rendered prose in BuildRefinement2Brief. Adds the
// stitched-output pointer block to the pin coverage so a composer edit
// that drops or alters a prose paragraph fails before ship.
func TestRefinement2Brief_InlineProseAnchorsRender(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "inline-prose-pin",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
		},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	for _, want := range refinement2InlineProseAnchors {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("rendered refinement-2 brief missing inline-prose anchor %q (silent-drop guard)", want)
		}
	}
}

// refinementInlineProseAnchors — Issue 4 (Run-45 G2 follow-up) parallel
// sweep over BuildRefinementBrief (refinement-1 composer). Same
// silent-drop-guard pattern; pins every load-bearing paragraph composed
// inline at briefs_refinement.go:98-140.
var refinementInlineProseAnchors = []string{
	// Reference-atoms fetch block — header + dispatch instruction +
	// the canonical fetch command (load-bearing — the sub-agent reads
	// this URI shape to dispatch zerops_knowledge calls).
	"## Reference atoms — fetch on demand",
	"When investigating a suspect, fetch the matching reference atom via:",
	"zerops_knowledge uri=zerops://themes/refinement-references/<name>",
	// Reference catalog bullets — EVERY URI in refinementReferenceCatalog
	// (briefs_refinement.go::var). Full enumeration: partial pinning
	// would let entries silently drop while the test still passes.
	// If the catalog adds/renames/removes an entry, this list updates
	// deliberately alongside it.
	"`zerops://themes/refinement-references/kb_shapes`",
	"`zerops://themes/refinement-references/ig_one_mechanism`",
	"`zerops://themes/refinement-references/voice_patterns`",
	"`zerops://themes/refinement-references/yaml_comments`",
	"`zerops://themes/refinement-references/citations`",
	"`zerops://themes/refinement-references/trade_offs`",
	"`zerops://themes/refinement-references/refinement_thresholds`",
	// Reference catalog DESCRIPTIONS — load-bearing because they tell
	// the sub-agent which atom answers which symptom class. Silent
	// drift here mis-routes zerops_knowledge fetches.
	"KB stem symptom-first heuristic + worked-example anchors at 7.0/8.5/9.0.",
	"IG H3 one-mechanism-per-heading rule + fusion-split decision examples.",
	"Friendly-authority phrasing patterns for zerops.yaml + tier import.yaml comments.",
	"Yaml-comment shape: mechanism-first, no field-restatement preamble.",
	"Cite-by-name pattern + application-specific corollary phrasing.",
	"Two-sided trade-off bodies: name the chosen path + the rejected alternative.",
	"ACT vs HOLD decision rules, the 8 refinement actions, per-fragment edit cap.",
	// Stitched-output pointer block — header + orient sentence.
	"## Stitched output to refine",
	"Read each path; refine fragments where you can cite the violated rule",
	// Subsection labels — refinement-1's per-surface load routing.
	"**Root**",
	"**Tier environments**",
	"**Codebases**",
	"**Parent recipe (read-only)**",
	// Bullet-content suffixes — Fprintf-rendered prose tails.
	"` — root README",
	"/README.md` + `import.yaml`",
	"/README.md` + `zerops.yaml` + `CLAUDE.md`",
	"published surfaces (under the parent's source root). Refinement HOLDS on any fragment whose body would re-author parent material.",
}

// TestRefinementBrief_InlineProseAnchorsRender — Pillar A pin sweep for
// the inline Go-rendered prose in BuildRefinementBrief (refinement-1).
// Parent-recipe block requires a non-nil parent with SourceRoot set so
// that branch renders; without it the parent paragraph stays unrendered
// (deliberate composer behavior, pinned by other tests).
func TestRefinementBrief_InlineProseAnchorsRender(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "inline-prose-pin",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
		},
	}
	parent := &ParentRecipe{Slug: "fake-parent", SourceRoot: "/tmp/parent"}
	brief, err := BuildRefinementBrief(plan, parent, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	for _, want := range refinementInlineProseAnchors {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("rendered refinement-1 brief missing inline-prose anchor %q (silent-drop guard)", want)
		}
	}
}

// TestRefinement2Brief_WriterAuditorSingleQuestionTestParity — Issue 4
// writer/auditor cross-pin. Reads the writer's
// `content-surface-contracts.md` substrate (the Pillar B authoring
// contract), extracts each surface's single-question editorial test,
// and asserts the SAME string appears in the rendered refinement-2
// brief. Drift between writer and auditor re-opens the pre-run-45
// gap where authors classified one way and auditors curated patterns
// from another.
//
// Scope: writer covers S1-S4 (its own numbering — IG=S1, KB=S2,
// CLAUDE.md=S3, env-yaml=S4). Audit covers S3-S7 in its numbering
// (env-yaml=S3, IG=S4, KB=S5, CLAUDE.md=S6, codebase-yaml=S7). The
// SHARED single-question tests are the four the writer authors and
// the audit cross-walks; the audit's S7 test (codebase yaml comments)
// has no writer-side counterpart by design (the writer's contract
// scope intentionally stops at the four published-content surfaces).
func TestRefinement2Brief_WriterAuditorSingleQuestionTestParity(t *testing.T) {
	t.Parallel()

	// Read the writer's surface contracts from the embedded content
	// tree. This is the authoritative writer substrate.
	writerContracts, err := readWriterContentSurfaceContracts()
	if err != nil {
		t.Fatalf("read writer content-surface-contracts.md: %v", err)
	}

	// The four single-question tests the writer authors. The audit
	// renumbers (S1→S4, S2→S5, S3→S6, S4→S3 in audit numbering); the
	// test STRING itself is identical character-for-character on both
	// sides.
	sharedTests := []struct {
		name string
		test string
	}{
		{
			name: "IG / Surface 1 (writer) ↔ S4 (auditor)",
			test: "Would a porter who is NOT using this recipe as a template, but bringing their own code, need to copy THIS exact content into their own app?",
		},
		{
			name: "KB / Surface 2 (writer) ↔ S5 (auditor)",
			test: "Would a developer who read the Zerops docs AND the relevant framework docs STILL be surprised by this?",
		},
		{
			name: "CLAUDE.md / Surface 3 (writer) ↔ S6 (auditor)",
			test: "Is this useful for operating THIS repo specifically — not for deploying it, not for porting it to other code?",
		},
		{
			name: "env yaml / Surface 4 (writer) ↔ S3 (auditor)",
			test: "Does each service block explain a decision (why this service exists at this tier, why this scale, why this mode), rather than narrating what the field does?",
		},
	}

	plan := &Plan{
		Slug:      "writer-auditor-parity",
		Codebases: []Codebase{{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"}},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}

	for _, tc := range sharedTests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(writerContracts, tc.test) {
				t.Errorf("writer content-surface-contracts.md missing single-question test %q — writer contract drifted; reconcile", tc.test)
			}
			if !strings.Contains(brief.Body, tc.test) {
				t.Errorf("refinement-2 audit brief missing single-question test %q — auditor/writer drift; reconcile audit_checklist.md against the writer contract", tc.test)
			}
		})
	}

	// Aside (documented in test code per the prompt): the audit also
	// covers S7 (codebase yaml comments — "Does each comment explain
	// a trade-off the reader couldn't infer from the field name?")
	// which has no writer-side counterpart. Writer's scope stops at
	// its four published-content surfaces; audit extends to codebase
	// yaml comments deliberately. This is NOT drift; do NOT add S7
	// to the writer contract to satisfy this test.
	auditOnlyS7 := "Does each comment explain a trade-off the reader couldn't infer from the field name?"
	if !strings.Contains(brief.Body, auditOnlyS7) {
		t.Errorf("refinement-2 audit brief missing S7-only single-question test %q (audit extends past the writer's S1-S4 scope by design)", auditOnlyS7)
	}
}

// TestComposeRefinementFactsBlock_ProseAnchors — Pillar A pin for the
// refinement-1 facts-block composer. Three load-bearing prose
// surfaces: the unconditional heading, the eviction notice (rendered
// when budget forces oldest-first elision), and the eviction tail.
// Pinned at the unit level because the broader brief composer skips
// the block entirely when facts is empty; this test exercises the
// function directly with explicit budgets that force each path.
func TestComposeRefinementFactsBlock_ProseAnchors(t *testing.T) {
	t.Parallel()

	// Each fact body is padded so the all-facts-fit optimistic-path
	// total exceeds the eviction budget below; this forces the
	// eviction branch where notice + tail render. Kind=PorterChange
	// makes writeFactSummary include Why verbatim (the default-kind
	// path truncates Symptom to 100 chars and ignores Why).
	bigBody := strings.Repeat("ipsum dolor sit amet ", 20) // ~420 chars
	sample := []FactRecord{
		{Topic: "fact-1", Scope: "api", RecordedAt: "2026-05-13T07:00:00Z", Why: bigBody, Kind: FactKindPorterChange, CandidateClass: "intersection", CandidateSurface: "CODEBASE_KB"},
		{Topic: "fact-2", Scope: "api", RecordedAt: "2026-05-13T07:00:01Z", Why: bigBody, Kind: FactKindPorterChange, CandidateClass: "intersection", CandidateSurface: "CODEBASE_KB"},
		{Topic: "fact-3", Scope: "api", RecordedAt: "2026-05-13T07:00:02Z", Why: bigBody, Kind: FactKindPorterChange, CandidateClass: "intersection", CandidateSurface: "CODEBASE_KB"},
	}

	// Always-rendered heading. Calling with ample budget exercises the
	// non-eviction path; the heading still ships.
	block, evicted := composeRefinementFactsBlock(sample, 8192)
	if evicted != 0 {
		t.Fatalf("ample-budget call: got evicted=%d, want 0", evicted)
	}
	if !strings.Contains(block, "## Recorded facts (per-codebase scoped)") {
		t.Errorf("ample-budget block missing heading anchor — facts-section header dropped")
	}

	// Eviction-triggering budget. Tight enough that the optimistic
	// all-facts-fit path bails, loose enough that the eviction-path
	// overhead (heading + notice + tail at worst-case integer width)
	// still fits. Budget must clear ~310 chars of overhead but stay
	// below the full sample total; 500 sits in that window for the
	// 3-fact fixture above.
	evictBlock, evictCount := composeRefinementFactsBlock(sample, 500)
	if evictCount == 0 {
		t.Fatalf("small-budget call: expected eviction, got evicted=0 (test fixture needs a tighter budget)")
	}
	evictionAnchors := []string{
		"## Recorded facts (per-codebase scoped)",
		// Notice format (briefs_refinement.go:255) — pin the head,
		// middle, and tail of the sentence so an edit anywhere in it
		// fires the guard.
		"Refinement brief approached the cap;",
		"facts were elided to disk (oldest-first). The newest",
		"are listed below.",
		// Tail format (briefs_refinement.go:256) — same head/tail
		// coverage; the cap size (80 KB) is itself load-bearing.
		"facts elided to fit RefinementBriefCap (80 KB); the most recent",
		"are included above.",
		"Full fact stream is on disk under the run output directory.",
	}
	for _, want := range evictionAnchors {
		if !strings.Contains(evictBlock, want) {
			t.Errorf("eviction block missing prose anchor %q — facts-block silent-drop guard", want)
		}
	}
}
