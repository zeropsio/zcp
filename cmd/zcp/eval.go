package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/eval"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
)

const (
	statusPass  = "PASS"
	statusFail  = "FAIL"
	statusError = "ERROR"
)

func runEval(args []string) {
	if len(args) == 0 {
		printEvalUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "run":
		runEvalRun(args[1:])
	case "scenario":
		runEvalScenario(args[1:])
	case "scenario-suite":
		runEvalScenarioSuite(args[1:])
	case "suite":
		runEvalSuite(args[1:])
	case "create":
		runEvalCreate(args[1:])
	case "create-suite":
		runEvalCreateSuite(args[1:])
	case "cleanup":
		runEvalCleanup(args[1:])
	case "results":
		runEvalResults(args[1:])
	case "triage":
		runEvalTriage(args[1:])
	case "behavioral":
		runEvalBehavioral(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown eval subcommand: %s\n", args[0])
		printEvalUsage()
		os.Exit(1)
	}
}

func printEvalUsage() {
	fmt.Fprintln(os.Stderr, `Usage: zcp eval <command>

Commands:
  run            --recipe <name>[,name...]      Run evaluation for specific recipes
  scenario       --file <path>                  Run a single scenario file
  scenario-suite --ids <a,b,...> | --files <p,p>  Run scenarios sequentially under one suite ID
  suite          [--tag <tag>]                  Run evaluation for all recipes
  create         --framework <name> --tier <t>  Create a recipe via headless workflow
  create-suite   --frameworks <a,b> --tier <t>  Batch-create recipes
  cleanup        [--prefix <prefix>]            Full project cleanup (or prefix-only with --prefix)
  results        [--suite <id>]                 Show latest results summary
  triage         [--suite <id>] [--out <path>]  Aggregate scenario suite EVAL REPORTs into triage.md
  behavioral     <list|run|all> [args...]       Two-shot resume scenario runs (interactive C4 eval)`)
}

func runEvalRun(args []string) {
	var recipes string
	for i := 0; i < len(args); i++ {
		if args[i] == "--recipe" && i+1 < len(args) {
			recipes = args[i+1]
			i++
		}
	}
	if recipes == "" {
		fmt.Fprintln(os.Stderr, "error: --recipe required")
		os.Exit(1)
	}

	recipeList := strings.Split(recipes, ",")
	for i, r := range recipeList {
		recipeList[i] = strings.TrimSpace(r)
	}

	runner, store, ctx := initEvalRunner()
	suite := eval.NewSuite(runner)

	// Validate recipes exist
	for _, r := range recipeList {
		if _, err := store.Get("zerops://recipes/" + r); err != nil {
			fmt.Fprintf(os.Stderr, "error: recipe %q not found\n", r)
			os.Exit(1)
		}
	}

	result, err := suite.RunAll(ctx, recipeList)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	printSuiteResult(result)
}

func runEvalScenario(args []string) {
	var path string
	for i := 0; i < len(args); i++ {
		if args[i] == "--file" && i+1 < len(args) {
			path = args[i+1]
			i++
		}
	}
	if path == "" {
		fmt.Fprintln(os.Stderr, "error: --file <scenario.md> required")
		os.Exit(1)
	}
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintf(os.Stderr, "error: scenario file: %v\n", err)
		os.Exit(1)
	}

	runner, _, ctx := initEvalRunner()
	suiteID := time.Now().Format("2006-01-02-150405")

	fmt.Fprintf(os.Stderr, "Running scenario: %s (suite=%s)\n", path, suiteID)
	result, err := runner.RunScenario(ctx, path, suiteID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	printScenarioResult(result)
	if !result.Grade.Passed {
		os.Exit(1)
	}
}

// runEvalScenarioSuite runs a list of scenarios sequentially under one
// suite ID. Accepts either `--ids a,b,c` (resolved against
// internal/eval/scenarios/<id>.md) or `--files p1,p2,p3` (literal paths).
// Per-scenario PASS/FAIL is informational — the suite continues through
// all scenarios so triage gets full signal in one pass.
func runEvalScenarioSuite(args []string) {
	var ids, files string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--ids":
			if i+1 < len(args) {
				ids = args[i+1]
				i++
			}
		case "--files":
			if i+1 < len(args) {
				files = args[i+1]
				i++
			}
		}
	}
	if ids == "" && files == "" {
		fmt.Fprintln(os.Stderr, "error: --ids or --files required")
		os.Exit(1)
	}
	if ids != "" && files != "" {
		fmt.Fprintln(os.Stderr, "error: pass --ids OR --files, not both")
		os.Exit(1)
	}

	var paths []string
	if ids != "" {
		for id := range strings.SplitSeq(ids, ",") {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			path := filepath.Join("internal/eval/scenarios", id+".md")
			if _, err := os.Stat(path); err != nil {
				fmt.Fprintf(os.Stderr, "error: scenario id %q (looking at %s): %v\n", id, path, err)
				os.Exit(1)
			}
			paths = append(paths, path)
		}
	} else {
		for p := range strings.SplitSeq(files, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if _, err := os.Stat(p); err != nil {
				fmt.Fprintf(os.Stderr, "error: scenario file %q: %v\n", p, err)
				os.Exit(1)
			}
			paths = append(paths, p)
		}
	}

	if len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "error: no scenarios resolved")
		os.Exit(1)
	}

	runner, _, ctx := initEvalRunner()
	suite := eval.NewSuite(runner)

	fmt.Fprintf(os.Stderr, "Running scenario-suite: %d scenarios\n", len(paths))
	result, err := suite.RunAllScenarios(ctx, paths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	printScenarioSuiteResult(result)
}

