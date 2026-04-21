package workflow

import (
	"os"
	"strings"
	"testing"
)

// TestLoadAtom_EveryManifestEntry — every atom declared in the manifest
// must be backed by an embedded file. Scans all 120 atoms; any load
// error fails the test. Cross-validates C-3 (manifest) + C-4 (files)
// + C-5 (embed.FS wiring).
func TestLoadAtom_EveryManifestEntry(t *testing.T) {
	t.Parallel()
	missing := 0
	for _, a := range AllAtoms() {
		if _, err := LoadAtom(a.ID); err != nil {
			t.Errorf("LoadAtom(%q): %v", a.ID, err)
			missing++
			if missing > 5 {
				t.Fatal("too many load errors, stopping")
			}
		}
	}
}

// TestLoadAtom_UnknownID — unregistered IDs return an error naming the id.
func TestLoadAtom_UnknownID(t *testing.T) {
	t.Parallel()
	_, err := LoadAtom("no.such.atom")
	if err == nil {
		t.Fatal("expected error for unknown atom ID")
	}
	if !strings.Contains(err.Error(), "no.such.atom") {
		t.Errorf("error should name the id: %v", err)
	}
}

// TestConcatAtoms_SkipsEmpty — empty IDs in the list are skipped so
// tier-branching callers can pass "" for inapplicable atoms.
func TestConcatAtoms_SkipsEmpty(t *testing.T) {
	t.Parallel()
	got, err := concatAtoms("research.entry", "", "research.completion")
	if err != nil {
		t.Fatalf("concatAtoms: %v", err)
	}
	if !strings.Contains(got, "\n\n---\n\n") {
		t.Error("expected '---' separator between atoms")
	}
}

// TestBuildStepEntry_Research — composes the phase entry atom.
func TestBuildStepEntry_Research(t *testing.T) {
	t.Parallel()
	got, err := BuildStepEntry("research", RecipeTierShowcase)
	if err != nil {
		t.Fatalf("BuildStepEntry: %v", err)
	}
	if len(got) < 50 {
		t.Errorf("expected non-trivial composed output, got %d bytes", len(got))
	}
}

// TestBuildSubStepCompletion_FallsBackToPhaseLevel — when a substep
// doesn't declare its own completion atom, the phase-level completion
// is returned.
func TestBuildSubStepCompletion_FallsBackToPhaseLevel(t *testing.T) {
	t.Parallel()
	// research has only a phase-level completion atom, no substep-level.
	got, err := BuildSubStepCompletion("research", "nonexistent-substep")
	if err != nil {
		t.Fatalf("BuildSubStepCompletion: %v", err)
	}
	if len(got) == 0 {
		t.Error("expected fallback to phase-level completion")
	}
}

// TestBuildScaffoldDispatchBrief_ThreeRolesProduceDistinctOutput — api /
// app / worker addenda differentiate the composed brief.
func TestBuildScaffoldDispatchBrief_ThreeRolesProduceDistinctOutput(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{
		Tier: RecipeTierShowcase,
		Targets: []RecipeTarget{
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			{Hostname: "app", Type: "nodejs@22", Role: "app"},
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true, Role: "worker"},
			{Hostname: "db", Type: "postgresql@17"},
		},
	}
	plan.SymbolContract = BuildSymbolContract(plan)

	apiB, err := BuildScaffoldDispatchBrief(plan, "api")
	if err != nil {
		t.Fatalf("api brief: %v", err)
	}
	appB, err := BuildScaffoldDispatchBrief(plan, "app")
	if err != nil {
		t.Fatalf("app brief: %v", err)
	}
	workerB, err := BuildScaffoldDispatchBrief(plan, "worker")
	if err != nil {
		t.Fatalf("worker brief: %v", err)
	}

	if apiB == appB || apiB == workerB || appB == workerB {
		t.Error("expected three roles to produce three distinct briefs")
	}

	// Every brief must embed the SymbolContract JSON fragment.
	for name, b := range map[string]string{"api": apiB, "app": appB, "worker": workerB} {
		if !strings.Contains(b, "\"envVarsByKind\"") {
			t.Errorf("%s brief missing SymbolContract JSON", name)
		}
		if !strings.Contains(b, "```json") {
			t.Errorf("%s brief missing JSON code fence", name)
		}
	}
}

