package recipe

import (
	"slices"
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
	// Run-17 §6 — engine-emit shells are retracted, but the defensive
	// filterOutEngineEmitted still drops EngineEmitted=true entries if
	// a session log replay surfaces historical shells. The brief
	// composer no longer renders an "Engine-emitted fact shells"
	// section at all.
	facts := []FactRecord{
		{
			Topic:            "api-connect-db",
			Kind:             FactKindPorterChange,
			Scope:            "api/code",
			EngineEmitted:    true, // historical shell
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

	if strings.Contains(brief.Body, "## Engine-emitted fact shells") {
		t.Error("Run-17 retraction: brief should not render an Engine-emitted shells section")
	}
	if strings.Contains(brief.Body, "api-connect-db") {
		t.Error("historical engine-emitted shell leaked into brief")
	}
	if !strings.Contains(brief.Body, "api-cors-expose-x-cache") {
		t.Error("agent-recorded fact missing from brief")
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

func TestBuildCodebaseContentBrief_NoEngineEmittedShells_AfterRun17Retraction(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildCodebaseContentBrief(plan, plan.Codebases[0], nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	// Run-17 §6 — Class B + per-service shells retracted. None of the
	// historical shell topics may appear in the brief.
	for _, forbidden := range []string{
		"api-bind-and-trust-proxy",
		"api-sigterm-drain",
		"api-own-key-aliases",
		"api-connect-db",
		"api-connect-cache",
	} {
		if strings.Contains(brief.Body, forbidden) {
			t.Errorf("Run-17 retraction: brief still contains engine-emitted topic %q", forbidden)
		}
	}
	if strings.Contains(brief.Body, "## Engine-emitted fact shells") {
		t.Error("Run-17 retraction: brief still renders Engine-emitted shells section header")
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

func TestBuildCodebaseContentBrief_SizeUnderCap(t *testing.T) {
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

func TestBuildEnvContentBrief_SizeUnderCap(t *testing.T) {
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

// TestEnvContentBrief_LoadsCrossServiceURLsAtom — run-22 followup F-4.
// The env-content sub-agent authors per-tier import-comments fragments
// that routinely discuss URL constants (`${zeropsSubdomainHost}`,
// `STAGE_API_URL`, etc). Without principles/cross-service-urls.md the
// agent lacks the literal-stays-literal rule for the deliverable yaml
// AND the projectEnvVars channel-sync teaching that explains why those
// constants exist. Loaded unconditionally — the env-content brief has
// 27 KB of headroom under the 56 KB cap and the teaching is universally
// applicable to per-tier yaml comment authoring.
func TestEnvContentBrief_LoadsCrossServiceURLsAtom(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildEnvContentBrief(plan, nil, nil)
	if err != nil {
		t.Fatalf("BuildEnvContentBrief: %v", err)
	}

	// Parts entry pinned so the dispatch trace shows the atom landed.
	wantPart := "principles/cross-service-urls.md"
	if !slices.Contains(brief.Parts, wantPart) {
		t.Errorf("env-content brief Parts missing %q (got %v)", wantPart, brief.Parts)
	}

	// Body anchors — the literal-stays-literal teaching for the
	// deliverable yaml plus the projectEnvVars channel-sync rule are
	// the two halves env-content authors comments around.
	for _, anchor := range []string{
		"literal-stays-literal",
		"projectEnvVars",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("env-content brief missing cross-service-urls anchor %q", anchor)
		}
	}
}

// TestBuildEnvContentBrief_OmitsNATSWhenNoBroker — run-21 R3-2.
// Env-content brief loads `principles/nats-shapes.md` only when the
// plan declares a nats-family managed service. contentPhaseTestPlan
// has only postgres + valkey; the NATS atom must be absent.
func TestBuildEnvContentBrief_OmitsNATSWhenNoBroker(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	brief, err := BuildEnvContentBrief(plan, nil, nil)
	if err != nil {
		t.Fatalf("BuildEnvContentBrief: %v", err)
	}
	for _, p := range brief.Parts {
		if strings.Contains(p, "nats-shapes") {
			t.Errorf("env-content brief Parts unexpectedly carries %q for plan with no nats@* service", p)
		}
	}
}

// TestBuildEnvContentBrief_LoadsNATSWhenPlanHasBroker — run-21 R3-2
// counterpart. Plan with a nats@* managed service still loads the
// teaching (no regression of the run-20 C1 fix).
func TestBuildEnvContentBrief_LoadsNATSWhenPlanHasBroker(t *testing.T) {
	t.Parallel()
	plan := contentPhaseTestPlan()
	plan.Services = append(plan.Services, Service{
		Kind: ServiceKindManaged, Hostname: "broker", Type: "nats@2",
	})
	brief, err := BuildEnvContentBrief(plan, nil, nil)
	if err != nil {
		t.Fatalf("BuildEnvContentBrief: %v", err)
	}
	found := false
	for _, p := range brief.Parts {
		if strings.Contains(p, "nats-shapes") {
			found = true
			break
		}
	}
	if !found {
		t.Error("env-content brief Parts missing nats-shapes for plan with nats@* service")
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
