package recipe

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// Run-17 §9 — refinement sub-agent brief composition. Pins the
// transactional contract: brief carries seven distillation atoms +
// rubric + stitched-output pointer block + recorded facts; no
// filesystem-local references leak from the design-time author's
// machine.

// TestBuildRefinementBrief_AssemblesCoreAtoms — run-23 F-24 + F-25.
// The 7 reference distillation atoms moved off the inline brief and
// onto the discovery channel (zerops_knowledge uri=...). The composer
// now ships only the phase entry, synthesis workflow, embedded rubric,
// and a fetchable-references catalog; the agent fetches the matching
// reference atom when investigating a suspect class.
func TestBuildRefinementBrief_AssemblesCoreAtoms(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "synth-showcase",
		Tier: "showcase",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
			{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22", IsWorker: true},
		},
	}
	brief, err := BuildRefinementBrief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	if brief.Kind != BriefRefinement {
		t.Errorf("brief.Kind = %v, want %v", brief.Kind, BriefRefinement)
	}
	for _, want := range []string{
		"phase_entry/refinement.md",
		"briefs/refinement/synthesis_workflow.md",
		"briefs/refinement/derived_rules.md",
		"reference_atom_catalog",
	} {
		if !slices.Contains(brief.Parts, want) {
			t.Errorf("brief.Parts missing %q; got %v", want, brief.Parts)
		}
	}
	// Run-33 architectural fix #2 + run-34 Fix A — embedded_rubric.md
	// retired and the atom file deleted; rule-walk against stitched
	// output via derived_rules.md is the sole scoring substrate.
	if slices.Contains(brief.Parts, "briefs/refinement/embedded_rubric.md") {
		t.Errorf("brief.Parts unexpectedly carries embedded_rubric.md (retired in run-33 fix #2; deleted in run-34 Fix A)")
	}
}

// TestBuildRefinementBrief_OmitsReferenceDistillationAtoms — run-23 F-25.
// The 7 reference atoms must NOT appear in Brief.Parts (they moved to
// the discovery channel) and the inline brief body must NOT contain
// them. The composer instead lists the fetchable URIs.
func TestBuildRefinementBrief_OmitsReferenceDistillationAtoms(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "synth-showcase",
		Tier: "showcase",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
		},
	}
	brief, err := BuildRefinementBrief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	for _, forbidden := range []string{
		"briefs/refinement/reference_kb_shapes.md",
		"briefs/refinement/reference_ig_one_mechanism.md",
		"briefs/refinement/reference_voice_patterns.md",
		"briefs/refinement/reference_yaml_comments.md",
		"briefs/refinement/reference_citations.md",
		"briefs/refinement/reference_trade_offs.md",
		"briefs/refinement/refinement_thresholds.md",
	} {
		if slices.Contains(brief.Parts, forbidden) {
			t.Errorf("brief.Parts unexpectedly carries %q (atom moved to discovery channel)", forbidden)
		}
	}
	// Brief lists every fetchable URI under the catalog section.
	for _, want := range []string{
		"zerops://themes/refinement-references/kb_shapes",
		"zerops://themes/refinement-references/ig_one_mechanism",
		"zerops://themes/refinement-references/voice_patterns",
		"zerops://themes/refinement-references/yaml_comments",
		"zerops://themes/refinement-references/citations",
		"zerops://themes/refinement-references/trade_offs",
		"zerops://themes/refinement-references/refinement_thresholds",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("brief body missing fetchable reference URI %q", want)
		}
	}
}

