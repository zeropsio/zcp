package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/eval/farm"
)

// runEvalFarm dispatches `zcp eval farm <verb>`. It lives in the same binary
// as `zcp eval behavioral`, on the farm host, in the persistent zcp-farm
// project (docs/spec-eval-farm.md §3.1 FM-17). Gated maintainer-only, same
// discipline as runtime.Info.Authoring / ZCP_AUTHORING
// (internal/runtime/runtime.go:52) — the farm host holds an account-wide key
// and deletes projects, so an ungated route here is a defect the same way an
// ungated authoring route is. Every verb is refused, not just the mutating
// ones: a route reachable without the gate is a defect regardless of what
// that particular verb does.
//
// Farm verb names, as named constants rather than repeated string literals
// (goconst) — the not-implemented verbs in particular share their literal
// with an unrelated dispatcher elsewhere in this package (eval_behavioral.go
// "run", capture.go/telemetry_cmd.go "status", eval_capture.go "report").
const (
	farmVerbPush     = "push"
	farmVerbPull     = "pull"
	farmVerbRun      = "run"
	farmVerbStatus   = "status"
	farmVerbReport   = "report"
	farmVerbCoverage = "coverage"
	farmVerbGC       = "gc"

	// flagCandidate names the file under test — shared with
	// eval_behavioral.go's own --candidate execution-binding flag (a
	// different subsystem, the same concept: "the candidate under test").
	// One constant instead of a second literal avoids a goconst false
	// split across the two dispatchers.
	flagCandidate = "--candidate"

	// Bundle completeness (docs/spec-eval-farm.md §5: "bundle:
	// complete|partial|missing").
	bundleComplete = "complete"
	bundlePartial  = "partial"
	bundleMissing  = "missing"
)

