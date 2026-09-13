package eval

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	// Baseline is the node-postgres application oracle's pre-agent reading
	// of the unrelated service's active app-version id
	// (docs/spec-testing-architecture.md §10.3 "Baseline"), recorded right
	// after seed/init and before the initial agent invocation. Nil when the
	// scenario doesn't declare verification.nodePostgresRecord.
	Baseline *ScenarioBaseline `json:"baseline,omitempty"`
	// Preparation is the seed.expect verdict (docs/spec-eval-farm.md §4.5
	// FM-63/FM-64): "" (equivalent to "ok") when the scenario declares no
	// seed.expect or every entry matched; "mismatch: <reason>" when it did
	// not — in that case Error stays empty (a preparation mismatch is not
	// an execution error) and the agent is never spawned.
	Preparation string `json:"preparation,omitempty"`
	// Binding and ProcessIdentity are the §10.4 explicit-candidate-binding
	// dimensions: Binding is nil unless this run carried an explicit
	// binding; ProcessIdentity accumulates every observation taken while an
	// agent invocation ran.
	Binding         *ExecutionBindingRecord `json:"binding,omitempty"`
	ProcessIdentity []ProcessIdentity       `json:"processIdentity,omitempty"`
	// EvaluatorSha256 is the sha256 of the running evaluator binary
	// (os.Executable(), hashed once per run) and CandidateSha256 is the
	// value the explicit binding already verified
	// (execution_binding.go:CandidateSHA256) — the two farm digests a
	// bundle carries (docs/spec-eval-farm.md §1.2 FM-4, §2.4 FM-16).
	// Credential is "oauth-token" when CLAUDE_CODE_OAUTH_TOKEN is present
	// (the farm's only supported agent credential — owner decision, spec
	// 79ced2cc); AnthropicAPIKeyPresent is true whenever ANTHROPIC_API_KEY
	// is set (presence only, never a value) — farm/report.go's ReportRun
	// blocks a bundle carrying it, meta.json still records the fact.
	// ModelObserved is the model name read back from the capture's provider
	// records, empty when no capture is available or no model could be
	// read. Computed at writeBehavioralResult time (finding E7: at
	// result-construction time the capture's provider.jsonl is still empty
	// — no request has been sent yet), so it always reflects this run's own
	// captured traffic.
	EvaluatorSha256        string `json:"evaluatorSha256,omitempty"`
	CandidateSha256        string `json:"candidateSha256,omitempty"`
	Credential             string `json:"credential,omitempty"`
	AnthropicAPIKeyPresent bool   `json:"anthropicApiKeyPresent,omitempty"`
	ModelObserved          string `json:"modelObserved,omitempty"`
	// Usage is the cost/token summary computed from the transcript's and
	// retrospective's own claude headless "result" events (brief S14). Nil
	// when neither file yields a result line — "not observed" is never
	// confused with "zero cost".
	Usage *BehavioralUsage `json:"usage,omitempty"`
}

// UsagePhase is one phase's usage/cost figures, summed from every claude
// headless "result" event found for that phase (brief S14).
type UsagePhase struct {
	CostUsd                  float64 `json:"costUsd"`
	InputTokens              int64   `json:"inputTokens"`
	OutputTokens             int64   `json:"outputTokens"`
	CacheCreationInputTokens int64   `json:"cacheCreationInputTokens"`
	CacheReadInputTokens     int64   `json:"cacheReadInputTokens"`
	NumTurns                 int64   `json:"numTurns"`
	DurationMs               int64   `json:"durationMs"`
}

// BehavioralUsage is meta.json's cost/usage summary (brief S14). Main sums
// every "result" line in the transcript — the fresh run plus every user-sim
// resume, which append their own result events to the same file; Retrospective
// is the retrospective.jsonl's own result line(s). Either sub-object is nil
// — never a zeroed UsagePhase — when its source file carries no result line,
// so "not observed" is never confused with "cost $0".
type BehavioralUsage struct {
	Main          *UsagePhase `json:"main,omitempty"`
	Retrospective *UsagePhase `json:"retrospective,omitempty"`
	TotalCostUsd  float64     `json:"totalCostUsd"`
}

