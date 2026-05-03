package recipe

import (
	"slices"
	"strings"
	"testing"
)

// Run-22 followup F-6 — embedded-parent fallback reaches the three
// downstream phases (codebase-content + env-content + refinement) the
// way scaffold already did via R3-RC-0. When the chain resolver returns
// no parent (filesystem mount empty in the dogfood path) AND the slug
// has a recognized chain parent (`*-showcase` → `*-minimal`), the
// composer injects the embedded recipe `.md` as a baseline section so
// downstream sub-agents can inherit the parent's IG/KB framing,
// tier-decision facts, and published-surface coverage instead of
// re-authoring upstream material.
//
// Mirrors the R3-RC-0 scaffold pattern in
// `BuildScaffoldBriefWithResolver` (briefs.go ~L338-361). Filesystem
// mount wins when present; embedded fallback fires only in the dogfood
// path (parent == nil && parentSlugFor(slug) != "").
//
// Per-composer excerpt-size choice differs by phase:
//   - codebase-content uses a tighter 2500-byte excerpt because the
//     codebase-content brief sat at 56,084 bytes after R2 with only
//     1,260 bytes of headroom under the 56 KB cap.
//   - env-content uses 4000 bytes (matches scaffold) — env-content
//     sat at 34,558 bytes / 56 KB cap with 22,786 bytes of headroom.
//   - refinement uses 4000 bytes (matches scaffold) — refinement
//     has no enforced cap.

// showcaseSlugWithChainParent returns a plan + codebase tuned for the
// embedded-fallback path: slug is `nestjs-showcase` so parentSlugFor
// resolves to `nestjs-minimal` (which exists in the embedded recipes
// corpus per internal/knowledge/recipes/nestjs-minimal.md).
func showcaseSlugWithChainParent() *Plan {
	return &Plan{
		Slug:      "nestjs-showcase",
		Framework: "nestjs",
		Tier:      "showcase",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22", SourceRoot: "/var/www/apidev"},
			{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22", SourceRoot: "/var/www/workerdev", IsWorker: true},
		},
		Services: []Service{
			{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18", SupportsHA: true},
		},
	}
}

// minimalSlugNoChainParent returns a plan whose slug has no chain
// parent — `nestjs-minimal` does not match the `*-showcase` rule in
// parentSlugFor, so the embedded fallback must not fire.
func minimalSlugNoChainParent() *Plan {
	return &Plan{
		Slug:      "nestjs-minimal",
		Framework: "nestjs",
		Tier:      "minimal",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22", SourceRoot: "/var/www/apidev"},
		},
		Services: []Service{
			{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18", SupportsHA: true},
		},
	}
}

// -----------------------------------------------------------------------------
// codebase-content
// -----------------------------------------------------------------------------

func TestCodebaseContentBrief_EmbedsParentMD_WhenParentAbsent_ShowcaseSlug(t *testing.T) {
	t.Parallel()
	plan := showcaseSlugWithChainParent()
	// Worker codebase is the highest-load shape — it loads
	// `worker_kb_supplements.md` on top of the standard atom set.
	// Pin the cap regression on the worst case.
	worker := plan.Codebases[1]
	if !worker.IsWorker {
		t.Fatalf("test fixture drift: expected Codebases[1] to be the worker (got %+v)", worker)
	}
	brief, err := BuildCodebaseContentBrief(plan, worker, nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "Parent recipe baseline (embedded)") {
		t.Errorf("codebase-content brief missing embedded-parent-baseline section for showcase slug with no resolved parent")
	}
	if !slices.Contains(brief.Parts, "embedded_parent_baseline") {
		t.Errorf("codebase-content brief Parts missing embedded_parent_baseline (got %v)", brief.Parts)
	}
	// The embedded nestjs-minimal.md teaches `setup: prod`. Match that
	// substring to confirm the actual baseline content reached the brief.
	if !strings.Contains(brief.Body, "setup: prod") {
		t.Errorf("codebase-content brief embedded-parent block missing expected `setup: prod` content from nestjs-minimal.md")
	}
	// Cap regression — the embedded fallback path must stay under the
	// 56 KB codebase-content cap on the highest-load variant (worker).
	// 1500-byte excerpt was chosen specifically to satisfy this on
	// showcase + worker plans; tighter than scaffold's 4000-byte excerpt.
	if brief.Bytes > CodebaseContentBriefCap {
		t.Errorf("codebase-content brief over cap with embedded fallback: %d bytes (cap %d)", brief.Bytes, CodebaseContentBriefCap)
	}
}

func TestCodebaseContentBrief_OmitsEmbeddedParent_WhenParentMounted(t *testing.T) {
	t.Parallel()
	plan := showcaseSlugWithChainParent()
	parent := &ParentRecipe{
		Slug:       "nestjs-minimal",
		Tier:       "minimal",
		SourceRoot: "/recipes/nestjs-minimal",
		Codebases: map[string]ParentCodebase{
			"api": {README: "# parent api readme — load me, not the embed"},
		},
	}
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], parent, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	if strings.Contains(brief.Body, "Parent recipe baseline (embedded)") {
		t.Errorf("codebase-content brief should NOT include embedded-parent block when filesystem-mount parent is present")
	}
	if slices.Contains(brief.Parts, "embedded_parent_baseline") {
		t.Errorf("codebase-content brief Parts should NOT include embedded_parent_baseline when filesystem parent present (got %v)", brief.Parts)
	}
	// Existing filesystem-mount parent-pointer path still fires.
	if !strings.Contains(brief.Body, parent.SourceRoot) {
		t.Errorf("codebase-content brief should still carry filesystem parent SourceRoot pointer when parent mounted")
	}
}

