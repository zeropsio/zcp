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

// defaultModel is the runner's fallback Claude model when a caller doesn't
// set RunnerConfig.Model (NewRunner, runner.go). It is the eval package's
// own default for the agent invocations it drives — never a stand-in for
// what a run's own candidate model was: that is always read off the wire
// and recorded, never assumed from this constant (docs/spec-eval-farm.md
// §2.4 FM-16 keeps the two distinct for farm runs).
//
// claude-opus-4-6[1m]'s 1M-context window was pinned here for recipe
// creation specifically (v13 regressed badly on Sonnet/200k — see git log
// for the measurement) — that tradeoff no longer applies now that
// claude-sonnet-5 is current; override per-call where a specific eval still
// needs the wider window.
const defaultModel = "claude-sonnet-5"

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
