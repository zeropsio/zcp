// Package ops — BrowserBatch wraps agent-browser with guaranteed lifecycle.
//
// The recipe workflow's close-step browser verification repeatedly burned
// on two failure modes: (1) missing close → daemon stays alive holding a
// Chrome process → fork budget exhausted → next Bash call crashes with
// "Resource temporarily unavailable"; (2) sequencing several bash calls
// that each spawn a new daemon round-trip, racing the single Chrome
// instance. BrowserBatch exists to make both mistakes impossible:
//
//   - Exactly ONE agent-browser invocation per URL.
//   - Tool controls the batch shape: [open url] + caller commands +
//     [errors] + [console] + [close]. Any open/close the caller puts
//     inside commands is stripped — the canonical wrappers are the only
//     lifecycle markers.
//   - All calls serialized via a package-level mutex. Two tools cannot
//     run in parallel.
//   - Context timeout bounded (default 120s, max 300s). On timeout OR
//     fork-exhaustion signature in stderr, the tool runs pkill recovery
//     automatically and surfaces a ForkRecoveryAttempted flag instead of
//     propagating the raw error.
//   - Structured JSON output — errorsOutput and consoleOutput extracted
//     from the canonical penultimate steps so the caller doesn't have
//     to scan a string.
package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// BrowserBatchInput is the caller-facing input for a browser walk.
type BrowserBatchInput struct {
	// URL is the page to open. Required.
	URL string `json:"url"`

	// Commands are the inner agent-browser commands to run AFTER open and
	// BEFORE the auto-appended [errors], [console], [close]. Each element
	// is one agent-browser command as a string array — e.g. ["snapshot",
	// "-i", "-c"], ["click", "@e1"], ["find", "role", "button", "Submit",
	// "click"]. Any "open" or "close" elements are stripped; the tool
	// always prepends [open url] and appends [close].
	Commands [][]string `json:"commands,omitempty"`

	// TimeoutSeconds bounds the whole batch. Default 120, max 300.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
}

