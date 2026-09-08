package eval

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
	initcmd "github.com/zeropsio/zcp/internal/init"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// BehavioralResult captures the outcome of a two-shot behavioral scenario run:
// seed → run → retrospective → cleanup, with the artifact set being transcript
// + retrospective + self-review + meta.
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
	// UserSim captures the user-sim loop trace when the scenario triggered
	// at least one classify cycle. Nil when the agent terminated cleanly
	// before any classifier check (rare — the loop runs unconditionally).
	UserSim *UserSimResult `json:"userSim,omitempty"`
	Error   string         `json:"error,omitempty"`
	// Task and TaskEnd are the task-result and task-end-evidence dimensions
	// (docs/spec-testing-architecture.md §10.1/§10.2). Error keeps its
	// execution-only meaning; these are separate dimensions, never derived
	// from Error.
	Task    *TaskOutcome     `json:"task,omitempty"`
	TaskEnd *TaskEndEvidence `json:"taskEnd,omitempty"`
}

// RunBehavioralScenario executes a behavioral scenario (two-shot resume).
//
// Flow: seed → init → preseed → spawn agent → cleanup, with the agent
// spawned with session persistence ON; after it exits,
// session_id is captured from the transcript and a second `claude --resume`
// call asks the retrospective prompt. Self-review is extracted from the
// second call's last assistant text and written to <outDir>/self-review.md.
//
// The scenario.Prompt is sent as-is — no follow-up questions or assessment
// instructions are appended (those would create observer effect; the whole
// point of two-shot resume is the agent does not know it will be evaluated).
func (r *Runner) RunBehavioralScenario(ctx context.Context, scenarioPath, suiteID string) (result *BehavioralResult, returnErr error) {
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

	result = &BehavioralResult{
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

	// Owned-window gate for required results (docs/spec-capture-inspector.md
	// §6): a required scenario accepts only the private scoped window its
	// own invocation created. Refused before seed, init, or cleanup
	// registration — zero platform calls.
	if sc.IsRequired() && (r.config.Capture == nil || !r.config.CaptureOwned) {
		result.Error = "capture: required mode needs this invocation's own scoped capture window (run with --capture raw)"
		result.Duration = Duration(time.Since(startedAt))
		logBehavioralResultWrite(outDir, result)
		return result, nil
	}

	r.captureScenarioStart(ctx, suiteID, sc.ID)
	// Retention (§10.2): required mode performs no post-task cleanup, so its
	// cleanup defer is never registered. Registration order matters — the
	// cleanup defer (when present) is registered BEFORE the bundle defer so
	// it runs AFTER the bundle (LIFO): bundle → cleanup.
	if !sc.IsRequired() {
		defer func() {
			if cleanErr := CleanupProject(context.WithoutCancel(ctx), r.client, r.projectID, r.config.WorkDir); cleanErr != nil {
				fmt.Fprintf(os.Stderr, "warning: post-scenario cleanup: %v\n", cleanErr)
			}
		}()
	}
	defer func() {
		r.finishBehavioralCapture(context.WithoutCancel(ctx), suiteID, sc.ID, scenarioPath, outDir, result, returnErr)
	}()

	if errMsg := r.prepareBehavioralWork(ctx, sc, suiteID, outDir); errMsg != "" {
		result.Error = errMsg
		return r.notRunFailure(ctx, sc, outDir, result, startedAt), nil
	}

	if err := os.WriteFile(filepath.Join(outDir, "task-prompt.txt"), []byte(sc.Prompt), 0o600); err != nil {
		return nil, fmt.Errorf("write prompt: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "retrospective-prompt.txt"), []byte(retroPrompt), 0o600); err != nil {
		return nil, fmt.Errorf("write retrospective prompt: %w", err)
	}

	if err := cleanClaudeMemory(r.config.ClaudeHome); err != nil {
		fmt.Fprintf(os.Stderr, "warning: clean memory: %v\n", err)
	}

	transcriptFile := filepath.Join(outDir, "transcript.jsonl")
	result.TranscriptFile = transcriptFile

	scenarioCtx, cancelScenario := context.WithTimeout(ctx, r.config.Timeout)
	defer cancelScenario()

	sessionID, errMsg := r.runInitialAgent(ctx, scenarioCtx, sc, suiteID, transcriptFile, result)
	if errMsg != "" {
		result.Error = errMsg
		return r.notRunFailure(ctx, sc, outDir, result, startedAt), nil
	}

	// User-sim loop — drive multi-turn realistic conversation while the agent
	// is awaiting input. Cumulative resumes append to the same transcriptFile
	// so the classifier sees accumulated state. Stage timeout overrides the
	// scenario context's remaining budget when scenario.UserSim sets a tighter
	// cap. Errors here are non-fatal: we still want the retrospective to fire
	// and capture whatever the agent / user-sim produced.
	loopErr := r.runBehavioralUserSim(ctx, sc, suiteID, sessionID, transcriptFile, result)
	if loopErr != nil {
		fmt.Fprintf(os.Stderr, "warning: user-sim loop: %v\n", loopErr)
	}

	// Task end (docs/spec-testing-architecture.md §10.2): decided and
	// persisted here, BEFORE the retrospective. Required mode's task result
	// and the frozen artifacts never change after this call — a
	// retrospective failure below sets result.Error only.
	if sc.IsRequired() {
		r.freezeTaskEnd(context.WithoutCancel(ctx), sc, outDir, result, startedAt, true, loopErr)
	}

	retroFile := filepath.Join(outDir, "retrospective.jsonl")
	result.RetrospectiveFile = retroFile

	// Retrospective gets a tight 5-minute timeout — it is a single Q&A turn,
	// not a full scenario. Resume is generally fast.
	retroCtx, cancelRetro := context.WithTimeout(ctx, 5*time.Minute)
	defer cancelRetro()

	var selfReview string
	retroStart := time.Now()
	retroInvocationID := sc.ID + "/retrospective"
	retroInvocation := r.captureInvocationStart(retroCtx, suiteID, sc.ID, retroInvocationID, "retrospective", sessionID)
	if err := r.spawnClaudeResume(retroCtx, sessionID, retroPrompt, retroFile, captureProcessScope{evalRunID: suiteID, scenarioRunID: sc.ID, invocationID: retroInvocationID, phase: "retrospective"}); err != nil {
		retroInvocation.End(retroCtx, capture.CapturePartial, err)
		result.Error = fmt.Sprintf("retrospective: %v", err)
		result.RetroWallTime = Duration(time.Since(retroStart))
		if !sc.IsRequired() {
			r.observeTaskEnd(context.WithoutCancel(ctx), sc, outDir, result, startedAt, selfReview)
		}
		result.Duration = Duration(time.Since(startedAt))
		logBehavioralResultWrite(outDir, result)
		return result, nil
	}
	result.RetroWallTime = Duration(time.Since(retroStart))
	retroInvocation.End(retroCtx, capture.CaptureComplete, nil)

	selfReviewFile := filepath.Join(outDir, "self-review.md")
	result.SelfReviewFile = selfReviewFile
	selfReview, err = extractSelfReview(retroFile)
	if err != nil {
		result.Error = fmt.Sprintf("extract self-review: %v", err)
	} else {
		if err := os.WriteFile(selfReviewFile, []byte(selfReview), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "warning: write self-review: %v\n", err)
		}
	}

	result.CompactedDuringResume = detectCompaction(retroFile)

	// Observe mode keeps its legacy exit semantics: platform evidence is
	// collected AFTER the retrospective (so the advisory phrase check sees
	// the self-review), and cleanup runs after that (registered above).
	if !sc.IsRequired() {
		r.observeTaskEnd(context.WithoutCancel(ctx), sc, outDir, result, startedAt, selfReview)
	}

	result.Duration = Duration(time.Since(startedAt))
	logBehavioralResultWrite(outDir, result)

	return result, nil
}

// observeTaskEnd is observe mode's evidence-collection step: a single
// platform read taken AFTER the retrospective (so the advisory
// retrospective-phrase check sees the self-review), matching the legacy
// exit semantics documented in docs/spec-testing-architecture.md §10.2. It
// also records the observe-mode Task/TaskEnd dimensions
// (docs/spec-testing-architecture.md §10.1 point 8).
func (r *Runner) observeTaskEnd(ctx context.Context, sc *Scenario, outDir string, result *BehavioralResult, startedAt time.Time, selfReview string) {
	evidenceCtx, cancelEvidence := context.WithTimeout(ctx, 30*time.Second)
	defer cancelEvidence()

	readServices := sc.Verification != nil && len(sc.Verification.ExpectedServices) > 0
	readProcesses := sc.Verification != nil && sc.Verification.NoFailedProcesses
	observation := collectPlatformObservation(evidenceCtx, r.client, r.projectID, readServices, readProcesses)
	rows := generateRequiredChecks(evidenceCtx, sc, observation, r.httpDoer, startedAt, r.projectID)
	findings := projectRowsToFindings(rows)
	findings = append(findings, retrospectivePhraseFindings(sc, selfReview)...)
	snapshot := buildPlatformSnapshotFromObservation(r.projectID, startedAt, observation, findings)

	frozenAt := observation.observedAt
	taskResult := aggregateTaskResult(rows)
	result.Task = &TaskOutcome{Mode: VerificationObserve, Result: taskResult, FrozenAt: frozenAt}
	result.TaskEnd = &TaskEndEvidence{
		ObservedAt: frozenAt, Settled: true,
		Simulator: TaskEndSimulator{TerminatedBy: userSimTerminatedBy(result)},
	}

	if sc.Verification != nil {
		if err := WriteVerificationDocument(outDir, VerificationDocument{
			FormatVersion: VerificationDocumentFormat2, Mode: VerificationObserve, Result: taskResult,
			FrozenAt: frozenAt, Checks: rows, Advisory: findings,
		}); err != nil {
			result.TaskEnd.PersistError = err.Error()
			fmt.Fprintf(os.Stderr, "warning: write verification.json: %v\n", err)
		} else {
			result.TaskEnd.Persisted = true
		}
	} else {
		result.TaskEnd.Persisted = true
	}
	if err := WritePlatformSnapshot(outDir, snapshot); err != nil {
		result.TaskEnd.Persisted = false
		if result.TaskEnd.PersistError == "" {
			result.TaskEnd.PersistError = err.Error()
		}
		fmt.Fprintf(os.Stderr, "warning: write platform-snapshot.json: %v\n", err)
	}
}

// prepareBehavioralWork runs seed → init → capture MCP config → preseed,
// returning a human-readable error prefix ("seed: ...", "init: ...", etc.)
// on the first failure, or "" on success. Factored out of
// RunBehavioralScenario purely to keep that function's cyclomatic
// complexity in check — each step's failure meaning is unchanged.
func (r *Runner) prepareBehavioralWork(ctx context.Context, sc *Scenario, suiteID, outDir string) string {
	if err := r.seedScenario(ctx, sc, suiteID); err != nil {
		return fmt.Sprintf("seed: %v", err)
	}
	if err := resetGuidedForScenario(r.config.WorkDir); err != nil {
		return fmt.Sprintf("init: %v", err)
	}
	if err := initcmd.Run(r.config.WorkDir, runtime.Detect()); err != nil {
		return fmt.Sprintf("init: %v", err)
	}
	if err := r.prepareCaptureMCPConfig(outDir); err != nil {
		return fmt.Sprintf("capture MCP config: %v", err)
	}
	if err := r.runPreseedScript(ctx, sc, suiteID); err != nil {
		return fmt.Sprintf("preseed: %v", err)
	}
	return ""
}

// runInitialAgent spawns the agent's initial invocation and extracts its
// session id, binding/ending the capture invocation appropriately. Returns
// ("", "") is never valid — either sessionID is non-empty and errMsg is ""
// on success, or sessionID is "" and errMsg names the failure ("scenario
// spawn: ...", "extract session_id: ..."). Factored out of
// RunBehavioralScenario to keep its cyclomatic complexity in check.
func (r *Runner) runInitialAgent(ctx, scenarioCtx context.Context, sc *Scenario, suiteID, transcriptFile string, result *BehavioralResult) (sessionID, errMsg string) {
	scenarioStart := time.Now()
	initialInvocationID := sc.ID + "/agent.initial"
	initialInvocation := r.captureInvocationStart(ctx, suiteID, sc.ID, initialInvocationID, "agent.initial", "")
	if err := r.spawnClaudeFresh(scenarioCtx, sc.Prompt, transcriptFile, captureProcessScope{evalRunID: suiteID, scenarioRunID: sc.ID, invocationID: initialInvocationID, phase: "agent.initial"}); err != nil {
		initialInvocation.End(scenarioCtx, capture.CapturePartial, err)
		result.ScenarioWallTime = Duration(time.Since(scenarioStart))
		return "", fmt.Sprintf("scenario spawn: %v", err)
	}
	result.ScenarioWallTime = Duration(time.Since(scenarioStart))

	sessionID, err := extractSessionID(transcriptFile)
	if err != nil {
		initialInvocation.End(scenarioCtx, capture.CapturePartial, err)
		return "", fmt.Sprintf("extract session_id: %v", err)
	}
	result.SessionID = sessionID
	initialInvocation.Bind(scenarioCtx, sessionID)
	initialInvocation.End(scenarioCtx, capture.CaptureComplete, nil)
	return sessionID, ""
}

// notRunFailure freezes the task end as not-run (§10.1 point 7), stamps
// Duration, persists the final meta.json, and returns result — the shared
// tail of every early-failure branch (seed/init/preseed/spawn/session
// extract) before agent.initial has completed.
func (r *Runner) notRunFailure(ctx context.Context, sc *Scenario, outDir string, result *BehavioralResult, startedAt time.Time) *BehavioralResult {
	r.freezeTaskEnd(context.WithoutCancel(ctx), sc, outDir, result, startedAt, false, nil)
	result.Duration = Duration(time.Since(startedAt))
	logBehavioralResultWrite(outDir, result)
	return result
}

// livePIDsAfter returns the ids of processes created after startedAt whose
// status is still in flight (PENDING/RUNNING/ROLLBACKING/CANCELING).
func livePIDsAfter(observation platformObservation, startedAt time.Time) []string {
	var ids []string
	for _, process := range observation.processes {
		if !processCreatedAfter(process.Created, startedAt) {
			continue
		}
		switch process.Status {
		case platform.ProcessStatusPending, platform.ProcessStatusRunning, platform.ProcessStatusRollbacking, platform.ProcessStatusCanceling:
			ids = append(ids, process.ID)
		}
	}
	return ids
}

// blockRowsForUnsettled forces every non-not-run row to blocked when the
// task-end observation never settled, so an in-flight mutation can never
// read as passed (§10.2 step 3).
func blockRowsForUnsettled(rows []RequiredCheck, liveProcesses []string) []RequiredCheck {
	observed := strings.Join(liveProcesses, ", ")
	for i := range rows {
		if rows[i].Result == CheckNotRun {
			continue
		}
		rows[i].Result = CheckBlocked
		rows[i].Observed = observed
		rows[i].Message = fmt.Sprintf("task-end observation unsettled: live process(es) %s", observed)
	}
	return rows
}

// freezeTaskEnd implements docs/spec-testing-architecture.md §10.2: the
// task-end freeze and its persistence. Called once, before the optional
// retrospective, for both verification modes. When taskCompleted is false
// (execution failed before agent.initial finished), every declared row is
// frozen as not-run without any platform read (§10.1 point 7).
func (r *Runner) freezeTaskEnd(
	ctx context.Context,
	sc *Scenario,
	outDir string,
	result *BehavioralResult,
	startedAt time.Time,
	taskCompleted bool,
	simulatorErr error,
) {
	mode := VerificationObserve
	if sc.Verification != nil && sc.Verification.Mode != "" {
		mode = sc.Verification.Mode
	}
	frozenAt := time.Now().UTC()

	var rows []RequiredCheck
	var snapshot PlatformSnapshot
	settled := true
	var liveProcesses []string
	var findings []VerificationFinding

	if !taskCompleted {
		rows = notRunRows(sc, r.projectID)
		snapshot = PlatformSnapshot{
			FormatVersion: PlatformSnapshotFormat1, ProjectID: r.projectID,
			ScenarioStartedAt: startedAt.UTC(), ObservedAt: frozenAt,
			Diagnostics: []PlatformSnapshotDiagnostic{}, VerificationFindings: []VerificationFinding{},
		}
	} else {
		settleTimeout := r.config.TaskEndSettle
		if settleTimeout <= 0 {
			settleTimeout = 60 * time.Second
		}
		interval := max(settleTimeout/10, 50*time.Millisecond)
		deadline := time.Now().Add(settleTimeout)
		var observation platformObservation
		for {
			observation = collectPlatformObservation(ctx, r.client, r.projectID, true, true)
			liveProcesses = livePIDsAfter(observation, startedAt)
			if len(liveProcesses) == 0 {
				settled = true
				break
			}
			if time.Now().After(deadline) || ctx.Err() != nil {
				settled = false
				break
			}
			select {
			case <-ctx.Done():
				settled = false
			case <-time.After(interval):
				continue
			}
			break
		}
		rows = generateRequiredChecks(ctx, sc, observation, r.httpDoer, startedAt, r.projectID)
		if !settled {
			rows = blockRowsForUnsettled(rows, liveProcesses)
		}
		findings = projectRowsToFindings(rows)
		frozenAt = observation.observedAt
		snapshot = buildPlatformSnapshotFromObservation(r.projectID, startedAt, observation, findings)
	}

	taskResult := aggregateTaskResult(rows)
	result.Task = &TaskOutcome{Mode: mode, Result: taskResult, FrozenAt: frozenAt}
	result.TaskEnd = &TaskEndEvidence{
		ObservedAt: frozenAt, Settled: settled, LiveProcesses: liveProcesses,
		Simulator: TaskEndSimulator{TerminatedBy: userSimTerminatedBy(result), Error: errString(simulatorErr)},
	}

	// Persist in the order that keeps the frozen artifacts in agreement
	// (§10.2 step 5): the snapshot first, so a failure there downgrades the
	// verdict BEFORE verification.json is written; then verification.json;
	// then meta.json, which carries every persistence error. A failure never
	// removes what was already written.
	var errs []error
	if err := WritePlatformSnapshot(outDir, snapshot); err != nil {
		errs = append(errs, err)
		if result.Task.Result == CheckPassed {
			result.Task.Result = CheckBlocked
		}
	}
	if err := WriteVerificationDocument(outDir, VerificationDocument{
		FormatVersion: VerificationDocumentFormat2, Mode: mode, Result: result.Task.Result,
		FrozenAt: frozenAt, Checks: rows, Advisory: findings,
	}); err != nil {
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		if err := writeBehavioralResult(outDir, result); err != nil {
			errs = append(errs, err)
		}
	}
	if persistErr := errors.Join(errs...); persistErr != nil {
		result.TaskEnd.Persisted = false
		result.TaskEnd.PersistError = persistErr.Error()
		if result.Task.Result == CheckPassed {
			result.Task.Result = CheckBlocked
		}
		logBehavioralResultWrite(outDir, result)
		return
	}
	result.TaskEnd.Persisted = true
	if err := writeBehavioralResult(outDir, result); err != nil {
		result.TaskEnd.Persisted = false
		result.TaskEnd.PersistError = err.Error()
		if result.Task.Result == CheckPassed {
			result.Task.Result = CheckBlocked
		}
	}
}

// userSimTerminatedBy returns the user-sim loop's termination reason, or ""
// when the loop never ran.
func userSimTerminatedBy(result *BehavioralResult) string {
	if result.UserSim == nil {
		return ""
	}
	return result.UserSim.TerminatedBy
}

// errString returns err.Error(), or "" for a nil err.
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// userSimRunner returns the UserSimRunner the loop should call for the given
// scenario. Production path: claudeUserSimRunner with the scenario-specified
// model (or default Haiku). Tests can override via WithUserSimRunner to
// inject a stub. Override is sticky — once set on the Runner it applies to
// every scenario in the suite.
func (r *Runner) userSimRunner(sc *Scenario) UserSimRunner {
	if r.userSimOverride != nil {
		return r.userSimOverride
	}
	model := defaultUserSimModel
	if sc.UserSim != nil && sc.UserSim.Model != "" {
		model = sc.UserSim.Model
	}
	var environment []string
	if r.config.Capture != nil {
		environment = r.claudeEnv()
	}
	return newClaudeUserSimRunner(model, environment)
}

// spawnClaudeFresh is spawnClaude minus --no-session-persistence so the
// session is captured by Claude Code's persistence layer for later --resume.
func (r *Runner) spawnClaudeFresh(ctx context.Context, prompt, logFile string, captureScope captureProcessScope) error {
	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--dangerously-skip-permissions",
		"--model", r.config.Model,
		"--max-turns", fmt.Sprintf("%d", r.config.MaxTurns),
	}
	args = r.appendMCPConfigArgs(args)
	return r.execClaude(ctx, args, logFile, false, captureScope)
}

