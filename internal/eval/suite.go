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

		status := "FAIL"
		if runResult.Success {
			status = "PASS"
		}
		if runResult.Error != "" {
			status = "ERROR"
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
