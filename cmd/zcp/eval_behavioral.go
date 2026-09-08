package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
	"github.com/zeropsio/zcp/internal/eval"
)

// runEvalBehavioral dispatches behavioral subcommands.
//
//	zcp eval behavioral list   --scenarios-dir <dir>
//	zcp eval behavioral run    --scenarios-dir <dir> --id <id>
//	zcp eval behavioral run    --file <scenario.md>
//	zcp eval behavioral all    --scenarios-dir <dir>
func runEvalBehavioral(args []string) int {
	if len(args) == 0 {
		printBehavioralUsage()
		return 1
	}
	switch args[0] {
	case "list":
		return runBehavioralList(args[1:])
	case "run":
		return runBehavioralRun(args[1:])
	case "run-local":
		return runBehavioralRunLocal(args[1:])
	case "all":
		return runBehavioralAll(args[1:])
	case "report":
		return runBehavioralReport(args[1:], os.Stdout, os.Stderr)
	default:
		fmt.Fprintf(os.Stderr, "unknown behavioral subcommand: %s\n", args[0])
		printBehavioralUsage()
		return 1
	}
}

func printBehavioralUsage() {
	fmt.Fprintln(os.Stderr, `Usage: zcp eval behavioral <command>

Commands:
  list       --scenarios-dir <dir>             List behavioral scenarios in dir
  run        --scenarios-dir <dir> --id <id> [--capture raw]
                                               Run one scenario by id (container mode)
  run        --file <scenario.md> [--capture raw]
                                               Run one scenario by absolute path
  all        --scenarios-dir <dir> [--capture raw]
                                               Run every scenario in dir sequentially
  run-local  --id <id> [--scenarios-dir <dir>] [--cleanup-workdir] [--capture raw]
                                               Run one scenario in LOCAL mode on this Mac
                                               (isolated workdir + claude HOME under /tmp).
  report     --capture <dir> --eval <id> --scenario <id> [--format text|json]
                                               Read-only deterministic single-run report
                                               from a finalized capture window (no network/
                                               provider/platform/model call).

The 'run' family runs the agent inside a Zerops container (existing flow-eval).
'run-local' runs the agent directly on this machine: requires ZCP_API_KEY env
and 'zcp' on PATH (via 'make install'); workdir/results live under
/tmp/zcp-flow-eval-local/<suite>/<id>/ with results mirrored back to
eval/behavioral/runs-local/<suite>/<id>/. Outputs from container-mode runs land
under $ZCP_EVAL_RESULTS_DIR/<suiteId>/<scenarioId>/.

A scenario with 'verification.mode: required' needs --capture raw on its own
'run'/'all' invocation — a global or inherited capture window is refused.`)
}