// printScenarioSuiteResult prints the per-scenario verdict roll-up. Mirrors
// printSuiteResult shape for visual consistency, keyed off Grade.Passed
// instead of Success (scenario world tracks pass through the grader).
func printScenarioSuiteResult(result *eval.ScenarioSuiteResult) {
	pass, fail, errCount := 0, 0, 0
	for _, r := range result.Results {
		switch {
		case r.Error != "":
			errCount++
		case r.Grade.Passed:
			pass++
		default:
			fail++
		}
	}

	fmt.Fprintf(os.Stderr, "\n=== Scenario suite %s ===\n", result.SuiteID)
	fmt.Fprintf(os.Stderr, "Total: %d | Pass: %d | Fail: %d | Error: %d\n",
		len(result.Results), pass, fail, errCount)
	fmt.Fprintf(os.Stderr, "Duration: %s\n\n", result.Duration)

	for _, r := range result.Results {
		status := statusFail
		if r.Grade.Passed {
			status = statusPass
		}
		if r.Error != "" {
			status = statusError
		}
		fmt.Fprintf(os.Stderr, "  %-40s %s  %s\n", r.ScenarioID, status, r.Duration)
		if r.Error != "" {
			fmt.Fprintf(os.Stderr, "    error: %s\n", r.Error)
		}
		for _, f := range r.Grade.Failures {
			fmt.Fprintf(os.Stderr, "    - %s\n", f)
		}
	}
}

func printScenarioResult(r *eval.ScenarioResult) {
	status := statusPass
	if !r.Grade.Passed {
		status = statusFail
	}
	if r.Error != "" {
		status = statusError
	}
	fmt.Fprintf(os.Stderr, "\n=== Scenario %s ===\n", r.ScenarioID)
	fmt.Fprintf(os.Stderr, "%s  %s\n", status, r.Duration)
	fmt.Fprintf(os.Stderr, "Log: %s\n", r.LogFile)
	if r.Error != "" {
		fmt.Fprintf(os.Stderr, "Error: %s\n", r.Error)
	}
	if len(r.Grade.Failures) > 0 {
		fmt.Fprintln(os.Stderr, "\nFailures:")
		for _, f := range r.Grade.Failures {
			fmt.Fprintf(os.Stderr, "  - %s\n", f)
		}
	}
}

