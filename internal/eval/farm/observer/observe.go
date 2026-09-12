package observer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval"
)

// unparsedRawStoreCap is how much of an unparsed answer's raw text is kept
// in the stored observation document (§7.5: "raw keeps the answer, capped
// at 20,000 chars"). render.go's unparsedRawDisplayCap is a separate,
// smaller cap on how much of that stored text a rendering shows — never
// the other way around.
const unparsedRawStoreCap = 20000

// ObserveConfig carries every input Observe needs beyond the bundle itself
// (§7.2-§7.5). Environ and Now default to os.Environ and time.Now
// respectively when nil is not acceptable to the caller — Observe itself
// requires both non-nil, since RunObserver reads Environ() unconditionally;
// callers (the local verb, the console worker) always supply them.
type ObserveConfig struct {
	RunID      string
	Model      string
	ClaudePath string
	OAuthToken string
	Timeout    time.Duration
	Environ    func() []string
	Now        func() time.Time
	// SummaryJSON is the batch's summary.json bytes, nil when absent
	// (§7.5's checks.verdict rule: the run's row in the batch summary when
	// it exists, else meta.json.task.result).
	SummaryJSON []byte
	// Source is the observation's source (§7.5): "worker" (console
	// automatic), "action" (a console action) or "local"
	// (`zcp eval farm observe`) — supplied by the caller, never guessed.
	Source string
}

// Observe runs the whole observer pipeline (§7.2-§7.5) over bundle: loads
// the required and optional inputs, numbers the transcript into steps,
// builds the digest, invokes `claude -p` once, and validates and
// evidence-checks the model's answer. It always returns a complete
// Observation — status "error" (with Error set) at whichever step first
// fails, never a bare error return, so the caller always has something to
// store and render.
func Observe(ctx context.Context, bundle Bundle, cfg ObserveConfig) Observation {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	createdAt := now().UTC()
	obs := Observation{
		FormatVersion: ObservationFormat2,
		RunID:         cfg.RunID,
		ObsID:         ObsID(createdAt, cfg.Model),
		Model:         cfg.Model,
		CreatedAt:     createdAt,
		PromptSha256:  PromptSha256(),
		Source:        cfg.Source,
	}

	fail := func(kind string, err error) Observation {
		obs.Status = statusError
		obs.ErrorKind = kind
		obs.Error = err.Error()
		return obs
	}

	resultsDir, err := ResultsDir(bundle)
	if err != nil {
		return fail(ErrorKindBundle, err)
	}
	taskPrompt, err := LoadTaskPrompt(bundle, resultsDir)
	if err != nil {
		return fail(ErrorKindBundle, fmt.Errorf("task-prompt.txt: %w", err))
	}
	transcript, err := LoadTranscript(bundle, resultsDir)
	if err != nil {
		return fail(ErrorKindBundle, fmt.Errorf("transcript.jsonl: %w", err))
	}
	meta, err := LoadMeta(bundle, resultsDir)
	if err != nil {
		return fail(ErrorKindBundle, fmt.Errorf("meta.json: %w", err))
	}
	verification, err := LoadVerification(bundle, resultsDir)
	if err != nil {
		return fail(ErrorKindBundle, fmt.Errorf("verification.json: %w", err))
	}
	selfReview, err := LoadSelfReview(bundle, resultsDir)
	if err != nil {
		return fail(ErrorKindBundle, fmt.Errorf("self-review.md: %w", err))
	}
	platformSnapshot, err := LoadPlatformSnapshot(bundle, resultsDir)
	if err != nil {
		return fail(ErrorKindBundle, fmt.Errorf("platform-snapshot.json: %w", err))
	}
	scenarioMD := loadScenarioMD(bundle, resultsDir)

	steps, err := BuildSteps(taskPrompt, transcript, ResumeReplies(meta))
	if err != nil {
		return fail(ErrorKindBundle, fmt.Errorf("transcript.jsonl: %w", err))
	}

	verdict := resolveVerdict(cfg.SummaryJSON, cfg.RunID, meta)

	costUsd := 0.0
	if meta.Usage != nil {
		costUsd = meta.Usage.TotalCostUsd
	}
	digestText, digestSha256 := BuildDigest(DigestInput{
		RunID: cfg.RunID, ScenarioID: meta.ScenarioID, Verdict: verdict,
		Duration: time.Duration(meta.Duration), CostUsd: costUsd, Model: cfg.Model,
		TaskPrompt: taskPrompt, ScenarioMD: scenarioMD,
		Checks: verification.Checks, Steps: steps,
		FinalState: platformSnapshot, SelfReview: selfReview,
	})
	obs.DigestSha256 = digestSha256

	runResult, err := RunObserver(ctx, RunConfig{
		ClaudePath: cfg.ClaudePath,
		Model:      cfg.Model,
		OAuthToken: cfg.OAuthToken,
		Timeout:    cfg.Timeout,
		Environ:    cfg.Environ,
	}, PromptText, digestText)
	if err != nil {
		return fail(classifyRunError(err), err)
	}
	obs.DurationMs = runResult.DurationMs
	obs.CostUsd = runResult.TotalCostUsd

	// claude can exit 0 while its own JSON output reports is_error — an
	// expired or rejected credential is exactly this shape. That is a call
	// failure, not an answer that merely failed to parse: never let it fall
	// through to ParseAndValidate and render as "unparsed".
	if runResult.IsError {
		kind := ErrorKindOther
		if isErrorLooksLikeCredential(runResult.ResultText) {
			kind = ErrorKindCredential
		}
		return fail(kind, errors.New(runResult.ResultText))
	}
	if strings.TrimSpace(runResult.ResultText) == "" {
		return fail(ErrorKindModel, errors.New("observer returned an empty reply"))
	}

	facts := buildRunFacts(steps, verification.Checks, verdict)
	ans, warnings, reason, ok := ParseAndValidate(runResult.ResultText, facts)
	if !ok {
		obs.Status = statusUnparsed
		obs.Raw = firstN(runResult.ResultText, unparsedRawStoreCap)
		obs.Error = reason
		return obs
	}

	obs.Status = "ok"
	obs.Warnings = warnings
	obs.Headline = ans.Headline
	obs.Story = ans.Story
	obs.Goal = ans.Goal
	obs.Findings = VerifyEvidence(steps, facts.ChecksBody, ans.Findings)
	obs.Checks = Checks{
		Verdict: verdict, // never the model's own opinion (§7.5)
		Judged:  ans.Checks.Judged,
		Agree:   DeriveAgree(ans.Checks.Judged, obs.Findings),
	}
	obs.SelfReview = ans.SelfReview
	obs.Outcome = DeriveOutcome(ans.Story.Ending, len(obs.Findings))
	return obs
}

