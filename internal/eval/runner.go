package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// RunnerConfig holds configuration for the eval runner.
type RunnerConfig struct {
	MCPConfig  string              // Path to MCP config file (e.g., ~/.mcp.json)
	ResultsDir string              // Base directory for results output
	WorkDir    string              // Working directory on zcp (default: /var/www)
	ClaudeHome string              // Override for the agent's .claude config dir; empty = ~/.claude. Local-mode eval sets a scenario-scoped temp dir so it doesn't wipe the operator's real Claude memory between scenarios.
	Model      string              // Claude model to use (default: "sonnet")
	MaxTurns   int                 // Max turns per eval (default: 100)
	Timeout    time.Duration       // Timeout per recipe (default: 15 min)
	Capture    *capture.Connection // Active raw-capture window; nil when capture is off.
}

// Runner executes single recipe evaluations.
type Runner struct {
	config          RunnerConfig
	store           *knowledge.Store
	client          platform.Client
	projectID       string
	httpDoer        ops.HTTPDoer
	userSimOverride UserSimRunner
	strictMCPConfig bool
}

// NewRunner creates a new eval runner.
func NewRunner(config RunnerConfig, store *knowledge.Store, client platform.Client, projectID string) *Runner {
	if config.Model == "" {
		config.Model = defaultModel
	}
	if config.MaxTurns == 0 {
		// 100 (was 60). Multi-runtime fullstack scenarios and frontend SSR
		// recipes routinely cross 60 in suite 20260504-104436 — both
		// greenfield-fullstack-multi-runtime and recipe-nextjs-ssr-frontend-
		// standard hit error_max_turns there. 100 is the smallest cap that
		// covered every observed legitimate happy-path run while still
		// bounding runaway sessions.
		config.MaxTurns = 100
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Minute
	}
	if config.WorkDir == "" {
		config.WorkDir = "/var/www"
	}
	return &Runner{
		config:    config,
		store:     store,
		client:    client,
		projectID: projectID,
		// 10s is enough for a single GET against a freshly-deployed subdomain;
		// the scenario-level timeout already bounds the full run.
		httpDoer: &http.Client{Timeout: 10 * time.Second},
	}
}