// spawnClaudeResume runs `claude --resume <sessionID> -p <retroPrompt>` with a
// short max-turns cap (3). Output is streamed into logFile alongside the
// scenario transcript so the operator can see the entire two-shot exchange.
// The retrospective uses its own logFile so we can extract self-review.md
// independently — separate from the transcript that captured the agent's run.
func (r *Runner) spawnClaudeResume(ctx context.Context, sessionID, retroPrompt, logFile string, captureScope captureProcessScope) error {
	args := []string{
		"--resume", sessionID,
		"-p", retroPrompt,
		"--output-format", "stream-json",
		"--verbose",
		"--dangerously-skip-permissions",
		"--max-turns", "3",
	}
	args = r.appendMCPConfigArgs(args)
	return r.execClaude(ctx, args, logFile, false, captureScope)
}

// spawnClaudeResumeAppend resumes the agent session with userMsg and APPENDS
// the resulting events to transcriptFile. Used by the user-sim loop so the
// classifier sees cumulative state across multiple resumes. Higher max-turns
// cap than the retrospective resume because user-sim turns can drive
// substantial follow-up work (deploy, verify, etc.). Mirrors spawnClaudeFresh
// argument set otherwise.
func (r *Runner) spawnClaudeResumeAppend(ctx context.Context, sessionID, userMsg, transcriptFile string, captureScope captureProcessScope) error {
	args := []string{
		"--resume", sessionID,
		"-p", userMsg,
		"--output-format", "stream-json",
		"--verbose",
		"--dangerously-skip-permissions",
		"--model", r.config.Model,
		"--max-turns", fmt.Sprintf("%d", r.config.MaxTurns),
	}
	args = r.appendMCPConfigArgs(args)
	return r.execClaude(ctx, args, transcriptFile, true, captureScope)
}