// buildRunFacts assembles RunFacts from the run's own steps, checks and
// resolved verdict (§7.5 "Parse, validate, repair") — read once here rather
// than re-derived inside the parser.
func buildRunFacts(steps []Step, checks []eval.RequiredCheck, verdict string) RunFacts {
	toolNames := make(map[string]bool)
	for _, s := range steps {
		if s.Kind == StepTool && s.ToolName != "" {
			toolNames[displayToolName(s.ToolName)] = true
		}
	}
	checkIDs := make(map[string]bool, len(checks))
	failedOrBlocked := make(map[string]bool)
	for _, c := range checks {
		checkIDs[c.ID] = true
		if c.Result == eval.CheckFailed || c.Result == eval.CheckBlocked {
			failedOrBlocked[c.ID] = true
		}
	}
	return RunFacts{
		Steps:                   steps,
		ChecksBody:              ChecksBody(checks),
		ToolNames:               toolNames,
		CheckIDs:                checkIDs,
		FailedOrBlockedCheckIDs: failedOrBlocked,
		Verdict:                 verdict,
	}
}

// classifyRunError maps a RunObserver failure to §7.5's errorKind: first via
// the sentinel errors RunObserver wraps its own preflight/timeout/signal
// failures in (run.go); a non-zero claude exit carries no sentinel (it
// surfaces the JSON result's own text instead, per RunObserver's "still
// parse stdout on non-zero exit" behavior), so that text is checked for a
// credential marker the same way the is_error-but-exit-0 path is.
func classifyRunError(err error) string {
	switch {
	case errors.Is(err, ErrObserverCredential):
		return ErrorKindCredential
	case errors.Is(err, ErrObserverTimeout):
		return ErrorKindTimeout
	case errors.Is(err, ErrObserverKilled):
		return ErrorKindKilled
	case isErrorLooksLikeCredential(err.Error()):
		return ErrorKindCredential
	default:
		return ErrorKindOther
	}
}

// isErrorLooksLikeCredential reports whether claude's own is_error result
// text names an authentication/credential problem (§7.5 errorKind
// "credential": "OAuth token missing/rejected") rather than some other call
// failure — the only distinguishing signal claude's JSON output gives us,
// since is_error carries no separate error code.
func isErrorLooksLikeCredential(resultText string) bool {
	lower := strings.ToLower(resultText)
	for _, marker := range []string{"api key", "oauth", "login", "credential", "unauthorized", "authentication"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// loadScenarioMD resolves the optional capture scenario.md (§7.2); any
// failure to locate or read it renders as NotRecorded rather than failing
// the whole observation — it is explicitly optional.
func loadScenarioMD(bundle Bundle, resultsDir string) string {
	suiteScenario := strings.TrimPrefix(resultsDir, "results/")
	path, err := CaptureScenarioMD(bundle, suiteScenario)
	if err != nil || path == "" {
		return ""
	}
	data, err := bundle.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// ResumeReplies extracts meta.json's userSim.turns[].reply, in order, for
// BuildSteps' resumed-segment numbering (§7.2).
func ResumeReplies(meta eval.BehavioralResult) []string {
	if meta.UserSim == nil {
		return nil
	}
	replies := make([]string, len(meta.UserSim.Turns))
	for i, turn := range meta.UserSim.Turns {
		replies[i] = turn.Reply
	}
	return replies
}

// resolveVerdict implements §7.5's checks.verdict rule: the batch summary's
// row result for this run when summaryJSON is non-nil and parses with a
// matching row, else meta.json.task.result. Never the model's own opinion.
func resolveVerdict(summaryJSON []byte, runID string, meta eval.BehavioralResult) string {
	metaResult := ""
	if meta.Task != nil {
		metaResult = string(meta.Task.Result)
	}
	if summaryJSON == nil {
		return ResolveVerdict("", false, metaResult)
	}
	result, found, err := FindSummaryResult(summaryJSON, runID)
	if err != nil {
		return ResolveVerdict("", false, metaResult)
	}
	return ResolveVerdict(result, found, metaResult)
}

// Models are the observer models the farm accepts (§3.3, §8.5 FM-53), in the
// order pickers offer them; DefaultModel is the first.
var Models = []string{"claude-sonnet-5", "claude-opus-5", "claude-fable-5-1"}

// DefaultModel observes every run unless a batch or an action names another.
const DefaultModel = "claude-sonnet-5"

// ValidModel reports whether model is one of Models.
func ValidModel(model string) bool { return slices.Contains(Models, model) }