// TestBuildRefinementBrief_BodyUnderShrinkTarget — run-23 F-24 soft cap
// regression. Pre-shrink the brief was ~167 KB on a typical showcase;
// the F-24 rewrite targets ~30-50 KB. Soft cap 60 KB catches a
// regression where someone re-inlines the reference atoms.
func TestBuildRefinementBrief_BodyUnderShrinkTarget(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "synth-showcase",
		Tier: "showcase",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
			{Hostname: "app", Role: RoleFrontend, BaseRuntime: "nodejs@22"},
			{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22", IsWorker: true},
		},
	}
	brief, err := BuildRefinementBrief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	// Run-32 phase 2 — soft cap raised 60→75 KB to accommodate the
	// derived_rules.md atom (golden-grounded rule substrate added
	// alongside embedded_rubric.md). Run-33 — bumped 75→76 KB to fit
	// Y13 (causal-command-sequence rule restored from
	// run-32-rules-from-jetstream.md:405). Run-34 Fix A — embedded_rubric
	// retired and deleted (~35 KB removed); cap dropped to 45 KB to
	// catch any future re-inlining regression. Run-43 F1 — cap raised
	// 45→46 KB to accommodate the classification-field guidance
	// (record-fragment on CODEBASE_KB/CODEBASE_IG requires
	// `classification` per spec; recurring run-40 + run-42 failure
	// where the field was omitted). Run-43 F5 + F6 — cap raised
	// 46→48 KB for F-XSURF-REF LLM-judgment reframe (extra examples
	// + GOOD worked anchor) + new F-FRIENDLY-AUTH derived rule
	// (declarative + invitation + porter-side trigger pattern;
	// LLM-judgment based, not regex). Run-43 F7 — cap raised 48→50
	// KB for the phase-entry anti-redispatch tightening (status-check
	// + exactly-once-per-recipe guidance against run-42 third-pass
	// failure). Run-49 reconcile (2026-05-25) — cap raised 50→52 KB
	// after merging v9.98.0 (R49-I1/I2/I3/I4 corpus closures: rolling-
	// deploy mechanism, cross-service env model, zsc noop retirement,
	// execOnce burn-recovery scoping; +8/6/2 lines across
	// derived_rules.md) plus v9.99.0 (Surface 5 dual-shape,
	// kb-self-inflicted-reversible gate; +2 lines on derived_rules.md).
	// Resulting brief lands at ~51.4 KB; cap set at 52 KB with ~0.6 KB
	// headroom.
	const briefShrinkCap = 52 * 1024
	if brief.Bytes > briefShrinkCap {
		t.Errorf("refinement brief %d bytes exceeds %d cap (F-24 shrink target; run-32 phase 2 raised to 75K)", brief.Bytes, briefShrinkCap)
	}
}

// TestRefinementBrief_LoadsDerivedRulesNotRubric — run-34 Fix A.
// Pins that the refinement brief loads `derived_rules.md` content and
// does NOT load `embedded_rubric.md` content. The rubric atom was
// deleted in run-34 Fix A; this test guards against any future
// regression that re-introduces it (file or load).
func TestRefinementBrief_LoadsDerivedRulesNotRubric(t *testing.T) {
	t.Parallel()
	plan := &Plan{Slug: "x", Codebases: []Codebase{{Hostname: "api"}}}
	brief, err := BuildRefinementBrief(plan, nil, "/run", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	// derived_rules.md content present (canonical rule headings).
	for _, ruleAnchor := range []string{
		"V1 — porter-clones-and-runs",
		"Y8 — no tier-promotion narrative",
		"F-SUBDOMAIN — Zerops subdomains do NOT rotate",
	} {
		if !strings.Contains(brief.Body, ruleAnchor) {
			t.Errorf("brief missing derived_rules anchor %q", ruleAnchor)
		}
	}
	// embedded_rubric.md content absent (rubric criterion headings).
	for _, rubricAnchor := range []string{
		"Criterion 1 — Stem shape",
		"Criterion 2 — Voice",
		"Criterion 3 — Citation",
	} {
		if strings.Contains(brief.Body, rubricAnchor) {
			t.Errorf("brief unexpectedly carries rubric anchor %q (retired in run-33 fix #2; atom deleted in run-34 Fix A)", rubricAnchor)
		}
	}
	// The atom file itself must be gone — readAtom should fail.
	if _, err := readAtom("briefs/refinement/embedded_rubric.md"); err == nil {
		t.Error("embedded_rubric.md atom should be deleted; readAtom should fail")
	}
}

// TestBuildRefinementBrief_EmbedsDerivedRules — run-33 architectural
// fix #2. The embedded rubric (5 criteria × 3 anchors each) is retired;
// rule-walk against stitched output (golden-grounded principle-shaped
// rules in derived_rules.md) is the primary scoring substrate.
func TestBuildRefinementBrief_EmbedsDerivedRules(t *testing.T) {
	t.Parallel()
	plan := &Plan{Slug: "x", Codebases: []Codebase{{Hostname: "api"}}}
	brief, err := BuildRefinementBrief(plan, nil, "/run", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	// derived_rules.md is now the substrate — assert representative
	// rule ids land in the brief body.
	for _, anchor := range []string{
		"V1 — porter-clones-and-runs",
		"V3 — link text references real concepts",
		"V6 — no authoring vocabulary",
		"IG6 — every IG step",
		"R1 — short",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("brief missing derived-rules anchor %q", anchor)
		}
	}
	// Rubric criterion anchors must be absent — the rubric atom is no
	// longer loaded into the brief.
	for _, forbidden := range []string{
		"Criterion 1",
		"Criterion 2",
		"Criterion 3",
		"Criterion 4",
		"Criterion 5",
	} {
		if strings.Contains(brief.Body, forbidden) {
			t.Errorf("brief unexpectedly carries rubric anchor %q (retired in run-33 fix #2)", forbidden)
		}
	}
}

func TestBuildRefinementBrief_ListsStitchedPaths(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "x",
		Codebases: []Codebase{
			{Hostname: "api"},
			{Hostname: "worker"},
		},
	}
	brief, err := BuildRefinementBrief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "/run/dir/api/README.md") {
		t.Error("brief missing api codebase pointer")
	}
	if !strings.Contains(brief.Body, "/run/dir/worker/README.md") {
		t.Error("brief missing worker codebase pointer")
	}
	if !strings.Contains(brief.Body, "/run/dir/environments/0 — AI Agent/README.md") {
		t.Error("brief missing tier-0 environments pointer")
	}
	if !strings.Contains(brief.Body, "/run/dir/environments/4 — Small Production/README.md") {
		t.Error("brief missing tier-4 environments pointer")
	}
}

