package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/zeropsio/zcp/internal/eval"
	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/platform"
)

// defaultRunBudget is used when `--run-budget` is not given.
const defaultRunBudget = 45 * time.Minute

// flagBatch names the --batch flag, shared between runFarmRun's own parse
// loop and planDetachRun's re-exec argv rewrite (goconst).
const flagBatch = "--batch"

// runFarmRun implements `zcp eval farm run` (docs/spec-eval-farm.md §3.3
// FM-21/FM-22, §3.1 FM-18's --detach).
func runFarmRun(args []string) int {
	var candidate, scenariosDigest, set, batch, runBudgetStr, evaluatorFlag string
	detach := false
	for i := 0; i < len(args); i++ {
		arg := args[i] //nolint:gosec // G602 false positive: i is loop-bounded by i < len(args) each iteration
		switch arg {
		case flagCandidate:
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: %s requires a value\n", arg)
				return 1
			}
			candidate = args[i+1]
			i++
		case "--scenarios":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: %s requires a value\n", arg)
				return 1
			}
			scenariosDigest = args[i+1]
			i++
		case "--evaluator":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: %s requires a value\n", arg)
				return 1
			}
			evaluatorFlag = args[i+1]
			i++
		case "--set":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: %s requires a value\n", arg)
				return 1
			}
			set = args[i+1]
			i++
		case flagBatch:
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: %s requires a value\n", arg)
				return 1
			}
			batch = args[i+1]
			i++
		case "--run-budget":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: %s requires a value\n", arg)
				return 1
			}
			runBudgetStr = args[i+1]
			i++
		case "--detach":
			detach = true
		}
	}
	if candidate == "" || scenariosDigest == "" || set == "" {
		fmt.Fprintln(os.Stderr, "error: --candidate, --scenarios, and --set are required")
		return 1
	}
	if batch == "" {
		batch = fmt.Sprintf("batch-%d", time.Now().Unix())
	}

	if detach {
		return runFarmRunDetach(args, batch)
	}

	runBudget := defaultRunBudget
	if runBudgetStr != "" {
		d, err := time.ParseDuration(runBudgetStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: --run-budget: %v\n", err)
			return 1
		}
		runBudget = d
	}

	oauthToken, err := resolveOAuthToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	cfg, err := farm.ConfigFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	sink := farm.NewSinkClient(cfg)
	// R3: Ctrl-C / SIGTERM cancels ctx instead of leaving the process with
	// no way to end the batch cleanly — RunBatch reacts to cancellation by
	// stopping its wait, recording every unsettled run "interrupted", and
	// keeping their projects (docs/spec-eval-farm.md §1.4 FM-9).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	scenarios, err := resolveScenarios(ctx, sink, scenariosDigest, set)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: resolve scenario set: %v\n", err)
		return 1
	}

	accountToken := os.Getenv("ZCP_FARM_ACCOUNT_TOKEN")
	clientID := os.Getenv("ZCP_FARM_CLIENT_ID")
	if accountToken == "" || clientID == "" {
		fmt.Fprintln(os.Stderr, "error: ZCP_FARM_ACCOUNT_TOKEN and ZCP_FARM_CLIENT_ID are required")
		return 1
	}

	evaluatorSHA, err := resolveEvaluatorSHA(ctx, sink, evaluatorFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	client, closer, err := farm.NewAccountClient(accountToken, os.Getenv("ZCP_API_HOST"), clientID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer closer()

	opts := farm.RunOptions{
		Batch: batch, ClientID: clientID, Set: set,
		CandidateSHA256: candidate, EvaluatorSHA256: evaluatorSHA, ScenariosDigest: scenariosDigest,
		Scenarios: scenarios, OAuthToken: oauthToken,
		Sink:      farm.Sink(cfg), // farm.Config and farm.Sink share the same field names/types/order
		RunBudget: runBudget,
	}
	results, err := farm.RunBatch(ctx, client, sink, opts)
	if err != nil {
		if isIntegrationTokenMintForbidden(err) {
			fmt.Fprintln(os.Stderr, "error: ZCP_FARM_ACCOUNT_TOKEN must be a personal access token: integration tokens cannot mint run tokens")
			return 1
		}
		fmt.Fprintf(os.Stderr, "error: farm run: %v\n", err)
		return 1
	}

	if len(results) == 0 {
		// D10: zero runs scheduled or created is never a quiet success — no
		// summary.json row for the operator to notice something went wrong.
		fmt.Fprintln(os.Stderr, "error: no run was created")
		return 1
	}

	allPassed := true
	interrupted := false
	for _, r := range results {
		fmt.Fprintf(os.Stdout, "%s %s %s\n", r.RunID, r.Scenario, r.Result)
		if r.Result != farm.ResultPassed {
			allPassed = false
		}
		if r.Detail == farm.DetailInterrupted {
			interrupted = true
		}
	}
	if interrupted {
		// R3: Ctrl-C/SIGTERM already stopped the batch (ctx cancelled) by
		// the time this line runs — name where the recorded state landed,
		// since the operator's own signal may have cut off whatever else
		// they were watching.
		fmt.Fprintf(os.Stderr, "farm run interrupted: batch state recorded in batches/%s/summary.json\n", batch)
	}
	if !allPassed {
		return 1
	}
	return 0
}

// isIntegrationTokenMintForbidden reports whether err's chain carries
// platform.ErrDelegationUnavailable — the typed code
// platform.MintProjectScopedToken returns when ZCP_FARM_ACCOUNT_TOKEN is
// itself an integration token without delegation (either the current or
// the legacy apiCode; docs/spec-eval-farm.md §2.4 FM-15). RunBatch aborts
// the whole batch on this error (rolling back the one shell project it
// created), so the preflight message here is the caller's one line naming
// the fix.
func isIntegrationTokenMintForbidden(err error) bool {
	var pe *platform.PlatformError
	return errors.As(err, &pe) && pe.Code == platform.ErrDelegationUnavailable
}

// resolveOAuthToken reads the run's sole model-request credential. The
// agent credential is CLAUDE_CODE_OAUTH_TOKEN only — no api-key mode, no
// fallback (§2.4/FM-16, spec commit 79ced2cc). ANTHROPIC_API_KEY being set
// in this process's own environment is refused outright: Claude Code lets
// an API key shadow an OAuth profile, so its mere presence here would make
// the run's actual credential ambiguous even though ZCP itself never reads
// its value.
func resolveOAuthToken() (string, error) {
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY must not be set in the farm host's environment — the agent credential is CLAUDE_CODE_OAUTH_TOKEN only (§2.4/FM-16)")
	}
	oauth := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")
	if oauth == "" {
		return "", fmt.Errorf("CLAUDE_CODE_OAUTH_TOKEN is required (the agent credential is OAuth-only, §2.4/FM-16)")
	}
	return oauth, nil
}

