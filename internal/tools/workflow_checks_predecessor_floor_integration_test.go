package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// testKnowledgeProvider is a knowledge.Provider stub that only implements
// the two methods PredecessorGotchaStems actually calls (ListRecipes,
// GetRecipe). It keeps the integration test self-contained — the workflow
// package has a similar mock but it's unexported.
type testKnowledgeProvider struct {
	recipes map[string]string
}

func (p *testKnowledgeProvider) Get(string) (*knowledge.Document, error)     { return nil, nil } //nolint:nilnil // stub
func (p *testKnowledgeProvider) Search(string, int) []knowledge.SearchResult { return nil }
func (p *testKnowledgeProvider) GetCore() (string, error)                    { return "", nil }
func (p *testKnowledgeProvider) GetUniversals() (string, error)              { return "", nil }
func (p *testKnowledgeProvider) GetModel() (string, error)                   { return "", nil }
func (p *testKnowledgeProvider) GetBriefing(string, []string, string, []platform.ServiceStackType) (string, error) {
	return "", nil
}
func (p *testKnowledgeProvider) GetRecipe(name, _ string) (string, error) {
	if content, ok := p.recipes[name]; ok {
		return content, nil
	}
	return "", nil
}
func (p *testKnowledgeProvider) ListRecipes() []string {
	names := make([]string, 0, len(p.recipes))
	for name := range p.recipes {
		names = append(names, name)
	}
	return names
}

// TestCheckRecipeGenerate_ShowcaseFloor_DualRuntime_MixedPassFail exercises
// the full wiring surface of the predecessor-floor check:
//
//   - a non-nil knowledge.Provider (not the kp == nil test fast path)
//   - showcase tier gating (predecessor resolution must succeed)
//   - per-hostname name prefixing on the emitted check
//   - one target's README clones the predecessor (apidev — must fail)
//   - one target's README adds 3 net-new gotchas (appdev — must pass)
//
// The dual-runtime shape is the highest-leverage case: it proves the check
// can fail one codebase while passing another in the same run, which is
// exactly the mixed-quality pattern v10 produced (apidev cloned the
// predecessor, appdev was fine).
func TestCheckRecipeGenerate_ShowcaseFloor_DualRuntime_MixedPassFail(t *testing.T) {
	t.Parallel()

	kp := &testKnowledgeProvider{
		recipes: map[string]string{
			"nestjs-minimal": `# Nest.js Minimal

## Gotchas
- **No ` + "`.env`" + ` files on Zerops** — platform injects OS env vars.
- **TypeORM ` + "`synchronize: true`" + ` in production** — drops columns.
- **NestJS listens on ` + "`localhost`" + ` by default** — bind 0.0.0.0.
- **` + "`ts-node`" + ` needs devDependencies** — dev setup uses npm install.

## 1. Adding ` + "`zerops.yaml`" + `

` + "```yaml\nzerops:\n  - setup: prod\n    build:\n      base: nodejs@22\n```" + `
`,
		},
	}

	plan := &workflow.RecipePlan{
		Framework:   "nestjs",
		Tier:        workflow.RecipeTierShowcase,
		Slug:        "nestjs-showcase",
		RuntimeType: "nodejs@22",
		Research: workflow.ResearchData{
			ServiceType:    "nodejs",
			PackageManager: "npm",
			HTTPPort:       3000,
			BuildCommands:  []string{"npm ci"},
			DeployFiles:    []string{"dist"},
			StartCommand:   "node dist/main.js",
		},
		Targets: []workflow.RecipeTarget{
			{Hostname: "app", Type: "static", Role: "app"},
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true, SharesCodebaseWith: "api"},
			{Hostname: "db", Type: "postgresql@17"},
		},
	}

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// apidev README: clones the 4 predecessor gotchas verbatim → must fail.
	apidevREADME := readmeShellWith(apiZeropsYamlWithWorker,
		"No .env files on Zerops.",
		"TypeORM `synchronize: true` must never run in the application process.",
		"NestJS listens on `localhost` by default.",
		"ts-node requires devDependencies.",
	)

	// appdev README: 3 net-new gotchas unique to the frontend → must pass.
	appdevREADME := readmeShellWith(frontendZeropsYaml,
		"Static base only ships in prod — dev uses nodejs runtime.",
		"VITE_* vars are baked at build time, not runtime.",
		"Vite dev-server host check rejects unknown hosts — allow .zerops.app.",
	)

	appDir := filepath.Join(dir, "appdev")
	apiDir := filepath.Join(dir, "apidev")
	for _, d := range []string{appDir, apiDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(appDir, "zerops.yaml"), frontendZeropsYaml)
	writeFile(t, filepath.Join(appDir, "README.md"), appdevREADME)
	writeFile(t, filepath.Join(apiDir, "zerops.yaml"), apiZeropsYamlWithWorker)
	writeFile(t, filepath.Join(apiDir, "README.md"), apidevREADME)

	checker := checkRecipeGenerate(stateDir, nil, kp)
	result, err := checker(context.Background(), plan, testRecipeState())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	byName := make(map[string]workflow.StepCheck, len(result.Checks))
	for _, c := range result.Checks {
		byName[c.Name] = c
	}

	apiCheck, ok := byName["api_knowledge_base_exceeds_predecessor"]
	if !ok {
		t.Fatalf("missing api_knowledge_base_exceeds_predecessor — wiring did not reach apidev README. Got checks: %v", checkNames(result.Checks))
	}
	if apiCheck.Status != statusFail {
		t.Errorf("api_knowledge_base_exceeds_predecessor should fail (all 4 stems clone the predecessor), got %s: %s", apiCheck.Status, apiCheck.Detail)
	}

	appCheck, ok := byName["app_knowledge_base_exceeds_predecessor"]
	if !ok {
		t.Fatalf("missing app_knowledge_base_exceeds_predecessor — wiring did not reach appdev README")
	}
	if appCheck.Status != statusPass {
		t.Errorf("app_knowledge_base_exceeds_predecessor should pass (3 net-new gotchas), got %s: %s", appCheck.Status, appCheck.Detail)
	}

	// Final result: must be overall fail because apidev cloned the predecessor.
	if result.Passed {
		t.Error("expected overall fail because apidev README cloned the predecessor, but result.Passed=true")
	}
}