// claudeEnv builds the environment shared by every Claude subprocess path.
// nil preserves normal inheritance when neither HOME isolation nor capture is
// active. Eval capture is injected here because an isolated HOME does not read
// the operator's persistent Claude settings.
func (r *Runner) claudeEnv() []string {
	if r.config.ClaudeHome == "" && r.config.Capture == nil {
		return nil
	}
	overrides := make(map[string]string)
	if r.config.ClaudeHome != "" {
		overrides["HOME"] = r.config.ClaudeHome
	}
	if r.config.Capture != nil {
		overrides["ANTHROPIC_BASE_URL"] = r.config.Capture.ProxyURL
		overrides[capture.EnvSessionID] = r.config.Capture.CaptureID
		overrides[capture.EnvSessionDir] = r.config.Capture.SessionDir
	}
	return environmentWithOverrides(os.Environ(), overrides)
}

type captureProcessScope struct {
	evalRunID     string
	scenarioRunID string
	invocationID  string
	phase         string
}

func (r *Runner) claudeEnvForScope(scope captureProcessScope) []string {
	environment := r.claudeEnv()
	if r.config.Capture == nil || scope.invocationID == "" {
		return environment
	}
	if environment == nil {
		environment = os.Environ()
	}
	return environmentWithOverrides(environment, map[string]string{
		capture.EnvEvalRunID:       scope.evalRunID,
		capture.EnvScenarioRunID:   scope.scenarioRunID,
		capture.EnvInvocationID:    scope.invocationID,
		capture.EnvInvocationPhase: scope.phase,
	})
}

