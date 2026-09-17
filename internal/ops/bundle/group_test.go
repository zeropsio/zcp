package bundle

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/recipe"
	"github.com/zeropsio/zcp/internal/topology"
)

// groupFiles renders a composed layout through the published recipe layout —
// the only shape these tiers are ever written in.
func groupFiles(t *testing.T, layout recipe.Layout) map[string]string {
	t.Helper()
	built, err := recipe.Build(layout)
	if err != nil {
		t.Fatalf("recipe.Build: %v", err)
	}
	out := make(map[string]string, len(built))
	for _, f := range built {
		out[f.Path] = f.Body
	}
	return out
}

const groupZeropsYAML = `zerops:
  - setup: api
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
      deployFiles: ./
    run:
      base: nodejs@22
      start: npm start
`

func groupInputsFixture() GroupRecipeInputs {
	return GroupRecipeInputs{
		Name:            "acme",
		Title:           "Acme",
		MateProjectName: "acme-mate-1",
		Runtimes: []GroupRuntime{{
			DevHostname:      "apidev",
			StageHostname:    "apistage",
			ServiceType:      "nodejs@22",
			RepoURL:          "https://git.example.com/acme/apidev.git",
			SetupName:        "api",
			ZeropsYAMLBody:   groupZeropsYAML,
			SubdomainEnabled: true,
			Scaling:          &Scaling{MinContainers: 1, MaxContainers: 1, CPUMode: "SHARED", MinCPU: 1, MaxCPU: 4},
		}},
		ManagedServices: []ManagedServiceEntry{{Hostname: "db", Type: "postgresql:single@18", Profile: "oltp-nano"}},
		ProjectEnvs:     []ProjectEnvVar{{Key: "APP_ENV", Value: "dev"}},
	}
}

// tierDoc unmarshals the import.yaml of the tier with the given title.
func tierDoc(t *testing.T, files map[string]string, dir string) map[string]any {
	t.Helper()
	body, ok := files[dir+"/import.yaml"]
	if !ok {
		t.Fatalf("no import.yaml under %q; have %v", dir, mapKeys(files))
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("tier %q import.yaml does not parse: %v", dir, err)
	}
	return doc
}

func mapKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// serviceEntries indexes a tier's services[] by hostname.
func serviceEntries(t *testing.T, doc map[string]any) map[string]map[string]any {
	t.Helper()
	raw, ok := doc["services"].([]any)
	if !ok {
		t.Fatalf("tier has no services[]: %#v", doc)
	}
	out := map[string]map[string]any{}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("services[] entry is not a mapping: %#v", item)
		}
		host, _ := entry["hostname"].(string)
		out[host] = entry
	}
	return out
}

