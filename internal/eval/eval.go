// Package eval runs LLM recipe evaluations via Claude CLI headless mode.
//
// For each recipe in the knowledge base, it spawns a Claude agent that performs
// a full bootstrap workflow (import → deploy → verify), then self-assesses
// what went well/wrong. The complete log + self-assessment is stored for analysis.
package eval

import (
	"encoding/json"
	"fmt"
	"time"
)

// defaultModel is the default Claude model for eval runs.
//
// Recipe creation specifically requires Opus with the 1M-token context window:
// the workflow pulls ~80 KB of guidance topics, ~30 KB of schemas, plus the
// agent's own code-writing context. v13 shipped on Sonnet/200k by accident and
// doubled the wall-clock time (40.7 → 79.8 min) plus regressed the close-step
// severity from 5 WRONG → 2 CRITICAL + 1 WRONG. Do not lower this default —
// override per-call when a weaker model is genuinely acceptable (e.g. simple
// instruction evals, not recipe creation).
const defaultModel = "claude-opus-4-6[1m]"

// RecipeMetadata holds parsed recipe data used for prompt generation.
type RecipeMetadata struct {
	Name     string       `json:"name"`
	Title    string       `json:"title"`
	Runtime  string       `json:"runtime"`
	Services []ServiceDef `json:"services"`
}

// ServiceDef defines a managed service from a recipe's import.yaml.
type ServiceDef struct {
	Type string `json:"type"` // e.g., "postgresql@16"
	Role string `json:"role"` // e.g., "db", "cache", "storage"
}

// RunResult captures the outcome of a single recipe evaluation.
type RunResult struct {
	Recipe     string    `json:"recipe"`
	RunID      string    `json:"runId"`
	Success    bool      `json:"success"`
	Assessment string    `json:"assessment"`      // Agent's self-assessment markdown
	LogFile    string    `json:"logFile"`         // Path to stream-json log
	Duration   Duration  `json:"duration"`        // Wall-clock time
	StartedAt  time.Time `json:"startedAt"`       // When the run started
	Error      string    `json:"error,omitempty"` // Non-empty if run failed before completion
}

// SuiteResult aggregates results from running multiple recipes.
type SuiteResult struct {
	SuiteID   string      `json:"suiteId"`
	Results   []RunResult `json:"results"`
	StartedAt time.Time   `json:"startedAt"`
	Duration  Duration    `json:"duration"`
}

// Duration wraps time.Duration for JSON serialization as a human-readable string.
type Duration time.Duration

func (d Duration) String() string {
	return time.Duration(d).String()
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("unmarshal duration: %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// ToolCall represents a single MCP tool invocation extracted from the log.
type ToolCall struct {
	Name   string `json:"name"`
	Input  string `json:"input"`  // JSON string of input
	Result string `json:"result"` // Result text
}