// TestBuildScaffoldDispatchBrief_ContractJSONIdenticalAcrossRoles —
// P3 invariant: every scaffold dispatch for the same plan sees byte-
// identical SymbolContract JSON. Guards against per-role contract
// mutation that would divide parallel sub-agents.
func TestBuildScaffoldDispatchBrief_ContractJSONIdenticalAcrossRoles(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{
		Tier: RecipeTierShowcase,
		Targets: []RecipeTarget{
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			{Hostname: "app", Type: "nodejs@22", Role: "app"},
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true, Role: "worker"},
			{Hostname: "db", Type: "postgresql@17"},
			{Hostname: "queue", Type: "nats@2"},
		},
	}
	plan.SymbolContract = BuildSymbolContract(plan)

	fragments := make([]string, 0, 3)
	for _, role := range []string{"api", "app", "worker"} {
		b, err := BuildScaffoldDispatchBrief(plan, role)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		// Extract the JSON code block.
		start := strings.Index(b, "```json\n")
		end := strings.Index(b, "\n```\n")
		if start < 0 || end < 0 || end <= start {
			t.Fatalf("%s: JSON code fence not found", role)
		}
		fragments = append(fragments, b[start+len("```json\n"):end])
	}
	for i := 1; i < len(fragments); i++ {
		if fragments[i] != fragments[0] {
			t.Errorf("role %d SymbolContract JSON diverges from role 0", i)
		}
	}
}

// TestBuildFeatureDispatchBrief_IncludesContractJSON — same P3 invariant
// for the feature sub-agent lane.
func TestBuildFeatureDispatchBrief_IncludesContractJSON(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{
		Tier:    RecipeTierShowcase,
		Targets: []RecipeTarget{{Hostname: "api", Type: "nodejs@22", Role: "api"}},
	}
	plan.SymbolContract = BuildSymbolContract(plan)

	b, err := BuildFeatureDispatchBrief(plan)
	if err != nil {
		t.Fatalf("BuildFeatureDispatchBrief: %v", err)
	}
	if !strings.Contains(b, "```json") {
		t.Error("feature brief missing JSON code fence for SymbolContract")
	}
}

// TestBuildWriterDispatchBrief_IncludesFactsPath — when a factsLogPath
// is supplied, the brief names it as an input reference.
func TestBuildWriterDispatchBrief_IncludesFactsPath(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{Tier: RecipeTierShowcase}
	b, err := BuildWriterDispatchBrief(plan, "/tmp/zcp-facts-xyz.jsonl")
	if err != nil {
		t.Fatalf("BuildWriterDispatchBrief: %v", err)
	}
	if !strings.Contains(b, "/tmp/zcp-facts-xyz.jsonl") {
		t.Error("writer brief missing facts log path reference")
	}
}

// TestBuildCodeReviewDispatchBrief_IncludesManifestPath — code-review
// brief names its content manifest input.
func TestBuildCodeReviewDispatchBrief_IncludesManifestPath(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{Tier: RecipeTierShowcase}
	b, err := BuildCodeReviewDispatchBrief(plan, "/output/ZCP_CONTENT_MANIFEST.json")
	if err != nil {
		t.Fatalf("BuildCodeReviewDispatchBrief: %v", err)
	}
	if !strings.Contains(b, "ZCP_CONTENT_MANIFEST.json") {
		t.Error("code-review brief missing manifest path reference")
	}
}

// TestBuildEditorialReviewDispatchBrief_NoPriorDiscoveries — the
// editorial-review composer must NEVER prepend prior discoveries (per
// P7 + refinement §10 #6 porter-premise).
func TestBuildEditorialReviewDispatchBrief_NoPriorDiscoveries(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{Tier: RecipeTierShowcase}
	b, err := BuildEditorialReviewDispatchBrief(plan, "/tmp/facts.jsonl", "/out/manifest.json")
	if err != nil {
		t.Fatalf("BuildEditorialReviewDispatchBrief: %v", err)
	}
	if strings.Contains(b, "Prior discoveries") {
		t.Error("editorial-review brief must NOT contain a Prior Discoveries block (porter-premise)")
	}
	// Pointer inputs appear explicitly so the reviewer can open them.
	if !strings.Contains(b, "Pointer inputs") {
		t.Error("editorial-review brief should name the pointer inputs section")
	}
}