func TestCodebaseContentBrief_OmitsEmbeddedParent_WhenSlugIsMinimal(t *testing.T) {
	t.Parallel()
	plan := minimalSlugNoChainParent()
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	if strings.Contains(brief.Body, "Parent recipe baseline (embedded)") {
		t.Errorf("codebase-content brief should NOT embed parent baseline when slug has no chain parent (minimal/hello-world)")
	}
	if slices.Contains(brief.Parts, "embedded_parent_baseline") {
		t.Errorf("codebase-content brief Parts should NOT include embedded_parent_baseline for minimal slug (got %v)", brief.Parts)
	}
}

// -----------------------------------------------------------------------------
// env-content
// -----------------------------------------------------------------------------

func TestEnvContentBrief_EmbedsParentMD_WhenParentAbsent_ShowcaseSlug(t *testing.T) {
	t.Parallel()
	plan := showcaseSlugWithChainParent()
	brief, err := BuildEnvContentBrief(plan, nil, nil)
	if err != nil {
		t.Fatalf("BuildEnvContentBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "Parent recipe baseline (embedded)") {
		t.Errorf("env-content brief missing embedded-parent-baseline section for showcase slug with no resolved parent")
	}
	if !slices.Contains(brief.Parts, "embedded_parent_baseline") {
		t.Errorf("env-content brief Parts missing embedded_parent_baseline (got %v)", brief.Parts)
	}
	// Confirm parent baseline content reached the brief — `setup: prod`
	// is present in nestjs-minimal.md.
	if !strings.Contains(brief.Body, "setup: prod") {
		t.Errorf("env-content brief embedded-parent block missing expected `setup: prod` content from nestjs-minimal.md")
	}
	if brief.Bytes > EnvContentBriefCap {
		t.Errorf("env-content brief over cap with embedded fallback: %d bytes (cap %d)", brief.Bytes, EnvContentBriefCap)
	}
}

func TestEnvContentBrief_OmitsEmbeddedParent_WhenParentMounted(t *testing.T) {
	t.Parallel()
	plan := showcaseSlugWithChainParent()
	parent := &ParentRecipe{
		Slug:       "nestjs-minimal",
		Tier:       "minimal",
		SourceRoot: "/recipes/nestjs-minimal",
	}
	brief, err := BuildEnvContentBrief(plan, parent, nil)
	if err != nil {
		t.Fatalf("BuildEnvContentBrief: %v", err)
	}
	if strings.Contains(brief.Body, "Parent recipe baseline (embedded)") {
		t.Errorf("env-content brief should NOT include embedded-parent block when filesystem-mount parent is present")
	}
	if slices.Contains(brief.Parts, "embedded_parent_baseline") {
		t.Errorf("env-content brief Parts should NOT include embedded_parent_baseline when filesystem parent present (got %v)", brief.Parts)
	}
	// Existing filesystem parent-pointer still fires.
	if !strings.Contains(brief.Body, parent.SourceRoot) {
		t.Errorf("env-content brief should still carry filesystem parent SourceRoot pointer when parent mounted")
	}
}

func TestEnvContentBrief_OmitsEmbeddedParent_WhenSlugIsMinimal(t *testing.T) {
	t.Parallel()
	plan := minimalSlugNoChainParent()
	brief, err := BuildEnvContentBrief(plan, nil, nil)
	if err != nil {
		t.Fatalf("BuildEnvContentBrief: %v", err)
	}
	if strings.Contains(brief.Body, "Parent recipe baseline (embedded)") {
		t.Errorf("env-content brief should NOT embed parent baseline when slug has no chain parent (minimal/hello-world)")
	}
	if slices.Contains(brief.Parts, "embedded_parent_baseline") {
		t.Errorf("env-content brief Parts should NOT include embedded_parent_baseline for minimal slug (got %v)", brief.Parts)
	}
}

// -----------------------------------------------------------------------------
// refinement
// -----------------------------------------------------------------------------

func TestRefinementBrief_EmbedsParentMD_WhenParentAbsent_ShowcaseSlug(t *testing.T) {
	t.Parallel()
	plan := showcaseSlugWithChainParent()
	brief, err := BuildRefinementBrief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "Parent recipe baseline (embedded)") {
		t.Errorf("refinement brief missing embedded-parent-baseline section for showcase slug with no resolved parent")
	}
	if !slices.Contains(brief.Parts, "embedded_parent_baseline") {
		t.Errorf("refinement brief Parts missing embedded_parent_baseline (got %v)", brief.Parts)
	}
	// Confirm parent baseline content reached the brief.
	if !strings.Contains(brief.Body, "setup: prod") {
		t.Errorf("refinement brief embedded-parent block missing expected `setup: prod` content from nestjs-minimal.md")
	}
}