func runEvalSuite(_ []string) {
	runner, store, ctx := initEvalRunner()
	suite := eval.NewSuite(runner)

	recipes := store.ListRecipes()

	if len(recipes) == 0 {
		fmt.Fprintln(os.Stderr, "no recipes to evaluate")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Running eval suite: %d recipes\n", len(recipes))
	result, err := suite.RunAll(ctx, recipes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	printSuiteResult(result)
}

func runEvalCreate(args []string) {
	var framework, tier string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--framework":
			if i+1 < len(args) {
				framework = args[i+1]
				i++
			}
		case "--tier":
			if i+1 < len(args) {
				tier = args[i+1]
				i++
			}
		}
	}
	if framework == "" {
		fmt.Fprintln(os.Stderr, "error: --framework required")
		os.Exit(1)
	}
	if tier == "" {
		tier = "minimal"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	config := eval.RecipeCreateConfig{
		MCPConfig:  evalMCPConfig(),
		ResultsDir: evalResultsDir(),
	}
	creator := eval.NewRecipeCreator(config)

	fmt.Fprintf(os.Stderr, "Creating %s %s recipe...\n", framework, tier)
	result, err := creator.CreateRecipe(ctx, framework, tier)
	stop()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	status := "SUCCESS"
	if !result.Success {
		status = statusFail
	}
	if result.Error != "" {
		status = "ERROR: " + result.Error
	}
	fmt.Fprintf(os.Stderr, "\n%s: %s (%s)\nLog: %s\n", result.Slug, status, result.Duration, result.LogFile)
}

func runEvalCreateSuite(args []string) {
	var frameworks, tier string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--frameworks":
			if i+1 < len(args) {
				frameworks = args[i+1]
				i++
			}
		case "--tier":
			if i+1 < len(args) {
				tier = args[i+1]
				i++
			}
		}
	}
	if frameworks == "" {
		fmt.Fprintln(os.Stderr, "error: --frameworks required")
		os.Exit(1)
	}
	if tier == "" {
		tier = "minimal"
	}

	frameworkList := strings.Split(frameworks, ",")
	for i, f := range frameworkList {
		frameworkList[i] = strings.TrimSpace(f)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	config := eval.RecipeCreateConfig{
		MCPConfig:  evalMCPConfig(),
		ResultsDir: evalResultsDir(),
	}
	creator := eval.NewRecipeCreator(config)
	suite := eval.NewRecipeCreateSuite(creator)

	fmt.Fprintf(os.Stderr, "Creating %d recipes (%s tier)...\n", len(frameworkList), tier)
	result, err := suite.CreateAll(ctx, frameworkList, tier)
	stop()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	printSuiteResult(result)
}

func runEvalCleanup(args []string) {
	var prefix string
	for i := 0; i < len(args); i++ {
		if args[i] == "--prefix" && i+1 < len(args) {
			prefix = args[i+1]
			i++
		}
	}

	client, projectID, ctx := initPlatformClient()

	if prefix != "" {
		// Prefix mode: only delete services matching the prefix
		fmt.Fprintf(os.Stderr, "Cleaning up eval services with prefix %q...\n", prefix)
		if err := eval.CleanupEvalServices(ctx, client, projectID, prefix); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Full cleanup: delete all services (except zcp), clean files, reset workflow
		workDir := evalWorkDir()
		fmt.Fprintf(os.Stderr, "Full project cleanup (workDir=%s)...\n", workDir)
		if err := eval.CleanupProject(ctx, client, projectID, workDir); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Fprintln(os.Stderr, "Cleanup complete.")
}

func runEvalResults(args []string) {
	resultsDir := evalResultsDir()

	var suiteID string
	for i := 0; i < len(args); i++ {
		if args[i] == "--suite" && i+1 < len(args) {
			suiteID = args[i+1]
			i++
		}
	}

	if suiteID == "" {
		// Find the latest suite
		entries, err := os.ReadDir(resultsDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: no results found in %s\n", resultsDir)
			os.Exit(1)
		}
		if len(entries) == 0 {
			fmt.Fprintln(os.Stderr, "no results found")
			os.Exit(1)
		}
		suiteID = entries[len(entries)-1].Name()
	}

	suiteFile := filepath.Join(resultsDir, suiteID, "suite.json")
	data, err := os.ReadFile(suiteFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	var result eval.SuiteResult
	if err := json.Unmarshal(data, &result); err != nil {
		fmt.Fprintf(os.Stderr, "error: parse suite.json: %v\n", err)
		os.Exit(1)
	}

	printSuiteResult(&result)
}

// runEvalTriage loads a scenario-suite's suite.json, aggregates the per-
// scenario EVAL REPORTs into a Triage view, and writes a human-readable
// triage.md alongside (default <ResultsDir>/<suiteID>/triage.md). When
// --suite is omitted, picks the latest suite directory.
func runEvalTriage(args []string) {
	resultsDir := evalResultsDir()

	var suiteID, outPath string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--suite":
			if i+1 < len(args) {
				suiteID = args[i+1]
				i++
			}
		case "--out":
			if i+1 < len(args) {
				outPath = args[i+1]
				i++
			}
		}
	}

	if suiteID == "" {
		entries, err := os.ReadDir(resultsDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: read results dir %s: %v\n", resultsDir, err)
			os.Exit(1)
		}
		if len(entries) == 0 {
			fmt.Fprintln(os.Stderr, "no results found")
			os.Exit(1)
		}
		suiteID = entries[len(entries)-1].Name()
	}

	suiteFile := filepath.Join(resultsDir, suiteID, "suite.json")
	data, err := os.ReadFile(suiteFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read %s: %v\n", suiteFile, err)
		os.Exit(1)
	}

	var suite eval.ScenarioSuiteResult
	if err := json.Unmarshal(data, &suite); err != nil {
		fmt.Fprintf(os.Stderr, "error: parse suite.json as scenario-suite: %v\n", err)
		os.Exit(1)
	}
	if len(suite.Results) == 0 || suite.Results[0].ScenarioID == "" {
		fmt.Fprintf(os.Stderr, "error: %s does not look like a scenario-suite (no scenarioId on results — was this a recipe suite?)\n", suiteFile)
		os.Exit(1)
	}

	triage := eval.AggregateScenarioSuite(&suite)
	md := eval.RenderTriageMarkdown(triage)

	if outPath == "" {
		outPath = filepath.Join(resultsDir, suiteID, "triage.md")
	}
	if err := os.WriteFile(outPath, []byte(md), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "error: write %s: %v\n", outPath, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Triage written: %s (%d scenarios, %d failure-chains across %d root causes)\n",
		outPath, len(triage.Scenarios), countTriageFailures(triage), len(triage.GroupedByRootCause))
}

