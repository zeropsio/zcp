package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

// writeRootREADME writes a root README.md with the given body to a temp dir.
func writeRootREADME(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// multiCodebaseShowcasePlan builds a showcase plan with 3 runtime codebases
// (apidev, appdev, workerdev) + managed services — the v22 shape.
func multiCodebaseShowcasePlan() *workflow.RecipePlan {
	return &workflow.RecipePlan{
		Framework: "nestjs",
		Tier:      workflow.RecipeTierShowcase,
		Slug:      "nestjs-showcase",
		Targets: []workflow.RecipeTarget{
			{Hostname: "apidev", Type: "nodejs@24"},
			{Hostname: "appdev", Type: "static", DevBase: "nodejs@24"},
			{Hostname: "workerdev", Type: "nodejs@24", IsWorker: true},
			{Hostname: "db", Type: "postgresql@18"},
			{Hostname: "queue", Type: "nats@2.12"},
		},
	}
}

func TestCheckArchitectureNarrative_ShowcaseMultiCodebase_Passes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plan := multiCodebaseShowcasePlan()

	writeRootREADME(t, dir, `# NestJS Showcase

Intro.

## Architecture

Three codebases, one project.

- **apidev** — NestJS API. Publishes jobs to NATS; serves CRUD + search endpoints to appdev over CORS.
- **appdev** — Svelte SPA. Calls apidev over DEV_API_URL; never speaks to NATS, DB, or the worker directly.
- **workerdev** — NATS consumer in the workers queue group. Subscribes to jobs.process, writes results back to the shared db.

## Environments

- AI Agent
- Remote (CDE)
`)

	checks := checkArchitectureNarrative(dir, plan)
	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d: %+v", len(checks), checks)
	}
	if checks[0].Status != statusPass {
		t.Errorf("expected pass, got %s: %s", checks[0].Status, checks[0].Detail)
	}
	if checks[0].Name != "recipe_architecture_narrative" {
		t.Errorf("unexpected name: %s", checks[0].Name)
	}
}

func TestCheckArchitectureNarrative_ShowcaseMultiCodebase_MissingSection_Fails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plan := multiCodebaseShowcasePlan()

	// v22 shape: root README lists environments but has no architecture section.
	writeRootREADME(t, dir, `# NestJS Showcase

A NestJS recipe.

## Environments

- AI Agent
- Local
- Stage
- Small Production
`)

	checks := checkArchitectureNarrative(dir, plan)
	if len(checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(checks))
	}
	if checks[0].Status != statusFail {
		t.Errorf("expected fail for missing section, got %s", checks[0].Status)
	}
	for _, needle := range []string{"architecture-narrative section", "apidev", "appdev", "workerdev", "publish"} {
		if !strings.Contains(checks[0].Detail, needle) {
			t.Errorf("detail missing %q: %s", needle, checks[0].Detail)
		}
	}
}

func TestCheckArchitectureNarrative_MissingHostname_Fails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plan := multiCodebaseShowcasePlan()

	// Architecture section exists but doesn't name workerdev — one of
	// the three hostnames is missing.
	writeRootREADME(t, dir, `# NestJS Showcase

## Architecture

- **apidev** — publishes jobs.
- **appdev** — consumes API over CORS.

## Environments

- stage
`)

	checks := checkArchitectureNarrative(dir, plan)
	if checks[0].Status != statusFail {
		t.Errorf("expected fail for missing hostname, got %s: %s", checks[0].Status, checks[0].Detail)
	}
	if !strings.Contains(checks[0].Detail, "workerdev") {
		t.Errorf("detail must name missing workerdev: %s", checks[0].Detail)
	}
}

func TestCheckArchitectureNarrative_NoContractVerbs_Fails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plan := multiCodebaseShowcasePlan()

	// Section names all hostnames but uses no contract verbs — pure
	// description without inter-service interaction language.
	writeRootREADME(t, dir, `# NestJS Showcase

## Architecture

- apidev is the backend.
- appdev is the frontend.
- workerdev is the background processor.

## Environments
`)

	checks := checkArchitectureNarrative(dir, plan)
	if checks[0].Status != statusFail {
		t.Errorf("expected fail for no contract verbs, got %s", checks[0].Status)
	}
	for _, needle := range []string{"contract", "publish", "consume"} {
		if !strings.Contains(checks[0].Detail, needle) {
			t.Errorf("detail missing %q: %s", needle, checks[0].Detail)
		}
	}
}

