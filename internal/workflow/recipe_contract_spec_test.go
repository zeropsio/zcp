package workflow

import (
	"strings"
	"testing"
)

// TestGenerateContractSpec_Showcase_HasAllFourSections — v8.86 §3.5. The
// contract spec is the cross-codebase binding the scaffold sub-agents
// consult before authoring, so the spec template must carry the four
// categories that produced v23 CRITs (response shape, DB schema, NATS
// queue group, graceful shutdown) even when a given plan exercises only
// a subset.
func TestGenerateContractSpec_Showcase_HasAllFourSections(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{
		Tier: RecipeTierShowcase,
		Targets: []RecipeTarget{
			{Hostname: "api", Type: "nodejs@22", Role: RecipeRoleAPI},
			{Hostname: "app", Type: "static", DevBase: "nodejs@22", Role: RecipeRoleApp},
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true},
			{Hostname: "db", Type: "postgresql@17"},
			{Hostname: "queue", Type: "nats@2.10"},
		},
	}

	spec, err := GenerateContractSpec(plan)
	if err != nil {
		t.Fatalf("GenerateContractSpec: %v", err)
	}
	if spec == "" {
		t.Fatal("empty contract spec")
	}

	required := []string{
		"contract_spec:",
		"http_endpoints:",
		"database_tables:",
		"nats_subjects:",
		"graceful_shutdown:",
		"/api/status",
		"queue_group",
	}
	for _, needle := range required {
		if !strings.Contains(spec, needle) {
			t.Errorf("contract spec missing %q:\n%s", needle, spec)
		}
	}
}

func TestGenerateContractSpec_Minimal_OmitsWorkerSections(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{
		Tier: RecipeTierMinimal,
		Targets: []RecipeTarget{
			{Hostname: "app", Type: "nodejs@22", Role: RecipeRoleApp},
			{Hostname: "db", Type: "postgresql@17"},
		},
	}
	spec, err := GenerateContractSpec(plan)
	if err != nil {
		t.Fatalf("GenerateContractSpec: %v", err)
	}
	// Minimal with no worker and no queue: NATS section must not populate
	// a queue_group; graceful_shutdown section may be absent entirely.
	if strings.Contains(spec, "queue_group:") {
		t.Errorf("minimal recipe with no worker should not carry queue_group lines:\n%s", spec)
	}
}

func TestGenerateContractSpec_DualRuntime_NamesConsumers(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{
		Tier: RecipeTierShowcase,
		Targets: []RecipeTarget{
			{Hostname: "api", Type: "nodejs@22", Role: RecipeRoleAPI},
			{Hostname: "app", Type: "static", DevBase: "nodejs@22", Role: RecipeRoleApp},
		},
	}
	spec, err := GenerateContractSpec(plan)
	if err != nil {
		t.Fatalf("GenerateContractSpec: %v", err)
	}
	// /api/status must name the frontend component that consumes it.
	if !strings.Contains(spec, "appdev/") && !strings.Contains(spec, "app/") {
		t.Errorf("spec should name appdev/ as consumer of /api/status:\n%s", spec)
	}
}

func TestGenerateContractSpec_NilPlan_Errors(t *testing.T) {
	t.Parallel()
	if _, err := GenerateContractSpec(nil); err == nil {
		t.Error("expected error on nil plan")
	}
}

func TestGenerateContractSpec_IsDeterministic(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{
		Tier: RecipeTierShowcase,
		Targets: []RecipeTarget{
			{Hostname: "api", Type: "nodejs@22", Role: RecipeRoleAPI},
			{Hostname: "app", Type: "static", DevBase: "nodejs@22", Role: RecipeRoleApp},
		},
	}
	a, err := GenerateContractSpec(plan)
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateContractSpec(plan)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Error("GenerateContractSpec must be deterministic for the same plan")
	}
}