// Run executes a single recipe evaluation.
func (r *Runner) Run(ctx context.Context, recipeName, suiteID string) (result *RunResult, returnErr error) {
	startedAt := time.Now()
	result = &RunResult{
		Recipe:    recipeName,
		RunID:     suiteID,
		StartedAt: startedAt,
	}

	// 1. Load recipe from knowledge store
	doc, err := r.store.Get("zerops://recipes/" + recipeName)
	if err != nil {
		result.Error = fmt.Sprintf("load recipe: %v", err)
		result.Duration = Duration(time.Since(startedAt))
		return result, nil
	}

	// 2. Parse recipe metadata
	meta, err := ParseRecipeMetadata(recipeName, doc.Content)
	if err != nil {
		result.Error = fmt.Sprintf("parse recipe: %v", err)
		result.Duration = Duration(time.Since(startedAt))
		return result, nil
	}

	// 3. Generate hostnames and prompt
	hostnames := GenerateHostnames(meta)
	prompt := BuildFullPrompt(meta, hostnames)

	// 4. Create output directory
	outDir := filepath.Join(r.config.ResultsDir, suiteID, recipeName)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}
	r.captureScenarioStart(ctx, suiteID, recipeName)
	defer func() {
		status := capture.CaptureComplete
		var scenarioErr error
		if returnErr != nil {
			status = capture.CapturePartial
			scenarioErr = returnErr
		} else if result != nil && result.Error != "" {
			status = capture.CapturePartial
			scenarioErr = fmt.Errorf("%s", result.Error)
		}
		if r.config.Capture != nil {
			paths, bundleErr := capture.BundleEvalScenario(r.config.Capture.SessionDir, suiteID, recipeName, "", outDir)
			if bundleErr != nil {
				fmt.Fprintf(os.Stderr, "warning: capture eval artifacts: %v\n", bundleErr)
				status = capture.CapturePartial
				if scenarioErr == nil {
					scenarioErr = bundleErr
				}
			}
			for _, path := range paths {
				r.captureMark(context.Background(), capture.LifecycleMarker{Kind: capture.LifecycleArtifact, EvalRunID: suiteID, ScenarioRunID: recipeName, ArtifactPath: path})
			}
		}
		r.captureScenarioEnd(context.Background(), suiteID, recipeName, status, scenarioErr)
	}()

	// 5. Write prompt and metadata
	if err := os.WriteFile(filepath.Join(outDir, "task-prompt.txt"), []byte(prompt), 0o600); err != nil {
		return nil, fmt.Errorf("write prompt: %w", err)
	}

	metaJSON, err := json.MarshalIndent(struct {
		Recipe    string            `json:"recipe"`
		RunID     string            `json:"runId"`
		Hostnames map[string]string `json:"hostnames"`
		Meta      *RecipeMetadata   `json:"metadata"`
		StartedAt time.Time         `json:"startedAt"`
	}{recipeName, suiteID, hostnames, meta, startedAt}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "metadata.json"), metaJSON, 0o600); err != nil {
		return nil, fmt.Errorf("write metadata: %w", err)
	}
	if err := r.prepareCaptureMCPConfig(outDir); err != nil {
		return nil, err
	}

	// 6. Clean Claude auto-memory to prevent cross-contamination
	if err := cleanClaudeMemory(r.config.ClaudeHome); err != nil {
		fmt.Fprintf(os.Stderr, "warning: clean memory: %v\n", err)
	}

	// 7. Spawn Claude CLI
	logFile := filepath.Join(outDir, "log.jsonl")
	evalCtx, cancel := context.WithTimeout(ctx, r.config.Timeout)
	defer cancel()

	invocationID := recipeName + "/agent.initial"
	invocation := r.captureInvocationStart(ctx, suiteID, recipeName, invocationID, "agent.initial", "")
	spawnErr := r.spawnClaude(evalCtx, prompt, logFile, captureProcessScope{evalRunID: suiteID, scenarioRunID: recipeName, invocationID: invocationID, phase: "agent.initial"})
	if spawnErr != nil {
		result.Error = fmt.Sprintf("claude cli: %v", spawnErr)
		invocation.End(context.Background(), capture.CapturePartial, spawnErr)
	} else {
		if sessionID, sessionErr := extractSessionID(logFile); sessionErr == nil {
			invocation.Bind(context.Background(), sessionID)
		} else {
			fmt.Fprintf(os.Stderr, "warning: capture recipe session binding: %v\n", sessionErr)
		}
		invocation.End(context.Background(), capture.CaptureComplete, nil)
	}

	// 8. Extract assessment from log
	logData, _ := os.ReadFile(logFile)
	logStr := string(logData)

	assessment, found := ExtractAssessment(logStr)
	if found {
		result.Assessment = assessment
		result.Success = isSuccessfulAssessment(assessment)
		if err := os.WriteFile(filepath.Join(outDir, "assessment.md"), []byte(assessment), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "warning: write assessment: %v\n", err)
		}
	}

	// 9. Extract tool calls
	calls := ExtractToolCalls(logStr)
	if len(calls) > 0 {
		callsJSON, err := json.MarshalIndent(calls, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: marshal tool calls: %v\n", err)
		} else if err := os.WriteFile(filepath.Join(outDir, "tool-calls.json"), callsJSON, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "warning: write tool calls: %v\n", err)
		}
	}

	result.LogFile = logFile
	result.Duration = Duration(time.Since(startedAt))

	// 10. Full project cleanup: delete all services (except zcp), clean files, reset workflow
	fmt.Fprintf(os.Stderr, "cleanup: %s...\n", recipeName)
	if cleanErr := CleanupProject(ctx, r.client, r.projectID, r.config.WorkDir); cleanErr != nil {
		fmt.Fprintf(os.Stderr, "warning: cleanup %s: %v\n", recipeName, cleanErr)
	}

	return result, nil
}

// spawnClaude invokes the Claude CLI in headless mode and captures output.
func (r *Runner) spawnClaude(ctx context.Context, prompt, logFile string, captureScope captureProcessScope) error {
	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--no-session-persistence",
		"--dangerously-skip-permissions",
		"--model", r.config.Model,
		"--max-turns", fmt.Sprintf("%d", r.config.MaxTurns),
	}

	args = r.appendMCPConfigArgs(args)

	cmd := exec.CommandContext(ctx, "claude", args...)
	if r.config.WorkDir != "" {
		cmd.Dir = r.config.WorkDir
	}
	cmd.Env = r.claudeEnvForScope(captureScope)

	outFile, err := os.Create(logFile)
	if err != nil {
		return fmt.Errorf("create log file: %w", err)
	}
	defer outFile.Close()

	var stderr bytes.Buffer
	cmd.Stdout = outFile
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Context cancellation is expected (timeout)
		if ctx.Err() != nil {
			return fmt.Errorf("timeout after %s", r.config.Timeout)
		}
		return fmt.Errorf("claude exited: %w (stderr: %s)", err, stderr.String())
	}

	return nil
}

// isSuccessfulAssessment checks if the assessment indicates SUCCESS.
func isSuccessfulAssessment(assessment string) bool {
	// Look for "State: SUCCESS" in the deployment outcome section
	for _, line := range []string{"State: SUCCESS", "SUCCESS"} {
		idx := findAfter(assessment, "Deployment outcome", line)
		if idx >= 0 {
			return true
		}
	}
	return false
}

// findAfter returns the position of needle after the first occurrence of after.
func findAfter(text, after, needle string) int {
	afterIdx := strings.Index(text, after)
	if afterIdx < 0 {
		return -1
	}
	rest := text[afterIdx:]
	idx := strings.Index(rest, needle)
	if idx < 0 {
		return -1
	}
	return afterIdx + idx
}
