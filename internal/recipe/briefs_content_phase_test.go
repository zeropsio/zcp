package recipe

import (
	"strings"
	"testing"
)

func contentPhaseTestPlan() *Plan {
	return &Plan{
		Slug:      "synth-showcase",
		Framework: "synth",
		Tier:      "showcase",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22", SourceRoot: "/var/www/apidev"},
		},
		Services: []Service{
			{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18", SupportsHA: true},
			{Kind: ServiceKindManaged, Hostname: "cache", Type: "valkey@7", SupportsHA: true},
		},
	}
}

func TestBuildCodebaseContentBrief_NoClaudeMdSlots(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	// The codebase-content brief explicitly does NOT instruct the agent
	// to author claude-md fragments — that's the sibling claudemd-author's
	// job. The brief mentions claudemd-author once (sibling note) but
	// must not list claude-md as one of the slots-to-fill.
	if strings.Contains(brief.Body, "Author the codebase/<h>/claude-md") {
		t.Error("codebase-content brief should not instruct authoring claude-md slot")
	}
}

func TestBuildCodebaseContentBrief_PointsAtSourceRootAndSpec(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "spec-content-surfaces.md") {
		t.Error("brief must point at spec-content-surfaces.md")
	}
	if !strings.Contains(brief.Body, "/var/www/apidev/zerops.yaml") {
		t.Error("brief must point at the codebase's zerops.yaml")
	}
}

// Run-16 §6.2 + reviewer D-4 — agent-recorded fact threading. The whole
// architecture pivot rests on facts being the bridge between deploy and
// content phases; if facts don't reach the brief, the codebase-content
// sub-agent has no insight into what the deploy agent learned.

func TestBuildCodebaseContentBrief_CarriesFilteredPorterChangeFacts(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	facts := []FactRecord{
		{
			Topic:            "api-cors-expose-x-cache",
			Kind:             FactKindPorterChange,
			Scope:            "api/code/src/main.ts",
			Why:              "Browsers strip non-CORS-safelisted response headers cross-origin.",
			CandidateClass:   "intersection",
			CandidateSurface: "CODEBASE_KB",
			CandidateHeading: "Custom response headers across origins",
		},
		{
			Topic:            "other-codebase-unrelated",
			Kind:             FactKindPorterChange,
			Scope:            "appdev/code",
			Why:              "Unrelated to api codebase.",
			CandidateClass:   "intersection",
			CandidateSurface: "CODEBASE_KB",
		},
	}
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], nil, facts)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	// api codebase brief carries the api-scoped fact and NOT the appdev fact.
	if !strings.Contains(brief.Body, "api-cors-expose-x-cache") {
		t.Error("brief missing api-scoped porter_change fact")
	}
	if strings.Contains(brief.Body, "other-codebase-unrelated") {
		t.Error("brief leaked another codebase's fact (FilterByCodebase broken)")
	}
	// Section header must be present when facts exist.
	if !strings.Contains(brief.Body, "## Recorded facts (codebase scope)") {
		t.Error("brief missing recorded-facts section header")
	}
}

func TestBuildCodebaseContentBrief_CarriesFilteredFieldRationaleFacts(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	facts := []FactRecord{
		{
			Topic:     "api-s3-region",
			Kind:      FactKindFieldRationale,
			Scope:     "api/zerops.yaml/run.envVariables.S3_REGION",
			FieldPath: "run.envVariables.S3_REGION",
			Why:       "us-east-1 is the only region MinIO accepts.",
		},
	}
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], nil, facts)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "api-s3-region") {
		t.Error("brief missing field_rationale fact")
	}
	if !strings.Contains(brief.Body, "run.envVariables.S3_REGION") {
		t.Error("brief missing field_rationale FieldPath rendering")
	}
}

