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
	case farmVerbReport:
		return runFarmReport(args[1:])
	case farmVerbRun, farmVerbStatus, farmVerbCoverage, farmVerbGC:
		fmt.Fprintf(os.Stderr, "zcp eval farm %s: not implemented\n", args[0])
		return 1
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
	var candidate, evaluator, scenarios, wrapper string
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
	}
	if scenarios != "" {
		digest, err := pushScenarioTree(ctx, client, scenarios)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: push scenarios: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stdout, "scenarios: %s\n", digest)
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
		if err := client.Put(ctx, "farm/wrapper.sh", body); err != nil {
			fmt.Fprintf(os.Stderr, "error: push wrapper: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stdout, "wrapper: %s\n", digest)
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

// farmBatchManifest is the subset of batches/<batch>/manifest.json
// (docs/spec-eval-farm.md §1.4, FM-9) this reads: the run ids the batch
// created. FM-9 does not pin an exact field name for that list; "runs" is
// this slice's working assumption — the slice that first writes
// manifest.json (farm run, excluded from S2's write-set) is the one that
// fixes this shape for real, and this reader adjusts to match it then.
type farmBatchManifest struct {
	Runs []string `json:"runs"`
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
		var manifest farmBatchManifest
		if err := json.Unmarshal(manifestBody, &manifest); err != nil {
			fmt.Fprintf(os.Stderr, "error: parse batch manifest: %v\n", err)
			return 1
		}
		runIDs = manifest.Runs
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
  push     --candidate <file> | --evaluator <file> | --scenarios <dir> | --wrapper <file>
                                               Upload one part to the farm bucket, print its digest
  pull     <runId>|--batch <batch> --out <dir> Download a run's (or a batch's) bundle
  run      (not implemented yet)
  status   (not implemented yet)
  report   [--evaluator-sha256 <sha>] <dir>    Report over a pulled batch or run dir (no network)
  coverage (not implemented yet)
  gc       (not implemented yet)`)
}
