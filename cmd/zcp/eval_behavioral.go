package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval"
)

// runEvalBehavioral dispatches behavioral subcommands.
//
//	zcp eval behavioral list   --scenarios-dir <dir>
//	zcp eval behavioral run    --scenarios-dir <dir> --id <id>
//	zcp eval behavioral run    --file <scenario.md>
//	zcp eval behavioral all    --scenarios-dir <dir>
func runEvalBehavioral(args []string) {
	if len(args) == 0 {
		printBehavioralUsage()
		os.Exit(1)
	}
	switch args[0] {
	case "list":
		runBehavioralList(args[1:])
	case "run":
		runBehavioralRun(args[1:])
	case "all":
		runBehavioralAll(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown behavioral subcommand: %s\n", args[0])
		printBehavioralUsage()
		os.Exit(1)
	}
}

func printBehavioralUsage() {
	fmt.Fprintln(os.Stderr, `Usage: zcp eval behavioral <command>

Commands:
  list  --scenarios-dir <dir>             List behavioral scenarios in dir
  run   --scenarios-dir <dir> --id <id>   Run one scenario by id
  run   --file <scenario.md>              Run one scenario by absolute path
  all   --scenarios-dir <dir>             Run every scenario in dir sequentially

The scenarios dir is anchored on the runner host (zcp container in normal use,
local FS for dev). Outputs land under $ZCP_EVAL_RESULTS_DIR/<suiteId>/<scenarioId>/.`)
}

func runBehavioralList(args []string) {
	dir := flagValue(args, "--scenarios-dir")
	if dir == "" {
		fmt.Fprintln(os.Stderr, "error: --scenarios-dir <dir> required")
		os.Exit(1)
	}
	scenarios, err := loadBehavioralScenarios(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if len(scenarios) == 0 {
		fmt.Fprintf(os.Stderr, "no behavioral scenarios in %s\n", dir)
		os.Exit(1)
	}
	for _, sc := range scenarios {
		printScenarioListEntry(sc)
	}
}

func runBehavioralRun(args []string) {
	file := flagValue(args, "--file")
	dir := flagValue(args, "--scenarios-dir")
	id := flagValue(args, "--id")

	var path string
	switch {
	case file != "":
		if dir != "" || id != "" {
			fmt.Fprintln(os.Stderr, "error: pass --file OR (--scenarios-dir + --id), not both")
			os.Exit(1)
		}
		path = file
	case dir != "" && id != "":
		path = filepath.Join(dir, id+".md")
	default:
		fmt.Fprintln(os.Stderr, "error: --file <scenario.md> OR (--scenarios-dir <dir> --id <id>) required")
		os.Exit(1)
	}
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintf(os.Stderr, "error: scenario file: %v\n", err)
		os.Exit(1)
	}

	runner, _, ctx := initEvalRunner()
	suiteID := time.Now().UTC().Format("20060102-150405")

	fmt.Fprintf(os.Stderr, "Running behavioral scenario: %s (suite=%s)\n", path, suiteID)
	result, err := runner.RunBehavioralScenario(ctx, path, suiteID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	printBehavioralResult(result)
	if result.Error != "" {
		os.Exit(1)
	}
}

func runBehavioralAll(args []string) {
	dir := flagValue(args, "--scenarios-dir")
	if dir == "" {
		fmt.Fprintln(os.Stderr, "error: --scenarios-dir <dir> required")
		os.Exit(1)
	}
	scenarios, err := loadBehavioralScenarios(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if len(scenarios) == 0 {
		fmt.Fprintf(os.Stderr, "no behavioral scenarios in %s\n", dir)
		os.Exit(1)
	}

	runner, _, ctx := initEvalRunner()
	suiteID := time.Now().UTC().Format("20060102-150405")

	fmt.Fprintf(os.Stderr, "Running behavioral scenario-suite (%d scenarios, suite=%s)\n", len(scenarios), suiteID)

	failures := 0
	for _, sc := range scenarios {
		fmt.Fprintf(os.Stderr, "\n=== %s ===\n", sc.ID)
		result, err := runner.RunBehavioralScenario(ctx, sc.SourcePath, suiteID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  fatal: %v\n", err)
			failures++
			continue
		}
		printBehavioralResult(result)
		if result.Error != "" {
			failures++
		}
		// Honor cancellation between scenarios.
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "suite cancelled")
			os.Exit(1)
		default:
		}
	}
	fmt.Fprintf(os.Stderr, "\nSuite done: %d/%d ok\n", len(scenarios)-failures, len(scenarios))
	if failures > 0 {
		os.Exit(1)
	}
}

func printBehavioralResult(r *eval.BehavioralResult) {
	fmt.Fprintf(os.Stderr, "\n=== Behavioral %s ===\n", r.ScenarioID)
	fmt.Fprintf(os.Stderr, "Suite:        %s\n", r.SuiteID)
	fmt.Fprintf(os.Stderr, "Mode:         %s\n", r.Mode)
	fmt.Fprintf(os.Stderr, "Model:        %s\n", r.Model)
	fmt.Fprintf(os.Stderr, "Total:        %s\n", time.Duration(r.Duration))
	fmt.Fprintf(os.Stderr, "Scenario:     %s\n", time.Duration(r.ScenarioWallTime))
	fmt.Fprintf(os.Stderr, "Retrospective:%s\n", time.Duration(r.RetroWallTime))
	fmt.Fprintf(os.Stderr, "SessionID:    %s\n", r.SessionID)
	fmt.Fprintf(os.Stderr, "Compacted:    %v\n", r.CompactedDuringResume)
	fmt.Fprintf(os.Stderr, "Output dir:   %s\n", r.OutputDir)
	fmt.Fprintf(os.Stderr, "Self-review:  %s\n", r.SelfReviewFile)
	if r.Error != "" {
		fmt.Fprintf(os.Stderr, "Error:        %s\n", r.Error)
	}
}

func printScenarioListEntry(sc *eval.Scenario) {
	fmt.Println(sc.ID)
	desc := strings.TrimSpace(sc.Description)
	for line := range strings.SplitSeq(desc, "\n") {
		fmt.Printf("  %s\n", strings.TrimSpace(line))
	}
	if len(sc.Tags) > 0 {
		fmt.Printf("  tags:  %s\n", strings.Join(sc.Tags, ", "))
	}
	if sc.Area != "" {
		fmt.Printf("  area:  %s\n", sc.Area)
	}
	fmt.Println()
}

// loadBehavioralScenarios walks dir/*.md, parses each, returns only those
// flagged behavioral (retrospective set). Sort by id for stable output.
func loadBehavioralScenarios(dir string) ([]*eval.Scenario, error) {
	pattern := filepath.Join(dir, "*.md")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob %s: %w", pattern, err)
	}
	var out []*eval.Scenario
	for _, f := range files {
		sc, err := eval.ParseScenario(f)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", f, err)
		}
		if !sc.IsBehavioral() {
			continue
		}
		out = append(out, sc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func flagValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(a, name+"="); ok {
			return v
		}
	}
	return ""
}

// initEvalRunner already exists in eval.go — referenced for context.
// behavioral subcommand reuses initEvalRunner + initPlatformClient unchanged.
var _ = context.Background