// resolveScenarios expands --set (gate|all|<id,id,...>) to the
// farm.ScenarioRun list RunBatch needs, marking each scenario Launch when
// its front matter's area starts with "launch" (the two gate-set launch
// scenarios use "launch" and "launch-production-recovery"). The farm host
// runs the binary only, no repo checkout (docs/spec-eval-farm.md §3.1
// FM-17/FM-18), so every scenario fact is read from the bucket at
// scenariosDigest, never from disk.
func resolveScenarios(ctx context.Context, sink *farm.SinkClient, scenariosDigest, set string) ([]farm.ScenarioRun, error) {
	var ids []string
	var err error
	switch set {
	case "gate":
		ids, err = readGateSetFromBucket(ctx, sink, scenariosDigest)
	case categoryAll: // "all" — shared with sync.go's own --category all (goconst)
		ids, err = listAllScenarioIDsFromBucket(ctx, sink, scenariosDigest)
	default:
		for id := range strings.SplitSeq(set, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				ids = append(ids, id)
			}
		}
	}
	if err != nil {
		return nil, err
	}

	scenarios := make([]farm.ScenarioRun, 0, len(ids))
	for _, id := range ids {
		launch, err := scenarioIsLaunch(ctx, sink, scenariosDigest, id)
		if err != nil {
			return nil, err
		}
		scenarios = append(scenarios, farm.ScenarioRun{ID: id, Launch: launch})
	}
	return scenarios, nil
}