func TestCheckArchitectureNarrative_ServiceTopologyHeader_Passes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plan := multiCodebaseShowcasePlan()

	// Alternative header phrasing: "Service Topology" should also match.
	writeRootREADME(t, dir, `# NestJS Showcase

## Service Topology

- **apidev** routes HTTP requests and publishes NATS messages.
- **appdev** calls apidev via DEV_API_URL.
- **workerdev** subscribes to jobs.process.

Body.
`)

	checks := checkArchitectureNarrative(dir, plan)
	if checks[0].Status != statusPass {
		t.Errorf("expected pass with Service Topology header, got %s: %s", checks[0].Status, checks[0].Detail)
	}
}

func TestCheckArchitectureNarrative_SingleCodebaseShowcase_SkipsCheck(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plan := &workflow.RecipePlan{
		Tier: workflow.RecipeTierShowcase,
		Targets: []workflow.RecipeTarget{
			{Hostname: "app", Type: "nodejs@24"},
			{Hostname: "db", Type: "postgresql@18"}, // managed service, doesn't count
		},
	}
	writeRootREADME(t, dir, `# Recipe`)

	checks := checkArchitectureNarrative(dir, plan)
	if len(checks) != 0 {
		t.Errorf("single-codebase showcase should skip check entirely, got %+v", checks)
	}
}

func TestCheckArchitectureNarrative_MinimalTier_SkipsCheck(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plan := &workflow.RecipePlan{
		Tier: workflow.RecipeTierMinimal,
		Targets: []workflow.RecipeTarget{
			{Hostname: "apidev", Type: "nodejs@24"},
			{Hostname: "appdev", Type: "static", DevBase: "nodejs@24"},
		},
	}
	writeRootREADME(t, dir, `# Minimal`)

	checks := checkArchitectureNarrative(dir, plan)
	if len(checks) != 0 {
		t.Errorf("minimal tier should skip check, got %+v", checks)
	}
}

func TestCheckArchitectureNarrative_MissingRootREADME_Fails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // empty
	plan := multiCodebaseShowcasePlan()

	checks := checkArchitectureNarrative(dir, plan)
	if checks[0].Status != statusFail {
		t.Errorf("expected fail for missing README.md")
	}
	if !strings.Contains(checks[0].Detail, "not found") {
		t.Errorf("detail should say not found: %s", checks[0].Detail)
	}
}

func TestCheckArchitectureNarrative_SharedCodebaseWorkerDoesNotCount(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// A worker that shares its codebase with apidev (e.g., Laravel + Horizon
	// in one repo) should NOT count as a separate codebase. The plan has 2
	// runtime targets but only 1 distinct codebase — the check must skip.
	plan := &workflow.RecipePlan{
		Tier: workflow.RecipeTierShowcase,
		Targets: []workflow.RecipeTarget{
			{Hostname: "app", Type: "nodejs@24"},
			{Hostname: "worker", Type: "nodejs@24", IsWorker: true, SharesCodebaseWith: "app"},
			{Hostname: "db", Type: "postgresql@18"},
		},
	}
	writeRootREADME(t, dir, `# Minimal`)

	checks := checkArchitectureNarrative(dir, plan)
	if len(checks) != 0 {
		t.Errorf("shared-codebase worker + single app = 1 codebase; check should skip. Got %+v", checks)
	}
}

func TestRuntimeCodebaseHostnames_Sorted(t *testing.T) {
	t.Parallel()
	plan := &workflow.RecipePlan{
		Targets: []workflow.RecipeTarget{
			{Hostname: "workerdev", Type: "nodejs@24", IsWorker: true},
			{Hostname: "apidev", Type: "nodejs@24"},
			{Hostname: "appdev", Type: "static", DevBase: "nodejs@24"},
			{Hostname: "db", Type: "postgresql@18"},
		},
	}
	got := runtimeCodebaseHostnames(plan)
	want := []string{"apidev", "appdev", "workerdev"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
