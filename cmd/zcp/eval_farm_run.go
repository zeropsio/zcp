package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/zeropsio/zcp/internal/eval"
	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
	"github.com/zeropsio/zcp/internal/platform"
)

// maxNoteChars is `farm run --note`'s length limit (§3.3).
const maxNoteChars = 200

// defaultRunBudget is used when `--run-budget` is not given.
const defaultRunBudget = 45 * time.Minute

// flagBatch names the --batch flag, shared between runFarmRun's own parse
// loop and planDetachRun's re-exec argv rewrite (goconst).
const flagBatch = "--batch"

// flagDetach is `farm run`'s background-mode flag, stripped again by
// planDetachRun before the detached re-exec.
const flagDetach = "--detach"

// runFarmRun implements `zcp eval farm run` (docs/spec-eval-farm.md §3.3
// FM-21/FM-22, §3.1 FM-18's --detach).
func runFarmRun(args []string, envr *farm.EnvResolver) int {
	flags, err := parseFarmRunFlags(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	batch := flags.batch
	if batch == "" {
		// Nanosecond precision keeps concurrently launched commands from
		// selecting the same human-facing id; the conditional manifest claim
		// remains the authoritative collision guard.
		batch = fmt.Sprintf("batch-%d", time.Now().UnixNano())
	}
	if !farm.ValidBatchID(batch) {
		fmt.Fprintf(os.Stderr, "error: --batch: %q does not match the batch-id grammar (docs/spec-eval-farm.md §7.6 FM-47: ^[a-z0-9][a-z0-9-]{0,62}$)\n", batch)
		return 2
	}
	observer, err := resolveObserver(flags.observer)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: --observer: %v\n", err)
		return 2
	}
	if n := utf8.RuneCountInString(flags.note); n > maxNoteChars {
		fmt.Fprintf(os.Stderr, "error: --note: %d characters, want at most %d\n", n, maxNoteChars)
		return 2
	}

	if flags.detach {
		return runFarmRunDetach(args, batch, startDetached)
	}

	runBudget := defaultRunBudget
	if flags.runBudget != "" {
		d, err := time.ParseDuration(flags.runBudget)
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

	cfg, err := farmConfigFromResolver(envr)
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

	scenarios, err := resolveScenarios(ctx, sink, batch, flags.scenariosDigest, flags.set)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: resolve scenario set: %v\n", err)
		return 1
	}

	accountToken := envr.Lookup("ZCP_FARM_ACCOUNT_TOKEN")
	clientID := envr.Lookup("ZCP_FARM_CLIENT_ID")
	if accountToken == "" || clientID == "" {
		fmt.Fprintln(os.Stderr, "error: ZCP_FARM_ACCOUNT_TOKEN and ZCP_FARM_CLIENT_ID are required")
		return 1
	}

	evaluatorSHA, err := resolvePin(ctx, sink, flags.evaluator, "--evaluator", "evaluators/current")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	wrapperSHA, err := resolvePin(ctx, sink, flags.wrapper, "--wrapper", "farm/wrapper/current")
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

	candidateInfo, err := resolveCandidateInfo(ctx, sink, flags.candidate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: read candidate info: %v\n", err)
	}

	opts := farm.RunOptions{
		Batch: batch, ClientID: clientID, Set: flags.set,
		CandidateSHA256: flags.candidate, EvaluatorSHA256: evaluatorSHA, WrapperSHA256: wrapperSHA, ScenariosDigest: flags.scenariosDigest,
		Scenarios: scenarios, OAuthToken: oauthToken, Observer: observer,
		Sink:      farm.Sink(cfg), // farm.Config and farm.Sink share the same field names/types/order
		RunBudget: runBudget,
		Note:      flags.note, RunBudgetSec: int(runBudget.Seconds()), CandidateInfo: candidateInfo,
	}
	results, err := farm.RunBatch(ctx, client, sink, opts)
	if err != nil {
		printFarmRecoveryIDs(results)
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

func printFarmRecoveryIDs(results []farm.RunResult) {
	for _, r := range results {
		if r.ProjectID != "" {
			fmt.Fprintf(os.Stderr, "recovery: run %s projectId=%s\n", r.RunID, r.ProjectID)
		}
		if r.LaunchTokenID != "" {
			fmt.Fprintf(os.Stderr, "recovery: run %s launchTokenId=%s\n", r.RunID, r.LaunchTokenID)
		}
	}
}

// farmRunFlags is `zcp eval farm run`'s parsed command line.
type farmRunFlags struct {
	candidate, scenariosDigest, set, batch, runBudget, evaluator, wrapper, observer, note string
	detach                                                                                bool
}

// parseFarmRunFlags parses `farm run`'s flags; unknown arguments are
// ignored, a valued flag without its value and a missing required flag
// (--candidate, --scenarios, --set) are errors.
func parseFarmRunFlags(args []string) (farmRunFlags, error) {
	var f farmRunFlags
	valued := map[string]*string{
		flagCandidate: &f.candidate, "--scenarios": &f.scenariosDigest, "--evaluator": &f.evaluator,
		"--wrapper": &f.wrapper, "--set": &f.set, flagBatch: &f.batch, "--run-budget": &f.runBudget,
		"--observer": &f.observer, "--note": &f.note,
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == flagDetach {
			f.detach = true
			continue
		}
		dst, ok := valued[arg]
		if !ok {
			continue
		}
		if i+1 >= len(args) {
			return f, fmt.Errorf("%s requires a value", arg)
		}
		*dst = args[i+1]
		i++
	}
	if f.candidate == "" || f.scenariosDigest == "" || f.set == "" {
		return f, fmt.Errorf("--candidate, --scenarios, and --set are required")
	}
	return f, nil
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

// resolveObserver validates --observer's value against §3.3/§7.7's
// allow-list ("off", or one of observer.Models), defaulting to
// observer.DefaultModel when flag is empty. Any other
// value is a flag error — the caller exits 2.
func resolveObserver(flag string) (string, error) {
	if flag == "" {
		return observer.DefaultModel, nil
	}
	if flag == "off" || observer.ValidModel(flag) {
		return flag, nil
	}
	return "", fmt.Errorf("%q is not \"off\" or one of %s", flag, strings.Join(observer.Models, ", "))
}

// resolveScenarios expands --set (gate|all|<id,id,...>) to the
// farm.ScenarioRun list RunBatch needs, marking each scenario Launch when
// its front matter's area starts with "launch" (the two gate-set launch
// scenarios use "launch" and "launch-production-recovery"). The farm host
// runs the binary only, no repo checkout (docs/spec-eval-farm.md §3.1
// FM-17/FM-18), so every scenario fact is read from the bucket at
// scenariosDigest, never from disk.
func resolveScenarios(ctx context.Context, sink *farm.SinkClient, batch, scenariosDigest, set string) ([]farm.ScenarioRun, error) {
	var ids []string
	gateSet := set == "gate"
	allSet := set == categoryAll
	switch set {
	case "gate", categoryAll: // "all" — shared with sync.go's own --category all (goconst)
	default:
		for id := range strings.SplitSeq(set, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				ids = append(ids, id)
			}
		}
	}
	if !gateSet && !allSet && len(ids) == 0 {
		return []farm.ScenarioRun{}, nil
	}

	scenariosDir, err := downloadVerifiedScenarioTree(ctx, sink, scenariosDigest)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(scenariosDir) }()
	if gateSet {
		ids, err = readGateSetFromDir(scenariosDir)
	} else if allSet {
		ids, err = listAllScenarioIDsFromDir(scenariosDir)
	}
	if err != nil {
		return nil, err
	}

	scenarios := make([]farm.ScenarioRun, 0, len(ids))
	for _, id := range ids {
		if !farm.ValidScenarioID(id) {
			return nil, fmt.Errorf("invalid scenario %q in set %q", id, set)
		}
		launch, production, err := resolveScenarioOwnership(scenariosDir, id, batch)
		if err != nil {
			return nil, err
		}
		scenarios = append(scenarios, farm.ScenarioRun{ID: id, Launch: launch, ProductionProjectName: production})
	}
	return scenarios, nil
}

// downloadVerifiedScenarioTree snapshots scenarios/<digest>/, rejects every
// object key that cannot stay beneath that root, and verifies the complete
// downloaded tree before a gate list or scenario front matter is trusted.
func downloadVerifiedScenarioTree(ctx context.Context, sink *farm.SinkClient, scenariosDigest string) (dir string, err error) {
	prefix := "scenarios/" + scenariosDigest + "/"
	keys, err := sink.List(ctx, prefix)
	if err != nil {
		return "", fmt.Errorf("list scenario tree %s: %w", scenariosDigest, err)
	}
	dir, err = os.MkdirTemp("", "farm-scenarios-verify-")
	if err != nil {
		return "", fmt.Errorf("create scenario verification directory: %w", err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(dir)
		}
	}()
	for _, key := range keys {
		rel, ok := strings.CutPrefix(key, prefix)
		if !ok || !validScenarioObjectPath(rel) {
			return "", fmt.Errorf("invalid scenario object key %q", key)
		}
		body, err := sink.Get(ctx, key)
		if err != nil {
			return "", fmt.Errorf("read scenario object %s: %w", key, err)
		}
		dest := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", fmt.Errorf("create scenario directory for %s: %w", key, err)
		}
		if err := os.WriteFile(dest, body, 0o600); err != nil {
			return "", fmt.Errorf("write scenario object %s: %w", key, err)
		}
	}
	got, err := farm.TreeDigest(dir)
	if err != nil {
		return "", fmt.Errorf("digest downloaded scenario tree: %w", err)
	}
	if got != scenariosDigest {
		return "", fmt.Errorf("scenario tree digest mismatch: expected %s, got %s", scenariosDigest, got)
	}
	keep = true
	return dir, nil
}