// readGateSetFromBucket reads one scenario id per line from
// sets/<scenariosDigest>/gate.txt (uploaded by `farm push --scenarios`).
func readGateSetFromBucket(ctx context.Context, sink *farm.SinkClient, scenariosDigest string) ([]string, error) {
	key := fmt.Sprintf("sets/%s/gate.txt", scenariosDigest)
	body, err := sink.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read gate set %s: %w", key, err)
	}
	var ids []string
	for line := range strings.SplitSeq(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			ids = append(ids, line)
		}
	}
	return ids, nil
}

// listAllScenarioIDsFromBucket returns the basename (minus ".md") of every
// top-level scenario markdown file under scenarios/<scenariosDigest>/,
// sorted. Every scenario currently authored under
// eval/behavioral/scenarios/ is container-run; a scenario meant only for a
// local, non-farm lane would need its own marker to be excluded here — none
// carries one yet.
func listAllScenarioIDsFromBucket(ctx context.Context, sink *farm.SinkClient, scenariosDigest string) ([]string, error) {
	prefix := fmt.Sprintf("scenarios/%s/", scenariosDigest)
	keys, err := sink.List(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("list scenarios %s: %w", prefix, err)
	}
	var ids []string
	for _, key := range keys {
		rel := strings.TrimPrefix(key, prefix)
		if strings.Contains(rel, "/") || !strings.HasSuffix(rel, ".md") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(rel, ".md"))
	}
	sort.Strings(ids)
	return ids, nil
}

// scenarioIsLaunch reports whether scenario id's front matter area names a
// launch scenario (§3.4 FM-23 mints/revokes a token only for these). The
// scenario body is read from scenarios/<scenariosDigest>/<id>.md in the
// bucket and parsed via eval.ParseScenario, which reads from a path — so
// the fetched body is staged to a temp file for that one parse.
func scenarioIsLaunch(ctx context.Context, sink *farm.SinkClient, scenariosDigest, id string) (bool, error) {
	key := fmt.Sprintf("scenarios/%s/%s.md", scenariosDigest, id)
	body, err := sink.Get(ctx, key)
	if err != nil {
		return false, fmt.Errorf("resolve scenario %s: %w", id, err)
	}
	tmp, err := os.CreateTemp("", "farm-scenario-*.md")
	if err != nil {
		return false, fmt.Errorf("resolve scenario %s: %w", id, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return false, fmt.Errorf("resolve scenario %s: %w", id, err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("resolve scenario %s: %w", id, err)
	}
	sc, err := eval.ParseScenario(tmp.Name())
	if err != nil {
		return false, fmt.Errorf("resolve scenario %s: %w", id, err)
	}
	return strings.HasPrefix(sc.Area, "launch"), nil
}

// resolveEvaluatorSHA returns the evaluator pin: the --evaluator flag when
// given, else the bucket's evaluators/current pointer (written by `farm
// push --evaluator`, plain-text sha256, trimmed on read). ZCP_FARM_EVALUATOR_SHA
// is no longer read here — the run project still receives the resolved
// value via project_yaml, unchanged.
func resolveEvaluatorSHA(ctx context.Context, sink *farm.SinkClient, evaluatorFlag string) (string, error) {
	if evaluatorFlag != "" {
		return evaluatorFlag, nil
	}
	body, err := sink.Get(ctx, "evaluators/current")
	if err != nil {
		return "", fmt.Errorf("--evaluator was not given and evaluators/current: %w", err)
	}
	return strings.TrimSpace(string(body)), nil
}

// detachStarter starts argv detached (new session, stdout/stderr appended
// to logPath) and returns immediately without waiting for it to finish —
// a package var so tests can swap in a recording fake instead of actually
// forking a process (§3.1 FM-18: "no daemon... kickoff SSH session may
// drop").
var detachStarter = func(argv []string, logPath string) error {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open log %s: %w", logPath, err)
	}
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec // G204: argv[0] is this same binary's own resolved path (os.Executable), never user input
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start detached: %w", err)
	}
	return nil
}