func runBehavioralList(args []string) int {
	dir := flagValue(args, "--scenarios-dir")
	if dir == "" {
		fmt.Fprintln(os.Stderr, "error: --scenarios-dir <dir> required")
		return 1
	}
	scenarios, err := loadBehavioralScenarios(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if len(scenarios) == 0 {
		fmt.Fprintf(os.Stderr, "no behavioral scenarios in %s\n", dir)
		return 1
	}
	for _, sc := range scenarios {
		printScenarioListEntry(sc)
	}
	return 0
}

func runBehavioralRun(rawArgs []string) int {
	args, bindingFlags, err := parseExecutionBindingFlags(rawArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}

	file := flagValue(args, "--file")
	dir := flagValue(args, "--scenarios-dir")
	id := flagValue(args, "--id")

	var path string
	switch {
	case file != "":
		if dir != "" || id != "" {
			fmt.Fprintln(os.Stderr, "error: pass --file OR (--scenarios-dir + --id), not both")
			return 1
		}
		path = file
	case dir != "" && id != "":
		path = filepath.Join(dir, id+".md")
	default:
		fmt.Fprintln(os.Stderr, "error: --file <scenario.md> OR (--scenarios-dir <dir> --id <id>) required")
		return 1
	}
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintf(os.Stderr, "error: scenario file: %v\n", err)
		return 1
	}

	suiteID := time.Now().UTC().Format("20060102-150405")
	applyEvalDirOverrides(bindingFlags)
	binding := buildExecutionBinding(bindingFlags, suiteID)

	runner, _, ctx, ok := initEvalRunnerFor(binding)
	if !ok {
		return 1
	}

	fmt.Fprintf(os.Stderr, "Running behavioral scenario: %s (suite=%s)\n", path, suiteID)
	runner.BeginCaptureEvalRun(ctx, suiteID)
	result, err := runner.RunBehavioralScenario(ctx, path, suiteID)
	if err != nil {
		runner.EndCaptureEvalRun(ctx, suiteID, capture.CapturePartial, err)
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	accepted, reason := behavioralAccepted(result)
	// The window's status is an evidence fact, not the task verdict: a failed
	// task inside a complete capture is a normal bundle
	// (docs/spec-capture-inspector.md §8). Only an execution error degrades it.
	status := capture.CaptureComplete
	if result.Error != "" {
		status = capture.CapturePartial
	}
	runner.EndCaptureEvalRun(ctx, suiteID, status, errorFromString(result.Error))
	printBehavioralResult(result)
	if !accepted {
		fmt.Fprintf(os.Stderr, "rejected: %s\n", reason)
		return 1
	}
	return 0
}

// applyEvalDirOverrides implements §10.4 "--work-dir/--results-dir override
// ZCP_EVAL_WORK_DIR/ZCP_EVAL_RESULTS_DIR before initEvalRunner resolves
// them" by setting the environment the resolver functions read.
func applyEvalDirOverrides(flags executionBindingFlags) {
	if flags.workDir != "" {
		_ = os.Setenv("ZCP_EVAL_WORK_DIR", flags.workDir)
	}
	if flags.resultsDir != "" {
		_ = os.Setenv("ZCP_EVAL_RESULTS_DIR", flags.resultsDir)
	}
}

// buildExecutionBinding builds an eval.ExecutionBinding from parsed CLI
// flags, or nil when no binding flags were given. defaultRunID is the suite
// id (§10.4 "--run-id defaults to the suite id"). PrivateBin/ClaudeHome are
// derived siblings of the (possibly overridden) work dir so the candidate's
// own PATH/HOME never collide with the evaluator's.
func buildExecutionBinding(flags executionBindingFlags, defaultRunID string) *eval.ExecutionBinding {
	if !flags.any {
		return nil
	}
	runID := flags.runID
	if runID == "" {
		runID = defaultRunID
	}
	workDir, _ := evalWorkDir()
	if flags.workDir != "" {
		workDir = flags.workDir
	}
	resultsDir := evalResultsDir()
	if flags.resultsDir != "" {
		resultsDir = flags.resultsDir
	}
	base := filepath.Dir(workDir)
	return &eval.ExecutionBinding{
		Candidate:       flags.candidate,
		CandidateSHA256: flags.sha256,
		ProjectID:       flags.projectID,
		AckDisposable:   flags.ack,
		WorkDir:         workDir,
		ResultsDir:      resultsDir,
		RunID:           runID,
		PrivateBin:      filepath.Join(base, "candidate-bin"),
		ClaudeHome:      filepath.Join(base, "candidate-claude-home"),
	}
}

func runBehavioralAll(args []string) int {
	if err := rejectBindingFlagsForAll(args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}
	dir := flagValue(args, "--scenarios-dir")
	if dir == "" {
		fmt.Fprintln(os.Stderr, "error: --scenarios-dir <dir> required")
		return 1
	}
	scenarios, err := loadBehavioralScenarios(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if len(scenarios) == 0 {
		fmt.Fprintf(os.Stderr, "no behavioral scenarios in %s\n", dir)
		return 1
	}
	total := len(scenarios)
	scenarios = selectForAll(scenarios)
	if excluded := total - len(scenarios); excluded > 0 {
		fmt.Fprintf(os.Stderr, "excluded %d scenario(s) tagged excludeFromAll (run by --id to execute them directly)\n", excluded)
	}
	if len(scenarios) == 0 {
		fmt.Fprintf(os.Stderr, "no behavioral scenarios left to run in %s after excludeFromAll filtering\n", dir)
		os.Exit(1)
	}

	runner, _, ctx, ok := initEvalRunner()
	if !ok {
		return 1
	}
	suiteID := time.Now().UTC().Format("20060102-150405")

	fmt.Fprintf(os.Stderr, "Running behavioral scenario-suite (%d scenarios, suite=%s)\n", len(scenarios), suiteID)
	runner.BeginCaptureEvalRun(ctx, suiteID)

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
		accepted, reason := behavioralAccepted(result)
		if !accepted {
			fmt.Fprintf(os.Stderr, "rejected: %s\n", reason)
			failures++
		}
		// Honor cancellation between scenarios.
		select {
		case <-ctx.Done():
			runner.EndCaptureEvalRun(context.Background(), suiteID, capture.CapturePartial, ctx.Err())
			fmt.Fprintln(os.Stderr, "suite cancelled")
			return 1
		default:
		}
	}
	fmt.Fprintf(os.Stderr, "\nSuite done: %d/%d ok\n", len(scenarios)-failures, len(scenarios))
	status := capture.CaptureComplete
	var suiteErr error
	if failures > 0 {
		status = capture.CapturePartial
		suiteErr = fmt.Errorf("%d behavioral scenario(s) failed", failures)
	}
	runner.EndCaptureEvalRun(ctx, suiteID, status, suiteErr)
	if failures > 0 {
		return 1
	}
	return 0
}

func errorFromString(value string) error {
	if value == "" {
		return nil
	}
	return fmt.Errorf("%s", value)
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
	printBehavioralDimensions(r)
}

// printBehavioralDimensions prints the three CLI acceptance dimensions
// (docs/spec-testing-architecture.md §10.1 "CLI acceptance"): Execution,
// Task, and Task-end evidence. Required mode also names retention, since the
// CLI claims no copy and no cleanup (§10.2).
func printBehavioralDimensions(r *eval.BehavioralResult) {
	if r.Error != "" {
		fmt.Fprintf(os.Stderr, "Execution:    error: %s\n", r.Error)
	} else {
		fmt.Fprintln(os.Stderr, "Execution:    ok")
	}
	if r.Task != nil {
		if r.Task.Mode == eval.VerificationObserve {
			fmt.Fprintf(os.Stderr, "Task:         observe %s (advisory)\n", r.Task.Result)
		} else {
			fmt.Fprintf(os.Stderr, "Task:         %s %s\n", r.Task.Mode, r.Task.Result)
		}
	}
	if r.TaskEnd != nil {
		persisted := "persisted"
		if !r.TaskEnd.Persisted {
			persisted = fmt.Sprintf("not persisted: %s", r.TaskEnd.PersistError)
		}
		settled := "settled"
		if !r.TaskEnd.Settled {
			settled = fmt.Sprintf("unsettled (%s)", strings.Join(r.TaskEnd.LiveProcesses, ", "))
		}
		fmt.Fprintf(os.Stderr, "Task-end evidence: %s, %s\n", persisted, settled)
	}
	if r.Task != nil && r.Task.Mode == eval.VerificationRequired {
		fmt.Fprintln(os.Stderr, "Retained:     project and results left in place for operator copy/cleanup")
	}
	if r.Binding != nil {
		if ok, reason := eval.ProcessIdentityAccepted(r); ok {
			fmt.Fprintf(os.Stderr, "Process identity: ok (pid %d)\n", eval.MatchingProcessIdentityPID(r))
		} else {
			fmt.Fprintf(os.Stderr, "Process identity: blocked: %s\n", strings.TrimPrefix(reason, "process identity: "))
		}
	}
}

// behavioralAccepted decides the CLI exit rule (docs/spec-testing-architecture.md
// §10.1 "CLI acceptance"): observe mode accepts on execution success alone;
// required mode also needs a passed task result with persisted task-end
// evidence. reason names the failing dimension for the caller to print.
func behavioralAccepted(r *eval.BehavioralResult) (ok bool, reason string) {
	if r.Error != "" {
		return false, fmt.Sprintf("execution: %s", r.Error)
	}
	if r.Task == nil || r.Task.Mode != eval.VerificationRequired {
		return true, ""
	}
	if r.Task.Result != eval.CheckPassed {
		return false, fmt.Sprintf("task: required result %s", r.Task.Result)
	}
	if r.TaskEnd == nil || !r.TaskEnd.Persisted {
		return false, "task-end evidence: not persisted"
	}
	if r.Binding != nil {
		if ok, reason := eval.ProcessIdentityAccepted(r); !ok {
			return false, reason
		}
	}
	return true, ""
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

// selectForAll drops scenarios flagged ExcludeFromAll from an all-run set.
// Enforced (unlike Tags, which are descriptive only and never gate
// selection) — used by `behavioral all` right after loadBehavioralScenarios.
// `behavioral list` and direct execution by id do NOT call this: excluded
// scenarios stay discoverable and directly runnable, only a routine
// full-suite sweep skips them (e.g. scenarios that consume a live one-shot
// credential).
func selectForAll(scenarios []*eval.Scenario) []*eval.Scenario {
	out := make([]*eval.Scenario, 0, len(scenarios))
	for _, sc := range scenarios {
		if sc.ExcludeFromAll {
			continue
		}
		out = append(out, sc)
	}
	return out
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

// executionBindingFlags is the parsed §10.4 explicit-candidate-binding flag
// set. any reports whether at least one of the four core binding flags
// (candidate/sha256/project-id/ack) was present.
type executionBindingFlags struct {
	candidate, sha256, projectID, ack string
	workDir, resultsDir, runID        string
	any                               bool
}

// bindingFlagNames are the four core binding flags: all-or-nothing
// (docs/spec-testing-architecture.md §10.4 "Binding").
var bindingFlagNames = []string{"--candidate", "--candidate-sha256", "--project-id", "--ack-disposable-project"}

// parseExecutionBindingFlags extracts the §10.4 binding flags from args,
// returning the remaining args untouched (order preserved) plus the parsed
// flags. --work-dir/--results-dir/--run-id are extracted unconditionally;
// the four core binding flags are all-or-nothing.
func parseExecutionBindingFlags(args []string) (clean []string, flags executionBindingFlags, err error) {
	clean = make([]string, 0, len(args))
	present := map[string]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--candidate":
			if i+1 >= len(args) {
				return nil, flags, fmt.Errorf("%s requires a value", a)
			}
			flags.candidate = args[i+1]
			present[a] = true
			i++
		case "--candidate-sha256":
			if i+1 >= len(args) {
				return nil, flags, fmt.Errorf("%s requires a value", a)
			}
			flags.sha256 = args[i+1]
			present[a] = true
			i++
		case "--project-id":
			if i+1 >= len(args) {
				return nil, flags, fmt.Errorf("%s requires a value", a)
			}
			flags.projectID = args[i+1]
			present[a] = true
			i++
		case "--ack-disposable-project":
			if i+1 >= len(args) {
				return nil, flags, fmt.Errorf("%s requires a value", a)
			}
			flags.ack = args[i+1]
			present[a] = true
			i++
		case "--work-dir":
			if i+1 >= len(args) {
				return nil, flags, fmt.Errorf("%s requires a value", a)
			}
			flags.workDir = args[i+1]
			i++
		case "--results-dir":
			if i+1 >= len(args) {
				return nil, flags, fmt.Errorf("%s requires a value", a)
			}
			flags.resultsDir = args[i+1]
			i++
		case "--run-id":
			if i+1 >= len(args) {
				return nil, flags, fmt.Errorf("%s requires a value", a)
			}
			flags.runID = args[i+1]
			i++
		default:
			clean = append(clean, a)
		}
	}
	for _, name := range bindingFlagNames {
		if present[name] {
			flags.any = true
			break
		}
	}
	if flags.any {
		var missing []string
		for _, name := range bindingFlagNames {
			if !present[name] {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			return nil, flags, fmt.Errorf("--candidate/--candidate-sha256/--project-id/--ack-disposable-project must all be given together; missing %s", strings.Join(missing, ", "))
		}
	}
	return clean, flags, nil
}

// rejectBindingFlagsForAll implements §10.4 "behavioral all with a binding
// is refused": `behavioral all` never accepts an explicit binding.
func rejectBindingFlagsForAll(args []string) error {
	_, flags, err := parseExecutionBindingFlags(args)
	if err != nil {
		return err
	}
	if flags.any || flags.workDir != "" || flags.resultsDir != "" || flags.runID != "" {
		return fmt.Errorf("'behavioral all' does not accept an explicit binding — required-mode retention makes a second scenario on the same target fail freshness by design; run the scenario directly with 'behavioral run'")
	}
	return nil
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