// BrowserStepResult is one step from agent-browser's --json output.
type BrowserStepResult struct {
	Command []string        `json:"command"`
	Success bool            `json:"success"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *string         `json:"error,omitempty"`
}

// BrowserBatchResult is the structured return value.
type BrowserBatchResult struct {
	URL                   string              `json:"url"`
	Steps                 []BrowserStepResult `json:"steps,omitempty"`
	ErrorsOutput          json.RawMessage     `json:"errorsOutput,omitempty"`
	ConsoleOutput         json.RawMessage     `json:"consoleOutput,omitempty"`
	DurationMs            int64               `json:"durationMs"`
	ForkRecoveryAttempted bool                `json:"forkRecoveryAttempted,omitempty"`
	Message               string              `json:"message,omitempty"`
}

// browserRunner abstracts the agent-browser invocation for testability.
type browserRunner interface {
	LookPath() (string, error)
	Run(ctx context.Context, stdin string, timeout time.Duration) (stdout, stderr string, err error)
	RecoverFork(ctx context.Context)
}

// execBrowserRunner is the production runner.
type execBrowserRunner struct{}

func (execBrowserRunner) LookPath() (string, error) {
	return exec.LookPath("agent-browser")
}

func (execBrowserRunner) Run(ctx context.Context, stdin string, timeout time.Duration) (string, string, error) {
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(rctx, "agent-browser", "batch", "--json")
	cmd.Stdin = strings.NewReader(stdin)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	// If the parent ctx is still alive but the child ctx deadlined, normalise.
	if err != nil && errors.Is(rctx.Err(), context.DeadlineExceeded) {
		err = context.DeadlineExceeded
	}
	return out.String(), errBuf.String(), err
}

func (execBrowserRunner) RecoverFork(ctx context.Context) {
	// Best-effort — ignore errors. pkill with no matching processes returns
	// exit 1, which is fine. The two patterns match the agent-browser
	// daemon binary and the Chrome helper processes it spawns.
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = exec.CommandContext(pctx, "pkill", "-9", "-f", "agent-browser-darwin").Run()
	_ = exec.CommandContext(pctx, "pkill", "-9", "-f", "agent-browser-chrome-").Run()
	// Let the kernel reap the processes before the next attempt.
	time.Sleep(2 * time.Second)
}

// browserRun is the active runner. Tests override via OverrideBrowserRunnerForTest.
var browserRun browserRunner = execBrowserRunner{}

// browserMu serializes all BrowserBatch calls. agent-browser uses a
// single persistent daemon per container — concurrent calls either race
// the daemon or spawn a second one that exceeds the fork budget.
var browserMu sync.Mutex

// OverrideBrowserRunnerForTest replaces the browser runner. Returns restore.
func OverrideBrowserRunnerForTest(r browserRunner) func() {
	old := browserRun
	browserRun = r
	return func() { browserRun = old }
}

// AgentBrowserAvailable reports whether agent-browser is on PATH. Used by
// server registration to gate the zerops_browser tool — it is pointless to
// expose a tool that will always fail its LookPath check.
func AgentBrowserAvailable() bool {
	_, err := browserRun.LookPath()
	return err == nil
}

const (
	browserDefaultTimeout = 120 * time.Second
	browserMaxTimeout     = 300 * time.Second
	// browserCmdClose is the agent-browser close command. Named constant
	// so the three usages below stay in sync if the CLI ever changes.
	browserCmdClose = "close"
)

// BrowserBatch runs one bounded agent-browser session against the given URL.
// See package doc for the lifecycle contract.
func BrowserBatch(ctx context.Context, input BrowserBatchInput) (*BrowserBatchResult, error) {
	if strings.TrimSpace(input.URL) == "" {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"url is required",
			"Pass the subdomain URL to walk (e.g. the appstage zerops.app URL from zerops_discover)",
		)
	}

	timeout := time.Duration(input.TimeoutSeconds) * time.Second
	switch {
	case timeout <= 0:
		timeout = browserDefaultTimeout
	case timeout > browserMaxTimeout:
		timeout = browserMaxTimeout
	}

	batch := buildCanonicalBatch(input.URL, input.Commands)
	stdinBytes, err := json.Marshal(batch)
	if err != nil {
		return nil, fmt.Errorf("marshal batch: %w", err)
	}

	// Serialize: only one agent-browser session at a time across the whole
	// process. Acquire before LookPath so a concurrent caller doesn't race
	// on the daemon if two tools arrive at the same instant.
	browserMu.Lock()
	defer browserMu.Unlock()

	if _, err := browserRun.LookPath(); err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"agent-browser not found on PATH",
			"This tool is only available inside the ZCP container. agent-browser is pre-installed there.",
		)
	}

	start := time.Now()
	stdout, stderr, runErr := browserRun.Run(ctx, string(stdinBytes), timeout)
	duration := time.Since(start)

	result := &BrowserBatchResult{
		URL:        input.URL,
		DurationMs: duration.Milliseconds(),
	}

	// Fork-exhaustion detection runs BEFORE exit-code checks — the daemon
	// sometimes exits 0 after logging the error, sometimes exits non-zero.
	combined := stdout + "\n" + stderr
	if isForkExhausted(combined) {
		browserRun.RecoverFork(ctx)
		result.ForkRecoveryAttempted = true
		result.Message = "Fork budget exhausted (agent-browser or Chrome could not spawn a process). " +
			"pkill recovery ran automatically. Before retrying, stop background dev processes on every dev container " +
			"(e.g. `ssh apidev \"pkill -f 'nest start'\"`) — those are the usual culprit."
		return result, nil
	}

	// Context-deadline timeout → recovery + clear message.
	if runErr != nil && errors.Is(runErr, context.DeadlineExceeded) {
		browserRun.RecoverFork(ctx)
		result.ForkRecoveryAttempted = true
		result.Message = fmt.Sprintf("agent-browser timed out after %s. pkill recovery ran automatically. "+
			"Retry with a shorter command sequence, or raise timeoutSeconds (max %ds).",
			timeout, int(browserMaxTimeout.Seconds()))
		return result, nil
	}

	// Any other non-zero exit: keep going — agent-browser emits partial
	// JSON on step failures, which is still useful. Record the raw err.
	if runErr != nil {
		result.Message = fmt.Sprintf("agent-browser exited with error: %v", runErr)
		if trimmed := strings.TrimSpace(stderr); trimmed != "" {
			result.Message += "\nstderr: " + trimmed
		}
	}

	if strings.TrimSpace(stdout) != "" {
		if err := json.Unmarshal([]byte(stdout), &result.Steps); err != nil {
			result.Message = fmt.Sprintf("failed to parse agent-browser --json output: %v\nraw output:\n%s", err, stdout)
			return result, nil
		}
	}

	// Extract the canonical errors/console steps. Because buildCanonicalBatch
	// ALWAYS appends [errors], [console], [close] in that order, the last
	// three steps of a successful run are at positions n-3, n-2, n-1.
	if n := len(result.Steps); n >= 3 {
		if isCommand(result.Steps[n-3].Command, "errors") {
			result.ErrorsOutput = result.Steps[n-3].Result
		}
		if isCommand(result.Steps[n-2].Command, "console") {
			result.ConsoleOutput = result.Steps[n-2].Result
		}
	}

	return result, nil
}

// buildCanonicalBatch assembles [open url] + stripped caller commands +
// [errors] [console] [close]. Any open/close in the caller's commands is
// silently dropped — the canonical wrappers are the only lifecycle markers.
func buildCanonicalBatch(url string, commands [][]string) [][]string {
	inner := make([][]string, 0, len(commands))
	for _, cmd := range commands {
		if len(cmd) == 0 {
			continue
		}
		switch cmd[0] {
		case "open", browserCmdClose:
			// Strip — caller should not manage lifecycle.
			continue
		}
		inner = append(inner, cmd)
	}
	batch := make([][]string, 0, len(inner)+4)
	batch = append(batch, []string{"open", url})
	batch = append(batch, inner...)
	batch = append(batch, []string{"errors"}, []string{"console"}, []string{browserCmdClose})
	return batch
}

// isCommand reports whether a step's command starts with the given name.
func isCommand(cmd []string, name string) bool {
	return len(cmd) > 0 && cmd[0] == name
}

// isForkExhausted matches the two stderr signatures we've seen in
// production incidents (v4 and v5 of the recipe workflow both hit this).
func isForkExhausted(text string) bool {
	lc := strings.ToLower(text)
	return strings.Contains(lc, "resource temporarily unavailable") ||
		strings.Contains(lc, "pthread_create") ||
		strings.Contains(lc, "fork failed")
}
