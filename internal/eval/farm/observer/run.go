package observer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// defaultTimeout is the observer's invocation timeout (§7.4).
const defaultTimeout = 5 * time.Minute

// waitDelay bounds cmd.Wait() after a kill (see the comment at its use
// site below).
const waitDelay = 2 * time.Second

// RunConfig is everything RunObserver needs to invoke `claude -p` once
// (§7.4). No package-level state: every input arrives explicit, including
// Environ — the observer's own environment, read only to extract PATH (for
// the child's environment) and to refuse when ANTHROPIC_API_KEY is present.
type RunConfig struct {
	ClaudePath string
	Model      string
	OAuthToken string
	Timeout    time.Duration
	Environ    func() []string
}

// RunResult is one successful `claude -p --output-format json` call's
// parsed output.
type RunResult struct {
	ResultText   string
	TotalCostUsd float64
	IsError      bool
	DurationMs   int64
}

// claudeJSONOutput is `claude --output-format json`'s result object (§7.4).
// snake_case field names are the upstream schema, not negotiable here.
type claudeJSONOutput struct {
	Result       string  `json:"result"`
	TotalCostUsd float64 `json:"total_cost_usd"` //nolint:tagliatelle // upstream claude headless schema
	IsError      bool    `json:"is_error"`       //nolint:tagliatelle // upstream claude headless schema
}

// RunObserver invokes
// `claude -p <promptText> --model <model> --tools "" --max-turns 1
// --output-format json`, digest on stdin (§7.4). It refuses before
// starting any child when OAuthToken is empty or ANTHROPIC_API_KEY is set
// in cfg.Environ(). ClaudePath is resolved to an absolute path
// (exec.LookPath, then filepath.Abs) before the child starts, because the
// child's working directory is a fresh empty temp dir, not the caller's.
// The child's environment is built from scratch: PATH (from cfg.Environ()),
// HOME (a fresh private 0700 temp dir, removed before returning), and
// CLAUDE_CODE_OAUTH_TOKEN — nothing else. cfg.Timeout bounds the call
// (defaultTimeout when zero).
func RunObserver(ctx context.Context, cfg RunConfig, promptText, digest string) (RunResult, error) {
	if cfg.OAuthToken == "" {
		return RunResult{}, errors.New("CLAUDE_CODE_OAUTH_TOKEN is empty (docs/spec-eval-farm.md §7.4)")
	}

	var pathVal string
	for _, kv := range cfg.Environ() {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
			return RunResult{}, errors.New("ANTHROPIC_API_KEY is set in the observer's own environment (docs/spec-eval-farm.md §7.4)")
		}
		if rest, ok := strings.CutPrefix(kv, "PATH="); ok {
			pathVal = rest
		}
	}

	resolved, err := exec.LookPath(cfg.ClaudePath)
	if err != nil {
		return RunResult{}, fmt.Errorf("resolve claude binary %q: %w", cfg.ClaudePath, err)
	}
	absPath, err := filepath.Abs(resolved)
	if err != nil {
		return RunResult{}, fmt.Errorf("absolute path for %q: %w", resolved, err)
	}

	homeDir, err := os.MkdirTemp("", "zcp-observer-home-*")
	if err != nil {
		return RunResult{}, fmt.Errorf("create observer home: %w", err)
	}
	defer func() { _ = os.RemoveAll(homeDir) }()
	if err := os.Chmod(homeDir, 0o700); err != nil {
		return RunResult{}, fmt.Errorf("chmod observer home: %w", err)
	}

	workDir, err := os.MkdirTemp("", "zcp-observer-work-*")
	if err != nil {
		return RunResult{}, fmt.Errorf("create observer work dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, absPath,
		"-p", promptText,
		"--model", cfg.Model,
		"--tools", "",
		"--max-turns", "1",
		"--output-format", "json",
	)
	cmd.Dir = workDir
	cmd.Env = []string{"PATH=" + pathVal, "HOME=" + homeDir, "CLAUDE_CODE_OAUTH_TOKEN=" + cfg.OAuthToken}
	cmd.Stdin = strings.NewReader(digest)
	// WaitDelay bounds how long Wait() blocks copying stdout/stderr after a
	// kill: without it, a killed claude that itself forked a still-running
	// grandchild (inheriting the same pipe fds) can hang Wait() until that
	// grandchild exits on its own, well past the timeout that just fired.
	cmd.WaitDelay = waitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return RunResult{}, fmt.Errorf("observer timed out after %s", timeout)
	}
	if runErr != nil {
		return RunResult{}, fmt.Errorf("claude: %w (stderr: %s)", runErr, strings.TrimSpace(stderr.String()))
	}

	var out claudeJSONOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return RunResult{}, fmt.Errorf("parse claude output: %w", err)
	}
	return RunResult{
		ResultText:   out.Result,
		TotalCostUsd: out.TotalCostUsd,
		IsError:      out.IsError,
		DurationMs:   duration.Milliseconds(),
	}, nil
}
