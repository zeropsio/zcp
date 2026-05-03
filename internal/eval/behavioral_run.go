package eval

import (
	"bufio"
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

// BehavioralResult captures the outcome of a two-shot behavioral scenario run.
// It is the parallel of ScenarioResult on the grading side: same seed/cleanup
// envelope, but the artifact set is transcript + retrospective + self-review
// + meta instead of grade + assessment.
type BehavioralResult struct {
	ScenarioID            string    `json:"scenarioId"`
	SuiteID               string    `json:"suiteId"`
	Mode                  string    `json:"mode"` // always "two-shot-resume"
	StartedAt             time.Time `json:"startedAt"`
	Duration              Duration  `json:"duration"`
	ScenarioWallTime      Duration  `json:"scenarioWallTime"`
	RetroWallTime         Duration  `json:"retroWallTime"`
	SessionID             string    `json:"sessionId"`
	CompactedDuringResume bool      `json:"compactedDuringResume"`
	Model                 string    `json:"model"`
	WorkDir               string    `json:"workDir"`
	OutputDir             string    `json:"outputDir"`
	TranscriptFile        string    `json:"transcriptFile"`
	RetrospectiveFile     string    `json:"retrospectiveFile"`
	SelfReviewFile        string    `json:"selfReviewFile"`
	Error                 string    `json:"error,omitempty"`
}

// RunBehavioralScenario executes a behavioral scenario (two-shot resume).
//
// Flow mirrors RunScenario: seed → init → preseed → spawn agent → cleanup,
// but the agent is spawned with session persistence ON; after it exits,
// session_id is captured from the transcript and a second `claude --resume`
// call asks the retrospective prompt. Self-review is extracted from the
// second call's last assistant text and written to <outDir>/self-review.md.
//
// The scenario.Prompt is sent as-is — no follow-up questions or assessment
// instructions are appended (those would create observer effect; the whole
// point of two-shot resume is the agent does not know it will be evaluated).
func (r *Runner) RunBehavioralScenario(ctx context.Context, scenarioPath, suiteID string) (*BehavioralResult, error) {
	startedAt := time.Now()
	sc, err := ParseScenario(scenarioPath)
	if err != nil {
		return nil, err
	}
	if !sc.IsBehavioral() {
		return nil, fmt.Errorf("scenario %s is not behavioral (no retrospective config)", sc.ID)
	}

	retroPrompt, err := LoadRetrospectivePrompt(sc.Retrospective.PromptStyle)
	if err != nil {
		return nil, fmt.Errorf("scenario %s: %w", sc.ID, err)
	}

	result := &BehavioralResult{
		ScenarioID: sc.ID,
		SuiteID:    suiteID,
		Mode:       "two-shot-resume",
		StartedAt:  startedAt,
		Model:      r.config.Model,
		WorkDir:    r.config.WorkDir,
	}

	outDir := filepath.Join(r.config.ResultsDir, suiteID, sc.ID)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}
	result.OutputDir = outDir

	if err := r.seedScenario(ctx, sc, suiteID); err != nil {
		result.Error = fmt.Sprintf("seed: %v", err)
		result.Duration = Duration(time.Since(startedAt))
		writeBehavioralResult(outDir, result)
		return result, nil
	}

	if err := initcmd.Run(r.config.WorkDir, runtime.Detect()); err != nil {
		result.Error = fmt.Sprintf("init: %v", err)
		result.Duration = Duration(time.Since(startedAt))
		writeBehavioralResult(outDir, result)
		return result, nil
	}

	if err := r.runPreseedScript(ctx, sc, suiteID); err != nil {
		result.Error = fmt.Sprintf("preseed: %v", err)
		result.Duration = Duration(time.Since(startedAt))
		writeBehavioralResult(outDir, result)
		return result, nil
	}

	if err := os.WriteFile(filepath.Join(outDir, "task-prompt.txt"), []byte(sc.Prompt), 0o600); err != nil {
		return nil, fmt.Errorf("write prompt: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "retrospective-prompt.txt"), []byte(retroPrompt), 0o600); err != nil {
		return nil, fmt.Errorf("write retrospective prompt: %w", err)
	}

	if err := cleanClaudeMemory(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: clean memory: %v\n", err)
	}

	transcriptFile := filepath.Join(outDir, "transcript.jsonl")
	result.TranscriptFile = transcriptFile

	scenarioCtx, cancelScenario := context.WithTimeout(ctx, r.config.Timeout)
	defer cancelScenario()

	scenarioStart := time.Now()
	if err := r.spawnClaudeFresh(scenarioCtx, sc.Prompt, transcriptFile); err != nil {
		result.Error = fmt.Sprintf("scenario spawn: %v", err)
		result.ScenarioWallTime = Duration(time.Since(scenarioStart))
		result.Duration = Duration(time.Since(startedAt))
		writeBehavioralResult(outDir, result)
		// Cleanup even on error so next run starts clean.
		_ = CleanupProject(ctx, r.client, r.projectID, r.config.WorkDir)
		return result, nil
	}
	result.ScenarioWallTime = Duration(time.Since(scenarioStart))

	sessionID, err := extractSessionID(transcriptFile)
	if err != nil {
		result.Error = fmt.Sprintf("extract session_id: %v", err)
		result.Duration = Duration(time.Since(startedAt))
		writeBehavioralResult(outDir, result)
		_ = CleanupProject(ctx, r.client, r.projectID, r.config.WorkDir)
		return result, nil
	}
	result.SessionID = sessionID

	retroFile := filepath.Join(outDir, "retrospective.jsonl")
	result.RetrospectiveFile = retroFile

	// Retrospective gets a tight 5-minute timeout — it is a single Q&A turn,
	// not a full scenario. Resume is generally fast.
	retroCtx, cancelRetro := context.WithTimeout(ctx, 5*time.Minute)
	defer cancelRetro()

	retroStart := time.Now()
	if err := r.spawnClaudeResume(retroCtx, sessionID, retroPrompt, retroFile); err != nil {
		result.Error = fmt.Sprintf("retrospective spawn: %v", err)
		result.RetroWallTime = Duration(time.Since(retroStart))
		result.Duration = Duration(time.Since(startedAt))
		writeBehavioralResult(outDir, result)
		_ = CleanupProject(ctx, r.client, r.projectID, r.config.WorkDir)
		return result, nil
	}
	result.RetroWallTime = Duration(time.Since(retroStart))

	selfReviewFile := filepath.Join(outDir, "self-review.md")
	result.SelfReviewFile = selfReviewFile
	selfReview, err := extractSelfReview(retroFile)
	if err != nil {
		result.Error = fmt.Sprintf("extract self-review: %v", err)
	} else {
		if err := os.WriteFile(selfReviewFile, []byte(selfReview), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "warning: write self-review: %v\n", err)
		}
	}

	result.CompactedDuringResume = detectCompaction(retroFile)
	result.Duration = Duration(time.Since(startedAt))
	writeBehavioralResult(outDir, result)

	if cleanErr := CleanupProject(ctx, r.client, r.projectID, r.config.WorkDir); cleanErr != nil {
		fmt.Fprintf(os.Stderr, "warning: post-scenario cleanup: %v\n", cleanErr)
	}

	return result, nil
}