func TestBuildRefinementBrief_FactsLogPresent(t *testing.T) {
	t.Parallel()
	plan := &Plan{Slug: "x", Codebases: []Codebase{{Hostname: "api"}}}
	facts := []FactRecord{
		{Topic: "fact-1", Kind: FactKindPorterChange, Why: "test fact one"},
		{Topic: "fact-2", Kind: FactKindFieldRationale, FieldPath: "run.x", Why: "test fact two"},
	}
	brief, err := BuildRefinementBrief(plan, nil, "/run", facts)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "fact-1") {
		t.Error("brief missing fact-1")
	}
	if !strings.Contains(brief.Body, "fact-2") {
		t.Error("brief missing fact-2")
	}
	if !strings.Contains(brief.Body, "## Recorded facts") {
		t.Error("brief missing recorded-facts section header")
	}
}

func TestBuildRefinementBrief_ParentPointer_WhenParentNonNil(t *testing.T) {
	t.Parallel()
	plan := &Plan{Slug: "x", Codebases: []Codebase{{Hostname: "api"}}}
	parent := &ParentRecipe{Slug: "minimal", SourceRoot: "/parent/root"}
	brief, err := BuildRefinementBrief(plan, parent, "/run", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "minimal") {
		t.Error("brief should mention parent slug when parent set")
	}
	if !strings.Contains(brief.Body, "HOLDS on any fragment whose body would re-author parent material") {
		t.Error("brief should carry the parent re-authoring HOLD rule")
	}
}

func TestBuildRefinementBrief_NilPlan_Errors(t *testing.T) {
	t.Parallel()
	if _, err := BuildRefinementBrief(nil, nil, "/run", nil); err == nil {
		t.Error("expected error for nil plan")
	}
}

// TestBuildRefinementBrief_OverCap_TrimsFacts — run-28 fix #2.
// Synthetic plan with 200 facts must not blow `RefinementBriefCap`;
// surplus facts get evicted and the brief carries the marker so the
// agent reading the brief knows older facts were dropped to disk.
func TestBuildRefinementBrief_OverCap_TrimsFacts(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "synth-showcase",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
		},
	}
	// 200 large facts — each ~700 bytes of why-text. With no cap the
	// brief easily exceeds 80 KB; under the cap we expect eviction.
	bigPad := strings.Repeat("a fact text padding word ", 30)
	facts := make([]FactRecord, 200)
	for i := range facts {
		facts[i] = FactRecord{
			Topic: "synthetic-fact",
			Kind:  FactKindPorterChange,
			Why:   bigPad + " #" + strings.Repeat("x", 4),
		}
	}
	// Make each fact have a unique topic (so we can later check which
	// were kept vs evicted).
	for i := range facts {
		facts[i].Topic = "synthetic-fact-" + indexLabel(i)
	}
	brief, err := BuildRefinementBrief(plan, nil, "/run/dir", facts)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	if brief.Bytes > RefinementBriefCap {
		t.Errorf("refinement brief %d bytes exceeds RefinementBriefCap %d", brief.Bytes, RefinementBriefCap)
	}
	if !slices.Contains(brief.Parts, "facts_evicted_for_cap") {
		t.Errorf("brief.Parts missing facts_evicted_for_cap marker; got %v", brief.Parts)
	}
	if !strings.Contains(brief.Body, "facts elided to fit RefinementBriefCap") {
		t.Errorf("brief body missing elision-marker prose")
	}
}