func TestBuildCodebaseContentBrief_DropsEngineEmittedShellsFromRecordedFactsSection(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	// Engine-emitted shell + agent-filled record share scope but only
	// the agent-filled (EngineEmitted=false) one belongs in "Recorded
	// facts" — shells render in their own engine-emitted section.
	facts := []FactRecord{
		{
			Topic:            "api-connect-db",
			Kind:             FactKindPorterChange,
			Scope:            "api/code",
			EngineEmitted:    true, // unfilled shell
			CandidateSurface: "CODEBASE_IG",
			CitationGuide:    "managed-services-postgresql",
		},
		{
			Topic:            "api-cors-expose-x-cache",
			Kind:             FactKindPorterChange,
			Scope:            "api/code/src/main.ts",
			EngineEmitted:    false,
			Why:              "Browsers strip headers cross-origin.",
			CandidateClass:   "intersection",
			CandidateSurface: "CODEBASE_KB",
		},
	}
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], nil, facts)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}

	// Find the "Recorded facts" section and verify the shell isn't in it.
	hdrIdx := strings.Index(brief.Body, "## Recorded facts (codebase scope)")
	if hdrIdx < 0 {
		t.Fatal("recorded facts section missing")
	}
	nextHdrIdx := strings.Index(brief.Body[hdrIdx+1:], "\n## ")
	if nextHdrIdx < 0 {
		nextHdrIdx = len(brief.Body) - hdrIdx - 1
	}
	recordedSection := brief.Body[hdrIdx : hdrIdx+1+nextHdrIdx]
	if strings.Contains(recordedSection, "api-connect-db") {
		t.Error("engine-emitted shell leaked into Recorded facts section (should appear only under Engine-emitted shells)")
	}
	if !strings.Contains(recordedSection, "api-cors-expose-x-cache") {
		t.Error("agent-filled fact missing from Recorded facts section")
	}
}

func TestBuildEnvContentBrief_CarriesContractFacts(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	facts := []FactRecord{
		{
			Topic:       "nats-items-created-contract",
			Kind:        FactKindContract,
			Publishers:  []string{"api"},
			Subscribers: []string{"worker"},
			Subject:     "items.created",
			Purpose:     "Worker mirrors items into Meilisearch on create",
		},
	}
	brief, err := BuildEnvContentBrief(plan, nil, facts)
	if err != nil {
		t.Fatalf("BuildEnvContentBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "items.created") {
		t.Error("env-content brief missing contract fact subject")
	}
	if !strings.Contains(brief.Body, "## Cross-codebase contracts") {
		t.Error("env-content brief missing contracts section header")
	}
}

func TestBuildCodebaseContentBrief_CarriesEngineEmittedShells(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	// Class B + per-managed-service shells should appear.
	for _, want := range []string{
		"api-bind-and-trust-proxy",
		"api-sigterm-drain",
		"api-own-key-aliases",
		"api-connect-db",
		"api-connect-cache",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("brief missing engine-emitted topic %q", want)
		}
	}
}

func TestBuildCodebaseContentBrief_CarriesParentRecipePointer_WhenParentNonNil(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	parent := &ParentRecipe{Slug: "minimal", SourceRoot: "/Users/fxck/www/zcp/recipes/minimal"}
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], parent, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "minimal") {
		t.Error("brief should mention parent slug `minimal` when parent set")
	}
	if !strings.Contains(brief.Body, parent.SourceRoot) {
		t.Error("brief should include parent SourceRoot path")
	}
}

func TestBuildCodebaseContentBrief_NoParent_NoParentBlock(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	if strings.Contains(brief.Body, "parent recipe") && strings.Contains(strings.ToLower(brief.Body), "minimal") {
		t.Error("brief should not reference a parent slug when parent is nil")
	}
}

func TestBuildCodebaseContentBrief_SizeUnder40KB(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	if brief.Bytes > CodebaseContentBriefCap {
		t.Errorf("codebase-content brief over cap: %d bytes (cap %d) — accidental verbatim embed?", brief.Bytes, CodebaseContentBriefCap)
	}
}

func TestBuildEnvContentBrief_CarriesPerTierCapabilityMatrix(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildEnvContentBrief(plan, nil, nil)
	if err != nil {
		t.Fatalf("BuildEnvContentBrief: %v", err)
	}
	for _, want := range []string{"Tier 0", "Tier 1", "Tier 2", "Tier 3", "Tier 4", "Tier 5"} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("env-content brief missing %q in capability matrix", want)
		}
	}
}

func TestBuildEnvContentBrief_CarriesEngineEmittedTierDecisionFacts(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildEnvContentBrief(plan, nil, nil)
	if err != nil {
		t.Fatalf("BuildEnvContentBrief: %v", err)
	}
	// Per-service mode flip at tier 4→5 produces tier-5-db-mode and
	// tier-5-cache-mode (both postgres + valkey are HA-capable).
	for _, want := range []string{"tier-5-db-mode", "tier-5-cache-mode"} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("env-content brief missing engine-emitted tier_decision topic %q", want)
		}
	}
}