// spawnClaudeFresh is spawnClaude minus --no-session-persistence so the
// session is captured by Claude Code's persistence layer for later --resume.
func (r *Runner) spawnClaudeFresh(ctx context.Context, prompt, logFile string) error {
	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--dangerously-skip-permissions",
		"--model", r.config.Model,
		"--max-turns", fmt.Sprintf("%d", r.config.MaxTurns),
	}
	if r.config.MCPConfig != "" {
		args = append(args, "--mcp-config", r.config.MCPConfig)
	}
	return r.execClaude(ctx, args, logFile)
}

// spawnClaudeResume runs `claude --resume <sessionID> -p <retroPrompt>` with a
// short max-turns cap (3). Output is streamed into logFile alongside the
// scenario transcript so the operator can see the entire two-shot exchange.
func (r *Runner) spawnClaudeResume(ctx context.Context, sessionID, retroPrompt, logFile string) error {
	args := []string{
		"--resume", sessionID,
		"-p", retroPrompt,
		"--output-format", "stream-json",
		"--verbose",
		"--dangerously-skip-permissions",
		"--max-turns", "3",
	}
	if r.config.MCPConfig != "" {
		args = append(args, "--mcp-config", r.config.MCPConfig)
	}
	return r.execClaude(ctx, args, logFile)
}

