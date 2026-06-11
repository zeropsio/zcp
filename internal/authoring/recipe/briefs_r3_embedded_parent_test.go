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
//   - codebase-content uses a tighter 1000-byte excerpt because the
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
	// Run-30 Fix #1 PARTIAL — assert against content that lives WITHIN
	// the 1000-byte excerpt window. The prior assertion checked
	// `setup: prod` which is at byte 1695 of nestjs-minimal.md (well
	// outside the excerpt); the test only "passed" because `setup: prod`
	// also appeared inline in `cross-service-urls.md` which loads
	// elsewhere in the brief. Run-30 deferred that atom to a
	// fetch-on-demand pointer (Read-tool token-ceiling fix), exposing
	// the false-positive. The "No `.env` files on Zerops" gotcha is
	// unique to nestjs-minimal.md and lives within the first 700 bytes.
	if !strings.Contains(brief.Body, "No `.env` files on Zerops") {
		t.Errorf("codebase-content brief embedded-parent block missing expected nestjs-minimal.md gotcha content")
	}
	// Run-40 ENG-3 — the "Cross-reference shape" worked example was
	// dropped. Run-39 forensics showed the template literally handed
	// the agent the wording `See parent recipe <slug> for <topic>`,
	// which propagated to porter-facing surfaces (apidev/README.md
	// :351, `### See parent recipe for foundational NestJS-on-Zerops
	// traps`) — exactly the prose-shape the refinement-phase warning
	// forbids. The "Parent slug: %[1]s" knowledge-routing prefix
	// stays (subagents need it); only the worked example is gone.
	if strings.Contains(brief.Body, "See parent recipe `nestjs-minimal` for <topic>.") {
		t.Errorf("codebase-content framing must NOT emit the cross-reference shape worked example (run-40 ENG-3)")
	}
	if strings.Contains(brief.Body, "Cross-reference shape when parent already covers a topic") {
		t.Errorf("codebase-content framing must NOT emit the cross-reference-shape clause (run-40 ENG-3)")
	}
	// Knowledge-routing prefix stays.
	if !strings.Contains(brief.Body, "Parent slug: nestjs-minimal") {
		t.Errorf("codebase-content framing must still carry the `Parent slug:` knowledge-routing prefix")
	}
	// Run-31 Fix #1 closure — single-file CodebaseContentBriefCap is
	// gone. The embedded-parent excerpt cap (1000 bytes for showcase,
	// 4000 for scaffold) is now sized to fit comfortably inside a
	// single multi-file part; the per-part invariant is pinned by
	// `TestBuildCodebaseContentBrief_MultiFile_RealSlug_PartsUnderCap`
	// in briefs_multi_file_test.go.
	// Run-22 fixup BLOCKER 1 — the closing ``` fence must be preceded
	// by `\n`. Pre-fix the helper called HasSuffix on the FULL parent
	// body (which usually ends in `\n`) instead of the excerpt
	// (excerptREADME strips trailing newline); for any parent.md larger
	// than excerptCap (every production case — nestjs-minimal.md is
	// 6717 bytes) the newline insertion was skipped and the closing
	// fence ran together with the last line of content.
	assertEmbeddedParentFenceWellFormed(t, brief.Body)
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
	// Run-40 ENG-3 — same forbidden-shape rule for env-content. The
	// tier-shaped cross-reference clause is gone; "Parent slug: ..."
	// knowledge-routing prefix stays.
	if strings.Contains(brief.Body, "See parent recipe `nestjs-minimal` tier <N> for <topic>.") {
		t.Errorf("env-content framing must NOT emit the tier cross-reference shape worked example (run-40 ENG-3)")
	}
	if strings.Contains(brief.Body, "Cross-reference shape when parent already shaped a tier") {
		t.Errorf("env-content framing must NOT emit the tier cross-reference-shape clause (run-40 ENG-3)")
	}
	if !strings.Contains(brief.Body, "Parent slug: nestjs-minimal") {
		t.Errorf("env-content framing must still carry the `Parent slug:` knowledge-routing prefix")
	}
	// Run-31 Fix #1 closure — single-file EnvContentBriefCap is gone.
	// Per-part invariants pinned by
	// `TestBuildEnvContentBrief_MultiFile_RealSlug_PartsUnderCap`.
	assertEmbeddedParentFenceWellFormed(t, brief.Body)
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
	assertEmbeddedParentFenceWellFormed(t, brief.Body)
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

// assertEmbeddedParentFenceWellFormed scans the brief body for the
// embedded-parent block + asserts the closing markdown fence is on a
// line of its own (preceded by `\n`). Pre-fixup BLOCKER 1 the helper
// called HasSuffix on the FULL parent body — which usually ends in
// `\n` — instead of the excerpt returned by excerptREADME (which
// strips trailing newline). For any parent.md > excerptCap the
// newline-insertion was skipped + the closing fence ran together with
// the last line of content, breaking markdown rendering downstream.
//
// Asserts: the substring "```\n\n" (closing fence on its own line +
// trailing blank line) appears AFTER the "## Parent recipe baseline"
// (or the bold-paragraph variant) header. Detects the run-on bug
// because pre-fix the closing fence would land like
// "...some content```\n\n" — with the fence inline.
func assertEmbeddedParentFenceWellFormed(t *testing.T, body string) {
	t.Helper()
	headers := []string{
		"## Parent recipe baseline (embedded)",
		"**Parent recipe baseline (embedded)**",
	}
	var hdr string
	for _, h := range headers {
		if strings.Contains(body, h) {
			hdr = h
			break
		}
	}
	if hdr == "" {
		t.Fatalf("assertEmbeddedParentFenceWellFormed: no embedded-parent block in body")
	}
	hdrIdx := strings.Index(body, hdr)
	if hdrIdx < 0 {
		// Defensive — the header was found by strings.Contains above so
		// strings.Index must succeed; surface as a Fatal anyway in case
		// the body changes between the two calls.
		t.Fatalf("assertEmbeddedParentFenceWellFormed: header %q vanished from body", hdr)
	}
	rest := body[hdrIdx:]
	_, after, ok := strings.Cut(rest, "```md\n")
	if !ok {
		t.Fatalf("assertEmbeddedParentFenceWellFormed: missing ```md opening fence after %q", hdr)
	}
	closeIdx := strings.Index(after, "```")
	if closeIdx < 0 {
		t.Fatalf("assertEmbeddedParentFenceWellFormed: missing closing fence")
	}
	// The byte immediately before the closing fence MUST be `\n`. If it
	// isn't, the closing ``` ran together with the last line of
	// content (the BLOCKER 1 newline-insertion bug). Surface the
	// offending tail so the diagnostic points at the malformed shape.
	if closeIdx == 0 || after[closeIdx-1] != '\n' {
		const tailWindow = 80
		start := max(closeIdx-tailWindow, 0)
		t.Errorf("assertEmbeddedParentFenceWellFormed: closing fence not on its own line (BLOCKER 1 newline bug); tail before fence: %q", after[start:closeIdx])
	}
}