func TestBuildGroupRecipe_Rejects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*GroupRecipeInputs)
		wantErr string
	}{
		{"no name", func(in *GroupRecipeInputs) { in.Name = "" }, "Name required"},
		{"no runtime", func(in *GroupRecipeInputs) { in.Runtimes = nil }, "at least one runtime"},
		{"no hostname", func(in *GroupRecipeInputs) { in.Runtimes[0].DevHostname = "" }, "DevHostname required"},
		{"no type", func(in *GroupRecipeInputs) { in.Runtimes[0].ServiceType = "" }, "ServiceType required"},
		{"no repo", func(in *GroupRecipeInputs) { in.Runtimes[0].RepoURL = "" }, "RepoURL required"},
		{"no setup", func(in *GroupRecipeInputs) { in.Runtimes[0].SetupName = "" }, "SetupName required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := groupInputsFixture()
			tc.mutate(&in)
			_, _, err := BuildGroupRecipe(in, nil)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestBuildGroupRecipe_EmitsThreeTiers(t *testing.T) {
	t.Parallel()
	layout, _, err := BuildGroupRecipe(groupInputsFixture(), nil)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	if layout.Name != "acme" {
		t.Errorf("layout.Name = %q, want acme", layout.Name)
	}
	want := []struct {
		index       int
		title, slug string
	}{
		{0, "AI Agent", "ai-agent"},
		{3, "Stage", "stage"},
		{4, "Small Production", "small-production"},
	}
	if len(layout.Tiers) != len(want) {
		t.Fatalf("tiers = %d, want %d", len(layout.Tiers), len(want))
	}
	for i, w := range want {
		got := layout.Tiers[i]
		if got.Index != w.index || got.Title != w.title || got.Slug != w.slug {
			t.Errorf("tier[%d] = {%d %q %q}, want {%d %q %q}", i, got.Index, got.Title, got.Slug, w.index, w.title, w.slug)
		}
		if strings.TrimSpace(got.ImportYAML) == "" {
			t.Errorf("tier[%d] %q has no import yaml", i, got.Title)
		}
	}
}

// The AI Agent tier is the MATE's project (D12): both halves of every pair,
// scaling reproduced verbatim, managed deps as they run.
func TestBuildGroupRecipe_AIAgentTierIsTheMatesProject(t *testing.T) {
	t.Parallel()
	layout, _, err := BuildGroupRecipe(groupInputsFixture(), nil)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	files := groupFiles(t, layout)
	doc := tierDoc(t, files, "0 — AI Agent")
	project, _ := doc["project"].(map[string]any)
	if name, _ := project["name"].(string); name != "acme-mate-1" {
		t.Errorf("project.name = %q, want the Mate's own project name", name)
	}
	services := serviceEntries(t, doc)
	for _, host := range []string{"apidev", "apistage", "db"} {
		if _, ok := services[host]; !ok {
			t.Errorf("service %q missing; have %v", host, mapKeys(map[string]string{}))
		}
	}
	dev := services["apidev"]
	if got, _ := dev["buildFromGit"].(string); got != "https://git.example.com/acme/apidev" {
		t.Errorf("buildFromGit = %q, want the canonical clone URL", got)
	}
	if got, _ := dev["zeropsSetup"].(string); got != "api" {
		t.Errorf("zeropsSetup = %q, want api", got)
	}
	if got, _ := dev["minContainers"].(int); got != 1 {
		t.Errorf("minContainers = %d, want the source's 1 (identity)", got)
	}
	if _, promoted := services["db"]["type"].(string); !promoted {
		t.Fatalf("db has no type")
	}
	if got, _ := services["db"]["type"].(string); got != "postgresql:single@18" {
		t.Errorf("db type = %q, want the source type verbatim", got)
	}
}

// A group's stage and production run what the Mate's stage half runs. A pair
// records its halves' setups apart — dev and prod — and the group tiers named
// the dev half's: a stage deployed the dev setup, or the broker refused it for
// a setup the zerops.yaml did not have (measured 2026-09-17).
func TestBuildGroupRecipe_GroupEnvironmentsBuildTheStageHalfsSetup(t *testing.T) {
	t.Parallel()
	in := groupInputsFixture()
	in.Runtimes[0].SetupName = "dev"
	in.Runtimes[0].StageSetupName = "prod"
	in.Runtimes[0].ZeropsYAMLBody = "zerops:\n  - setup: dev\n    run:\n      base: nodejs@22\n  - setup: prod\n    run:\n      base: nodejs@22\n"
	layout, _, err := BuildGroupRecipe(in, nil)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	files := groupFiles(t, layout)
	agent := serviceEntries(t, tierDoc(t, files, "0 — AI Agent"))
	if got, _ := agent["apidev"]["zeropsSetup"].(string); got != "dev" {
		t.Errorf("AI Agent apidev zeropsSetup = %q, want dev", got)
	}
	if got, _ := agent["apistage"]["zeropsSetup"].(string); got != "prod" {
		t.Errorf("AI Agent apistage zeropsSetup = %q, want prod", got)
	}
	for _, dir := range []string{"3 — Stage", "4 — Small Production"} {
		services := serviceEntries(t, tierDoc(t, files, dir))
		if got, _ := services["api"]["zeropsSetup"].(string); got != "prod" {
			t.Errorf("%s api zeropsSetup = %q, want the stage half's prod", dir, got)
		}
	}
}

// Stage and Small Production are the production transform: the mode suffix
// stripped, one entry per pair, an HA floor on production.
func TestBuildGroupRecipe_ProductionTransform(t *testing.T) {
	t.Parallel()
	layout, _, err := BuildGroupRecipe(groupInputsFixture(), nil)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	files := groupFiles(t, layout)

	tests := []struct {
		dir              string
		wantProject      string
		wantMinContainer int
		wantCPUMode      string
		wantManagedType  string
	}{
		{"3 — Stage", "acme stage", 1, "SHARED", "postgresql:single@18"},
		{"4 — Small Production", "acme production", 2, "DEDICATED", "postgresql:ha@18"},
	}
	for _, tc := range tests {
		t.Run(tc.dir, func(t *testing.T) {
			t.Parallel()
			doc := tierDoc(t, files, tc.dir)
			project, _ := doc["project"].(map[string]any)
			if name, _ := project["name"].(string); name != tc.wantProject {
				t.Errorf("project.name = %q, want %q", name, tc.wantProject)
			}
			services := serviceEntries(t, doc)
			if _, ok := services["apidev"]; ok {
				t.Errorf("%q still carries the dev-half hostname", tc.dir)
			}
			api, ok := services["api"]
			if !ok {
				t.Fatalf("no renamed runtime %q", "api")
			}
			if got, _ := api["buildFromGit"].(string); got == "" {
				t.Error("buildFromGit dropped — a recipe tier builds from its service repo (4.3 converts it)")
			}
			if got, _ := api["zeropsSetup"].(string); got != "api" {
				t.Errorf("zeropsSetup = %q, want api", got)
			}
			if got, _ := api["minContainers"].(int); got != tc.wantMinContainer {
				t.Errorf("minContainers = %d, want %d", got, tc.wantMinContainer)
			}
			va, _ := api["verticalAutoscaling"].(map[string]any)
			if got, _ := va["cpuMode"].(string); got != tc.wantCPUMode {
				t.Errorf("cpuMode = %q, want %q", got, tc.wantCPUMode)
			}
			if got, _ := services["db"]["type"].(string); got != tc.wantManagedType {
				t.Errorf("db type = %q, want %q", got, tc.wantManagedType)
			}
		})
	}
}

// Secrets never reach the recipe verbatim: export's classification decides,
// and an unclassified user-set service env collapses to the placeholder.
func TestBuildGroupRecipe_SecretsAreClassifiedNeverVerbatim(t *testing.T) {
	t.Parallel()
	in := groupInputsFixture()
	in.Runtimes[0].ServiceEnvs = []ProjectEnvVar{
		{Key: "STRIPE_KEY", Value: "sk_live_realvalue"},
		{Key: "SESSION_SECRET", Value: "old"},
		{Key: "LOG_LEVEL", Value: "debug"},
	}
	classifications := map[string]topology.SecretClassification{
		"SESSION_SECRET": topology.SecretClassAutoSecret,
		"LOG_LEVEL":      topology.SecretClassPlainConfig,
	}
	layout, _, err := BuildGroupRecipe(in, classifications)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	for _, tier := range layout.Tiers {
		if strings.Contains(tier.ImportYAML, "sk_live_realvalue") {
			t.Fatalf("tier %q leaks an unclassified service env value", tier.Title)
		}
		if !strings.Contains(tier.ImportYAML, ExternalSecretPlaceholder) {
			t.Errorf("tier %q: unclassified secret did not collapse to %q", tier.Title, ExternalSecretPlaceholder)
		}
		if !strings.Contains(tier.ImportYAML, autoSecretPreprocessor) {
			t.Errorf("tier %q: auto-secret env did not emit the generator directive", tier.Title)
		}
		if !strings.HasPrefix(tier.ImportYAML, preprocessorHeader) {
			t.Errorf("tier %q: a generator directive without the preprocessor header on line 1", tier.Title)
		}
		if !strings.Contains(tier.ImportYAML, "debug") {
			t.Errorf("tier %q: plain-config env dropped", tier.Title)
		}
	}
}

// The same project composes to the same bytes — a recipe that rewrites itself
// on every pass makes a pull request that says nothing.
func TestBuildGroupRecipe_Deterministic(t *testing.T) {
	t.Parallel()
	in := groupInputsFixture()
	in.Runtimes = append(in.Runtimes, GroupRuntime{
		DevHostname: "workerdev", StageHostname: "workerstage", ServiceType: "nodejs@22",
		RepoURL: "https://git.example.com/acme/workerdev.git", SetupName: "api",
		ZeropsYAMLBody: groupZeropsYAML,
	})
	first, _, err := BuildGroupRecipe(in, nil)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	// Re-order the inputs: composition sorts, so the bytes must not move.
	in.Runtimes[0], in.Runtimes[1] = in.Runtimes[1], in.Runtimes[0]
	second, _, err := BuildGroupRecipe(in, nil)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	for i := range first.Tiers {
		if first.Tiers[i].ImportYAML != second.Tiers[i].ImportYAML {
			t.Fatalf("tier %q is not deterministic under input order", first.Tiers[i].Title)
		}
	}
}

// A runtime whose zerops.yaml does not declare the setup it names is reported,
// never fatal: this composes unattended in a reconcile, with nobody to ask.
func TestBuildGroupRecipe_UnknownSetupWarnsNotFails(t *testing.T) {
	t.Parallel()
	in := groupInputsFixture()
	in.Runtimes[0].SetupName = "nope"
	layout, warnings, err := BuildGroupRecipe(in, nil)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	if len(layout.Tiers) != 3 {
		t.Fatalf("tiers = %d, want 3", len(layout.Tiers))
	}
	if !warningsContain(warnings, "nope") {
		t.Errorf("warnings = %v, want one naming the missing setup", warnings)
	}
}

func warningsContain(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}
