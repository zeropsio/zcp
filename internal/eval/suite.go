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

	"github.com/zeropsio/zcp/internal/capture"
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
func (s *Suite) RunAll(ctx context.Context, recipes []string) (result *SuiteResult, returnErr error) {
	suiteID := generateSuiteID()
	startedAt := time.Now()

	result = &SuiteResult{
		SuiteID:   suiteID,
		StartedAt: startedAt,
	}
	s.runner.BeginCaptureEvalRun(ctx, suiteID)
	defer func() {
		status := capture.CaptureComplete
		var runErr error
		if returnErr != nil {
			status = capture.CapturePartial
			runErr = returnErr
		} else {
			for _, runResult := range result.Results {
				if runResult.Error != "" {
					status = capture.CapturePartial
					runErr = fmt.Errorf("one or more recipe evaluations failed")
					break
				}
			}
		}
		s.runner.EndCaptureEvalRun(context.WithoutCancel(ctx), suiteID, status, runErr)
	}()

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