func validScenarioObjectPath(rel string) bool {
	if rel == "" || path.IsAbs(rel) || strings.Contains(rel, "\\") {
		return false
	}
	cleaned := path.Clean(rel)
	return cleaned == rel && cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

func readGateSetFromDir(scenariosDir string) ([]string, error) {
	body, err := os.ReadFile(filepath.Join(scenariosDir, boundGateSetFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("scenario tree has no digest-bound gate set; repush it with this zcp version")
		}
		return nil, fmt.Errorf("read digest-bound gate set: %w", err)
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

// listAllScenarioIDsFromDir returns the basename (minus ".md") of every
// top-level scenario markdown file in the verified snapshot,
// sorted. Every scenario currently authored under
// eval/behavioral/scenarios/ is container-run; a scenario meant only for a
// local, non-farm lane would need its own marker to be excluded here — none
// carries one yet.
func listAllScenarioIDsFromDir(scenariosDir string) ([]string, error) {
	entries, err := os.ReadDir(scenariosDir)
	if err != nil {
		return nil, fmt.Errorf("list verified scenarios: %w", err)
	}
	var ids []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(entry.Name(), ".md"))
	}
	sort.Strings(ids)
	return ids, nil
}

// resolveScenarioOwnership reads and parses one scenario from the verified
// local snapshot exactly once.
// Its area controls launch-token handling. Only a structured launchShape
// target matching the canonical run-specific farm name grants the controller
// ownership for automatic deletion.
func resolveScenarioOwnership(scenariosDir, id, batch string) (bool, string, error) {
	sc, err := eval.ParseScenario(filepath.Join(scenariosDir, id+".md"))
	if err != nil {
		return false, "", fmt.Errorf("resolve scenario %s: %w", id, err)
	}
	launch := strings.HasPrefix(sc.Area, "launch")
	if sc.Verification == nil || sc.Verification.LaunchShape == nil || sc.Verification.LaunchShape.ProdProject == "" {
		return launch, "", nil
	}
	if batch == "" {
		return launch, "", nil
	}
	runID, err := farm.EncodeRunID(batch, id)
	if err != nil {
		return false, "", fmt.Errorf("resolve scenario %s: %w", id, err)
	}
	want := farm.ProductionProjectName(runID)
	got := strings.ReplaceAll(sc.Verification.LaunchShape.ProdProject, "{{runId}}", runID)
	if got != want {
		return false, "", fmt.Errorf("resolve scenario %s: foreign production project %q", id, got)
	}
	return launch, got, nil
}

// resolvePin returns a digest pin: flag when given, else the trimmed body of
// the bucket's plain-text pointer object (`evaluators/current` or
// `farm/wrapper/current`, both written by `farm push`). The error names both
// the flag and the pointer key. ZCP_FARM_EVALUATOR_SHA is not read here — the
// run project still receives the resolved value via project_yaml.
func resolvePin(ctx context.Context, sink *farm.SinkClient, flag, flagName, pointerKey string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	body, err := sink.Get(ctx, pointerKey)
	if err != nil {
		return "", fmt.Errorf("%s was not given and %s: %w", flagName, pointerKey, err)
	}
	return strings.TrimSpace(string(body)), nil
}

// resolveCandidateInfo reads the candidate binary's build info uploaded by
// `farm push --candidate` (candidates/<sha256>.info.json, §3.3). A missing
// object — the candidate was built without VCS stamping, or was pushed
// before this field existed — is not an error: it returns (nil, nil), and
// the manifest simply carries no candidateInfo (§3.3: "a candidate built
// without VCS stamping has no candidateInfo"). Any other read or parse
// failure is returned so the caller can warn without ever failing the run
// on it — none of these fields reaches a run project either way.
func resolveCandidateInfo(ctx context.Context, sink *farm.SinkClient, candidateSHA256 string) (*farm.CandidateInfo, error) {
	key := farm.CandidateInfoKey(candidateSHA256)
	body, err := sink.Get(ctx, key)
	if err != nil {
		if errors.Is(err, farm.ErrObjectNotFound) {
			return nil, nil //nolint:nilnil // absence is a legitimate third state, distinct from an error (§3.3)
		}
		return nil, fmt.Errorf("get %s: %w", key, err)
	}
	var info farm.CandidateInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("parse %s: %w", key, err)
	}
	return &info, nil
}