// planDetachRun computes the re-exec argv (this binary, "eval farm run",
// the original args with --detach removed and --batch pinned to batch) and
// the log path (<cwd>/farm-<batch>.log) for `farm run --detach`.
func planDetachRun(exe string, args []string, cwd, batch string) (argv []string, logPath string) {
	filtered := make([]string, 0, len(args)+2)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--detach":
			continue
		case flagBatch:
			i++ // also skip its value
			continue
		default:
			filtered = append(filtered, args[i])
		}
	}
	filtered = append(filtered, flagBatch, batch)
	// runFarmRun's own args never include the "run" verb itself (the
	// dispatcher strips it) — the re-exec argv, running the whole binary
	// from scratch, needs the full "eval farm run" path.
	argv = append([]string{exe, "eval", "farm", farmVerbRun}, filtered...)
	logPath = filepath.Join(cwd, fmt.Sprintf("farm-%s.log", batch))
	return argv, logPath
}

// runFarmRunDetach implements --detach: re-exec this binary without
// --detach, redirected to a log file, then return immediately so the
// kickoff SSH session may drop.
func runFarmRunDetach(args []string, batch string) int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: resolve own executable: %v\n", err)
		return 1
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: getwd: %v\n", err)
		return 1
	}
	argv, logPath := planDetachRun(exe, args, cwd, batch)
	if err := detachStarter(argv, logPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: detach: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stdout, "batch %s detached, log %s\n", batch, logPath)
	return 0
}

// runFarmStatus implements `zcp eval farm status [<batch>]`
// (docs/spec-eval-farm.md §3.2/§1.4): recomputes from the bucket listing
// (batches/, runs/*/done.json) and the live project list — no local state
// file.
func runFarmStatus(args []string) int {
	batchFilter := ""
	if len(args) > 0 {
		batchFilter = args[0]
	}

	cfg, err := farm.ConfigFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	sink := farm.NewSinkClient(cfg)

	accountToken := os.Getenv("ZCP_FARM_ACCOUNT_TOKEN")
	clientID := os.Getenv("ZCP_FARM_CLIENT_ID")
	if accountToken == "" || clientID == "" {
		fmt.Fprintln(os.Stderr, "error: ZCP_FARM_ACCOUNT_TOKEN and ZCP_FARM_CLIENT_ID are required")
		return 1
	}
	client, closer, err := farm.NewAccountClient(accountToken, os.Getenv("ZCP_API_HOST"), clientID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer closer()

	ctx := context.Background()
	batches, err := farm.ListBatches(ctx, sink)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: list batches: %v\n", err)
		return 1
	}

	projects, err := client.ListProjects(ctx, clientID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: list projects: %v\n", err)
		return 1
	}
	// liveStatus maps a project name to its REST status — a project mid
	// async-delete is still in this list (`DELETING`, not yet gone), so
	// "present" must not be read from mere list membership alone.
	liveStatus := make(map[string]string, len(projects))
	for _, p := range projects {
		liveStatus[p.Name] = p.Status
	}

	for _, batch := range batches {
		if batchFilter != "" && batch != batchFilter {
			continue
		}
		manifest, err := farm.GetManifest(ctx, sink, batch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: read manifest for %s: %v\n", batch, err)
			continue
		}
		// The summary is evidence layered on top of the bucket+project
		// recompute (FM-9), never a replacement for it: a batch with no
		// summary.json yet keeps today's bare "running" label for a
		// done.json-less run.
		hasSummary, err := farm.SummaryExists(ctx, sink, batch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: check summary for %s: %v\n", batch, err)
			continue
		}
		var summary farm.BatchSummary
		summaryByRun := map[string]farm.SummaryRun{}
		if hasSummary {
			summary, err = farm.GetSummary(ctx, sink, batch)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: read summary for %s: %v\n", batch, err)
				continue
			}
			for _, sr := range summary.Runs {
				summaryByRun[sr.RunID] = sr
			}
		}
		for _, run := range manifest.Runs {
			hasDone, _, err := sink.Head(ctx, "runs/"+run.RunID+"/done.json")
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: head done.json for %s: %v\n", run.RunID, err)
				continue
			}
			state := "running"
			if hasDone {
				state = "done"
			} else if hasSummary {
				if sr, ok := summaryByRun[run.RunID]; ok {
					state = fmt.Sprintf("%s: %s (batch ended by %s)", sr.Result, sr.Detail, summary.EndedBy)
				}
			}
			projState := "deleted"
			if status, ok := liveStatus[run.ProjectName]; ok {
				projState = "present"
				if status == "DELETING" {
					projState = "deleting"
				}
			}
			fmt.Fprintf(os.Stdout, "%s %s %s %s project=%s\n", batch, run.RunID, run.Scenario, state, projState)
		}
	}
	return 0
}

