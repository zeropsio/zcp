package recipe

import (
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
		"briefs/refinement/embedded_rubric.md",
		"reference_atom_catalog",
	} {
		if !slices.Contains(brief.Parts, want) {
			t.Errorf("brief.Parts missing %q; got %v", want, brief.Parts)
		}
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
	const briefShrinkCap = 60 * 1024
	if brief.Bytes > briefShrinkCap {
		t.Errorf("refinement brief %d bytes exceeds %d cap (F-24 shrink target)", brief.Bytes, briefShrinkCap)
	}
}

func TestBuildRefinementBrief_EmbedsRubric(t *testing.T) {
	t.Parallel()
	plan := &Plan{Slug: "x", Codebases: []Codebase{{Hostname: "api"}}}
	brief, err := BuildRefinementBrief(plan, nil, "/run", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	for _, anchor := range []string{
		"Criterion 1",
		"Criterion 2",
		"Criterion 3",
		"Criterion 4",
		"Criterion 5",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("brief missing rubric anchor %q", anchor)
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

// TestNoFilesystemReferenceLeak_RefinementBrief — pins §9.6: the
// composer reads only embedded atoms; design-time author's machine
// paths (/Users/fxck/...) must never appear in the brief body.
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
