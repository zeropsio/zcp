package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	initcmd "github.com/zeropsio/zcp/internal/init"
	"github.com/zeropsio/zcp/internal/runtime"
)

// ScenarioResult captures the outcome of a single scenario run.
type ScenarioResult struct {
	ScenarioID string         `json:"scenarioId"`
	SuiteID    string         `json:"suiteId"`
	Grade      GradeResult    `json:"grade"`
	LogFile    string         `json:"logFile"`
	Assessment string         `json:"assessment,omitempty"`
	FinalURL   *FinalURLProbe `json:"finalUrl,omitempty"`
	Duration   Duration       `json:"duration"`
	StartedAt  time.Time      `json:"startedAt"`
	Error      string         `json:"error,omitempty"`
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

	// Scenarios that test state DETECTION (incomplete bootstrap, bootstrapped
	// but not-yet-deployed, orphaned metas, etc.) need local state pre-populated
	// AFTER init wipes the workdir. The preseed script runs with CWD = WorkDir
	// and receives the scenario ID + suite ID in env so it can write
	// deterministic state files.
	if err := r.runPreseedScript(ctx, sc, suiteID); err != nil {
		result.Error = fmt.Sprintf("preseed: %v", err)
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

	var probe *FinalURLProbe
	if sc.Expect.FinalURLStatus != 0 {
		hostname := sc.Expect.FinalURLHostname
		if hostname == "" {
			// Auto-discover the single web-facing runtime — greenfield
			// scenarios let the LLM choose its own hostname, so probing
			// works without coupling the scenario to an implementation
			// detail. If 0 or >1 candidates exist the resolver surfaces
			// that as a grader failure.
			resolved, err := ResolveProbeHostname(ctx, r.client, r.projectID)
			if err != nil {
				probe = &FinalURLProbe{Err: err.Error()}
			} else {
				p := ProbeFinalURL(ctx, r.client, r.httpDoer, r.projectID, resolved)
				probe = &p
			}
		} else {
			p := ProbeFinalURL(ctx, r.client, r.httpDoer, r.projectID, hostname)
			probe = &p
		}
		result.FinalURL = probe
	}

	result.Grade = GradeWithProbe(sc, logStr, calls, assessment, probe)
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

// runPreseedScript executes the scenario's PreseedScript, if any, with CWD set
// to the eval workdir. The script receives ZCP_SCENARIO_ID, ZCP_SUITE_ID, and
// ZCP_WORK_DIR in env so it can write state files deterministically without
// hardcoding paths. No-op when PreseedScript is empty — most scenarios don't
// need any pre-population beyond what SeedImported / SeedDeployed provides.
func (r *Runner) runPreseedScript(ctx context.Context, sc *Scenario, suiteID string) error {
	if sc.PreseedScript == "" {
		return nil
	}
	path := sc.PreseedScript
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(sc.SourcePath), path)
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("preseed script %q: %w", path, err)
	}
	cmd := exec.CommandContext(ctx, "bash", path)
	cmd.Dir = r.config.WorkDir
	cmd.Env = append(os.Environ(),
		"ZCP_SCENARIO_ID="+sc.ID,
		"ZCP_SUITE_ID="+suiteID,
		"ZCP_WORK_DIR="+r.config.WorkDir,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("preseed script: %w\n--- output ---\n%s", err, string(out))
	}
	return nil
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
// verbatim, follow-up questions (if any) are appended so the agent answers
// them at the end of its run — giving us post-hoc reasoning capture without
// needing session resume — and when Expect.RequireAssessment is true, the
// shared assessmentInstructions block is appended so the grader can gate on
// the agent's own EVAL REPORT.
func buildScenarioPrompt(sc *Scenario) string {
	var b strings.Builder
	b.WriteString(sc.Prompt)
	if len(sc.FollowUp) > 0 {
		b.WriteString("\n\n---\n\nPo dokončení úkolu odpověz stručně na následující otázky (jedním odstavcem každá):\n")
		for i, q := range sc.FollowUp {
			fmt.Fprintf(&b, "%d. %s\n", i+1, q)
		}
	}
	if sc.Expect.RequireAssessment {
		b.WriteString("\n")
		b.WriteString(assessmentInstructions)
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
