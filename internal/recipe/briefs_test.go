package recipe

import (
	"strings"
	"testing"
)

func TestBriefCompose_ScaffoldUnderCap(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	for _, cb := range plan.Codebases {
		t.Run(cb.Hostname, func(t *testing.T) {
			t.Parallel()
			brief, err := BuildScaffoldBrief(plan, cb, nil)
			if err != nil {
				t.Fatalf("BuildScaffoldBrief: %v", err)
			}
			if brief.Bytes > ScaffoldBriefCap {
				t.Errorf("scaffold brief for %s: %d bytes exceeds %d cap",
					cb.Hostname, brief.Bytes, ScaffoldBriefCap)
			}
			if !strings.Contains(brief.Body, "# Scaffold brief — "+cb.Hostname) {
				t.Error("missing scaffold brief header")
			}
			if !strings.Contains(brief.Body, "Platform obligations") {
				t.Error("missing platform obligations section")
			}
		})
	}
}

// TestBuildFinalizeBrief_CorrectCodebasePaths — run-11 gap S-1.
// Brief body names each codebase's actual SourceRoot path verbatim;
// no obsolete pre-§L paths.
func TestBuildFinalizeBrief_CorrectCodebasePaths(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	for i := range plan.Codebases {
		plan.Codebases[i].SourceRoot = "/var/www/" + plan.Codebases[i].Hostname + "dev"
	}
	brief, err := BuildFinalizeBrief(plan)
	if err != nil {
		t.Fatalf("BuildFinalizeBrief: %v", err)
	}
	for _, cb := range plan.Codebases {
		if !strings.Contains(brief.Body, cb.SourceRoot) {
			t.Errorf("brief missing SourceRoot %q", cb.SourceRoot)
		}
	}
	if strings.Contains(brief.Body, "/var/www/synth-showcase/api/") {
		t.Error("brief carries obsolete pre-§L path /var/www/<slug>/<host>/")
	}
}

// TestBuildFinalizeBrief_CorrectFragmentMath — run-11 S-1. Fragment
// count derives from Plan structure, not hand-typed math.
func TestBuildFinalizeBrief_CorrectFragmentMath(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	for i := range plan.Codebases {
		plan.Codebases[i].SourceRoot = "/var/www/" + plan.Codebases[i].Hostname + "dev"
	}
	brief, err := BuildFinalizeBrief(plan)
	if err != nil {
		t.Fatalf("BuildFinalizeBrief: %v", err)
	}
	// 3 codebases + 4 services = 7 import-comment hosts
	// 6 tiers × (1 env intro + 1 project comment + 7 service comments) + 1 root intro = 6×9 + 1 = 55
	if !strings.Contains(brief.Body, "Total: 1 root intro") {
		t.Errorf("brief math section missing total expression: %q", brief.Body)
	}
	if !strings.Contains(brief.Body, "= 55 fragments") {
		t.Errorf("expected fragment count = 55 in brief, got: %q", brief.Body)
	}
}

// TestBuildFinalizeBrief_UnderCap — S-1 §6 watch.
func TestBuildFinalizeBrief_UnderCap(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	for i := range plan.Codebases {
		plan.Codebases[i].SourceRoot = "/var/www/" + plan.Codebases[i].Hostname + "dev"
	}
	brief, err := BuildFinalizeBrief(plan)
	if err != nil {
		t.Fatalf("BuildFinalizeBrief: %v", err)
	}
	if brief.Bytes > FinalizeBriefCap {
		t.Errorf("finalize brief: %d bytes exceeds %d cap", brief.Bytes, FinalizeBriefCap)
	}
}

// TestBrief_Scaffold_TeachesOwnKeyAliasing — run-12 §E. Scaffold brief
// teaches own-key aliasing as the recommended pattern; the run-11 wrong
// rule ("Do NOT declare DB_HOST: ${db_hostname}") is gone.
func TestBrief_Scaffold_TeachesOwnKeyAliasing(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	mustContain(t, brief.Body, "DB_HOST: ${db_hostname}")
	mustContain(t, brief.Body, "process.env.DB_HOST")
	mustContain(t, brief.Body, "Same-key shadow trap")
	mustContain(t, brief.Body, "db_hostname: ${db_hostname}")
	if strings.Contains(brief.Body, "Do NOT declare `DB_HOST: ${db_hostname}") {
		t.Errorf("brief still carries the run-11 wrong rule banning own-key alias")
	}
}

// TestBrief_Scaffold_TeachesAliasTypeContracts — run-12 §A. Scaffold
// brief teaches that `${<host>_zeropsSubdomain}` is a full HTTPS URL
// already, so sub-agents stop emitting `https://${<host>_zeropsSubdomain}`.
func TestBrief_Scaffold_TeachesAliasTypeContracts(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	mustContain(t, brief.Body, "Alias-type contracts")
	mustContain(t, brief.Body, "full HTTPS URL")
	mustContain(t, brief.Body, "do NOT prepend")
}

