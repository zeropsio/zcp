package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	initcmd "github.com/zeropsio/zcp/internal/init"
	"github.com/zeropsio/zcp/internal/runtime"
)

// ScenarioResult captures the outcome of a single scenario run.
type ScenarioResult struct {
	ScenarioID string      `json:"scenarioId"`
	SuiteID    string      `json:"suiteId"`
	Grade      GradeResult `json:"grade"`
	LogFile    string      `json:"logFile"`
	Assessment string      `json:"assessment,omitempty"`
	Duration   Duration    `json:"duration"`
	StartedAt  time.Time   `json:"startedAt"`
	Error      string      `json:"error,omitempty"`
}

// RunScenario executes one scenario file end-to-end: seed → spawn Claude →
// extract tool calls + assessment → grade against expectations → write results.
// Artifacts land under <ResultsDir>/<suiteID>/<scenarioID>/.
func (r *Runner) RunScenario(ctx context.Context, scenarioPath, suiteID string) (*ScenarioResult, error) {
	startedAt := time.Now()
	sc, err := ParseScenario(scenarioPath)
	if err != nil {
		return nil, err
	}

	result := &ScenarioResult{
		ScenarioID: sc.ID,
		SuiteID:    suiteID,
		StartedAt:  startedAt,
	}

	outDir := filepath.Join(r.config.ResultsDir, suiteID, sc.ID)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	if err := r.seedScenario(ctx, sc, suiteID); err != nil {
		result.Error = fmt.Sprintf("seed: %v", err)
		result.Duration = Duration(time.Since(startedAt))
		writeScenarioResult(outDir, result)
		return result, nil
	}

	// Regenerate CLAUDE.md (+ other init artifacts) after seed so every scenario
	// starts with a clean template reflecting current atom corpus — not stale
	// REFLOG / service references from a previous run.
	if err := initcmd.Run(r.config.WorkDir, runtime.Detect()); err != nil {
		result.Error = fmt.Sprintf("init: %v", err)
		result.Duration = Duration(time.Since(startedAt))
		writeScenarioResult(outDir, result)
		return result, nil
	}

	prompt := buildScenarioPrompt(sc)
	if err := os.WriteFile(filepath.Join(outDir, "task-prompt.txt"), []byte(prompt), 0o600); err != nil {
		return nil, fmt.Errorf("write prompt: %w", err)
	}

	if err := cleanClaudeMemory(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: clean memory: %v\n", err)
	}

	logFile := filepath.Join(outDir, "log.jsonl")
	evalCtx, cancel := context.WithTimeout(ctx, r.config.Timeout)
	defer cancel()

	if err := r.spawnClaude(evalCtx, prompt, logFile); err != nil {
		result.Error = fmt.Sprintf("claude cli: %v", err)
	}

	logData, _ := os.ReadFile(logFile)
	logStr := string(logData)

	assessment, _ := ExtractAssessment(logStr)
	result.Assessment = assessment

	calls := ExtractToolCalls(logStr)
	if len(calls) > 0 {
		if callsJSON, err := json.MarshalIndent(calls, "", "  "); err == nil {
			_ = os.WriteFile(filepath.Join(outDir, "tool-calls.json"), callsJSON, 0o600)
		}
	}

	result.Grade = Grade(sc, logStr, calls)
	result.LogFile = logFile
	result.Duration = Duration(time.Since(startedAt))

	// Always cleanup after scenario — leaves no stale state for the next run.
	if cleanErr := CleanupProject(ctx, r.client, r.projectID, r.config.WorkDir); cleanErr != nil {
		fmt.Fprintf(os.Stderr, "warning: post-scenario cleanup: %v\n", cleanErr)
	}

	writeScenarioResult(outDir, result)
	return result, nil
}

// seedScenario runs the seed mode specified by the scenario. Fixture paths are
// resolved relative to the scenario file.
func (r *Runner) seedScenario(ctx context.Context, sc *Scenario, suiteID string) error {
	switch sc.Seed {
	case ModeEmpty:
		return SeedEmpty(ctx, r.client, r.projectID, r.config.WorkDir)
	case ModeImported:
		fixture := resolveFixturePath(sc)
		return SeedImported(ctx, r.client, r.projectID, fixture, r.config.WorkDir, suiteID)
	case ModeDeployed:
		fixture := resolveFixturePath(sc)
		return SeedDeployed(ctx, r.client, r.projectID, fixture, r.config.WorkDir, suiteID)
	default:
		return fmt.Errorf("unknown seed mode %q", sc.Seed)
	}
}

// resolveFixturePath resolves fixture path relative to the scenario source.
// Absolute paths are returned unchanged.
func resolveFixturePath(sc *Scenario) string {
	if filepath.IsAbs(sc.Fixture) {
		return sc.Fixture
	}
	return filepath.Join(filepath.Dir(sc.SourcePath), sc.Fixture)
}

// buildScenarioPrompt composes the agent prompt. The scenario body is used
// verbatim, and follow-up questions (if any) are appended so the agent answers
// them at the end of its run — giving us post-hoc reasoning capture without
// needing session resume.
func buildScenarioPrompt(sc *Scenario) string {
	var b strings.Builder
	b.WriteString(sc.Prompt)
	if len(sc.FollowUp) > 0 {
		b.WriteString("\n\n---\n\nPo dokončení úkolu odpověz stručně na následující otázky (jedním odstavcem každá):\n")
		for i, q := range sc.FollowUp {
			fmt.Fprintf(&b, "%d. %s\n", i+1, q)
		}
	}
	return b.String()
}

func writeScenarioResult(outDir string, r *ScenarioResult) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(outDir, "result.json"), data, 0o600)
}