// TestCheckRecipeGenerate_ShowcaseFloor_SeparateCodebaseWorker — v11 shipped
// a workerdev README that cloned "No .env files on Zerops" verbatim from the
// predecessor and nothing caught it because the existing appTargets loop
// explicitly filters workers out (`!t.IsWorker`). Separate-codebase workers
// ship their own README and must also clear the predecessor-floor check.
// The floor is 3, same as the app/api loop. Shared-codebase workers (where
// `SharesCodebaseWith != ""`) don't have a standalone README and are skipped.
func TestCheckRecipeGenerate_ShowcaseFloor_SeparateCodebaseWorker(t *testing.T) {
	t.Parallel()

	kp := &testKnowledgeProvider{
		recipes: map[string]string{
			"nestjs-minimal": `# Nest.js Minimal

## Gotchas
- **No ` + "`.env`" + ` files on Zerops** — platform injects OS env vars.
- **TypeORM ` + "`synchronize: true`" + ` in production** — drops columns.
- **NestJS listens on ` + "`localhost`" + ` by default** — bind 0.0.0.0.
- **` + "`ts-node`" + ` needs devDependencies** — dev setup uses npm install.

## 1. Adding ` + "`zerops.yaml`" + `

` + "```yaml\nzerops:\n  - setup: prod\n    build:\n      base: nodejs@22\n```" + `
`,
		},
	}

	// Separate-codebase worker — SharesCodebaseWith is empty, so workerdev
	// owns its own repo and README.
	plan := &workflow.RecipePlan{
		Framework:   "nestjs",
		Tier:        workflow.RecipeTierShowcase,
		Slug:        "nestjs-showcase",
		RuntimeType: "nodejs@22",
		Research: workflow.ResearchData{
			ServiceType:    "nodejs",
			PackageManager: "npm",
			HTTPPort:       3000,
			BuildCommands:  []string{"npm ci"},
			DeployFiles:    []string{"dist"},
			StartCommand:   "node dist/main.js",
		},
		Targets: []workflow.RecipeTarget{
			{Hostname: "app", Type: "static", Role: "app"},
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true}, // SharesCodebaseWith empty
			{Hostname: "db", Type: "postgresql@17"},
		},
	}

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Healthy app + api READMEs (3 net-new each) so the final result's failure
	// is attributable to the worker alone.
	appdevREADME := readmeShellWith(frontendZeropsYaml,
		"Static base only ships in prod — dev uses nodejs runtime.",
		"VITE_* vars are baked at build time, not runtime.",
		"Vite dev-server host check rejects unknown hosts — allow .zerops.app.",
	)
	apidevREADME := readmeShellWith(apiZeropsYamlWithWorker,
		"Meilisearch SDK is ESM-only",
		"MinIO needs `forcePathStyle: true` and an explicit region",
		"CORS with dual-runtime frontend",
	)
	// workerdev README: 4 cloned stems, 0 net-new → must fail the floor.
	workerdevREADME := readmeShellWith(workerZeropsYaml,
		"No .env files on Zerops.",
		"TypeORM `synchronize: true` must never run in the application process.",
		"NestJS listens on `localhost` by default.",
		"ts-node requires devDependencies.",
	)

	appDir := filepath.Join(dir, "appdev")
	apiDir := filepath.Join(dir, "apidev")
	workerDir := filepath.Join(dir, "workerdev")
	for _, d := range []string{appDir, apiDir, workerDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(appDir, "zerops.yaml"), frontendZeropsYaml)
	writeFile(t, filepath.Join(appDir, "README.md"), appdevREADME)
	writeFile(t, filepath.Join(apiDir, "zerops.yaml"), apiZeropsYamlWithWorker)
	writeFile(t, filepath.Join(apiDir, "README.md"), apidevREADME)
	writeFile(t, filepath.Join(workerDir, "zerops.yaml"), workerZeropsYaml)
	writeFile(t, filepath.Join(workerDir, "README.md"), workerdevREADME)

	checker := checkRecipeGenerate(stateDir, nil, kp)
	result, err := checker(context.Background(), plan, testRecipeState())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	byName := make(map[string]workflow.StepCheck, len(result.Checks))
	for _, c := range result.Checks {
		byName[c.Name] = c
	}

	workerCheck, ok := byName["worker_knowledge_base_exceeds_predecessor"]
	if !ok {
		t.Fatalf("missing worker_knowledge_base_exceeds_predecessor — separate-codebase worker was not included in the floor loop. Got checks: %v", checkNames(result.Checks))
	}
	if workerCheck.Status != statusFail {
		t.Errorf("worker_knowledge_base_exceeds_predecessor should fail (all 4 stems clone the predecessor), got %s: %s", workerCheck.Status, workerCheck.Detail)
	}

	// App/api must still pass.
	if c := byName["app_knowledge_base_exceeds_predecessor"]; c.Status != statusPass {
		t.Errorf("app check should pass, got %s: %s", c.Status, c.Detail)
	}
	if c := byName["api_knowledge_base_exceeds_predecessor"]; c.Status != statusPass {
		t.Errorf("api check should pass, got %s: %s", c.Status, c.Detail)
	}

	if result.Passed {
		t.Error("expected overall fail because worker README cloned the predecessor, but result.Passed=true")
	}
}

