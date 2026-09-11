package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/eval"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// Defaults for `zcp eval farm observe` (docs/spec-eval-farm.md §7.7).
const (
	defaultObserverModel = "claude-sonnet-5"
	defaultClaudeBinary  = "claude"
	observerTimeout      = 5 * time.Minute
	unparsedRawCap       = 20000
)

// runFarmObserve implements the local verb `zcp eval farm observe <run-dir>
// [--model <m>] [--claude <path>]` (docs/spec-eval-farm.md §7.7): works over
// a pulled bundle, writes <run-dir>/observer/<obsId>.json, prints the
// rendering, and never touches the bucket. Exit 0 when the observation's
// own status is "ok", 1 otherwise (the file is written either way); 2 on
// bad flags.
func runFarmObserve(args []string) int {
	runDir, model, claudePath, err := parseObserveArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}

	obs := buildObservation(context.Background(), runDir, model, claudePath)

	if err := writeObservation(runDir, obs); err != nil {
		fmt.Fprintf(os.Stderr, "error: write observation: %v\n", err)
		return 1
	}
	fmt.Print(observer.Render(obs))

	if obs.Status == "ok" {
		return 0
	}
	return 1
}

func parseObserveArgs(args []string) (runDir, model, claudePath string, err error) {
	model = defaultObserverModel
	claudePath = defaultClaudeBinary
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--model":
			if i+1 >= len(args) {
				return "", "", "", fmt.Errorf("%s requires a value", arg)
			}
			model = args[i+1]
			i++
		case "--claude":
			if i+1 >= len(args) {
				return "", "", "", fmt.Errorf("%s requires a value", arg)
			}
			claudePath = args[i+1]
			i++
		default:
			if strings.HasPrefix(arg, "--") {
				return "", "", "", fmt.Errorf("unknown flag %s", arg)
			}
			if runDir != "" {
				return "", "", "", fmt.Errorf("unexpected argument %q", arg)
			}
			runDir = arg
		}
	}
	if runDir == "" {
		return "", "", "", errors.New("a run-dir is required")
	}
	return runDir, model, claudePath, nil
}

// buildObservation runs the whole local pipeline (§7.2-§7.5) over runDir,
// always returning a complete Observation — status "error" (with Error set)
// at whichever step first fails, never a bare error return, so the caller
// always has something to write and render.
func buildObservation(ctx context.Context, runDir, model, claudePath string) observer.Observation {
	runID := filepath.Base(runDir)
	createdAt := time.Now().UTC()
	obs := observer.Observation{
		FormatVersion: observer.ObservationFormat1,
		RunID:         runID,
		ObsID:         observer.ObsID(createdAt, model),
		Model:         model,
		CreatedAt:     createdAt,
		PromptSha256:  observer.PromptSha256(),
	}

	fail := func(err error) observer.Observation {
		obs.Status = "error"
		obs.Error = err.Error()
		return obs
	}

	bundle := observer.NewDirBundle(runDir)
	resultsDir, err := observer.ResultsDir(bundle)
	if err != nil {
		return fail(err)
	}
	taskPrompt, err := observer.LoadTaskPrompt(bundle, resultsDir)
	if err != nil {
		return fail(fmt.Errorf("task-prompt.txt: %w", err))
	}
	transcript, err := observer.LoadTranscript(bundle, resultsDir)
	if err != nil {
		return fail(fmt.Errorf("transcript.jsonl: %w", err))
	}
	meta, err := observer.LoadMeta(bundle, resultsDir)
	if err != nil {
		return fail(fmt.Errorf("meta.json: %w", err))
	}
	verification, err := observer.LoadVerification(bundle, resultsDir)
	if err != nil {
		return fail(fmt.Errorf("verification.json: %w", err))
	}
	selfReview, err := observer.LoadSelfReview(bundle, resultsDir)
	if err != nil {
		return fail(fmt.Errorf("self-review.md: %w", err))
	}
	platformSnapshot, err := observer.LoadPlatformSnapshot(bundle, resultsDir)
	if err != nil {
		return fail(fmt.Errorf("platform-snapshot.json: %w", err))
	}
	scenarioMD := loadScenarioMD(bundle, resultsDir)

	steps, err := observer.BuildSteps(taskPrompt, transcript, resumeReplies(meta))
	if err != nil {
		return fail(fmt.Errorf("transcript.jsonl: %w", err))
	}

	verdict := resolveRunVerdict(runDir, runID, meta)

	costUsd := 0.0
	if meta.Usage != nil {
		costUsd = meta.Usage.TotalCostUsd
	}
	digestText, digestSha256 := observer.BuildDigest(observer.DigestInput{
		RunID: runID, ScenarioID: meta.ScenarioID, Verdict: verdict,
		Duration: time.Duration(meta.Duration), CostUsd: costUsd, Model: model,
		TaskPrompt: taskPrompt, ScenarioMD: scenarioMD,
		Checks: verification.Checks, Steps: steps,
		FinalState: platformSnapshot, SelfReview: selfReview,
	})
	obs.DigestSha256 = digestSha256

	runResult, err := observer.RunObserver(ctx, observer.RunConfig{
		ClaudePath: claudePath,
		Model:      model,
		OAuthToken: os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"),
		Timeout:    observerTimeout,
		Environ:    os.Environ,
	}, observer.PromptText, digestText)
	if err != nil {
		return fail(err)
	}
	obs.DurationMs = runResult.DurationMs
	obs.CostUsd = runResult.TotalCostUsd

	ans, ok := observer.ParseAndValidate(runResult.ResultText)
	if !ok {
		obs.Status = "unparsed"
		obs.Raw = capRunes(runResult.ResultText, unparsedRawCap)
		return obs
	}

	obs.Status = "ok"
	obs.Headline = ans.Headline
	obs.Goal = ans.Goal
	obs.Checks = ans.Checks
	obs.Checks.Verdict = verdict // never the model's own opinion (§7.5)
	obs.Findings = observer.VerifyEvidence(steps, observer.ChecksBody(verification.Checks), ans.Findings)
	obs.SelfReview = ans.SelfReview
	return obs
}

// loadScenarioMD resolves the optional capture scenario.md (§7.2); any
// failure to locate or read it renders as notRecorded rather than failing
// the whole observation — it is explicitly optional.
func loadScenarioMD(bundle observer.Bundle, resultsDir string) string {
	suiteScenario := strings.TrimPrefix(resultsDir, "results/")
	path, err := observer.CaptureScenarioMD(bundle, suiteScenario)
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

// resolveRunVerdict implements §7.5's checks.verdict rule: the run's row in
// <run-dir>/../summary.json (a `farm pull --batch` layout) when present,
// else meta.json.task.result. Never the model's own opinion.
func resolveRunVerdict(runDir, runID string, meta eval.BehavioralResult) string {
	metaResult := ""
	if meta.Task != nil {
		metaResult = string(meta.Task.Result)
	}
	summaryPath := filepath.Join(filepath.Dir(runDir), "summary.json")
	data, err := os.ReadFile(summaryPath)
	if err != nil {
		return observer.ResolveVerdict("", false, metaResult)
	}
	result, found, err := observer.FindSummaryResult(data, runID)
	if err != nil {
		return observer.ResolveVerdict("", false, metaResult)
	}
	return observer.ResolveVerdict(result, found, metaResult)
}

func writeObservation(runDir string, obs observer.Observation) error {
	dir := filepath.Join(runDir, "observer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, obs.ObsID+".json")
	data, err := json.MarshalIndent(obs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal observation: %w", err)
	}
	data = append(data, '\n')
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// capRunes returns the first n runes of s.
func capRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