// TestBuildRefinementBrief_UnderCap_NoElision — run-28 fix #2. A
// small recipe with 20 facts comfortably fits under the cap; no
// elision marker, no eviction part.
func TestBuildRefinementBrief_UnderCap_NoElision(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "synth-showcase",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
		},
	}
	facts := make([]FactRecord, 20)
	for i := range facts {
		facts[i] = FactRecord{
			Topic: "small-fact-" + indexLabel(i),
			Kind:  FactKindPorterChange,
			Why:   "small why text",
		}
	}
	brief, err := BuildRefinementBrief(plan, nil, "/run/dir", facts)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	if slices.Contains(brief.Parts, "facts_evicted_for_cap") {
		t.Errorf("brief.Parts unexpectedly carries facts_evicted_for_cap on small input")
	}
	if strings.Contains(brief.Body, "facts elided to fit RefinementBriefCap") {
		t.Errorf("brief body should not carry elision marker when under cap")
	}
}

// TestBuildRefinementBrief_EvictionKeepsRecentFacts — run-28 fix #2.
// Eviction iterates most-recent-first; oldest facts are dropped. Pin
// that the LAST fact (index 199) survives and the FIRST (index 0)
// does not.
func TestBuildRefinementBrief_EvictionKeepsRecentFacts(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "synth-showcase",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
		},
	}
	bigPad := strings.Repeat("padding word ", 30)
	facts := make([]FactRecord, 200)
	for i := range facts {
		facts[i] = FactRecord{
			Topic: "fact-" + indexLabel(i),
			Kind:  FactKindPorterChange,
			Why:   bigPad,
		}
	}
	brief, err := BuildRefinementBrief(plan, nil, "/run/dir", facts)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	// Most recent (last) must be kept.
	if !strings.Contains(brief.Body, "fact-"+indexLabel(199)) {
		t.Errorf("most-recent fact (index 199) should be kept after eviction")
	}
	// Oldest (first) must be evicted under such pressure.
	if strings.Contains(brief.Body, "fact-"+indexLabel(0)+" |") {
		t.Errorf("oldest fact (index 0) should have been evicted")
	}
}

// TestBuildRefinementBrief_EvictionPreservesRubricBlocks — run-28
// fix #2. Eviction targets ONLY facts; phase-entry, synthesis_workflow,
// derived_rules (the run-34 Fix A successor to embedded_rubric),
// reference_atom_catalog, stitched-output-pointer, and
// engine_flagged_suspects must remain in Parts/body even when facts
// are heavily evicted.
func TestBuildRefinementBrief_EvictionPreservesRubricBlocks(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "synth-showcase",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
		},
	}
	bigPad := strings.Repeat("padding word ", 30)
	facts := make([]FactRecord, 200)
	for i := range facts {
		facts[i] = FactRecord{
			Topic: "fact-" + indexLabel(i),
			Kind:  FactKindPorterChange,
			Why:   bigPad,
		}
	}
	brief, err := BuildRefinementBrief(plan, nil, "/run/dir", facts)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	for _, want := range []string{
		"phase_entry/refinement.md",
		"briefs/refinement/synthesis_workflow.md",
		"briefs/refinement/derived_rules.md",
		"reference_atom_catalog",
		"stitched-output-pointer-block",
	} {
		if !slices.Contains(brief.Parts, want) {
			t.Errorf("brief.Parts missing teaching block %q after eviction; got %v", want, brief.Parts)
		}
	}
	for _, anchor := range []string{
		"V1 — porter-clones-and-runs",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("derived-rules anchor %q evicted unexpectedly", anchor)
		}
	}
}