// startDetached starts argv detached (new session, stdout/stderr appended
// to logPath) and returns immediately without waiting for it to finish
// (§3.1 FM-18: "no daemon... kickoff SSH session may drop"). It is the
// production implementation of runFarmRunDetach's starter parameter —
// R10a: an injected dependency, not a package-level mutable var, so tests
// pass in a recording fake instead of reaching into shared state.
func startDetached(argv []string, logPath string) error {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open log %s: %w", logPath, err)
	}
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec,noctx // G204: argv[0] is this same binary's own resolved path (os.Executable), never user input; noctx: the detached child must outlive this process's own context by design (§3.1 FM-18), so it is never bound to one
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
		case flagDetach:
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
// kickoff SSH session may drop. starter is the injected detach-and-start
// dependency (R10a) — production callers pass startDetached; tests pass a
// recording fake.
func runFarmRunDetach(args []string, batch string, starter func(argv []string, logPath string) error) int {
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
	if err := starter(argv, logPath); err != nil {
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
func runFarmStatus(args []string, envr *farm.EnvResolver) int {
	batchFilter := ""
	if len(args) > 0 {
		batchFilter = args[0]
	}

	cfg, err := farmConfigFromResolver(envr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	sink := farm.NewSinkClient(cfg)

	accountToken := envr.Lookup("ZCP_FARM_ACCOUNT_TOKEN")
	clientID := envr.Lookup("ZCP_FARM_CLIENT_ID")
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
func runFarmGC(args []string, envr *farm.EnvResolver) int {
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

	cfg, err := farmConfigFromResolver(envr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	sink := farm.NewSinkClient(cfg)

	accountToken := envr.Lookup("ZCP_FARM_ACCOUNT_TOKEN")
	clientID := envr.Lookup("ZCP_FARM_CLIENT_ID")
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