// execClaude is the shared exec path for spawn variants. Output goes to logFile
// (creates fresh file). cwd respects RunnerConfig.WorkDir.
func (r *Runner) execClaude(ctx context.Context, args []string, logFile string) error {
	cmd := exec.CommandContext(ctx, "claude", args...)
	if r.config.WorkDir != "" {
		cmd.Dir = r.config.WorkDir
	}
	out, err := os.Create(logFile)
	if err != nil {
		return fmt.Errorf("create log file: %w", err)
	}
	defer out.Close()
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("timeout: %w", ctx.Err())
		}
		return fmt.Errorf("claude exit: %w", err)
	}
	return nil
}

// streamJSON event field names — sourced from claude headless --output-format
// stream-json. Snake_case is the upstream schema, not negotiable here.
const (
	eventTypeSystem    = "system"
	eventTypeAssistant = "assistant"
	contentTypeText    = "text"
)

// extractSessionID scans a stream-json log for the first system-init event and
// returns its session_id. Returns an error with the first 10 events if not
// found, so a schema drift in claude headless is debuggable.
func extractSessionID(logFile string) (string, error) {
	f, err := os.Open(logFile)
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<22) // 4MiB cap per line
	var preview []string
	for scanner.Scan() {
		line := scanner.Text()
		if len(preview) < 10 {
			preview = append(preview, line)
		}
		var ev struct {
			Type      string `json:"type"`
			Subtype   string `json:"subtype"`
			SessionID string `json:"session_id"` //nolint:tagliatelle // upstream claude headless schema
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Type == eventTypeSystem && ev.SessionID != "" {
			return ev.SessionID, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan transcript: %w", err)
	}
	return "", fmt.Errorf("no system event with session_id found in %s; first %d events:\n%s",
		logFile, len(preview), strings.Join(preview, "\n"))
}

// extractSelfReview reads the retrospective stream-json log and returns the
// concatenated text of every assistant message's text content. The retrospective
// is short (max-turns=3, typically one assistant turn) so concatenation is
// usually one paragraph block.
func extractSelfReview(logFile string) (string, error) {
	f, err := os.Open(logFile)
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<22)
	var parts []string
	for scanner.Scan() {
		var ev struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Type != eventTypeAssistant {
			continue
		}
		for _, c := range ev.Message.Content {
			if c.Type == contentTypeText && strings.TrimSpace(c.Text) != "" {
				parts = append(parts, strings.TrimSpace(c.Text))
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan retrospective: %w", err)
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("no assistant text events found in %s", logFile)
	}
	return strings.Join(parts, "\n\n"), nil
}

// detectCompaction returns true if any stream-json event in logFile signals
// auto-compaction at resume. Heuristic: the system event has a "subtype" of
// "compact_boundary", or the first system event at resume has a "compacted"
// boolean flag, or the message contains the literal "Previous Conversation
// Compacted" marker. Errs on the side of false (under-detect rather than
// false-positive).
func detectCompaction(logFile string) bool {
	f, err := os.Open(logFile)
	if err != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, `"compact_boundary"`) ||
			strings.Contains(line, `"compacted":true`) ||
			strings.Contains(line, "Previous Conversation Compacted") {
			return true
		}
	}
	return false
}

func writeBehavioralResult(outDir string, r *BehavioralResult) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(outDir, "meta.json"), data, 0o600)
}