// resultLineEvent is the subset of a claude headless stream-json "result"
// event computeBehavioralUsage sums. snake_case field names are the upstream
// schema, not negotiable here (mirrors the eventType* constants below).
type resultLineEvent struct {
	Type         string  `json:"type"`
	TotalCostUsd float64 `json:"total_cost_usd"` //nolint:tagliatelle // upstream claude headless schema
	NumTurns     int64   `json:"num_turns"`      //nolint:tagliatelle // upstream claude headless schema
	DurationMs   int64   `json:"duration_ms"`    //nolint:tagliatelle // upstream claude headless schema
	Usage        struct {
		InputTokens              int64 `json:"input_tokens"`                //nolint:tagliatelle // upstream claude headless schema
		OutputTokens             int64 `json:"output_tokens"`               //nolint:tagliatelle // upstream claude headless schema
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"` //nolint:tagliatelle // upstream claude headless schema
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`     //nolint:tagliatelle // upstream claude headless schema
	} `json:"usage"`
}

// parseResultLines scans a stream-json log for every "result" event, in file
// order. A missing/unreadable file returns nil, no error — usage is
// best-effort telemetry, never a reason to fail the run.
func parseResultLines(logFile string) []resultLineEvent {
	if logFile == "" {
		return nil
	}
	f, err := os.Open(logFile)
	if err != nil {
		return nil
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<22)
	var events []resultLineEvent
	for scanner.Scan() {
		var ev resultLineEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Type != eventTypeResult {
			continue
		}
		events = append(events, ev)
	}
	return events
}

// sumUsagePhase sums every result event into one UsagePhase, or nil when
// events is empty (never a zeroed phase for "no result line found").
func sumUsagePhase(events []resultLineEvent) *UsagePhase {
	if len(events) == 0 {
		return nil
	}
	var phase UsagePhase
	for _, ev := range events {
		phase.CostUsd += ev.TotalCostUsd
		phase.InputTokens += ev.Usage.InputTokens
		phase.OutputTokens += ev.Usage.OutputTokens
		phase.CacheCreationInputTokens += ev.Usage.CacheCreationInputTokens
		phase.CacheReadInputTokens += ev.Usage.CacheReadInputTokens
		phase.NumTurns += ev.NumTurns
		phase.DurationMs += ev.DurationMs
	}
	return &phase
}

// computeBehavioralUsage builds meta.json's usage summary from the run's own
// artifacts: transcriptFile carries the fresh run's result line plus one per
// user-sim resume (all appended to the same file); retrospectiveFile carries
// the retrospective's own result line. Returns nil when neither file yields
// a result line (brief S14: "absent file / no result line → the sub-object
// is omitted, never zeros" — extended here to the whole summary).
func computeBehavioralUsage(transcriptFile, retrospectiveFile string) *BehavioralUsage {
	main := sumUsagePhase(parseResultLines(transcriptFile))
	retro := sumUsagePhase(parseResultLines(retrospectiveFile))
	if main == nil && retro == nil {
		return nil
	}
	usage := &BehavioralUsage{Main: main, Retrospective: retro}
	if main != nil {
		usage.TotalCostUsd += main.CostUsd
	}
	if retro != nil {
		usage.TotalCostUsd += retro.CostUsd
	}
	return usage
}

// credentialOAuthToken is the only agent credential a farm run carries
// (owner decision, spec 79ced2cc, docs/spec-eval-farm.md FM-16: the farm's
// agent credential is OAuth-only, no api-key mode).
const credentialOAuthToken = "oauth-token"

// credentialFieldsFromPresence derives meta.json's credential/
// anthropicApiKeyPresent fields from whether each credential env var is set
// — presence only, never the value itself (docs/spec-eval-farm.md §2.4).
// credential is "oauth-token" whenever CLAUDE_CODE_OAUTH_TOKEN is set (the
// only supported agent credential); anthropicApiKeyPresent is true whenever
// ANTHROPIC_API_KEY is set, regardless of the OAuth token's presence — a
// bundle recording both is not refused here, it is graded "blocked" by
// farm/report.go's ReportRun ("api key present; farm runs are oauth-only").
func credentialFieldsFromPresence(hasAPIKey, hasOAuthToken bool) (credential string, anthropicAPIKeyPresent bool) {
	if hasOAuthToken {
		credential = credentialOAuthToken
	}
	return credential, hasAPIKey
}

// observedModelFromProviderCapture reads sessionDir's provider capture
// records (capture/provider.jsonl) and returns the "model" field of the
// first provider request body it can decode, empty when the capture has no
// readable provider records yet (docs/spec-eval-farm.md §2.4 FM-16:
// "the model observed on the wire").
func observedModelFromProviderCapture(sessionDir string) string {
	if sessionDir == "" {
		return ""
	}
	records, err := capture.ReadRecords(filepath.Join(sessionDir, "provider.jsonl"))
	if err != nil {
		return ""
	}
	bodies := make(map[string][]byte)
	var order []string
	for _, rec := range records {
		if rec.Kind != capture.RecordProviderRequestBody || rec.BodyBase64 == "" {
			continue
		}
		chunk, decodeErr := base64.StdEncoding.DecodeString(rec.BodyBase64)
		if decodeErr != nil {
			continue
		}
		if _, seen := bodies[rec.ExchangeID]; !seen {
			order = append(order, rec.ExchangeID)
		}
		bodies[rec.ExchangeID] = append(bodies[rec.ExchangeID], chunk...)
	}
	for _, exchangeID := range order {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(bodies[exchangeID], &body); err != nil {
			continue
		}
		if body.Model != "" {
			return body.Model
		}
	}
	return ""
}

// newBehavioralResult builds the initial BehavioralResult for a scenario run,
// including the farm bundle fields (docs/spec-eval-farm.md §1.2 FM-4, §2.4
// FM-16). Split out of RunBehavioralScenario to keep that function's own
// cyclomatic/maintainability metrics from absorbing every field this result
// carries.
func (r *Runner) newBehavioralResult(sc *Scenario, suiteID string, startedAt time.Time) *BehavioralResult {
	result := &BehavioralResult{
		ScenarioID: sc.ID,
		SuiteID:    suiteID,
		Mode:       "two-shot-resume",
		StartedAt:  startedAt,
		Model:      r.config.Model,
		WorkDir:    r.config.WorkDir,
		Binding:    bindingRecord(r.config.Binding),
	}
	r.applyFarmBundleFields(result)
	return result
}

// applyFarmBundleFields sets result's evaluatorSha256/candidateSha256/
// credentialMode (docs/spec-eval-farm.md §1.2 FM-4, §2.4 FM-16).
// ModelObserved is NOT set here (finding E7): this runs at
// newBehavioralResult time, before the agent's first request, so the
// capture's provider.jsonl is still empty — it is computed instead at
// writeBehavioralResult time, from this run's own captured traffic. Split
// out of RunBehavioralScenario to keep that function's own complexity from
// growing with every bundle field this adds.
func (r *Runner) applyFarmBundleFields(result *BehavioralResult) {
	if evaluatorSha, evalErr := evaluatorSelfSHA256(); evalErr == nil {
		result.EvaluatorSha256 = evaluatorSha
	} else {
		fmt.Fprintf(os.Stderr, "warning: hash evaluator binary: %v\n", evalErr)
	}
	if r.config.Binding != nil {
		result.CandidateSha256 = r.config.Binding.CandidateSHA256
	}
	result.Credential, result.AnthropicAPIKeyPresent = credentialFieldsFromPresence(
		os.Getenv("ANTHROPIC_API_KEY") != "",
		os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") != "",
	)
}

// evaluatorSelfSHA256 hashes the running evaluator binary
// (docs/spec-eval-farm.md §1.2 FM-4: "evaluatorSha256").
func evaluatorSelfSHA256() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve evaluator executable: %w", err)
	}
	return sha256File(exe)
}

// ScenarioBaseline is the per-hostname baseline reading taken at scenario
// start (docs/spec-testing-architecture.md §10.3 "Baseline";
// docs/spec-eval-farm.md §4.1 FM-29): one active app-version id per hostname
// in the union of verification.unchanged, nodePostgresRecord.unrelated, and
// every verification.artifactPromotion[].from (finding E2 — the O7
// dev_unchanged row needs a baseline for the promotion's dev hostname too).
// A hostname absent from AppVersions was never recorded (a read failure, or
// the service not yet deployed) — every reader treats that as "no baseline
// for <host>", never a pass.
type ScenarioBaseline struct {
	AppVersions map[string]string `json:"appVersions"`
	ObservedAt  time.Time         `json:"observedAt"`
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
// parseScenarioForRun loads the scenario at path, then renders its
// {{runId}}/{{projectId}} placeholders with this run's suiteID and the
// runner's project id (internal/eval/scenario_template.go), before any
// seed/mutation for the run happens.
func (r *Runner) parseScenarioForRun(path, suiteID string) (*Scenario, error) {
	sc, err := ParseScenario(path)
	if err != nil {
		return nil, err
	}
	if err := sc.Render(TemplateValues{RunID: suiteID, ProjectID: r.projectID}); err != nil {
		return nil, fmt.Errorf("scenario %s: %w", sc.ID, err)
	}
	return sc, nil
}

func (r *Runner) RunBehavioralScenario(ctx context.Context, scenarioPath, suiteID string) (result *BehavioralResult, returnErr error) {
	startedAt := time.Now()
	sc, err := r.parseScenarioForRun(scenarioPath, suiteID)
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

	result = r.newBehavioralResult(sc, suiteID, startedAt)

	outDir := filepath.Join(r.config.ResultsDir, suiteID, sc.ID)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}
	result.OutputDir = outDir

	// Owned-window gate for required results (docs/spec-capture-inspector.md
	// §6) and the §10.4 binding requirement: a required scenario accepts
	// only the private scoped window its own invocation created AND an
	// explicit candidate binding. Refused before seed, init, or cleanup
	// registration — zero platform calls.
	if sc.IsRequired() && (r.config.Capture == nil || !r.config.CaptureOwned || r.config.Binding == nil) {
		result.Error = "capture: required mode needs this invocation's own scoped capture window (run with --capture raw) and an explicit binding"
		result.Duration = Duration(time.Since(startedAt))
		r.logBehavioralResultWrite(outDir, result)
		return result, nil
	}

	// Preflight (§10.4 "Preflight — zero mutation"): runs immediately after
	// the owned-window gate, before the scenario-start marker and before any
	// defer is registered, so a refusal here makes zero platform mutation.
	if r.config.Binding != nil {
		if reason := r.preflightBinding(ctx); reason != "" {
			return r.bindingRefusalResult(sc, outDir, result, startedAt, reason), nil
		}
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

	if r.prepareWorkOrFail(ctx, sc, suiteID, outDir, result, startedAt) {
		return result, nil
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

	// Baseline (§10.3 "Baseline"): recorded right after seed/init, before the
	// initial agent invocation, so the unchanged row has something to compare
	// against at the freeze — a baseline read after the agent has run would
	// no longer prove the artifact was untouched. Covers the union of
	// verification.unchanged and nodePostgresRecord.unrelated (FM-29:
	// standalone unchanged is independent of nodePostgresRecord).
	if hostnames := scenarioBaselineHostnames(sc); len(hostnames) > 0 {
		r.recordScenarioBaseline(ctx, hostnames, result)
	}

	transcriptFile := filepath.Join(outDir, "transcript.jsonl")
	result.TranscriptFile = transcriptFile

	scenarioCtx, cancelScenario := context.WithTimeout(ctx, r.config.Timeout)
	defer cancelScenario()

	sessionID, errMsg := r.runInitialAgent(ctx, scenarioCtx, sc, suiteID, transcriptFile, result)
	if errMsg != "" && sessionID == "" {
		result.Error = errMsg
		return r.notRunFailure(ctx, sc, outDir, result, startedAt), nil
	}
	if errMsg != "" {
		// Task work happened and the agent exited non-zero: record the exit
		// as the execution error, grade the platform at task end, skip the
		// user simulation and the retrospective (no session to continue).
		result.Error = errMsg
		if sc.IsRequired() {
			r.freezeTaskEnd(context.WithoutCancel(ctx), sc, outDir, result, startedAt, true, nil)
		} else {
			r.observeTaskEnd(context.WithoutCancel(ctx), sc, outDir, result, startedAt, "")
		}
		result.Duration = Duration(time.Since(startedAt))
		r.logBehavioralResultWrite(outDir, result)
		return result, nil
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
		if retrospectiveExhaustedTurns(retroFile) {
			result.Error = fmt.Sprintf("%s %v", retrospectiveMissingErrorPrefix, err)
		} else {
			result.Error = fmt.Sprintf("retrospective: %v", err)
		}
		result.RetroWallTime = Duration(time.Since(retroStart))
		if !sc.IsRequired() {
			r.observeTaskEnd(context.WithoutCancel(ctx), sc, outDir, result, startedAt, selfReview)
		}
		result.Duration = Duration(time.Since(startedAt))
		r.logBehavioralResultWrite(outDir, result)
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
	r.logBehavioralResultWrite(outDir, result)

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
	rows := generateRequiredChecks(evidenceCtx, sc, observation, r.httpDoer, startedAt, r.projectID, r.client, true, result.Baseline, r.scenarioRuntimeInputs(sc, result))
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

// scenarioBaselineHostnames returns the union of verification.unchanged,
// nodePostgresRecord.unrelated, and every artifactPromotion[].from for sc,
// deduplicated, in a stable order (unchanged entries first, then the
// nodePostgresRecord hostname if not already present, then each
// artifactPromotion dev hostname in declaration order). Empty when sc
// declares none of the three (FM-29: baseline capture is driven by the
// declared inputs, never by mode). Finding E2: artifactPromotion[].from was
// missing here, so the O7 dev_unchanged row had no baseline to compare
// against and blocked on every cross-deploy run.
func scenarioBaselineHostnames(sc *Scenario) []string {
	if sc.Verification == nil {
		return nil
	}
	seen := make(map[string]bool)
	var hostnames []string
	add := func(h string) {
		if h == "" || seen[h] {
			return
		}
		seen[h] = true
		hostnames = append(hostnames, h)
	}
	for _, h := range sc.Verification.Unchanged {
		add(h)
	}
	if sc.Verification.NodePostgresRecord != nil {
		add(sc.Verification.NodePostgresRecord.Unrelated)
	}
	for _, ap := range sc.Verification.ArtifactPromotion {
		add(ap.From)
	}
	return hostnames
}

// recordScenarioBaseline reads the active app-version id for every hostname
// in hostnames via one ListServicesDirect call and records them on result
// (§10.3 "Baseline"; FM-29). Best-effort per hostname: a read failure blocks
// every row; a hostname that resolves to no service, or to a service with no
// active app-version yet, is simply absent from the resulting map — which
// every reader (evaluateUnchangedFieldRow, the nodePostgresRecord unrelated
// row, the O7 artifact_promotion dev_unchanged row) treats as "no baseline
// for <host>" → blocked, never a silent pass.
func (r *Runner) recordScenarioBaseline(ctx context.Context, hostnames []string, result *BehavioralResult) {
	services, err := r.client.ListServicesDirect(ctx, r.projectID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: scenario baseline: ListServicesDirect: %v\n", err)
		return
	}
	appVersions := make(map[string]string)
	for _, hostname := range hostnames {
		svc := findServiceByHostname(services, hostname)
		if svc == nil || svc.ActiveAppVersion == nil || svc.ActiveAppVersion.ID == "" {
			continue
		}
		appVersions[hostname] = svc.ActiveAppVersion.ID
	}
	result.Baseline = &ScenarioBaseline{AppVersions: appVersions, ObservedAt: time.Now().UTC()}
}

// prepareWorkOrFail runs prepareBehavioralWork and, on any failure
// (execution error or FM-63 seed.expect mismatch), records the right
// dimension on result and freezes the task end as not-run — returning true
// so RunBehavioralScenario can return immediately, before the agent is ever
// spawned. Factored out purely to keep RunBehavioralScenario's own
// maintainability index in check.
func (r *Runner) prepareWorkOrFail(ctx context.Context, sc *Scenario, suiteID, outDir string, result *BehavioralResult, startedAt time.Time) bool {
	errMsg, mismatch := r.prepareBehavioralWork(ctx, sc, suiteID, outDir)
	if errMsg == "" && mismatch == "" {
		return false
	}
	if mismatch != "" {
		// FM-63: a seed.expect mismatch is preparation, not execution,
		// failure — result.Error stays empty and the agent is never
		// spawned (the caller returns right after this, before
		// runInitialAgent).
		result.Preparation = "mismatch: " + mismatch
	} else {
		result.Error = errMsg
	}
	r.notRunFailure(ctx, sc, outDir, result, startedAt)
	return true
}

// checkRequiredEnvVars returns "resource <NAME> missing" for the first
// scenario.RequiredEnvVars entry that is unset or empty in the evaluator's
// own environment (docs/spec-eval-farm.md §4.5 preparation extension;
// spec-scenarios.md §9.2 rule 7), or "" when every entry is present. Never
// logs or returns an env VALUE — only the variable NAME.
func checkRequiredEnvVars(names []string) string {
	for _, name := range names {
		if os.Getenv(name) == "" {
			return fmt.Sprintf("resource %s missing", name)
		}
	}
	return ""
}

// prepareBehavioralWork runs requiredEnvVars check → seed → init → capture
// MCP config → preseed → seed.expect check, returning a human-readable
// error prefix ("seed: ...", "init: ...", etc.) on the first failure, or ""
// on success. Factored out of RunBehavioralScenario purely to keep that
// function's cyclomatic complexity in check — each step's failure meaning
// is unchanged.
//
// When the scenario declares a requiredEnvVars entry that is missing, or a
// seed.expect that does not hold (docs/spec-eval-farm.md §4.5 FM-63),
// mismatch is non-empty and errMsg is ""; the caller MUST treat that as
// preparation, not execution, failure — r.notRunFailure still runs (every
// declared row freezes not-run) but result.Error stays empty and the agent
// is never spawned.
func (r *Runner) prepareBehavioralWork(ctx context.Context, sc *Scenario, suiteID, outDir string) (errMsg, mismatch string) {
	if reason := checkRequiredEnvVars(sc.RequiredEnvVars); reason != "" {
		return "", reason
	}
	if err := r.seedScenario(ctx, sc, suiteID); err != nil {
		return fmt.Sprintf("seed: %v", err), ""
	}
	if err := resetGuidedForScenario(r.config.WorkDir); err != nil {
		return fmt.Sprintf("init: %v", err), ""
	}
	if err := r.runInit(ctx); err != nil {
		return fmt.Sprintf("init: %v", err), ""
	}
	if err := r.prepareCaptureMCPConfig(outDir); err != nil {
		return fmt.Sprintf("capture MCP config: %v", err), ""
	}
	if err := r.runPreseedScript(ctx, sc, suiteID); err != nil {
		return fmt.Sprintf("preseed: %v", err), ""
	}
	if sc.SeedExpect != nil {
		reason, err := EvaluateSeedExpect(ctx, sc.SeedExpect, r.client, platform.NewSystemSSHDeployer(), r.projectID)
		if err != nil {
			return fmt.Sprintf("seed.expect: %v", err), ""
		}
		if reason != "" {
			return "", reason
		}
	}
	return "", ""
}

// runInit runs `zcp init` for the work dir. With a binding (§10.4 "Candidate
// owns the agent surface") this runs as `<candidate> init` under the
// candidate environment instead of the evaluator's own initcmd.Run — the
// only evaluator-side writes into the work dir then remain the
// guided-marker reset and the Claude memory clean.
func (r *Runner) runInit(ctx context.Context) error {
	if r.config.Binding == nil {
		return initcmd.Run(r.config.WorkDir, runtime.Detect())
	}
	b := r.config.Binding
	cmd := exec.CommandContext(ctx, b.Candidate, "init") //nolint:gosec // b.Candidate is the operator-supplied binding, SHA-256-verified by preflightBinding before this runs
	cmd.Dir = r.config.WorkDir
	cmd.Env = r.candidateEnv()
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s init: %w (%s)", b.Candidate, err, out.String())
	}
	return nil
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
	spawnErr := r.pollProcessIdentityDuring(scenarioCtx, result, func() error {
		return r.spawnClaudeFresh(scenarioCtx, sc.Prompt, transcriptFile, captureProcessScope{evalRunID: suiteID, scenarioRunID: sc.ID, invocationID: initialInvocationID, phase: "agent.initial"})
	})
	result.ScenarioWallTime = Duration(time.Since(scenarioStart))
	if err := spawnErr; err != nil {
		// The agent exited non-zero. If it ran far enough to leave a session
		// (a turn cap or a client error after real work), bind it so the
		// exchanges join and let the caller grade the platform (§10.1 point
		// 7); only a run with no session at all is not-run.
		if sessionID, sessionErr := extractSessionID(transcriptFile); sessionErr == nil && sessionID != "" {
			result.SessionID = sessionID
			initialInvocation.Bind(scenarioCtx, sessionID)
			initialInvocation.End(scenarioCtx, capture.CapturePartial, err)
			return sessionID, fmt.Sprintf("agent: claude exit: %v", err)
		}
		initialInvocation.End(scenarioCtx, capture.CapturePartial, err)
		return "", fmt.Sprintf("scenario spawn: %v", err)
	}

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
	r.logBehavioralResultWrite(outDir, result)
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
			if observation.processesErr != nil {
				// A failed process read proves nothing about in-flight
				// mutations; the freeze is unsettled until a read succeeds
				// or the budget runs out (§10.2 step 3).
				liveProcesses = []string{"process read failed: " + observation.processesErr.Error()}
			} else if len(liveProcesses) == 0 {
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
		rows = generateRequiredChecks(ctx, sc, observation, r.httpDoer, startedAt, r.projectID, r.client, settled, result.Baseline, r.scenarioRuntimeInputs(sc, result))
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
	// verification.json exists only for scenarios that declare a
	// verification block — the same rule the observe success path applies.
	if sc.Verification != nil {
		if err := WriteVerificationDocument(outDir, VerificationDocument{
			FormatVersion: VerificationDocumentFormat2, Mode: mode, Result: result.Task.Result,
			FrozenAt: frozenAt, Checks: rows, Advisory: findings,
		}); err != nil {
			errs = append(errs, err)
		}
	}
	// meta.json is written once, claiming Persisted=true; a failure of that
	// very write (or of anything before it) downgrades the in-memory result
	// and re-writes meta.json best-effort with the truthful state.
	result.TaskEnd.Persisted = len(errs) == 0
	if result.TaskEnd.Persisted {
		if err := r.writeBehavioralResult(outDir, result); err != nil {
			errs = append(errs, err)
		}
	}
	if persistErr := errors.Join(errs...); persistErr != nil {
		result.TaskEnd.Persisted = false
		result.TaskEnd.PersistError = persistErr.Error()
		if result.Task.Result == CheckPassed {
			result.Task.Result = CheckBlocked
		}
		r.logBehavioralResultWrite(outDir, result)
	}
}

// scenarioRuntimeInputs assembles the RuntimeInputs the O6/O7/O8 oracles
// and the askWhen rows need (docs/spec-eval-farm.md §4.1, §4.4): the
// run's transcript path, the user-sim loop's recorded turns, the mutating
// tool vocabulary from RunnerConfig, the
// captured MCP stream file paths for this scenario run (reusing
// ScenarioMCPStreamPaths' discovery, decision_rows.go), and the hex sha256
// of ZCP_E2E_LAUNCH_KEY when that env is present. The launch token value
// itself is read once here, hashed immediately, and never stored, logged,
// or passed anywhere else — only the digest crosses into RuntimeInputs.
func (r *Runner) scenarioRuntimeInputs(sc *Scenario, result *BehavioralResult) RuntimeInputs {
	runtime := RuntimeInputs{
		TranscriptPath: result.TranscriptFile, MutatingTools: r.config.MutatingTools,
		WorkDir: r.config.WorkDir, ExecSSH: platform.NewSystemSSHDeployer().ExecSSH,
	}
	if result.UserSim != nil {
		runtime.UserSimTurns = result.UserSim.Turns
	}
	if r.config.Capture != nil {
		if paths, err := ScenarioMCPStreamPaths(r.config.Capture.SessionDir, result.SuiteID, sc.ID); err == nil {
			runtime.MCPStreamPaths = paths
		} else {
			fmt.Fprintf(os.Stderr, "warning: scenario MCP stream paths: %v\n", err)
		}
	}
	if launchKey := os.Getenv("ZCP_E2E_LAUNCH_KEY"); launchKey != "" {
		sum := sha256.Sum256([]byte(launchKey))
		runtime.LaunchTokenSHA256 = hex.EncodeToString(sum[:])
	}
	return runtime
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

// spawnClaudeResume runs `claude --resume <sessionID> -p <retroPrompt>` as a
// TEXT-ONLY turn: no MCP config is attached (unlike every other spawn path)
// and tools are disabled via `--tools ""`, with the short max-turns cap (3)
// kept as a backstop. Live gate1 bundles (2026-09-10) showed the model
// answering the briefing prompt by ACTING — Read/Write or Bash/Write calls
// trying to write the self-review file itself — which burns the 3-turn cap
// and ends `error_max_turns` instead of returning the text summary this call
// needs (docs/spec-eval-farm.md §2.3 FM-13). Output is streamed into logFile
// alongside the scenario transcript so the operator can see the entire
// two-shot exchange. The retrospective uses its own logFile so we can
// extract self-review.md independently — separate from the transcript that
// captured the agent's run.
func (r *Runner) spawnClaudeResume(ctx context.Context, sessionID, retroPrompt, logFile string, captureScope captureProcessScope) error {
	args := []string{
		"--resume", sessionID,
		"-p", retroPrompt,
		"--output-format", "stream-json",
		"--verbose",
		"--dangerously-skip-permissions",
		"--tools", "",
		"--max-turns", "3",
	}
	return r.execClaude(ctx, args, logFile, false, captureScope)
}

// retrospectiveErrorPrefix marks a meta.json error string as originating
// from the retrospective phase — spawn failure or turn exhaustion alike —
// rather than "the run's execution failed". ExecutionDimension keys off
// this broader prefix (finding E6: FM-13 says ANY retrospective failure
// never changes the Execution line, not just the turn-exhaustion case
// retrospectiveMissingErrorPrefix names) so the task verdict, already
// frozen by the time the retrospective runs (§10.2), is never overridden by
// how that optional evidence-gathering step went (docs/spec-eval-farm.md
// §2.3 FM-13).
const retrospectiveErrorPrefix = "retrospective:"

// retrospectiveMissingErrorPrefix marks a meta.json error string specifically
// as "the retrospective self-review could not be obtained because the model
// exhausted its turn cap" — a retrospectiveErrorPrefix-prefixed string, so
// ExecutionDimension treats it the same as any other retrospective failure.
const retrospectiveMissingErrorPrefix = "retrospective: missing:"

// retrospectiveExhaustedTurns scans a retrospective stream-json log for a
// terminal "result" event with subtype "error_max_turns" — the shape a
// text-only retrospective still hits when the model spends its three turns
// on multi-part text instead of one answer, even with tools disabled at the
// argv level. Missing/unreadable log → false (err on treating it as a
// generic execution failure, not a swallowed one).
func retrospectiveExhaustedTurns(logFile string) bool {
	f, err := os.Open(logFile)
	if err != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for scanner.Scan() {
		var ev struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Type == eventTypeResult && ev.Subtype == "error_max_turns" {
			return true
		}
	}
	return false
}

// ExecutionDimension derives the CLI/farm "execution" acceptance dimension
// (docs/spec-testing-architecture.md §10.1) from a BehavioralResult's error
// field. The retrospective — turn exhaustion, a spawn error, any failure of
// that phase — is optional evidence gathered after the task verdict is
// already frozen, so it must never flip execution to an error (finding E6,
// docs/spec-eval-farm.md §2.3 FM-13: ANY retrospective failure, not just
// turn exhaustion). Every other recorded error keeps meaning "the run's
// execution failed".
func ExecutionDimension(r *BehavioralResult) string {
	if r.Error == "" || strings.HasPrefix(r.Error, retrospectiveErrorPrefix) {
		return "ok"
	}
	return "error: " + r.Error
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
	if r.config.ClaudeHome == "" && r.config.Capture == nil && r.config.Binding == nil {
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
	// §10.4 "Every claude child gets the same HOME/PATH/update setting":
	// HOME already comes from ClaudeHome above (RunnerConfig.ClaudeHome is
	// set to Binding.ClaudeHome when bound); PATH/ZCP_AUTO_UPDATE are added
	// here so the candidate symlink shadows any other `zcp` on PATH.
	if r.config.Binding != nil {
		if r.config.Binding.PrivateBin != "" {
			overrides["PATH"] = r.config.Binding.PrivateBin + string(os.PathListSeparator) + os.Getenv("PATH")
		}
		overrides["ZCP_AUTO_UPDATE"] = "0"
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
// artifact there, and a plain progress write elsewhere. A Runner method (not
// a free function) so it can read this run's own capture window: finding E7
// computes ModelObserved here, at finalization, instead of at
// newBehavioralResult time — every call site is a genuine "this result is
// being frozen/persisted" point, so recomputing from the capture's
// provider.jsonl on each call always reflects this run's own traffic so far.
func (r *Runner) writeBehavioralResult(outDir string, result *BehavioralResult) error {
	result.Usage = computeBehavioralUsage(result.TranscriptFile, result.RetrospectiveFile)
	if r.config.Capture != nil {
		result.ModelObserved = observedModelFromProviderCapture(r.config.Capture.SessionDir)
	}
	return writeJSONAtomic(outDir, "meta.json", result)
}

func (r *Runner) logBehavioralResultWrite(outDir string, result *BehavioralResult) {
	if err := r.writeBehavioralResult(outDir, result); err != nil {
		fmt.Fprintf(os.Stderr, "warning: write meta.json: %v\n", err)
	}
}