func countTriageFailures(t eval.Triage) int {
	n := 0
	for _, entries := range t.GroupedByRootCause {
		n += len(entries)
	}
	return n
}

// --- Helpers ---

func evalResultsDir() string {
	if dir := os.Getenv("ZCP_EVAL_RESULTS_DIR"); dir != "" {
		return dir
	}
	return ".zcp/eval/results"
}

func evalWorkDir() string {
	if dir := os.Getenv("ZCP_EVAL_WORK_DIR"); dir != "" {
		return dir
	}
	return "/var/www"
}

func evalMCPConfig() string {
	if cfg := os.Getenv("ZCP_EVAL_MCP_CONFIG"); cfg != "" {
		return cfg
	}
	// Check work dir first (Zerops container layout: /var/www/.mcp.json).
	workMCP := filepath.Join(evalWorkDir(), ".mcp.json")
	if _, err := os.Stat(workMCP); err == nil {
		return workMCP
	}
	// Fall back to ~/.mcp.json only if it exists — otherwise return empty so the
	// eval runner skips --mcp-config and Claude picks up its own default config
	// (e.g. ~/.claude.json written by `zcp init` on containers).
	home, _ := os.UserHomeDir()
	homeMCP := filepath.Join(home, ".mcp.json")
	if _, err := os.Stat(homeMCP); err == nil {
		return homeMCP
	}
	return ""
}

func initPlatformClient() (platform.Client, string, context.Context) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	creds, err := auth.ResolveCredentials()
	if err != nil {
		stop()
		fmt.Fprintf(os.Stderr, "auth error: %v\n", err)
		os.Exit(1)
	}

	client, err := platform.NewZeropsClient(creds.Token, creds.APIHost)
	if err != nil {
		stop()
		fmt.Fprintf(os.Stderr, "client error: %v\n", err)
		os.Exit(1)
	}

	authInfo, err := auth.Resolve(ctx, client)
	if err != nil {
		stop()
		fmt.Fprintf(os.Stderr, "auth error: %v\n", err)
		os.Exit(1)
	}

	return client, authInfo.ProjectID, ctx
}

func initEvalRunner() (*eval.Runner, *knowledge.Store, context.Context) {
	client, projectID, ctx := initPlatformClient()

	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "knowledge store error: %v\n", err)
		os.Exit(1)
	}

	config := eval.RunnerConfig{
		MCPConfig:  evalMCPConfig(),
		ResultsDir: evalResultsDir(),
		WorkDir:    evalWorkDir(),
	}

	runner := eval.NewRunner(config, store, client, projectID)
	return runner, store, ctx
}

func printSuiteResult(result *eval.SuiteResult) {
	pass, fail, errCount := 0, 0, 0
	for _, r := range result.Results {
		switch {
		case r.Error != "":
			errCount++
		case r.Success:
			pass++
		default:
			fail++
		}
	}

	fmt.Fprintf(os.Stderr, "\n=== Suite %s ===\n", result.SuiteID)
	fmt.Fprintf(os.Stderr, "Total: %d | Pass: %d | Fail: %d | Error: %d\n",
		len(result.Results), pass, fail, errCount)
	fmt.Fprintf(os.Stderr, "Duration: %s\n\n", result.Duration)

	for _, r := range result.Results {
		status := statusFail
		if r.Success {
			status = statusPass
		}
		if r.Error != "" {
			status = statusError
		}
		fmt.Fprintf(os.Stderr, "  %-25s %s  %s\n", r.Recipe, status, r.Duration)
		if r.Error != "" {
			fmt.Fprintf(os.Stderr, "    error: %s\n", r.Error)
		}
	}
}