func environmentWithOverrides(environment []string, overrides map[string]string) []string {
	out := make([]string, 0, len(environment)+len(overrides))
	for _, entry := range environment {
		key, _, found := strings.Cut(entry, "=")
		if found {
			if _, replaced := overrides[key]; replaced {
				continue
			}
		}
		out = append(out, entry)
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, key+"="+overrides[key])
	}
	return out
}

// execClaude is the shared exec path for spawn variants. Output goes to logFile;
// when appendMode is true the file is opened O_APPEND so multi-call resumes
// accumulate in one transcript. cwd respects RunnerConfig.WorkDir; HOME is
// overridden to claudeHome (when set) for sandboxed isolation of the agent's
// auto-memory, sessions, and CLAUDE.md auto-discovery.
func (r *Runner) execClaude(ctx context.Context, args []string, logFile string, appendMode bool, captureScope captureProcessScope) error {
	cmd := exec.CommandContext(ctx, "claude", args...)
	if r.config.WorkDir != "" {
		cmd.Dir = r.config.WorkDir
	}
	cmd.Env = r.claudeEnvForScope(captureScope)
	var out *os.File
	var err error
	if appendMode {
		out, err = os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	} else {
		out, err = os.Create(logFile)
	}
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
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
	eventTypeUser      = "user"
	eventTypeResult    = "result"
	contentTypeText    = "text"
	contentTypeToolUse = "tool_use"
	contentTypeToolRes = "tool_result"
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

// writeBehavioralResult persists meta.json via temp+fsync+rename
// (docs/spec-testing-architecture.md §10.2 step 5). Callers outside the
// task-end freeze only log the error: meta.json is the persisted task-end
// artifact there, and a plain progress write elsewhere.
func writeBehavioralResult(outDir string, r *BehavioralResult) error {
	return writeJSONAtomic(outDir, "meta.json", r)
}

func logBehavioralResultWrite(outDir string, r *BehavioralResult) {
	if err := writeBehavioralResult(outDir, r); err != nil {
		fmt.Fprintf(os.Stderr, "warning: write meta.json: %v\n", err)
	}
}
