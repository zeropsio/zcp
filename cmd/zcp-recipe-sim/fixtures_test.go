// Test fixtures shared across the sim driver tests. Builds a minimal
// simulation directory with one codebase, one fragment per IG slot,
// and the per-tier folder skeleton — enough to drive runStitch end-
// to-end against the real recipe assembler without depending on a
// frozen runs/<N>/ corpus.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/recipe"
)

// writeMinimalSimulationOpts lays out a simulation dir compatible
// with runStitch: environments/{plan.json,facts.jsonl}, one codebase
// (`apidev/`) with bare zerops.yaml, and authored fragments under
// fragments-new/api/. The fixture is intentionally small but exercises
// every IG slot the assembler reads.
//
// withYamlComments opt-in adds a `codebase/api/zerops-yaml-comments/...`
// fragment — required for triggering the run-19 inline-yaml block-
// doubling regression in tests that assert the regression-detection
// mechanism fires. Default false keeps the basic-idempotence tests
// independent of the engine E1 fix landing.
func writeMinimalSimulationOpts(t *testing.T, root string, withYamlComments bool) error {
	t.Helper()
	apiSrc := filepath.Join(root, "apidev")
	if err := os.MkdirAll(apiSrc, 0o755); err != nil {
		return err
	}
	envDir := filepath.Join(root, "environments")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(apiSrc, "zerops.yaml"), []byte(minimalYAML), 0o600); err != nil {
		return err
	}

	plan := minimalPlan(apiSrc)
	body, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if err := os.WriteFile(filepath.Join(envDir, "plan.json"), body, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(envDir, "facts.jsonl"), nil, 0o600); err != nil {
		return err
	}

	fragsDir := filepath.Join(root, "fragments-new", "api")
	if err := os.MkdirAll(fragsDir, 0o755); err != nil {
		return err
	}
	frags := map[string]string{
		"codebase/api/intro":               "API codebase: NestJS minimal entry.\n",
		"codebase/api/integration-guide/1": "Integration step one — read the zerops.yaml below.\n",
		"codebase/api/knowledge-base":      "## Operations\n\nAuthored knowledge-base body.\n",
		"codebase/api/claude-md":           "# api\n\nAuthored CLAUDE.md body for the api codebase.\n",
	}
	if withYamlComments {
		// Engine routes `zerops-yaml-comments/<directive>` fragments
		// through injectZeropsYamlComments — the path the run-19 inline-
		// yaml block-doubling regression fires through.
		frags["codebase/api/zerops-yaml-comments/deployFiles"] =
			"# deployFiles narrows what ships to the runtime — see deploy-files.\n"
	}
	for id, body := range frags {
		filename := fragmentIDToFilename(id) + ".md"
		if err := os.WriteFile(filepath.Join(fragsDir, filename), []byte(body), 0o600); err != nil {
			return err
		}
	}

	return nil
}

// minimalPlan returns a Plan with a single api codebase rooted at the
// staged sim path. SourceRoot must end in `dev` per engine M-1.
func minimalPlan(sourceRoot string) *recipe.Plan {
	return &recipe.Plan{
		Slug:      "fixture-recipe",
		Framework: "nestjs",
		Tier:      "minimal",
		Research: recipe.ResearchResult{
			CodebaseShape: "1",
			Description:   "Test fixture for sim driver — single api codebase.",
		},
		Codebases: []recipe.Codebase{
			{
				Hostname:    "api",
				Role:        "api",
				BaseRuntime: "nodejs@22",
				SourceRoot:  sourceRoot,
			},
		},
		Services: []recipe.Service{
			{
				Kind:       recipe.ServiceKindManaged,
				Hostname:   "db",
				Type:       "postgresql@18",
				Priority:   10,
				SupportsHA: true,
			},
		},
	}
}

// minimalYAML is a bare-comments zerops.yaml. Engine reads this from
// disk during stitch; the assembler's injectZeropsYamlComments stamps
// fragment-recorded comments above each directive group.
const minimalYAML = `#zeropsPreprocessor=on
zerops:
  - setup: api
    build:
      base: nodejs@22
      buildCommands:
        - pnpm install
        - pnpm build
      deployFiles: ./dist
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node dist/main
      envVariables:
        DATABASE_URL: ${db_connectionString}
`

// fragmentIDToFilename mirrors the replay-adapter convention: replace
// every `/` with `__` to flatten the id into a filesystem-safe name.
func fragmentIDToFilename(id string) string {
	out := make([]byte, 0, len(id)*2)
	for _, b := range []byte(id) {
		if b == '/' {
			out = append(out, '_', '_')
			continue
		}
		out = append(out, b)
	}
	return string(out)
}