// TestBrief_Scaffold_CLAUDEMDIsPorter — run-12 §C. Scaffold brief
// teaches that CLAUDE.md is porter-facing — no zcp MCP refs in dev-loop
// section; framework-canonical commands instead.
func TestBrief_Scaffold_CLAUDEMDIsPorter(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	mustContain(t, brief.Body, "framework-canonical")
	mustContain(t, brief.Body, "MCP tool invocations")
	mustContain(t, brief.Body, "porter-facing")
}

// TestBrief_Scaffold_IGScopeRule — run-12 §I. Scaffold brief carries
// the IG-scope rule: items 2+ are "what changes for Zerops" only;
// recipe-internal contracts route to KB or claude-md/notes.
func TestBrief_Scaffold_IGScopeRule(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	mustContain(t, brief.Body, "IG scope")
	mustContain(t, brief.Body, "Aim for 4-7 IG items")
}

// TestBuildFinalizeBrief_IncludesTierMap — run-12 §B. Engine-composed
// finalize brief carries the tier map (was hand-typed wrapper content).
func TestBuildFinalizeBrief_IncludesTierMap(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	for i := range plan.Codebases {
		plan.Codebases[i].SourceRoot = "/var/www/" + plan.Codebases[i].Hostname + "dev"
	}
	brief, err := BuildFinalizeBrief(plan)
	if err != nil {
		t.Fatalf("BuildFinalizeBrief: %v", err)
	}
	mustContain(t, brief.Body, "## Tier map")
	mustContain(t, brief.Body, "0 — AI Agent")
	mustContain(t, brief.Body, "5 — Highly-available Production")
}

// TestBuildFinalizeBrief_IncludesFragmentList — run-12 §B. Brief
// enumerates every fragment id the agent must author, derived from
// Plan structure. Replaces the hand-typed wrapper math.
func TestBuildFinalizeBrief_IncludesFragmentList(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	for i := range plan.Codebases {
		plan.Codebases[i].SourceRoot = "/var/www/" + plan.Codebases[i].Hostname + "dev"
	}
	brief, err := BuildFinalizeBrief(plan)
	if err != nil {
		t.Fatalf("BuildFinalizeBrief: %v", err)
	}
	mustContain(t, brief.Body, "## Fragments to author")
	mustContain(t, brief.Body, "`root/intro`")
	mustContain(t, brief.Body, "`env/0/intro`")
	mustContain(t, brief.Body, "`env/0/import-comments/api`")
	mustContain(t, brief.Body, "`env/5/import-comments/db`")
}

// TestBuildFinalizeBrief_IncludesAntiPatterns — run-12 §B. Brief
// inlines the anti-patterns atom (do NOT re-emit workspace yaml; do
// NOT touch codebase fragments at finalize; etc.).
func TestBuildFinalizeBrief_IncludesAntiPatterns(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	for i := range plan.Codebases {
		plan.Codebases[i].SourceRoot = "/var/www/" + plan.Codebases[i].Hostname + "dev"
	}
	brief, err := BuildFinalizeBrief(plan)
	if err != nil {
		t.Fatalf("BuildFinalizeBrief: %v", err)
	}
	mustContain(t, brief.Body, "What NOT to do")
	mustContain(t, brief.Body, "emit-yaml shape=workspace")
}

// TestBuildFinalizeBrief_SizeApproximatesDispatchPromptSize — run-12
// §B. After ship, dispatch prompt should be within 10% of brief size.
// Run 11: brief 3,427 vs dispatch 13,492 (393%). Run 12 target: brief
// large enough that main agent dispatches as-is without wrapper
// padding.
func TestBuildFinalizeBrief_SizeApproximatesDispatchPromptSize(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	for i := range plan.Codebases {
		plan.Codebases[i].SourceRoot = "/var/www/" + plan.Codebases[i].Hostname + "dev"
	}
	brief, err := BuildFinalizeBrief(plan)
	if err != nil {
		t.Fatalf("BuildFinalizeBrief: %v", err)
	}
	if brief.Bytes < 6000 {
		t.Errorf("finalize brief too small for dispatch-as-is: %d bytes", brief.Bytes)
	}
}

func TestBriefCompose_FeatureUnderCap(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildFeatureBrief(plan)
	if err != nil {
		t.Fatalf("BuildFeatureBrief: %v", err)
	}
	if brief.Bytes > FeatureBriefCap {
		t.Errorf("feature brief: %d bytes exceeds %d cap", brief.Bytes, FeatureBriefCap)
	}
	for _, cb := range plan.Codebases {
		if !strings.Contains(brief.Body, cb.Hostname) {
			t.Errorf("feature brief missing codebase %q in symbol table", cb.Hostname)
		}
	}
	for _, svc := range plan.Services {
		if !strings.Contains(brief.Body, svc.Hostname) {
			t.Errorf("feature brief missing service %q in symbol table", svc.Hostname)
		}
	}
}