func TestBuildEnvContentBrief_CarriesParentRecipePointer_WhenParentNonNil(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	parent := &ParentRecipe{Slug: "minimal", SourceRoot: "/path/to/parent"}
	brief, err := BuildEnvContentBrief(plan, parent, nil)
	if err != nil {
		t.Fatalf("BuildEnvContentBrief: %v", err)
	}
	if !strings.Contains(brief.Body, parent.SourceRoot) {
		t.Error("env-content brief should include parent SourceRoot when parent non-nil")
	}
}

func TestBuildEnvContentBrief_SizeUnder40KB(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildEnvContentBrief(plan, nil, nil)
	if err != nil {
		t.Fatalf("BuildEnvContentBrief: %v", err)
	}
	if brief.Bytes > EnvContentBriefCap {
		t.Errorf("env-content brief over cap: %d bytes (cap %d)", brief.Bytes, EnvContentBriefCap)
	}
}

func TestBuildClaudeMDBrief_ContainsHardProhibitionBlock(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildClaudeMDBrief(plan, plan.Codebases[0])
	if err != nil {
		t.Fatalf("BuildClaudeMDBrief: %v", err)
	}
	prohibition, err := readAtom("briefs/claudemd-author/zerops_free_prohibition.md")
	if err != nil {
		t.Fatalf("read prohibition atom: %v", err)
	}
	if !strings.Contains(brief.Body, prohibition) {
		t.Error("claudemd-author brief must contain the hard-prohibition block verbatim")
	}
}

func TestBuildClaudeMDBrief_NoVoiceAnchorPointers(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildClaudeMDBrief(plan, plan.Codebases[0])
	if err != nil {
		t.Fatalf("BuildClaudeMDBrief: %v", err)
	}
	for _, anti := range []string{
		"laravel-showcase-app/CLAUDE.md",
		"laravel-jetstream-app/CLAUDE.md",
	} {
		if strings.Contains(brief.Body, anti) {
			t.Errorf("claudemd-author brief must NOT point at reference recipe CLAUDE.md (%s) — those carry the wrong-shape precedent", anti)
		}
	}
}

func TestBuildClaudeMDBrief_NoZeropsYamlPointer(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildClaudeMDBrief(plan, plan.Codebases[0])
	if err != nil {
		t.Fatalf("BuildClaudeMDBrief: %v", err)
	}
	// Pointer-block must NOT include zerops.yaml in the Read-on-demand list.
	if strings.Contains(brief.Body, "zerops.yaml — ") || strings.Contains(brief.Body, "zerops.yaml`)") {
		t.Error("claudemd-author brief must NOT point at zerops.yaml — the prohibition forbids it")
	}
}

func TestBuildClaudeMDBrief_NoPlatformPrinciplesAtom(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildClaudeMDBrief(plan, plan.Codebases[0])
	if err != nil {
		t.Fatalf("BuildClaudeMDBrief: %v", err)
	}
	pp, err := readAtom("briefs/scaffold/platform_principles.md")
	if err != nil {
		// Atom may not exist in tests — composer asserts the atom is
		// NOT included regardless.
		return
	}
	// Take a substantive substring from the platform principles atom (not
	// just its title) to detect cross-include.
	if len(pp) > 200 && strings.Contains(brief.Body, pp[100:200]) {
		t.Error("claudemd-author brief must NOT include platform principles atom — that defeats the prohibition")
	}
}

func TestBuildClaudeMDBrief_PointsAtSourceRoot(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildClaudeMDBrief(plan, plan.Codebases[0])
	if err != nil {
		t.Fatalf("BuildClaudeMDBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "/var/www/apidev/package.json") {
		t.Error("claudemd-author brief should point at <SourceRoot>/package.json")
	}
}

func TestBuildClaudeMDBrief_SizeUnder8KB(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildClaudeMDBrief(plan, plan.Codebases[0])
	if err != nil {
		t.Fatalf("BuildClaudeMDBrief: %v", err)
	}
	if brief.Bytes > ClaudeMDBriefCap {
		t.Errorf("claudemd-author brief over cap: %d bytes (cap %d)", brief.Bytes, ClaudeMDBriefCap)
	}
}
