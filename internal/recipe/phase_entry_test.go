package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stageScaffoldYAMLs stages a minimal scaffold-authored zerops.yaml per
// codebase so stitch-content's codebase-scoped writes have a SourceRoot
// on disk. Plan codebases get their SourceRoot mutated to the staged dir.
func stageScaffoldYAMLs(t *testing.T, base string, plan *Plan) {
	t.Helper()
	for i, cb := range plan.Codebases {
		dir := filepath.Join(base, "workspace", cb.Hostname)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		body := "# " + cb.Hostname + " — because test\nzerops: []\n"
		if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(body), 0o600); err != nil {
			t.Fatalf("write yaml for %s: %v", cb.Hostname, err)
		}
		plan.Codebases[i].SourceRoot = dir
	}
}

func TestPhaseEntry_AllPhasesPresent(t *testing.T) {
	t.Parallel()
	for _, p := range Phases() {
		body := loadPhaseEntry(p)
		if body == "" {
			t.Errorf("phase %q: missing phase_entry atom", p)
		}
	}
}

func TestDispatch_Start_ReturnsResearchGuidance(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)

	res := dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase",
		OutputRoot: filepath.Join(dir, "run"),
	})
	if !res.OK {
		t.Fatalf("start: %+v", res)
	}
	if !strings.Contains(res.Guidance, "Research phase") {
		t.Errorf("start.Guidance missing research-phase marker; got %q", firstLine(res.Guidance))
	}
	if !strings.Contains(res.Guidance, "update-plan") {
		t.Error("start.Guidance does not mention update-plan action")
	}
	if !strings.Contains(res.Guidance, "shape 1") || !strings.Contains(res.Guidance, "shape 2 or 3") {
		t.Error("start.Guidance missing codebase-shape decision tree")
	}
	if !strings.Contains(res.Guidance, "Don't call `zerops_knowledge`") {
		t.Error("start.Guidance missing the prohibition against zerops_knowledge")
	}
	if !strings.Contains(res.Guidance, "postgresql@18") {
		t.Error("start.Guidance missing authoritative service versions")
	}
	// ParentStatus must be explicit (absent for cold-start with empty mount).
	if res.ParentStatus != "absent" {
		t.Errorf("ParentStatus = %q, want \"absent\"", res.ParentStatus)
	}
}

func TestDispatch_UpdatePlan_MergesFields(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase",
		OutputRoot: filepath.Join(dir, "run"),
	})

	// First patch — framework + tier.
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "update-plan", Slug: "synth-showcase",
		Plan: &Plan{Framework: "synth", Tier: "showcase"},
	})
	if !res.OK {
		t.Fatalf("update-plan #1: %+v", res)
	}

	// Second patch — research + codebases + services (plan from fixture).
	syn := syntheticShowcasePlan()
	res = dispatch(t.Context(), store, RecipeInput{
		Action: "update-plan", Slug: "synth-showcase",
		Plan: &Plan{
			Research:  syn.Research,
			Codebases: syn.Codebases,
			Services:  syn.Services,
		},
	})
	if !res.OK {
		t.Fatalf("update-plan #2: %+v", res)
	}

	sess, _ := store.Get("synth-showcase")
	if sess.Plan.Framework != "synth" {
		t.Errorf("framework = %q, want synth", sess.Plan.Framework)
	}
	if sess.Plan.Tier != "showcase" {
		t.Errorf("tier = %q, want showcase", sess.Plan.Tier)
	}
	if len(sess.Plan.Codebases) != 3 {
		t.Errorf("codebases = %d, want 3", len(sess.Plan.Codebases))
	}
}

func TestDispatch_BuildBrief_UnknownCodebase_ClearError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase",
		OutputRoot: filepath.Join(dir, "run"),
	})

	res := dispatch(t.Context(), store, RecipeInput{
		Action: "build-brief", Slug: "synth-showcase",
		BriefKind: "scaffold", Codebase: "api",
	})
	if res.OK {
		t.Fatal("expected error for missing codebase in plan")
	}
	if !strings.Contains(res.Error, "not in plan") {
		t.Errorf("error should cite missing plan, got %q", res.Error)
	}
	if !strings.Contains(res.Error, "update-plan") {
		t.Errorf("error should point to update-plan, got %q", res.Error)
	}
}

