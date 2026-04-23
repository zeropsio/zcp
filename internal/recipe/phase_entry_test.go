package recipe

import (
	"os"
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

func TestDispatch_StitchContent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := NewStore(dir)
	dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase",
		OutputRoot: filepath.Join(dir, "run"),
	})

	payload := map[string]any{
		"root_readme": "hello", "env_readmes": map[string]any{"0": "env0"},
		"env_import_comments": map[string]any{}, "codebase_readmes": map[string]any{},
		"codebase_claude": map[string]any{}, "codebase_zerops_yaml_comments": map[string]any{},
		"citations": map[string]any{}, "manifest": map[string]any{"surface_counts": map[string]any{}},
	}
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

// TestDispatch_StitchContent_MergesEnvFieldsAndRegenerates pins the
// real stitch contract: writer's env_import_comments + project_env_vars
// merge into the plan, all 6 deliverable yamls land on disk with the
// merged content, and writer-owned surface bodies (root README, env
// README, per-codebase README + CLAUDE.md) land at their canonical
// paths.
func TestDispatch_StitchContent_MergesEnvFieldsAndRegenerates(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run")
	store := NewStore(dir)
	dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outputRoot,
	})
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()

	payload := map[string]any{
		"root_readme": "# synth showcase\n\nroot body.",
		"env_readmes": map[string]any{
			"0": "# Env 0 — AI Agent\n",
			"5": "# Env 5 — HA Production\n",
		},
		"env_import_comments": map[string]any{
			"0": map[string]any{
				"project": "AI agent workspace — new.",
				"service": map[string]any{"apidev": "API dev slot (stitched)."},
			},
		},
		"project_env_vars": map[string]any{
			"0": map[string]any{
				"DEV_API_URL": "https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			},
			"5": map[string]any{
				"STAGE_API_URL": "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app",
			},
		},
		"codebase_readmes": map[string]any{
			"api": map[string]any{
				"integration_guide": "## IG for api\n- bind 0.0.0.0",
				"gotchas":           "## Gotchas\n- self-shadow",
			},
		},
		"codebase_claude": map[string]any{
			"api": "# CLAUDE.md for api\ndev loop...",
		},
		"citations": map[string]any{},
		"manifest":  map[string]any{"surface_counts": map[string]any{}},
	}

	res := dispatch(t.Context(), store, RecipeInput{
		Action: "stitch-content", Slug: "synth-showcase", Payload: payload,
	})
	if !res.OK {
		t.Fatalf("stitch-content: %+v", res)
	}

	// Plan merges: EnvComments["0"].Service["apidev"] should reflect the
	// stitched writer payload, not the synthetic fixture's old value.
	if got := sess.Plan.EnvComments["0"].Service["apidev"]; got != "API dev slot (stitched)." {
		t.Errorf("EnvComments[0].apidev = %q, want stitched value", got)
	}
	if got := sess.Plan.ProjectEnvVars["5"]["STAGE_API_URL"]; got == "" {
		t.Error("ProjectEnvVars[5].STAGE_API_URL not merged")
	}

	// Deliverable yamls on disk with merged content.
	for i := range Tiers() {
		tier, _ := TierAt(i)
		path := filepath.Join(outputRoot, tier.Folder, "import.yaml")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("tier %d: import.yaml not on disk: %v", i, err)
		}
	}
	tier0 := filepath.Join(outputRoot, "0 — AI Agent", "import.yaml")
	body, err := os.ReadFile(tier0)
	if err != nil {
		t.Fatalf("read tier 0: %v", err)
	}
	if !strings.Contains(string(body), "DEV_API_URL") {
		t.Errorf("tier 0 missing DEV_API_URL from project_env_vars:\n%s", string(body))
	}
	if !strings.Contains(string(body), "API dev slot (stitched).") {
		t.Errorf("tier 0 missing stitched env comment:\n%s", string(body))
	}
	if !strings.Contains(string(body), "${zeropsSubdomainHost}") {
		t.Errorf("tier 0 lost ${zeropsSubdomainHost} literal (template leak):\n%s", string(body))
	}

	// Content surfaces on disk.
	for _, want := range []string{
		filepath.Join(outputRoot, "README.md"),
		filepath.Join(outputRoot, "0 — AI Agent", "README.md"),
		filepath.Join(outputRoot, "codebases", "api", "README.md"),
		filepath.Join(outputRoot, "codebases", "api", "CLAUDE.md"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("missing surface %s: %v", want, err)
		}
	}
	// Fragment markers on per-codebase README.
	cbBody, _ := os.ReadFile(filepath.Join(outputRoot, "codebases", "api", "README.md"))
	if !strings.Contains(string(cbBody), "integration-guide-start") {
		t.Errorf("codebase README missing IG marker:\n%s", string(cbBody))
	}
	if !strings.Contains(string(cbBody), "knowledge-base-start") {
		t.Errorf("codebase README missing KB marker:\n%s", string(cbBody))
	}
}