// runFarmGC implements `zcp eval farm gc [--older-than <duration>] [--yes]`
// (docs/spec-eval-farm.md §3.6 FM-26): prints every zcp-farm- candidate and
// why it is or isn't eligible, deletes the eligible ones only with --yes,
// then revokes launch tokens orphaned by an earlier no-bundle exemption
// whose project is now gone.
func runFarmGC(args []string) int {
	var olderThan time.Duration
	yes := false
	for i := 0; i < len(args); i++ {
		arg := args[i] //nolint:gosec // G602 false positive: i is loop-bounded by i < len(args) each iteration
		switch arg {
		case "--older-than":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: %s requires a value\n", arg)
				return 1
			}
			d, err := time.ParseDuration(args[i+1])
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: --older-than: %v\n", err)
				return 1
			}
			olderThan = d
			i++
		case "--yes":
			yes = true
		}
	}

	cfg, err := farm.ConfigFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	sink := farm.NewSinkClient(cfg)

	accountToken := os.Getenv("ZCP_FARM_ACCOUNT_TOKEN")
	clientID := os.Getenv("ZCP_FARM_CLIENT_ID")
	if accountToken == "" || clientID == "" {
		fmt.Fprintln(os.Stderr, "error: ZCP_FARM_ACCOUNT_TOKEN and ZCP_FARM_CLIENT_ID are required")
		return 1
	}
	client, closer, err := farm.NewAccountClient(accountToken, os.Getenv("ZCP_API_HOST"), clientID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer closer()

	ctx := context.Background()
	candidates, err := farm.GC(ctx, client, sink, farm.GCOptions{ClientID: clientID, OlderThan: olderThan})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	for _, c := range candidates {
		if c.Exempt != "" {
			fmt.Fprintf(os.Stdout, "%s exempt: %s\n", c.Name, c.Exempt)
			continue
		}
		fmt.Fprintf(os.Stdout, "%s candidate\n", c.Name)
	}

	if !yes {
		return 0
	}

	if errs := farm.GCApply(ctx, client, candidates); len(errs) > 0 {
		for _, gcErr := range errs {
			fmt.Fprintf(os.Stderr, "error: %v\n", gcErr)
		}
		return 1
	}
	if err := farm.RevokeOrphanedLaunchTokens(ctx, client, sink, clientID); err != nil {
		fmt.Fprintf(os.Stderr, "error: revoke orphaned launch tokens: %v\n", err)
		return 1
	}
	return 0
}