func TestRefinementBrief_OmitsEmbeddedParent_WhenParentMounted(t *testing.T) {
	t.Parallel()
	plan := showcaseSlugWithChainParent()
	parent := &ParentRecipe{
		Slug:       "nestjs-minimal",
		Tier:       "minimal",
		SourceRoot: "/recipes/nestjs-minimal",
	}
	brief, err := BuildRefinementBrief(plan, parent, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	if strings.Contains(brief.Body, "Parent recipe baseline (embedded)") {
		t.Errorf("refinement brief should NOT include embedded-parent block when filesystem-mount parent is present")
	}
	if slices.Contains(brief.Parts, "embedded_parent_baseline") {
		t.Errorf("refinement brief Parts should NOT include embedded_parent_baseline when filesystem parent present (got %v)", brief.Parts)
	}
	// Existing filesystem parent block still names parent.Slug + the HOLDS rule.
	if !strings.Contains(brief.Body, "HOLDS on any fragment whose body would re-author parent material") {
		t.Errorf("refinement brief should still carry the existing filesystem parent HOLDS rule")
	}
}

func TestRefinementBrief_OmitsEmbeddedParent_WhenSlugIsMinimal(t *testing.T) {
	t.Parallel()
	plan := minimalSlugNoChainParent()
	brief, err := BuildRefinementBrief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	if strings.Contains(brief.Body, "Parent recipe baseline (embedded)") {
		t.Errorf("refinement brief should NOT embed parent baseline when slug has no chain parent (minimal/hello-world)")
	}
	if slices.Contains(brief.Parts, "embedded_parent_baseline") {
		t.Errorf("refinement brief Parts should NOT include embedded_parent_baseline for minimal slug (got %v)", brief.Parts)
	}
}
