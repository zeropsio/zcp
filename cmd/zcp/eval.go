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

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/eval"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
)

func runEval(args []string) {
	if len(args) == 0 {
		printEvalUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "run":
		runEvalRun(args[1:])
	case "suite":
		runEvalSuite(args[1:])
	case "cleanup":
		runEvalCleanup(args[1:])
	case "results":
		runEvalResults(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown eval subcommand: %s\n", args[0])
		printEvalUsage()
		os.Exit(1)
	}
}

func printEvalUsage() {
	fmt.Fprintln(os.Stderr, `Usage: zcp eval <command>

Commands:
  run      --recipe <name>[,name...]   Run evaluation for specific recipes
  suite    [--tag <tag>]               Run evaluation for all recipes
  cleanup  [--prefix <prefix>]         Full project cleanup (or prefix-only with --prefix)
  results  [--suite <id>]              Show latest results summary`)
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

func runEvalSuite(args []string) {
	var tag string
	for i := 0; i < len(args); i++ {
		if args[i] == "--tag" && i+1 < len(args) {
			tag = args[i+1]
			i++
		}
	}

	runner, store, ctx := initEvalRunner()
	suite := eval.NewSuite(runner)

	recipes := store.ListRecipes()
	if tag != "" {
		recipes = filterRecipesByTag(store, recipes, tag)
	}

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
		// Full cleanup: delete all services (except zcpx), clean files, reset workflow
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
	// Check work dir first (Zerops container layout: /var/www/.mcp.json)
	workMCP := filepath.Join(evalWorkDir(), ".mcp.json")
	if _, err := os.Stat(workMCP); err == nil {
		return workMCP
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".mcp.json")
}

func initPlatformClient() (platform.Client, string, context.Context) {
	ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	creds, err := auth.ResolveCredentials()
	if err != nil {
		fmt.Fprintf(os.Stderr, "auth error: %v\n", err)
		os.Exit(1)
	}

	client, err := platform.NewZeropsClient(creds.Token, creds.APIHost)
	if err != nil {
		fmt.Fprintf(os.Stderr, "client error: %v\n", err)
		os.Exit(1)
	}

	authInfo, err := auth.Resolve(ctx, client)
	if err != nil {
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
	}

	runner := eval.NewRunner(config, store, client, projectID)
	return runner, store, ctx
}

func filterRecipesByTag(store *knowledge.Store, recipes []string, tag string) []string {
	var filtered []string
	for _, r := range recipes {
		doc, err := store.Get("zerops://recipes/" + r)
		if err != nil {
			continue
		}
		for _, kw := range doc.Keywords {
			if strings.EqualFold(kw, tag) {
				filtered = append(filtered, r)
				break
			}
		}
	}
	return filtered
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
		status := "FAIL"
		if r.Success {
			status = "PASS"
		}
		if r.Error != "" {
			status = "ERROR"
		}
		fmt.Fprintf(os.Stderr, "  %-25s %s  %s\n", r.Recipe, status, r.Duration)
		if r.Error != "" {
			fmt.Fprintf(os.Stderr, "    error: %s\n", r.Error)
		}
	}
}