// No init() registration (CLAUDE.md forbids global mutable state): this is a
// plain switch. Later slices add their verb file and replace one
// notImplemented case each — run/status/report/coverage/gc land after S2.
func runEvalFarm(args []string) int {
	if os.Getenv("ZCP_AUTHORING") != "1" {
		fmt.Fprintln(os.Stderr, "zcp eval farm requires ZCP_AUTHORING=1 (docs/spec-eval-farm.md §3.1 FM-17)")
		return 1
	}
	if len(args) == 0 {
		printEvalFarmUsage()
		return 1
	}
	switch args[0] {
	case farmVerbPush:
		return runFarmPush(args[1:])
	case farmVerbPull:
		return runFarmPull(args[1:])
	case farmVerbCoverage:
		return runFarmCoverage(args[1:])
	case farmVerbReport:
		return runFarmReport(args[1:])
	case farmVerbRun:
		return runFarmRun(args[1:])
	case farmVerbStatus:
		return runFarmStatus(args[1:])
	case farmVerbGC:
		return runFarmGC(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown farm subcommand: %s\n", args[0])
		printEvalFarmUsage()
		return 1
	}
}

// runFarmPush uploads one or more parts to the farm bucket, under the
// layout of docs/spec-eval-farm.md §1.1, and prints the digest of each part
// it uploaded.
func runFarmPush(args []string) int {
	var candidate, evaluator, scenarios, wrapper, gateSet string
	for i := 0; i < len(args); i++ {
		arg := args[i] //nolint:gosec // G602 false positive: i is loop-bounded by i < len(args) each iteration (same shape as eval_behavioral.go parseExecutionBindingFlags)
		switch arg {
		case flagCandidate:
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: %s requires a value\n", arg)
				return 1
			}
			candidate = args[i+1]
			i++
		case "--evaluator":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: %s requires a value\n", arg)
				return 1
			}
			evaluator = args[i+1]
			i++
		case "--scenarios":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: %s requires a value\n", arg)
				return 1
			}
			scenarios = args[i+1]
			i++
		case "--wrapper":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: %s requires a value\n", arg)
				return 1
			}
			wrapper = args[i+1]
			i++
		case "--gate-set":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: %s requires a value\n", arg)
				return 1
			}
			gateSet = args[i+1]
			i++
		}
	}
	if candidate == "" && evaluator == "" && scenarios == "" && wrapper == "" {
		fmt.Fprintln(os.Stderr, "error: one of --candidate, --evaluator, --scenarios, or --wrapper is required")
		return 1
	}

	cfg, err := farm.ConfigFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	client := farm.NewSinkClient(cfg)
	ctx := context.Background()

	if candidate != "" {
		digest, err := pushSinglePart(ctx, client, "candidates", candidate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: push candidate: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stdout, "candidate: %s\n", digest)
	}
	if evaluator != "" {
		digest, err := pushSinglePart(ctx, client, "evaluators", evaluator)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: push evaluator: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stdout, "evaluator: %s\n", digest)

		if err := client.Put(ctx, "evaluators/current", []byte(digest)); err != nil {
			fmt.Fprintf(os.Stderr, "error: push evaluator pointer: %v\n", err)
			return 1
		}
		fmt.Fprintln(os.Stdout, "evaluator-pointer: evaluators/current")
	}
	if scenarios != "" {
		digest, err := pushScenarioTree(ctx, client, scenarios)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: push scenarios: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stdout, "scenarios: %s\n", digest)

		gateSetPath, err := resolveGateSetPath(scenarios, gateSet)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: push gate set: %v\n", err)
			return 1
		}
		gateKey, err := pushGateSet(ctx, client, digest, gateSetPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: push gate set: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stdout, "gate-set: %s\n", gateKey)
	}
	if wrapper != "" {
		digest, err := farm.FileDigest(wrapper)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: push wrapper: %v\n", err)
			return 1
		}
		body, err := os.ReadFile(wrapper)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: push wrapper: read %s: %v\n", wrapper, err)
			return 1
		}
		// Content-addressed (R5, LAND review): every run container holds a
		// write-capable bucket key, so an unpinned "farm/wrapper.sh" key
		// let one run's agent overwrite the wrapper for every later run —
		// code execution as `zerops`. The run project's init line now
		// fetches farm/wrapper/<sha256>.sh and verifies it against the
		// descriptor's ZCP_FARM_WRAPPER_SHA before exec (project_yaml.go);
		// there is no unpinned "farm/wrapper.sh" key to write anymore.
		key := fmt.Sprintf("farm/wrapper/%s.sh", digest)
		if err := client.Put(ctx, key, body); err != nil {
			fmt.Fprintf(os.Stderr, "error: push wrapper: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stdout, "wrapper: %s\n", digest)

		if err := client.Put(ctx, "farm/wrapper/current", []byte(digest)); err != nil {
			fmt.Fprintf(os.Stderr, "error: push wrapper pointer: %v\n", err)
			return 1
		}
		fmt.Fprintln(os.Stdout, "wrapper-pointer: farm/wrapper/current")
	}
	return 0
}

// pushSinglePart uploads the file at path to "<prefix>/<sha256(file)>/zcp"
// (the candidates/... and evaluators/... layout of §1.1) and returns that
// digest.
func pushSinglePart(ctx context.Context, client *farm.SinkClient, prefix, path string) (digest string, err error) {
	digest, err = farm.FileDigest(path)
	if err != nil {
		return "", err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	key := fmt.Sprintf("%s/%s/zcp", prefix, digest)
	if err := client.Put(ctx, key, body); err != nil {
		return "", err
	}
	return digest, nil
}

// pushScenarioTree uploads every regular file under dir to
// "scenarios/<treeDigest>/<relative-path>" and returns the tree digest.
func pushScenarioTree(ctx context.Context, client *farm.SinkClient, dir string) (digest string, err error) {
	digest, err = farm.TreeDigest(dir)
	if err != nil {
		return "", err
	}
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("rel %s: %w", path, err)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		key := fmt.Sprintf("scenarios/%s/%s", digest, filepath.ToSlash(rel))
		return client.Put(ctx, key, body)
	})
	if err != nil {
		return "", err
	}
	return digest, nil
}