// TestCheckRecipeGenerate_ShowcaseFloor_SharedCodebaseWorkerSkipped — when
// the worker target shares the host API's codebase (SharesCodebaseWith="api"),
// it does not have its own README. The floor check must NOT emit a check
// name for the shared worker; only the host codebase's README is checked
// (that's what the existing app/api loop already covers).
func TestCheckRecipeGenerate_ShowcaseFloor_SharedCodebaseWorkerSkipped(t *testing.T) {
	t.Parallel()

	kp := &testKnowledgeProvider{
		recipes: map[string]string{
			"nestjs-minimal": `# Nest.js Minimal

## Gotchas
- **No ` + "`.env`" + ` files on Zerops** — platform injects OS env vars.
- **TypeORM ` + "`synchronize: true`" + ` in production** — drops columns.
- **NestJS listens on ` + "`localhost`" + ` by default** — bind 0.0.0.0.
- **` + "`ts-node`" + ` needs devDependencies** — dev setup uses npm install.
`,
		},
	}

	plan := &workflow.RecipePlan{
		Framework:   "nestjs",
		Tier:        workflow.RecipeTierShowcase,
		Slug:        "nestjs-showcase",
		RuntimeType: "nodejs@22",
		Research: workflow.ResearchData{
			ServiceType:   "nodejs",
			HTTPPort:      3000,
			BuildCommands: []string{"npm ci"},
			DeployFiles:   []string{"dist"},
			StartCommand:  "node dist/main.js",
		},
		Targets: []workflow.RecipeTarget{
			{Hostname: "app", Type: "static", Role: "app"},
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true, SharesCodebaseWith: "api"},
		},
	}

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	appdevREADME := readmeShellWith(frontendZeropsYaml,
		"Static base only ships in prod.",
		"VITE_* vars are baked at build time, not runtime.",
		"Vite dev-server host check rejects unknown hosts.",
	)
	apidevREADME := readmeShellWith(apiZeropsYamlWithWorker,
		"Meilisearch SDK is ESM-only",
		"MinIO needs `forcePathStyle: true`",
		"Worker shares the API's TypeORM entities",
	)

	appDir := filepath.Join(dir, "appdev")
	apiDir := filepath.Join(dir, "apidev")
	for _, d := range []string{appDir, apiDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(appDir, "zerops.yaml"), frontendZeropsYaml)
	writeFile(t, filepath.Join(appDir, "README.md"), appdevREADME)
	writeFile(t, filepath.Join(apiDir, "zerops.yaml"), apiZeropsYamlWithWorker)
	writeFile(t, filepath.Join(apiDir, "README.md"), apidevREADME)

	checker := checkRecipeGenerate(stateDir, nil, kp)
	result, err := checker(context.Background(), plan, testRecipeState())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, c := range result.Checks {
		if c.Name == "worker_knowledge_base_exceeds_predecessor" {
			t.Errorf("shared-codebase worker should not emit a floor check (no standalone README), got %s: %s", c.Status, c.Detail)
		}
	}
}

