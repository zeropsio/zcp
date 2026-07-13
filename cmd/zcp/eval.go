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
	"github.com/zeropsio/zcp/internal/capture"
	"github.com/zeropsio/zcp/internal/eval"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
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
	case "suite":
		runEvalSuite(args[1:])
	case "cleanup":
		runEvalCleanup(args[1:])
	case "results":
		runEvalResults(args[1:])
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
  suite          [--tag <tag>]                  Run evaluation for all recipes
  cleanup        [--prefix <prefix>]            Full project cleanup (or prefix-only with --prefix)
  results        [--suite <id>]                 Show latest results summary
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

// --- Helpers ---

func evalResultsDir() string {
	if dir := os.Getenv("ZCP_EVAL_RESULTS_DIR"); dir != "" {
		return dir
	}
	return ".zcp/eval/results"
}

func evalWorkDir() string {
	dir, ok, msg := resolveEvalWorkDir(os.Getenv("ZCP_EVAL_WORK_DIR"), runtime.Detect().InContainer)
	if !ok {
		fmt.Fprintln(os.Stderr, "error: "+msg)
		os.Exit(1)
	}
	return dir
}

// resolveEvalWorkDir is the testable policy split out of evalWorkDir.
// CleanupProject runs `rm -rf` on workDir between scenarios; an implicit
// /var/www default outside the zcp container fails confusingly when the
// path is missing and is destructive when an unrelated /var/www exists.
// Outside the container, the operator must set ZCP_EVAL_WORK_DIR to a
// disposable scenario directory (the local-mode runner sets it under
// /tmp/zcp-flow-eval-local/...).
func resolveEvalWorkDir(envValue string, inContainer bool) (dir string, ok bool, msg string) {
	if envValue != "" {
		return envValue, true, ""
	}
	if !inContainer {
		return "", false, "ZCP_EVAL_WORK_DIR is required outside the zcp container — the eval runner does rm -rf on workDir between scenarios. Set it to a disposable directory, e.g. /tmp/zcp-eval-workdir."
	}
	return "/var/www", true, ""
}

// evalClaudeHome returns the override for the agent's `.claude/` config dir
// containing `projects/<slug>/memory/`. Empty falls back to ~/.claude inside
// the runner — safe on the zcp container, destructive on a developer's Mac
// where it would wipe every Claude project's memory across the whole
// machine. flow-eval-local.sh sets this to a scenario-scoped temp dir.
func evalClaudeHome() string {
	return os.Getenv("ZCP_EVAL_CLAUDE_HOME")
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

	captureConnection, err := activeEvalCapture(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture error: %v\n", err)
		os.Exit(1)
	}
	config := eval.RunnerConfig{
		MCPConfig:  evalMCPConfig(),
		ResultsDir: evalResultsDir(),
		WorkDir:    evalWorkDir(),
		ClaudeHome: evalClaudeHome(),
		Capture:    captureConnection,
	}
	if captureConnection != nil {
		fmt.Fprintf(os.Stderr, "capture: attached %s (%s)\n", captureConnection.CaptureID, captureConnection.SessionDir)
	}

	runner := eval.NewRunner(config, store, client, projectID)
	return runner, store, ctx
}

func activeEvalCapture(ctx context.Context) (*capture.Connection, error) {
	connection, configured, err := capture.ConnectionFromEnvironment(ctx)
	if err != nil {
		return nil, err
	}
	if configured {
		return connection, nil
	}
	manager, err := newDefaultCaptureManager()
	if err != nil {
		return nil, err
	}
	connection, status, err := manager.ActiveConnection(ctx)
	if err != nil {
		return nil, fmt.Errorf("global capture state %s: %w", status.State, err)
	}
	return connection, nil
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
