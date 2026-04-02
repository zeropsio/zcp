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
)

// InstructionEvalConfig configures the instruction A/B evaluation.
type InstructionEvalConfig struct {
	MCPConfigTemplate string        // Path to base .mcp.json (ZCP server config)
	ResultsDir        string        // Output directory for results
	Model             string        // Claude model (default: "sonnet")
	MaxTurns          int           // Max turns per eval (default: 5)
	Timeout           time.Duration // Timeout per eval (default: 2 min)
	Variants          []string      // Variant IDs to test (empty = all)
	Scenarios         []string      // Scenario IDs to test (empty = all)
}

// InstructionEvalResult holds the result of a single variant × scenario evaluation.
type InstructionEvalResult struct {
	VariantID  string     `json:"variantId"`
	ScenarioID string     `json:"scenarioId"`
	Prompt     string     `json:"prompt"`
	ToolCalls  []ToolCall `json:"toolCalls"`
	Score      EvalScore  `json:"score"`
	Duration   Duration   `json:"duration"`
	Error      string     `json:"error,omitempty"`
}

// EvalScore grades the LLM's behavior.
type EvalScore struct {
	DiscoverFirst bool `json:"discoverFirst"` // zerops_discover called before any Read/Edit/Grep
	WorkflowFirst bool `json:"workflowFirst"` // zerops_workflow called before any Read/Edit/Grep
	AnyZCPFirst   bool `json:"anyZcpFirst"`   // any ZCP tool called before file operations
	Pass          bool `json:"pass"`          // overall pass: discover or workflow before file ops
}

// RunInstructionEval executes the full A/B evaluation matrix.
func RunInstructionEval(ctx context.Context, config InstructionEvalConfig) ([]InstructionEvalResult, error) {
	if config.Model == "" {
		config.Model = defaultModel
	}
	if config.MaxTurns == 0 {
		config.MaxTurns = 5
	}
	if config.Timeout == 0 {
		config.Timeout = 2 * time.Minute
	}

	variants := filterVariants(InstructionVariants(), config.Variants)
	scenarios := filterScenarios(InstructionScenarios(), config.Scenarios)

	if err := os.MkdirAll(config.ResultsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create results dir: %w", err)
	}

	var results []InstructionEvalResult

	for _, v := range variants {
		for _, s := range scenarios {
			fmt.Fprintf(os.Stderr, "eval: %s × %s ...\n", v.ID, s.ID)

			result := runSingleEval(ctx, config, v, s)
			results = append(results, result)

			// Write individual result.
			outPath := filepath.Join(config.ResultsDir, fmt.Sprintf("%s_%s.json", v.ID, s.ID))
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return nil, fmt.Errorf("marshal result: %w", err)
			}
			_ = os.WriteFile(outPath, data, 0o600)

			fmt.Fprintf(os.Stderr, "  → pass=%v discover=%v workflow=%v (%s)\n",
				result.Score.Pass, result.Score.DiscoverFirst, result.Score.WorkflowFirst, result.Duration)
		}
	}

	// Write summary.
	summaryPath := filepath.Join(config.ResultsDir, "summary.json")
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal summary: %w", err)
	}
	_ = os.WriteFile(summaryPath, data, 0o600)

	// Print matrix.
	printMatrix(results, variants, scenarios)

	return results, nil
}

func runSingleEval(ctx context.Context, config InstructionEvalConfig, v InstructionVariant, s InstructionScenario) InstructionEvalResult {
	start := time.Now()
	result := InstructionEvalResult{
		VariantID:  v.ID,
		ScenarioID: s.ID,
		Prompt:     s.Prompt,
	}

	evalCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	// Create temp MCP config with custom instructions.
	mcpConfig, err := createMCPConfigWithInstructions(config.MCPConfigTemplate, v)
	if err != nil {
		result.Error = fmt.Sprintf("create mcp config: %v", err)
		result.Duration = Duration(time.Since(start))
		return result
	}
	defer os.Remove(mcpConfig)

	// Create temp log file.
	logFile := filepath.Join(config.ResultsDir, fmt.Sprintf("%s_%s.jsonl", v.ID, s.ID))

	// Spawn Claude CLI.
	args := []string{
		"-p", s.Prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--no-session-persistence",
		"--dangerously-skip-permissions",
		"--model", config.Model,
		"--max-turns", fmt.Sprintf("%d", config.MaxTurns),
		"--mcp-config", mcpConfig,
	}

	cmd := exec.CommandContext(evalCtx, "claude", args...)
	outFile, err := os.Create(logFile)
	if err != nil {
		result.Error = fmt.Sprintf("create log: %v", err)
		result.Duration = Duration(time.Since(start))
		return result
	}
	cmd.Stdout = outFile
	cmd.Stderr = os.Stderr

	_ = cmd.Run() // ignore error — timeout is expected (we limit turns)
	outFile.Close()

	// Parse tool calls from log.
	logData, _ := os.ReadFile(logFile)
	result.ToolCalls = ExtractToolCalls(string(logData))

	// Score.
	result.Score = scoreToolCalls(result.ToolCalls)
	result.Duration = Duration(time.Since(start))

	return result
}

