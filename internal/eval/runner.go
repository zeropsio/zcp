package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
)

// RunnerConfig holds configuration for the eval runner.
type RunnerConfig struct {
	MCPConfig  string        // Path to MCP config file (e.g., ~/.mcp.json)
	ResultsDir string        // Base directory for results output
	WorkDir    string        // Working directory on zcpx (default: /var/www)
	Model      string        // Claude model to use (default: "sonnet")
	MaxTurns   int           // Max turns per eval (default: 60)
	Timeout    time.Duration // Timeout per recipe (default: 15 min)
}

// Runner executes single recipe evaluations.
type Runner struct {
	config    RunnerConfig
	store     *knowledge.Store
	client    platform.Client
	projectID string
}

// NewRunner creates a new eval runner.
func NewRunner(config RunnerConfig, store *knowledge.Store, client platform.Client, projectID string) *Runner {
	if config.Model == "" {
		config.Model = "sonnet"
	}
	if config.MaxTurns == 0 {
		config.MaxTurns = 60
	}
	if config.Timeout == 0 {
		config.Timeout = 15 * time.Minute
	}
	if config.WorkDir == "" {
		config.WorkDir = "/var/www"
	}
	return &Runner{
		config:    config,
		store:     store,
		client:    client,
		projectID: projectID,
	}
}

// Run executes a single recipe evaluation.
func (r *Runner) Run(ctx context.Context, recipeName, suiteID string) (*RunResult, error) {
	startedAt := time.Now()
	result := &RunResult{
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

	// 6. Clean Claude auto-memory to prevent cross-contamination
	if err := cleanClaudeMemory(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: clean memory: %v\n", err)
	}

	// 7. Spawn Claude CLI
	logFile := filepath.Join(outDir, "log.jsonl")
	evalCtx, cancel := context.WithTimeout(ctx, r.config.Timeout)
	defer cancel()

	if err := r.spawnClaude(evalCtx, prompt, logFile); err != nil {
		result.Error = fmt.Sprintf("claude cli: %v", err)
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

	// 10. Full project cleanup: delete all services (except zcpx), clean files, reset workflow
	fmt.Fprintf(os.Stderr, "cleanup: %s...\n", recipeName)
	if cleanErr := CleanupProject(ctx, r.client, r.projectID, r.config.WorkDir); cleanErr != nil {
		fmt.Fprintf(os.Stderr, "warning: cleanup %s: %v\n", recipeName, cleanErr)
	}

	return result, nil
}

// spawnClaude invokes the Claude CLI in headless mode and captures output.
func (r *Runner) spawnClaude(ctx context.Context, prompt, logFile string) error {
	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--no-session-persistence",
		"--dangerously-skip-permissions",
		"--model", r.config.Model,
		"--max-turns", fmt.Sprintf("%d", r.config.MaxTurns),
	}

	if r.config.MCPConfig != "" {
		args = append(args, "--mcp-config", r.config.MCPConfig)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)

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
	afterIdx := indexOf(text, after)
	if afterIdx < 0 {
		return -1
	}
	rest := text[afterIdx:]
	idx := indexOf(rest, needle)
	if idx < 0 {
		return -1
	}
	return afterIdx + idx
}

func indexOf(text, substr string) int {
	for i := 0; i <= len(text)-len(substr); i++ {
		if text[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
