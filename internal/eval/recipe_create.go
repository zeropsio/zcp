package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/schema"
)

// RecipeCreateConfig holds configuration for headless recipe creation.
type RecipeCreateConfig struct {
	MCPConfig  string        // Path to MCP config file
	ResultsDir string        // Base directory for results output
	Model      string        // Claude model to use (default: "sonnet")
	MaxTurns   int           // Max turns per recipe (default: 120)
	Timeout    time.Duration // Timeout per recipe (default: 30 min)
}

// RecipeCreateResult holds the outcome of a headless recipe creation.
type RecipeCreateResult struct {
	Framework string    `json:"framework"`
	Tier      string    `json:"tier"`
	Slug      string    `json:"slug"`
	Success   bool      `json:"success"`
	LogFile   string    `json:"logFile"`
	Duration  Duration  `json:"duration"`
	StartedAt time.Time `json:"startedAt"`
	Error     string    `json:"error,omitempty"`
}

// RecipeCreator executes headless recipe creation via Claude CLI.
type RecipeCreator struct {
	config RecipeCreateConfig
}

// NewRecipeCreator creates a new headless recipe creator.
func NewRecipeCreator(config RecipeCreateConfig) *RecipeCreator {
	if config.Model == "" {
		config.Model = defaultModel
	}
	if config.MaxTurns == 0 {
		config.MaxTurns = 120
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Minute
	}
	return &RecipeCreator{config: config}
}

// CreateRecipe spawns Claude CLI headlessly to create a recipe via ZCP workflow.
func (c *RecipeCreator) CreateRecipe(ctx context.Context, framework, tier string) (*RecipeCreateResult, error) {
	startedAt := time.Now()
	slug := framework + "-hello-world"
	if tier == "showcase" {
		slug = framework + "-showcase"
	}

	// Create output directory.
	runID := fmt.Sprintf("create-%s-%s", slug, startedAt.Format("20060102t150405"))
	outDir := filepath.Join(c.config.ResultsDir, runID)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	// Build prompt.
	prompt := BuildRecipeCreatePrompt(ctx, framework, tier)

	// Write prompt to file for inspection.
	promptFile := filepath.Join(outDir, "prompt.md")
	if err := os.WriteFile(promptFile, []byte(prompt), 0o600); err != nil {
		return nil, fmt.Errorf("write prompt: %w", err)
	}

	// Write metadata.
	meta := map[string]string{
		"framework": framework,
		"tier":      tier,
		"slug":      slug,
		"model":     c.config.Model,
	}
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "metadata.json"), metaData, 0o600); err != nil {
		return nil, fmt.Errorf("write metadata: %w", err)
	}

	// Spawn Claude CLI.
	logFile := filepath.Join(outDir, "claude.jsonl")
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	spawnErr := c.spawnClaude(ctx, prompt, logFile)

	duration := Duration(time.Since(startedAt))
	result := &RecipeCreateResult{
		Framework: framework,
		Tier:      tier,
		Slug:      slug,
		LogFile:   logFile,
		Duration:  duration,
		StartedAt: startedAt,
	}

	if spawnErr != nil {
		result.Error = spawnErr.Error()
		return result, nil
	}

	// Check for success in log output.
	result.Success = checkCreateSuccess(logFile)

	return result, nil
}

// spawnClaude invokes the Claude CLI in headless mode for recipe creation.
func (c *RecipeCreator) spawnClaude(ctx context.Context, prompt, logFile string) error {
	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--no-session-persistence",
		"--dangerously-skip-permissions",
		"--model", c.config.Model,
		"--max-turns", fmt.Sprintf("%d", c.config.MaxTurns),
	}

	if c.config.MCPConfig != "" {
		args = append(args, "--mcp-config", c.config.MCPConfig)
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
		if ctx.Err() != nil {
			return fmt.Errorf("timeout after %s", c.config.Timeout)
		}
		return fmt.Errorf("claude exited: %w (stderr: %s)", err, stderr.String())
	}

	return nil
}

