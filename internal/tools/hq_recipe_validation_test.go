package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestGroupRecipeScaling_FlatMinCPU_RefusesBeforeProposal(t *testing.T) {
	t.Parallel()
	lab := newRecipeLab(t)
	lab.schemas = newRecipeSchemaCache(t, false)
	lab.hq.seed(hq.RecipeRepo)
	if outcome := lab.proposeRecipe(flipRecipeClient(2)); outcome.Change != 1 {
		t.Fatalf("the valid flip shape was not proposed: %+v", outcome)
	}
	lab.hq.mergeChange(hq.RecipeRepo, 1)
	const path = "3 — Stage/import.yaml"
	files := lab.hq.recipeFiles("main")
	body := strings.Replace(files[path], "hostname: search\n", "hostname: search\n    minCpu: 1\n", 1)
	if body == files[path] {
		t.Fatal("fixture did not locate search")
	}
	lab.hq.landOnMain(hq.RecipeRepo, map[string]string{path: body})
	main := lab.hq.recipeHead("main")
	answer, text := lab.proposeScaling(flipRecipeClient(4), "search")
	if answer != nil {
		t.Fatalf("invalid tier was proposed: %s", text)
	}
	for _, want := range []string{"search", path, "additionalProperties", "minCpu", "correction"} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal must name %q: %s", want, text)
		}
	}
	if lab.hq.changeIn(hq.RecipeRepo, 2) != nil {
		t.Error("invalid tier opened a proposal")
	}
	if lab.hq.recipeHead("main") != main {
		t.Error("invalid tier moved main")
	}
}

func TestGroupRecipe_FlipShape_ProposesAgainstPublishedSchema(t *testing.T) {
	t.Parallel()
	lab := newRecipeLab(t)
	lab.schemas = newRecipeSchemaCache(t, false)

	lab.hq.seed(hq.RecipeRepo)
	result, _, err := handleGroupRecipe(t.Context(), flipRecipeClient(2), lab.hq.srv.Client(), lab.rt, lab.stateDir, lab.schemas)
	if err != nil || result.IsError {
		t.Fatalf("group recipe failed: %v %s", err, getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, `"committed":true`) {
		t.Fatalf("nested flip shape was not proposed: %s", text)
	}
	files := lab.hq.recipeFiles(recipeBranch(1))
	for _, path := range hq.RecipeTierPaths {
		body := files[path]
		for _, want := range []string{"verticalAutoscaling:", "type: object-storage", "objectStorageSize: 1"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s lacks %q: %s", path, want, body)
			}
		}
	}
}

func TestGroupRecipe_SchemaUnavailable_ReportsUnavailableWithoutProposal(t *testing.T) {
	t.Parallel()
	lab := newRecipeLab(t)
	lab.hq.seed(hq.RecipeRepo)
	result, _, err := handleGroupRecipe(t.Context(), lab.mock, lab.hq.srv.Client(), lab.rt, lab.stateDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "validation unavailable") || strings.Contains(text, "invalid recipe") {
		t.Fatalf("unavailable publication must have an unavailable verdict: %s", text)
	}
	if lab.hq.changeIn(hq.RecipeRepo, 1) != nil {
		t.Fatal("unavailable validation opened a proposal")
	}
}

// flipRecipeClient keeps the real nested storage/scaling shape on the proposal path.
func flipRecipeClient(searchRAM float64) *platform.Mock {
	services := []platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/nodejs@24", ServiceStackTypeCategoryName: "USER"}, CustomAutoscaling: &platform.CustomAutoscaling{CPUMode: "SHARED", MinCPU: 1, MaxCPU: 8, MinRAM: 0.125, MaxRAM: 48}},
		{ID: "svc-appstage", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/nodejs@24", ServiceStackTypeCategoryName: "USER"}, CustomAutoscaling: &platform.CustomAutoscaling{CPUMode: "SHARED", MinCPU: 1, MaxCPU: 8, MinRAM: 0.125, MaxRAM: 48}},
		{ID: "svc-storage", Name: "storage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "object-storage", ServiceStackTypeCategoryName: "USER"}},
		{ID: "svc-search", Name: "search", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "meilisearch:single@1.44", ServiceStackTypeCategoryName: "USER"}, CustomAutoscaling: &platform.CustomAutoscaling{CPUMode: "SHARED", MinCPU: 1, MaxCPU: 8, MinRAM: searchRAM, MaxRAM: 48, MinFreeRAMGB: 1}},
	}
	return platform.NewMock().WithProject(&platform.Project{ID: labMate, Name: "flip.studio", Status: "ACTIVE"}).WithServicesDirect(services).WithServices(services).
		WithServiceEnv("svc-storage", []platform.ServiceEnvVar{{Key: "quotaGBytes", Content: "1"}}).
		WithServiceExportYAML("services:\n  - hostname: storage\n    type: object-storage\n    objectStorageSize: 1\n")
}