// resolveGateSetPath resolves the local gate scenario list `farm push`
// reads, always relative to the --scenarios argument, never to cwd (S22
// finding 3: cwd-relative resolution made `farm push` fail from anywhere
// but the repo root). override, when non-empty, is `--gate-set` and wins
// outright; otherwise the default is "<scenariosDir>/../../farm/gate-set.txt"
// resolved against scenariosDir — the repo layout is eval/behavioral/scenarios
// beside eval/farm/gate-set.txt, so the gate set is two levels up. The result is always absolute so a
// resolution failure's error names the exact path that was tried,
// regardless of whether the inputs were relative or absolute.
func resolveGateSetPath(scenariosDir, override string) (string, error) {
	path := override
	if path == "" {
		path = filepath.Join(scenariosDir, "..", "..", "farm", "gate-set.txt")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve gate-set path %s: %w", path, err)
	}
	return abs, nil
}

// pushGateSet uploads the local gate scenario list at gateSetPath (resolved
// by resolveGateSetPath) to "sets/<scenariosDigest>/gate.txt" — the bucket
// location `farm run --set gate` reads on the farm host, which has no
// checkout of this file (docs/spec-eval-farm.md §3.1 FM-17/FM-18). Keyed by
// the scenario tree digest it was just pushed against, so a set list always
// names ids that actually exist in that tree.
func pushGateSet(ctx context.Context, client *farm.SinkClient, scenariosDigest, gateSetPath string) (key string, err error) {
	body, err := os.ReadFile(gateSetPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", gateSetPath, err)
	}
	key = fmt.Sprintf("sets/%s/gate.txt", scenariosDigest)
	if err := client.Put(ctx, key, body); err != nil {
		return "", err
	}
	return key, nil
}

// runFarmPull downloads runs/<runId>/** (or every run a batch's manifest
// lists) to <out>/<runId>/, and prints each run's bundle completeness
// (docs/spec-eval-farm.md §5: "bundle: complete|partial|missing").
func runFarmPull(args []string) int {
	var runID, batch, out string
	for i := 0; i < len(args); i++ {
		arg := args[i] //nolint:gosec // G602 false positive: i is loop-bounded by i < len(args) each iteration (same shape as eval_behavioral.go parseExecutionBindingFlags)
		switch arg {
		case "--batch":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: %s requires a value\n", arg)
				return 1
			}
			batch = args[i+1]
			i++
		case "--out":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "error: %s requires a value\n", arg)
				return 1
			}
			out = args[i+1]
			i++
		default:
			if !strings.HasPrefix(arg, "--") && runID == "" {
				runID = arg
			}
		}
	}
	if out == "" {
		fmt.Fprintln(os.Stderr, "error: --out <dir> is required")
		return 1
	}
	if runID == "" && batch == "" {
		fmt.Fprintln(os.Stderr, "error: a runId or --batch <batch> is required")
		return 1
	}

	cfg, err := farm.ConfigFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	client := farm.NewSinkClient(cfg)
	ctx := context.Background()

	runIDs := []string{runID}
	if batch != "" {
		manifestBody, err := client.Get(ctx, fmt.Sprintf("batches/%s/manifest.json", batch))
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: read batch manifest: %v\n", err)
			return 1
		}
		var manifest farm.BatchManifest
		if err := json.Unmarshal(manifestBody, &manifest); err != nil {
			fmt.Fprintf(os.Stderr, "error: parse batch manifest: %v\n", err)
			return 1
		}
		runIDs = make([]string, 0, len(manifest.Runs))
		for _, run := range manifest.Runs {
			runIDs = append(runIDs, run.RunID)
		}

		if err := writeBatchManifestAndSummary(ctx, client, batch, manifestBody, out); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
	}

	for _, id := range runIDs {
		status, err := pullRunBundle(ctx, client, id, out)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: pull %s: %v\n", id, err)
			return 1
		}
		fmt.Fprintf(os.Stdout, "%s: bundle: %s\n", id, status)
	}
	return 0
}