// scoreToolCalls evaluates whether ZCP tools were called before file operations.
func scoreToolCalls(calls []ToolCall) EvalScore {
	var score EvalScore

	fileOps := map[string]bool{
		"Read": true, "Edit": true, "Write": true, "Grep": true, "Glob": true,
		"Bash": true, "Agent": true,
	}
	zcpTools := map[string]bool{
		"zerops_discover": true, "zerops_workflow": true, "zerops_knowledge": true,
		"zerops_deploy": true, "zerops_manage": true, "zerops_scale": true,
		"zerops_import": true, "zerops_verify": true, "zerops_env": true,
		"zerops_subdomain": true, "zerops_mount": true, "zerops_logs": true,
		"zerops_events": true, "zerops_process": true, "zerops_delete": true,
	}

	sawFileOp := false
	for _, c := range calls {
		name := c.Name
		if fileOps[name] {
			sawFileOp = true
			continue
		}
		if zcpTools[name] && !sawFileOp {
			score.AnyZCPFirst = true
			if name == "zerops_discover" {
				score.DiscoverFirst = true
			}
			if name == "zerops_workflow" {
				score.WorkflowFirst = true
			}
		}
	}

	// Pass if discover or workflow was called before any file operation.
	score.Pass = score.DiscoverFirst || score.WorkflowFirst

	return score
}

// createMCPConfigWithInstructions creates a temporary .mcp.json that configures
// ZCP with custom instruction variant via ZCP_INSTRUCTION_OVERRIDE env var.
func createMCPConfigWithInstructions(templatePath string, v InstructionVariant) (string, error) {
	// Read template.
	data, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("read template: %w", err)
	}

	// Parse and inject instruction override env var.
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("parse config: %w", err)
	}

	servers, ok := config["mcpServers"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("missing mcpServers")
	}

	// Find the ZCP server entry (try "zerops", "zcp").
	var serverEntry map[string]any
	for _, name := range []string{"zerops", "zcp"} {
		if entry, ok := servers[name].(map[string]any); ok {
			serverEntry = entry
			break
		}
	}
	if serverEntry == nil {
		return "", fmt.Errorf("no zerops/zcp server in config")
	}

	// Set instruction override in env.
	env, _ := serverEntry["env"].(map[string]any)
	if env == nil {
		env = make(map[string]any)
	}
	env["ZCP_INSTRUCTION_BASE"] = v.Base
	env["ZCP_INSTRUCTION_CONTAINER"] = v.Container
	env["ZCP_INSTRUCTION_LOCAL"] = v.Local
	serverEntry["env"] = env

	// Write to temp file.
	out, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal config: %w", err)
	}

	tmp, err := os.CreateTemp("", "mcp-eval-*.json")
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("write temp: %w", err)
	}
	tmp.Close()
	return tmp.Name(), nil
}

func filterVariants(all []InstructionVariant, ids []string) []InstructionVariant {
	if len(ids) == 0 {
		return all
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	var filtered []InstructionVariant
	for _, v := range all {
		if set[v.ID] {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

func filterScenarios(all []InstructionScenario, ids []string) []InstructionScenario {
	if len(ids) == 0 {
		return all
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	var filtered []InstructionScenario
	for _, s := range all {
		if set[s.ID] {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

func printMatrix(results []InstructionEvalResult, variants []InstructionVariant, scenarios []InstructionScenario) {
	// Header.
	fmt.Fprintf(os.Stderr, "\n%-15s", "variant")
	for _, s := range scenarios {
		fmt.Fprintf(os.Stderr, " %-12s", s.ID)
	}
	fmt.Fprintf(os.Stderr, " TOTAL\n")
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("-", 15+13*len(scenarios)+6))

	// Rows.
	for _, v := range variants {
		fmt.Fprintf(os.Stderr, "%-15s", v.ID)
		total := 0
		for _, s := range scenarios {
			for _, r := range results {
				if r.VariantID == v.ID && r.ScenarioID == s.ID {
					if r.Score.Pass {
						fmt.Fprintf(os.Stderr, " %-12s", "✓")
						total++
					} else {
						fmt.Fprintf(os.Stderr, " %-12s", "✗")
					}
				}
			}
		}
		fmt.Fprintf(os.Stderr, " %d/%d\n", total, len(scenarios))
	}
}