// indexLabel returns a stable padded decimal label for synthetic
// fact topics so eviction tests can assert ordering without relying
// on lexicographic surprises.
func indexLabel(i int) string {
	const pad = "00000"
	s := pad
	n := i
	for j := len(s) - 1; j >= 0 && n > 0; j-- {
		s = s[:j] + string(rune('0'+n%10)) + s[j+1:]
		n /= 10
	}
	return s
}

// TestRefinementAtoms_TeachesBareCodebaseFragmentId — run-23 F-17 pin.
// The synthesis_workflow.md atom uses unresolved `<h>` placeholders for
// codebase fragment ids; agents inferred `<h>` from facts' `service`
// field, which can be slot-named (`workerdev`) and got rejected by the
// engine ("unknown codebase 'workerdev'") 3x in run-23 before reshaping
// to the bare codebase name. Pin the worked example that disambiguates
// bare-codebase-name vs slot hostname so the teaching lands in the
// assembled refinement brief.
func TestRefinementAtoms_TeachesBareCodebaseFragmentId(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "synth-showcase",
		Tier: "showcase",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
			{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22", IsWorker: true},
		},
	}
	brief, err := BuildRefinementBrief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	for _, want := range []string{
		"bare-host",
		"slot suffix",
		"workerdev",
		"codebase/worker/knowledge-base",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("refinement brief missing bare-codebase-name worked-example token %q", want)
		}
	}
}

// TestBuildRefinementBriefMultiFile_StitchedPointerBlock_PopulatedFromOutputRoot
// pins the multi-file refinement composer's runDir wiring: when the
// dispatcher passes outputRoot, the stitched-output pointer block in
// the references part must list every per-tier README + per-codebase
// README rooted at outputRoot. Regression guard for the run-31 wiring
// where the multi-file dispatch path passed empty runDir despite
// having outputRoot in scope, blanking the pointer block and leaving
// the refinement sub-agent without Read-targets.
func TestBuildRefinementBriefMultiFile_StitchedPointerBlock_PopulatedFromOutputRoot(t *testing.T) {
	t.Parallel()
	plan := realShowcasePlan()
	outRoot := t.TempDir()

	brief, err := buildRefinementBriefMultiFile(plan, nil, outRoot, nil, outRoot)
	if err != nil {
		t.Fatalf("buildRefinementBriefMultiFile: %v", err)
	}
	if brief.IndexPath == "" {
		t.Fatalf("expected IndexPath populated; got empty")
	}

	// Concatenate every persisted part body so we can assert against
	// the references-part regardless of which part-N-references.md
	// number Persist allocates.
	var combined strings.Builder
	for _, p := range brief.PartPaths {
		body, rErr := os.ReadFile(p)
		if rErr != nil {
			t.Fatalf("read part %s: %v", p, rErr)
		}
		combined.Write(body)
		combined.WriteByte('\n')
	}
	body := combined.String()

	// Per-codebase Read-targets — every codebase in realShowcasePlan
	// must appear as a `<outputRoot>/<host>/README.md` line.
	for _, cb := range plan.Codebases {
		want := outRoot + "/" + cb.Hostname + "/README.md"
		if !strings.Contains(body, want) {
			t.Errorf("refinement brief missing per-codebase pointer %q", want)
		}
	}

	// Per-tier Read-targets — every tier folder gets a
	// `<outputRoot>/environments/<folder>/README.md` line.
	for _, tier := range Tiers() {
		want := outRoot + "/environments/" + tier.Folder + "/README.md"
		if !strings.Contains(body, want) {
			t.Errorf("refinement brief missing per-tier pointer %q", want)
		}
	}

	// Root README pointer must reference outputRoot.
	rootWant := outRoot + "/README.md"
	if !strings.Contains(body, rootWant) {
		t.Errorf("refinement brief missing root README pointer %q", rootWant)
	}

	// Pointer block heading must be present (defends against future
	// composer rearrangements that drop the section entirely).
	if !strings.Contains(body, "## Stitched output to refine") {
		t.Errorf("refinement brief missing '## Stitched output to refine' heading")
	}
}