// TestCheckRecipeGenerate_ShowcaseFloor_NilKPNoOp locks in the graceful
// fall-through when no knowledge provider is available (e.g. the existing
// non-showcase tests that pass nil). The check must emit nothing rather
// than crash, and the rest of the generate checks must still run.
func TestCheckRecipeGenerate_ShowcaseFloor_NilKPNoOp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(appDir, "zerops.yaml"), `zerops:
  - setup: dev
    build:
      base: php-nginx@8.4
      buildCommands:
        - composer install
      deployFiles:
        - .
    run:
      envVariables:
        APP_ENV: local
        APP_DEBUG: "true"
      ports:
        - port: 80
          httpSupport: true
  - setup: prod
    build:
      base: php-nginx@8.4
      buildCommands:
        - composer install --no-dev --optimize-autoloader
      deployFiles:
        - app
        - vendor
    run:
      envVariables:
        APP_ENV: production
        APP_DEBUG: "false"
      ports:
        - port: 80
          httpSupport: true
`)
	writeFile(t, filepath.Join(appDir, "README.md"), validREADME)

	checker := checkRecipeGenerate(stateDir, nil, nil)
	result, err := checker(context.Background(), testRecipePlan(), testRecipeState())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, c := range result.Checks {
		if c.Name == "app_knowledge_base_exceeds_predecessor" {
			t.Errorf("floor check emitted with nil kp — expected no-op, got %s: %s", c.Status, c.Detail)
		}
	}
}

// readmeShellWith builds a full codebase README containing an intro,
// integration-guide with an embedded zerops.yaml block, and a
// knowledge-base with the supplied gotcha stems. This mirrors the exact
// shape the generate-step check parses, so the test exercises
// extractFragmentContent + ExtractGotchaStems + the per-hostname wiring.
func readmeShellWith(zerops string, stems ...string) string {
	const (
		header = "# Showcase Codebase\n\n" +
			"<!-- #ZEROPS_EXTRACT_START:intro# -->\n\nA test recipe codebase.\n\n<!-- #ZEROPS_EXTRACT_END:intro# -->\n\n" +
			"## Integration Guide\n\n<!-- #ZEROPS_EXTRACT_START:integration-guide# -->\n\n### zerops.yaml\n\n" +
			"```yaml\n"
		afterYAML = "\n```\n\n" +
			"<!-- #ZEROPS_EXTRACT_END:integration-guide# -->\n\n" +
			"## Knowledge Base\n\n<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n\n### Gotchas\n\n"
		footer       = "\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n"
		bulletOpen   = "- **"
		bulletMiddle = "** — body.\n"
	)
	capacity := len(header) + len(zerops) + len(afterYAML) + len(footer)
	for _, s := range stems {
		capacity += len(bulletOpen) + len(s) + len(bulletMiddle)
	}
	b := make([]byte, 0, capacity)
	b = append(b, header...)
	b = append(b, zerops...)
	b = append(b, afterYAML...)
	for _, s := range stems {
		b = append(b, bulletOpen...)
		b = append(b, s...)
		b = append(b, bulletMiddle...)
	}
	b = append(b, footer...)
	return string(b)
}

func checkNames(checks []workflow.StepCheck) []string {
	out := make([]string, 0, len(checks))
	for _, c := range checks {
		out = append(out, c.Name)
	}
	return out
}