// writeBatchManifestAndSummary writes batches/<batch>/manifest.json (already
// fetched as manifestBody) and, when present, batches/<batch>/summary.json
// directly under <out>/ — the batch dir itself — which is where
// `farm report <out>` reads them for its roll-up (printFarmRollup); every
// SUBdirectory of <out> is taken as a run bundle, so the files must not
// live in one (docs/spec-eval-farm.md §1.4 FM-9).
func writeBatchManifestAndSummary(ctx context.Context, client *farm.SinkClient, batch string, manifestBody []byte, out string) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", out, err)
	}
	if err := os.WriteFile(filepath.Join(out, "manifest.json"), manifestBody, 0o600); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	hasSummary, err := farm.SummaryExists(ctx, client, batch)
	if err != nil {
		return fmt.Errorf("check summary for %s: %w", batch, err)
	}
	if !hasSummary {
		return nil
	}
	summaryBody, err := client.Get(ctx, fmt.Sprintf("batches/%s/summary.json", batch))
	if err != nil {
		return fmt.Errorf("read batch summary: %w", err)
	}
	if err := os.WriteFile(filepath.Join(out, "summary.json"), summaryBody, 0o600); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}
	return nil
}

// pullRunBundle downloads every object under runs/<runID>/ to
// <out>/<runID>/, preserving the key's structure below the run id. It
// returns the run's bundle completeness: "missing" when the run has no
// objects at all, "complete" once a done.json was among what it fetched
// (FM-3), "partial" otherwise.
func pullRunBundle(ctx context.Context, client *farm.SinkClient, runID, out string) (status string, err error) {
	prefix := "runs/" + runID + "/"
	keys, err := client.List(ctx, prefix)
	if err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return bundleMissing, nil
	}

	hasDone := false
	for _, key := range keys {
		body, err := client.Get(ctx, key)
		if err != nil {
			return "", fmt.Errorf("get %s: %w", key, err)
		}
		rel := strings.TrimPrefix(key, prefix)
		dest := filepath.Join(out, runID, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", fmt.Errorf("mkdir for %s: %w", dest, err)
		}
		if err := os.WriteFile(dest, body, 0o600); err != nil {
			return "", fmt.Errorf("write %s: %w", dest, err)
		}
		if rel == "done.json" {
			hasDone = true
		}
	}
	if hasDone {
		return bundleComplete, nil
	}
	return bundlePartial, nil
}

func printEvalFarmUsage() {
	fmt.Fprintln(os.Stderr, `Usage: zcp eval farm <command>

Commands (ZCP_AUTHORING=1 required):
  push     --candidate <file> | --evaluator <file> | --scenarios <dir> [--gate-set <path>] | --wrapper <file>
                                               Upload one part to the farm bucket, print its digest.
                                               --gate-set overrides the gate scenario list read alongside
                                               --scenarios (default: <scenariosDir>/../../farm/gate-set.txt)
  pull     <runId>|--batch <batch> --out <dir> Download a run's (or a batch's) bundle
  run      --candidate <sha256> --scenarios <digest> --set gate|all|<ids> [--batch <id>] [--run-budget 45m] [--detach]
                                               Create zcp-farm-<runId> projects, watch the bucket, delete after done.json
  status   [<batch>]                           Recompute batch/run state from the bucket and the project list
  report   [--evaluator-sha256 <sha>] <dir>    Report over a pulled batch or run dir (no network)
  coverage <dir> [--since <batch>]           Derive (scenario, step, decision) coverage cells from pulled bundles
  gc       [--older-than <duration>] [--yes]   Delete zcp-farm-* projects no running batch references`)
}
