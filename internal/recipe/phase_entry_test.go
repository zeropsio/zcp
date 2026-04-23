package recipe

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

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
	patch1, err := json.Marshal(Plan{Framework: "synth", Tier: "showcase"})
	if err != nil {
		t.Fatal(err)
	}
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "update-plan", Slug: "synth-showcase", Plan: patch1,
	})
	if !res.OK {
		t.Fatalf("update-plan #1: %+v", res)
	}

	// Second patch — research + codebases + services (plan from fixture).
	syn := syntheticShowcasePlan()
	patch2, err := json.Marshal(Plan{
		Research:  syn.Research,
		Codebases: syn.Codebases,
		Services:  syn.Services,
	})
	if err != nil {
		t.Fatal(err)
	}
	res = dispatch(t.Context(), store, RecipeInput{
		Action: "update-plan", Slug: "synth-showcase", Plan: patch2,
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

func TestDispatch_StitchContent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase",
		OutputRoot: filepath.Join(dir, "run"),
	})

	payload := []byte(`{"root_readme":"hello","env_readmes":{"0":"env0"},"env_import_comments":{},"codebase_readmes":{},"codebase_claude":{},"codebase_zerops_yaml_comments":{},"citations":{},"manifest":{"surface_counts":{}}}`)
	res := dispatch(t.Context(), store, RecipeInput{
		Action: "stitch-content", Slug: "synth-showcase", Payload: payload,
	})
	if !res.OK {
		t.Fatalf("stitch-content: %+v", res)
	}
	if res.StitchedPath == "" {
		t.Error("StitchedPath empty after successful stitch")
	}
}