func TestResearchGate_FullStackMonolith(t *testing.T) {
	t.Parallel()

	// Full-stack shape 1 with single monolith codebase and one managed
	// service: should pass the research gate for minimal tier.
	ctx := GateContext{
		Plan: &Plan{
			Slug: "synth-minimal", Framework: "synth", Tier: "minimal",
			Research: ResearchResult{CodebaseShape: "1"},
			Codebases: []Codebase{
				{Hostname: "app", Role: RoleMonolith, BaseRuntime: "php@8.4"},
			},
			Services: []Service{
				{Hostname: "db", Type: "postgresql@18", Kind: ServiceKindManaged, Priority: 10},
			},
		},
	}
	v := RunGates(researchGates(), ctx)
	if len(v) != 0 {
		t.Errorf("expected no violations, got %+v", v)
	}
}

func TestResearchGate_APIFirstShape3(t *testing.T) {
	t.Parallel()

	// Shape 3 showcase with api + frontend + separate worker + all 5
	// canonical managed services.
	ctx := GateContext{
		Plan: &Plan{
			Slug: "synth-showcase", Framework: "synth", Tier: "showcase",
			Research: ResearchResult{CodebaseShape: "3"},
			Codebases: []Codebase{
				{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
				{Hostname: "app", Role: RoleFrontend, BaseRuntime: "nodejs@22"},
				{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22", IsWorker: true},
			},
			Services: []Service{
				{Hostname: "db", Type: "postgresql@18", Kind: ServiceKindManaged, Priority: 10},
				{Hostname: "cache", Type: "valkey@7", Kind: ServiceKindManaged, Priority: 10},
				{Hostname: "broker", Type: "nats@2", Kind: ServiceKindManaged, Priority: 10},
				{Hostname: "storage", Type: "object-storage", Kind: ServiceKindStorage},
				{Hostname: "search", Type: "meilisearch@1", Kind: ServiceKindManaged, Priority: 10},
			},
		},
	}
	v := RunGates(researchGates(), ctx)
	if len(v) != 0 {
		t.Errorf("expected no violations, got %+v", v)
	}
}

func TestResearchGate_RejectsDogfoodPathology(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		plan     *Plan
		wantCode string
	}{
		{
			name: "missing framework",
			plan: &Plan{
				Slug: "x-showcase", Tier: "showcase",
				Research: ResearchResult{CodebaseShape: "3"},
			},
			wantCode: "plan-framework-missing",
		},
		{
			name: "invalid shape",
			plan: &Plan{
				Slug: "x-showcase", Framework: "x", Tier: "showcase",
			},
			wantCode: "plan-codebase-shape-invalid",
		},
		{
			name: "showcase with shape 1 + Handlebars monolith (v1 pathology)",
			plan: &Plan{
				Slug: "nestjs-showcase", Framework: "nestjs", Tier: "showcase",
				Research: ResearchResult{CodebaseShape: "1"},
				Codebases: []Codebase{
					{Hostname: "app", Role: RoleMonolith, BaseRuntime: "nodejs@22"},
				},
				Services: []Service{
					{Hostname: "db", Type: "postgresql@18", Kind: ServiceKindManaged, Priority: 10},
					{Hostname: "cache", Type: "valkey@7", Kind: ServiceKindManaged, Priority: 10},
					{Hostname: "broker", Type: "nats@2", Kind: ServiceKindManaged, Priority: 10},
					{Hostname: "storage", Type: "object-storage", Kind: ServiceKindStorage},
					{Hostname: "search", Type: "meilisearch@1", Kind: ServiceKindManaged, Priority: 10},
				},
			},
			// shape=1 is monolith (1 codebase is valid), but the fact the
			// agent chose monolith for an API-first framework is a plan-
			// level semantic choice the engine can't disprove without a
			// framework catalog. The gate we CAN enforce is that shape 1
			// declares exactly one role=monolith. That's already enforced;
			// the pathology this case triggers is showcase with 1 codebase
			// ok-per-shape but wrong-by-API-first. Flag via shape-
			// codebase-mismatch or via parent-recipe awareness in a
			// later iteration — for now, confirm the minimum gates pass.
			wantCode: "",
		},
		{
			name: "showcase missing half the canonical services (laravel-like with mailpit detour)",
			plan: &Plan{
				Slug: "nestjs-showcase", Framework: "nestjs", Tier: "showcase",
				Research: ResearchResult{CodebaseShape: "3"},
				Codebases: []Codebase{
					{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
					{Hostname: "app", Role: RoleFrontend, BaseRuntime: "nodejs@22"},
					{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22", IsWorker: true},
				},
				Services: []Service{
					{Hostname: "db", Type: "postgresql@18", Kind: ServiceKindManaged, Priority: 10},
					{Hostname: "mailpit", Type: "go@1", Kind: ServiceKindUtility},
				},
			},
			wantCode: "showcase-service-set-incomplete",
		},
		{
			name: "shape 3 without separate worker",
			plan: &Plan{
				Slug: "x-showcase", Framework: "x", Tier: "showcase",
				Research: ResearchResult{CodebaseShape: "3"},
				Codebases: []Codebase{
					{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
					{Hostname: "app", Role: RoleFrontend, BaseRuntime: "nodejs@22"},
					{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22", IsWorker: true, SharesCodebaseWith: "api"},
				},
				Services: []Service{
					{Hostname: "db", Type: "postgresql@18", Kind: ServiceKindManaged, Priority: 10},
					{Hostname: "cache", Type: "valkey@7", Kind: ServiceKindManaged, Priority: 10},
					{Hostname: "broker", Type: "nats@2", Kind: ServiceKindManaged, Priority: 10},
					{Hostname: "storage", Type: "object-storage", Kind: ServiceKindStorage},
					{Hostname: "search", Type: "meilisearch@1", Kind: ServiceKindManaged, Priority: 10},
				},
			},
			wantCode: "shape3-needs-separate-worker",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := RunGates(researchGates(), GateContext{Plan: tc.plan})
			if tc.wantCode == "" {
				if len(v) != 0 {
					t.Errorf("expected no violations, got %+v", v)
				}
				return
			}
			found := false
			for _, vio := range v {
				if vio.Code == tc.wantCode {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected violation code %q; got %+v", tc.wantCode, v)
			}
		})
	}
}

// TestDispatch_StitchContent_ReportsMissingFragments — the A1 stitch
// walks every surface template and reports any unfilled marker as a
// missing fragment id. Empty plan fragments → every surface surfaces
// its own missing ids → stitch returns error with the list.
func TestDispatch_StitchContent_ReportsMissingFragments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run")
	store := NewStore(dir)
	dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outputRoot,
	})
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()
	// A2: stage scaffold-authored yaml per codebase so stitch reaches the
	// fragment-missing path (A2's hard-fail would otherwise short-circuit).
	stageScaffoldYAMLs(t, dir, sess.Plan)

	res := dispatch(t.Context(), store, RecipeInput{
		Action: "stitch-content", Slug: "synth-showcase",
	})
	if res.OK {
		t.Fatal("stitch-content with no fragments should report missing")
	}
	if !strings.Contains(res.Error, "missing fragments") {
		t.Errorf("expected 'missing fragments' error, got: %s", res.Error)
	}
	// Even with missing fragments, surfaces still lay down on disk so
	// the next record-fragment + stitch iteration can overwrite.
	if _, err := os.Stat(filepath.Join(outputRoot, "README.md")); err != nil {
		t.Errorf("root README should be written even with missing fragments: %v", err)
	}
}

// TestDispatch_StitchContent_AssemblesFromFragments — populating every
// fragment via record-fragment + env import-comments lands a complete
// set of surfaces. Tier yamls regenerate with env comments; markers
// carry fragment bodies.
func TestDispatch_StitchContent_AssemblesFromFragments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run")
	store := NewStore(dir)
	dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outputRoot,
	})
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()
	stageScaffoldYAMLs(t, dir, sess.Plan)

	fragments := map[string]string{
		"root/intro":                              "synth showcase intro",
		"env/0/intro":                             "AI Agent tier",
		"env/1/intro":                             "Remote CDE tier",
		"env/2/intro":                             "Local tier",
		"env/3/intro":                             "Stage tier",
		"env/4/intro":                             "Small Prod tier",
		"env/5/intro":                             "HA Prod tier",
		"codebase/api/intro":                      "api intro",
		"codebase/api/integration-guide":          "1. Bind to 0.0.0.0",
		"codebase/api/knowledge-base":             "- **x** — because Y",
		"codebase/api/claude-md/service-facts":    "port 3000",
		"codebase/api/claude-md/notes":            "dev loop",
		"codebase/app/intro":                      "app intro",
		"codebase/app/integration-guide":          "1. Bind to 0.0.0.0",
		"codebase/app/knowledge-base":             "- **x** — because Y",
		"codebase/app/claude-md/service-facts":    "port 5173",
		"codebase/app/claude-md/notes":            "dev loop",
		"codebase/worker/intro":                   "worker intro",
		"codebase/worker/integration-guide":       "1. queue group",
		"codebase/worker/knowledge-base":          "- **x** — because Y",
		"codebase/worker/claude-md/service-facts": "worker queue",
		"codebase/worker/claude-md/notes":         "dev loop",
	}
	for id, body := range fragments {
		res := dispatch(t.Context(), store, RecipeInput{
			Action: "record-fragment", Slug: "synth-showcase",
			FragmentID: id, Fragment: body,
		})
		if !res.OK {
			t.Fatalf("record-fragment %s: %+v", id, res)
		}
	}

	res := dispatch(t.Context(), store, RecipeInput{
		Action: "stitch-content", Slug: "synth-showcase",
	})
	if !res.OK {
		t.Fatalf("stitch-content: %+v", res)
	}

	// Tier yamls on disk.
	for i := range Tiers() {
		tier, _ := TierAt(i)
		if _, err := os.Stat(filepath.Join(outputRoot, tier.Folder, "import.yaml")); err != nil {
			t.Errorf("tier %d import.yaml missing: %v", i, err)
		}
	}

	// Find the api codebase's SourceRoot to locate its apps-repo outputs.
	var apiSourceRoot string
	for _, cb := range sess.Plan.Codebases {
		if cb.Hostname == "api" {
			apiSourceRoot = cb.SourceRoot
		}
	}
	if apiSourceRoot == "" {
		t.Fatal("api codebase missing from plan")
	}

	// Surfaces on disk — recipes-repo shape at outputRoot, apps-repo
	// shape at each SourceRoot.
	for _, want := range []string{
		filepath.Join(outputRoot, "README.md"),
		filepath.Join(outputRoot, "0 — AI Agent", "README.md"),
		filepath.Join(apiSourceRoot, "README.md"),
		filepath.Join(apiSourceRoot, "CLAUDE.md"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("missing surface %s: %v", want, err)
		}
	}

	// Root README carries the intro fragment.
	rootBody, _ := os.ReadFile(filepath.Join(outputRoot, "README.md"))
	if !strings.Contains(string(rootBody), "synth showcase intro") {
		t.Errorf("root README missing intro fragment:\n%s", rootBody)
	}

	// Per-codebase README carries integration-guide + knowledge-base bodies.
	apiREADME, _ := os.ReadFile(filepath.Join(apiSourceRoot, "README.md"))
	if !strings.Contains(string(apiREADME), "1. Bind to 0.0.0.0") {
		t.Errorf("api README missing IG body:\n%s", apiREADME)
	}
	if !strings.Contains(string(apiREADME), "**x** — because Y") {
		t.Errorf("api README missing KB body:\n%s", apiREADME)
	}
}