// TestNoFilesystemReferenceLeak_RefinementBrief — pins §9.6: the
// composer reads only embedded atoms; design-time author's machine
// paths (/Users/fxck/...) must never appear in the brief body.
// TestRefinementBrief_RunDirTrailingSlash_NoDoubleSlash — Run-40 S-3.
// Run-39's BuildRefinementBrief was called with a trailing-slash
// runDir (`/var/www/zcprecipator/nestjs-showcase/`); the fmt.Fprintf
// path-concat then produced `nestjs-showcase//README.md` etc. across
// every stitched-output pointer line in the brief. Reads succeeded
// on POSIX (double-slash collapses) but porter-facing citations
// carried the broken shape. Pin the normalization.
func TestRefinementBrief_RunDirTrailingSlash_NoDoubleSlash(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "synth-showcase",
		Codebases: []Codebase{
			{Hostname: "api"}, {Hostname: "worker"},
		},
	}
	brief, err := BuildRefinementBrief(plan, nil, "/var/www/zcprecipator/nestjs-showcase/", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	if strings.Contains(brief.Body, "nestjs-showcase//") {
		t.Errorf("brief still contains double-slash path; got body excerpt:\n%s", brief.Body)
	}
	if !strings.Contains(brief.Body, "nestjs-showcase/README.md") {
		t.Errorf("brief should carry single-slash README path; got body excerpt:\n%s", brief.Body)
	}
}

func TestNoFilesystemReferenceLeak_RefinementBrief(t *testing.T) {
	t.Parallel()
	plan := &Plan{Slug: "x", Codebases: []Codebase{{Hostname: "api"}}}
	brief, err := BuildRefinementBrief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	for _, forbidden := range []string{
		"/Users/",
		"/home/",
		"/var/www/laravel-",
		"/var/www/recipes/",
	} {
		if strings.Contains(brief.Body, forbidden) {
			t.Errorf("brief leaks filesystem path prefix %q", forbidden)
		}
	}
}

// TestRefinementBrief_NoResidualRubricLanguage — run-34 Fix A closure.
// The rubric was retired in run-33 fix #2; the embedded_rubric.md atom
// was deleted and the threshold language flipped to rule-walk against
// `derived_rules.md`. This test pins that no residual rubric vocabulary
// leaks into the assembled refinement brief body — every prose-side
// teaching of "what to cite at refinement time" must use rule-walk
// vocabulary so the agent doesn't get a contradictory teaching shape.
//
// Historical references in atom prose (e.g. synthesis_workflow.md
// noting that "the legacy 5-criteria rubric (`embedded_rubric.md`) was
// retired") are scoped: they explain WHY the substrate changed rather
// than INSTRUCT the agent to score against criteria. The forbid list
// covers the instruction-shaped tokens.
func TestRefinementBrief_NoResidualRubricLanguage(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "synth-showcase",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
			{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22", IsWorker: true},
		},
	}
	brief, err := BuildRefinementBrief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	for _, forbidden := range []string{
		"violated rubric criterion",
		"rubric criterion",
		"rubric anchor",
		"rubric remains your authority",
		"score against the rubric",
		"score Criterion",
		"Score against each rubric criterion",
	} {
		if strings.Contains(brief.Body, forbidden) {
			t.Errorf("refinement brief carries residual rubric vocabulary %q — run-34 Fix A retired the rubric in favor of rule-walk against `derived_rules.md`", forbidden)
		}
	}
	// And the multi-file path must hold the same shape — exercise it via
	// the exported multi-file composer (the production-facing entry; cmd/
	// zcp-recipe-sim/refine.go uses this variant) with an outputRoot tempdir.
	outRoot := t.TempDir()
	mf, err := BuildRefinementBriefMultiFile(plan, nil, outRoot, nil, outRoot, "", "")
	if err != nil {
		t.Fatalf("BuildRefinementBriefMultiFile: %v", err)
	}
	var combined strings.Builder
	for _, p := range mf.PartPaths {
		body, rErr := os.ReadFile(p)
		if rErr != nil {
			t.Fatalf("read part %s: %v", p, rErr)
		}
		combined.Write(body)
		combined.WriteByte('\n')
	}
	mfBody := combined.String()
	for _, forbidden := range []string{
		"violated rubric criterion",
		"rubric criterion",
		"rubric anchor",
		"rubric remains your authority",
		"score against the rubric",
		"score Criterion",
		"Score against each rubric criterion",
	} {
		if strings.Contains(mfBody, forbidden) {
			t.Errorf("refinement multi-file brief carries residual rubric vocabulary %q — run-34 Fix A retired the rubric in favor of rule-walk against `derived_rules.md`", forbidden)
		}
	}
}
