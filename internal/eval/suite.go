package eval

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Status constants used in console summaries (recipe + scenario suites).
const (
	statusPass  = "PASS"
	statusFail  = "FAIL"
	statusError = "ERROR"
)

// Suite runs evaluations across multiple recipes sequentially.
type Suite struct {
	runner *Runner
}

// NewSuite creates a new suite runner.
func NewSuite(runner *Runner) *Suite {
	return &Suite{runner: runner}
}

// RunAll executes evaluations for the given recipes sequentially.
func (s *Suite) RunAll(ctx context.Context, recipes []string) (*SuiteResult, error) {
	suiteID := generateSuiteID()
	startedAt := time.Now()

	result := &SuiteResult{
		SuiteID:   suiteID,
		StartedAt: startedAt,
	}

	for _, recipe := range recipes {
		select {
		case <-ctx.Done():
			result.Duration = Duration(time.Since(startedAt))
			return result, ctx.Err()
		default:
		}

		fmt.Fprintf(os.Stderr, "=== eval: %s ===\n", recipe)
		runResult, err := s.runner.Run(ctx, recipe, suiteID)
		if err != nil {
			return nil, fmt.Errorf("run %s: %w", recipe, err)
		}

		result.Results = append(result.Results, *runResult)

		status := statusFail
		if runResult.Success {
			status = statusPass
		}
		if runResult.Error != "" {
			status = statusError
		}
		fmt.Fprintf(os.Stderr, "--- %s: %s (%s)\n", recipe, status, time.Duration(runResult.Duration))
	}

	result.Duration = Duration(time.Since(startedAt))

	// Write suite summary
	suiteDir := filepath.Join(s.runner.config.ResultsDir, suiteID)
	suiteJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: marshal suite.json: %v\n", err)
	} else if err := os.WriteFile(filepath.Join(suiteDir, "suite.json"), suiteJSON, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "warning: write suite.json: %v\n", err)
	}

	return result, nil
}

// generateSuiteID creates a unique suite identifier.
func generateSuiteID() string {
	ts := time.Now().Format("20060102t150405")
	randBytes := make([]byte, 3)
	if _, err := rand.Read(randBytes); err != nil {
		return ts
	}
	return ts + "-" + hex.EncodeToString(randBytes)
}

// ScenarioSuiteResult aggregates results from running multiple scenarios
// under a single suite ID. Distinct from SuiteResult (recipes) so the
// downstream aggregator can key off the per-scenario Grade + Assessment
// without an awkward recipe-shaped envelope.
type ScenarioSuiteResult struct {
	SuiteID   string           `json:"suiteId"`
	Results   []ScenarioResult `json:"results"`
	StartedAt time.Time        `json:"startedAt"`
	Duration  Duration         `json:"duration"`
}

// RunAllScenarios executes the given scenario files sequentially under a
// single suite ID. Cleanup between scenarios is handled by RunScenario
// itself (post-scenario CleanupProject in scenario_run.go). Per-scenario
// failures do NOT abort the suite — the user wants the full triage signal,
// not a fast-fail.
//
// Returns the aggregated result; suite.json is also written to
// <ResultsDir>/<suiteID>/suite.json so `zcp eval results` can load it
// (the file shape mirrors SuiteResult enough for the existing reader to
// not crash, and the dedicated scenario-suite reader gets the full shape).
func (s *Suite) RunAllScenarios(ctx context.Context, scenarioPaths []string) (*ScenarioSuiteResult, error) {
	suiteID := generateSuiteID()
	startedAt := time.Now()

	result := &ScenarioSuiteResult{
		SuiteID:   suiteID,
		StartedAt: startedAt,
	}

	for _, path := range scenarioPaths {
		select {
		case <-ctx.Done():
			result.Duration = Duration(time.Since(startedAt))
			return result, ctx.Err()
		default:
		}

		fmt.Fprintf(os.Stderr, "=== scenario: %s ===\n", path)
		scenarioResult, err := s.runner.RunScenario(ctx, path, suiteID)
		if err != nil {
			// Hard-fail propagates only when we couldn't even READ the
			// scenario or produce a result file — per-run grade failures
			// land on scenarioResult.Grade.Failures, not here.
			return nil, fmt.Errorf("run %s: %w", path, err)
		}

		result.Results = append(result.Results, *scenarioResult)

		status := statusFail
		if scenarioResult.Grade.Passed {
			status = statusPass
		}
		if scenarioResult.Error != "" {
			status = statusError
		}
		fmt.Fprintf(os.Stderr, "--- %s: %s (%s)\n", scenarioResult.ScenarioID, status, time.Duration(scenarioResult.Duration))
	}

	result.Duration = Duration(time.Since(startedAt))

	// Write suite summary alongside per-scenario result.json files. Same
	// path convention as recipe Suite so `zcp eval results --suite <id>`
	// finds it without per-mode dispatching.
	suiteDir := filepath.Join(s.runner.config.ResultsDir, suiteID)
	if err := os.MkdirAll(suiteDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "warning: mkdir suite dir: %v\n", err)
	}
	suiteJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: marshal scenario-suite: %v\n", err)
	} else if err := os.WriteFile(filepath.Join(suiteDir, "suite.json"), suiteJSON, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "warning: write suite.json: %v\n", err)
	}

	return result, nil
}
