package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/zeropsio/zcp/internal/eval/farm"
)

// runFarmReport implements `zcp eval farm report <dir>` (docs/spec-eval-farm.md
// §5.2 FM-37): entirely over pulled bundles, no network. <dir> is either a
// pulled batch dir (one subdirectory per run, from `farm pull --batch`) or a
// single pulled run dir (from `farm pull <runId>`). Batch manifests supply
// the full expected run inventory; legacy directories without a manifest
// are discovered from their immediate subdirectories.
func runFarmReport(args []string) int {
	var dir, evaluatorPin string
	for i := 0; i < len(args); i++ {
		arg := args[i] //nolint:gosec // G602 false positive: i is loop-bounded by i < len(args) each iteration (same shape as eval_farm.go's flag parsers)
		switch arg {
		case "--evaluator-sha256":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: %s requires a value\n", arg)
				return 1
			}
			evaluatorPin = args[i+1]
			i++
		default:
			if dir == "" {
				dir = arg
			}
		}
	}
	if dir == "" {
		fmt.Fprintln(os.Stderr, "error: zcp eval farm report requires a directory argument")
		return 1
	}
	if evaluatorPin == "" {
		evaluatorPin = os.Getenv("ZCP_FARM_EVALUATOR_SHA")
	}

	runDirs, err := farmReportRunDirs(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if len(runDirs) == 0 {
		fmt.Fprintf(os.Stderr, "error: no run bundles found under %s\n", dir)
		return 1
	}

	exitCode := 0
	counts := map[string]int{}
	for _, runDir := range runDirs {
		outcome := farm.ReportRun(runDir, evaluatorPin)
		printFarmRunOutcome(outcome)
		counts[outcome.Verdict]++
		if outcome.Verdict != farm.VerdictPassed && outcome.Verdict != farm.VerdictUnpinned {
			exitCode = 1
		}
	}
	printFarmRollup(dir, runDirs, counts)
	return exitCode
}

// printFarmRunOutcome prints one run's block: the single-run report text
// when it was built (spec-testing-architecture.md §10.5), else the reason
// this bundle could not be graded.
func printFarmRunOutcome(outcome farm.RunOutcome) {
	fmt.Fprintf(os.Stdout, "== run %s (scenario=%s) verdict=%s ==\n", outcome.RunID, outcome.ScenarioID, outcome.Verdict)
	if outcome.Reason != "" {
		fmt.Fprintln(os.Stdout, outcome.Reason)
	}
	if outcome.ReportText != "" {
		fmt.Fprint(os.Stdout, outcome.ReportText)
	}
	fmt.Fprintln(os.Stdout)
}

// printFarmRollup prints the batch roll-up docs/spec-eval-farm.md §5.2 FM-37
// requires: per-scenario verdict counts and which manifest/summary (FM-9)
// this report read from, when a batch manifest/summary was pulled alongside
// the run dirs.
func printFarmRollup(dir string, runDirs []string, counts map[string]int) {
	fmt.Fprintln(os.Stdout, "== Roll-up ==")
	fmt.Fprintf(os.Stdout, "runs: %d\n", len(runDirs))
	verdicts := make([]string, 0, len(counts))
	for v := range counts {
		verdicts = append(verdicts, v)
	}
	sort.Strings(verdicts)
	for _, v := range verdicts {
		fmt.Fprintf(os.Stdout, "  %s: %d\n", v, counts[v])
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if _, err := os.Stat(manifestPath); err == nil {
		fmt.Fprintf(os.Stdout, "manifest: %s\n", manifestPath)
	} else {
		fmt.Fprintln(os.Stdout, "manifest: none found (docs/spec-eval-farm.md §1.4 FM-9)")
	}
	summaryPath := filepath.Join(dir, "summary.json")
	if _, err := os.Stat(summaryPath); err == nil {
		fmt.Fprintf(os.Stdout, "summary: %s\n", summaryPath)
	} else {
		fmt.Fprintln(os.Stdout, "summary: none found (docs/spec-eval-farm.md §1.4 FM-9)")
	}
}

// farmReportRunDirs resolves dir into the list of run directories to grade:
// dir itself when it looks like a single run (has done.json, results/, or
// capture/ directly under it), otherwise the batch manifest's full inventory,
// including missing bundles. Without a manifest, discover legacy directories.
func farmReportRunDirs(dir string) ([]string, error) {
	if isFarmRunDir(dir) {
		return []string{dir}, nil
	}
	body, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err == nil {
		var manifest farm.BatchManifest
		if err := json.Unmarshal(body, &manifest); err != nil {
			return nil, fmt.Errorf("parse batch manifest: %w", err)
		}
		seen := make(map[string]bool)
		var runDirs []string
		for _, run := range manifest.Runs {
			if !farm.ValidRunID(run.RunID) || seen[run.RunID] {
				return nil, fmt.Errorf("batch manifest: invalid or duplicate run id %q", run.RunID)
			}
			seen[run.RunID] = true
			runDirs = append(runDirs, filepath.Join(dir, run.RunID))
		}
		sort.Strings(runDirs)
		return runDirs, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read batch manifest: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	var runDirs []string
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		runDirs = append(runDirs, filepath.Join(dir, name))
	}
	return runDirs, nil
}

// isFarmRunDir reports whether dir itself is one run's bundle (as opposed to
// a batch dir holding one subdirectory per run).
func isFarmRunDir(dir string) bool {
	for _, name := range []string{"done.json", "results", "capture"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}
