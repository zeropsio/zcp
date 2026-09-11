package observer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval"
)

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
		FormatVersion: ObservationFormat1,
		RunID:         cfg.RunID,
		ObsID:         ObsID(createdAt, cfg.Model),
		Model:         cfg.Model,
		CreatedAt:     createdAt,
		PromptSha256:  PromptSha256(),
	}

	fail := func(err error) Observation {
		obs.Status = "error"
		obs.Error = err.Error()
		return obs
	}

	resultsDir, err := ResultsDir(bundle)
	if err != nil {
		return fail(err)
	}
	taskPrompt, err := LoadTaskPrompt(bundle, resultsDir)
	if err != nil {
		return fail(fmt.Errorf("task-prompt.txt: %w", err))
	}
	transcript, err := LoadTranscript(bundle, resultsDir)
	if err != nil {
		return fail(fmt.Errorf("transcript.jsonl: %w", err))
	}
	meta, err := LoadMeta(bundle, resultsDir)
	if err != nil {
		return fail(fmt.Errorf("meta.json: %w", err))
	}
	verification, err := LoadVerification(bundle, resultsDir)
	if err != nil {
		return fail(fmt.Errorf("verification.json: %w", err))
	}
	selfReview, err := LoadSelfReview(bundle, resultsDir)
	if err != nil {
		return fail(fmt.Errorf("self-review.md: %w", err))
	}
	platformSnapshot, err := LoadPlatformSnapshot(bundle, resultsDir)
	if err != nil {
		return fail(fmt.Errorf("platform-snapshot.json: %w", err))
	}
	scenarioMD := loadScenarioMD(bundle, resultsDir)

	steps, err := BuildSteps(taskPrompt, transcript, resumeReplies(meta))
	if err != nil {
		return fail(fmt.Errorf("transcript.jsonl: %w", err))
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
		return fail(err)
	}
	obs.DurationMs = runResult.DurationMs
	obs.CostUsd = runResult.TotalCostUsd

	// claude can exit 0 while its own JSON output reports is_error — an
	// expired credential is exactly this shape. That is a call failure, not
	// an answer that merely failed to parse: never let it fall through to
	// ParseAndValidate and render as "unparsed".
	if runResult.IsError {
		return fail(errors.New(runResult.ResultText))
	}

	ans, ok := ParseAndValidate(runResult.ResultText)
	if !ok {
		obs.Status = "unparsed"
		obs.Raw = firstN(runResult.ResultText, unparsedRawCap)
		return obs
	}

	obs.Status = "ok"
	obs.Headline = ans.Headline
	obs.Goal = ans.Goal
	obs.Checks = ans.Checks
	obs.Checks.Verdict = verdict // never the model's own opinion (§7.5)
	obs.Findings = VerifyEvidence(steps, ChecksBody(verification.Checks), ans.Findings)
	obs.SelfReview = ans.SelfReview
	return obs
}

// loadScenarioMD resolves the optional capture scenario.md (§7.2); any
// failure to locate or read it renders as notRecorded rather than failing
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

// resumeReplies extracts meta.json's userSim.turns[].reply, in order, for
// BuildSteps' resumed-segment numbering (§7.2).
func resumeReplies(meta eval.BehavioralResult) []string {
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