// BuildRecipeCreatePrompt generates the prompt for headless recipe creation.
// Includes live schema context (best-effort) so the LLM knows valid service types
// and build/run bases before starting the workflow.
func BuildRecipeCreatePrompt(ctx context.Context, framework, tier string) string {
	schemaCtx := fetchSchemaContext(ctx)
	return schemaCtx + fmt.Sprintf(`Create a %s %s recipe for Zerops. Follow ZCP's recipe workflow exactly:

1. Start the recipe workflow:
   zerops_workflow action="start" workflow="recipe" intent="Create a %s %s recipe" tier="%s"

2. Follow each step's guidance precisely. The workflow has 6 steps:
   - research: Fill in all framework research fields, submit structured RecipePlan
   - provision: Create workspace services via import.yaml
   - generate: Write app code with zerops.yaml and README fragments
   - deploy: Deploy and verify health
   - finalize: Generate all 6 environment import.yaml files + READMEs
   - close: Complete or skip

3. For each step, read the guidance carefully and use the recommended tools.
   Complete each step with a descriptive attestation.

4. Quality requirements:
   - All README fragments must use exact ZEROPS_EXTRACT markers
   - Comment ratio >= 30%% in all YAML files
   - No PLACEHOLDER_* or TODO strings
   - All import.yaml files must be valid YAML with proper scaling per environment

Start now with step 1.
`, framework, tier, framework, tier, tier)
}

// fetchSchemaContext fetches live schemas and formats a compact context block.
// Best-effort: returns empty string on failure (eval continues without schema).
func fetchSchemaContext(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	schemas, err := schema.FetchSchemas(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zcp: eval: schema fetch failed (non-fatal): %v\n", err)
		return ""
	}

	out := schema.FormatBothForLLM(schemas)
	if out == "" {
		return ""
	}
	return "# Zerops Platform Schema (live)\n\n" + out + "\n---\n\n"
}

// RecipeCreateSuite runs batch recipe creation.
type RecipeCreateSuite struct {
	creator *RecipeCreator
}

// NewRecipeCreateSuite creates a new batch recipe creator.
func NewRecipeCreateSuite(creator *RecipeCreator) *RecipeCreateSuite {
	return &RecipeCreateSuite{creator: creator}
}

// CreateAll creates recipes for all specified frameworks sequentially.
func (s *RecipeCreateSuite) CreateAll(ctx context.Context, frameworks []string, tier string) (*SuiteResult, error) {
	suiteID := time.Now().Format("20060102t150405") + "-create"
	startedAt := time.Now()

	result := &SuiteResult{
		SuiteID:   suiteID,
		StartedAt: startedAt,
	}

	for _, framework := range frameworks {
		if ctx.Err() != nil {
			break
		}

		fmt.Fprintf(os.Stderr, "Creating %s %s recipe...\n", framework, tier)
		createResult, err := s.creator.CreateRecipe(ctx, framework, tier)
		if err != nil {
			return nil, fmt.Errorf("create %s: %w", framework, err)
		}

		// Convert to RunResult for suite compatibility.
		runResult := RunResult{
			Recipe:    createResult.Slug,
			RunID:     fmt.Sprintf("create-%s", createResult.Slug),
			Success:   createResult.Success,
			LogFile:   createResult.LogFile,
			Duration:  createResult.Duration,
			StartedAt: createResult.StartedAt,
			Error:     createResult.Error,
		}
		result.Results = append(result.Results, runResult)

		status := "PASS"
		if !createResult.Success {
			status = "FAIL"
		}
		if createResult.Error != "" {
			status = "ERROR"
		}
		fmt.Fprintf(os.Stderr, "  %s: %s (%s)\n", createResult.Slug, status, createResult.Duration)
	}

	result.Duration = Duration(time.Since(startedAt))
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("suite interrupted: %w", err)
	}
	return result, nil
}

// checkCreateSuccess inspects the log file for indicators of successful recipe creation.
func checkCreateSuccess(logFile string) bool {
	data, err := os.ReadFile(logFile)
	if err != nil {
		return false
	}
	content := string(data)
	// Check for recipe completion indicators in the log.
	return strings.Contains(content, "Recipe complete") || strings.Contains(content, "All steps finished")
}