// TestMarshalSymbolContract_Deterministic — two calls with the same
// plan produce byte-identical JSON.
func TestMarshalSymbolContract_Deterministic(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{
		Tier: RecipeTierShowcase,
		Targets: []RecipeTarget{
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			{Hostname: "db", Type: "postgresql@17"},
		},
	}
	plan.SymbolContract = BuildSymbolContract(plan)
	a, err := marshalSymbolContract(plan)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	b, err := marshalSymbolContract(plan)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if a != b {
		t.Errorf("contract JSON not deterministic:\nfirst:  %s\nsecond: %s", a, b)
	}
}

// TestSymbolContractWiredAtResearchComplete — CompleteStep on the
// research step populates plan.SymbolContract idempotently.
func TestSymbolContractWiredAtResearchComplete(t *testing.T) {
	plan := &RecipePlan{
		Tier: RecipeTierShowcase,
		Targets: []RecipeTarget{
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			{Hostname: "db", Type: "postgresql@17"},
		},
	}
	state := &RecipeState{
		Active: true,
		Steps: []RecipeStep{
			{Name: RecipeStepResearch, Status: stepInProgress},
			{Name: RecipeStepProvision, Status: stepPending},
		},
		CurrentStep: 0,
		Plan:        plan,
	}
	// Before complete: contract not set.
	if len(plan.SymbolContract.EnvVarsByKind) != 0 {
		t.Fatal("pre-condition: SymbolContract should be empty")
	}
	if err := state.CompleteStep(RecipeStepResearch, "research complete; targets populated"); err != nil {
		t.Fatalf("CompleteStep: %v", err)
	}
	if _, ok := plan.SymbolContract.EnvVarsByKind["db"]; !ok {
		t.Error("SymbolContract not populated after research complete")
	}
	if len(plan.SymbolContract.FixRecurrenceRules) != 12 {
		t.Errorf("expected 12 seeded rules, got %d", len(plan.SymbolContract.FixRecurrenceRules))
	}
}

// TestBuildPriorDiscoveriesBlockForLane_FiltersByRouteTo — lane-aware
// filtering only includes facts whose RouteTo is in the lane's allow-list.
func TestBuildPriorDiscoveriesBlockForLane_FiltersByRouteTo(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	logPath := tmpDir + "/facts.jsonl"

	// Append a mix of routeTo values via a quick hand-authored jsonl.
	// Each fact is downstream-scoped so the Scope filter passes; they
	// differ only in RouteTo so the lane filter is exercised.
	records := []string{
		`{"ts":"2026-04-20T10:00:00Z","substep":"deploy.init-commands","type":"platform_observation","title":"fact for scaffold lane","scope":"downstream","routeTo":"scaffold_preamble"}`,
		`{"ts":"2026-04-20T10:01:00Z","substep":"deploy.init-commands","type":"platform_observation","title":"fact for writer lane","scope":"downstream","routeTo":"content_gotcha"}`,
		`{"ts":"2026-04-20T10:02:00Z","substep":"deploy.init-commands","type":"platform_observation","title":"fact for code-review","scope":"downstream","routeTo":"claude_md"}`,
		`{"ts":"2026-04-20T10:03:00Z","substep":"deploy.init-commands","type":"platform_observation","title":"legacy no-route fact","scope":"downstream"}`,
	}
	joined := strings.Join(records, "\n") + "\n"
	if err := writeTestFile(logPath, joined); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Scaffold lane: accepts scaffold_preamble + legacy (empty RouteTo).
	got := buildPriorDiscoveriesBlockFromPathForLane(logPath, "deploy.readmes", "scaffold")
	if !strings.Contains(got, "fact for scaffold lane") {
		t.Error("scaffold lane missing scaffold_preamble fact")
	}
	if !strings.Contains(got, "legacy no-route fact") {
		t.Error("scaffold lane missing legacy-routed fact (empty RouteTo should broadcast)")
	}
	if strings.Contains(got, "fact for writer lane") {
		t.Error("scaffold lane leaked writer-lane fact")
	}
	if strings.Contains(got, "fact for code-review") {
		t.Error("scaffold lane leaked code-review fact")
	}

	// Writer lane: accepts content_* + claude_md + legacy.
	got = buildPriorDiscoveriesBlockFromPathForLane(logPath, "deploy.readmes", "writer")
	if !strings.Contains(got, "fact for writer lane") {
		t.Error("writer lane missing content_gotcha fact")
	}
	if strings.Contains(got, "fact for scaffold lane") {
		t.Error("writer lane leaked scaffold fact")
	}
}

// writeTestFile is a tiny helper to write a one-shot jsonl fixture.
func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
