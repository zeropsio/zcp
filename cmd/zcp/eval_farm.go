package main

import (
	"context"
	"debug/buildinfo"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/platform"
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
	farmVerbObserve  = "observe"
	farmVerbConsole  = "console"

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
	// envr is the ZCP_FARM_* overlay every verb below reads its
	// configuration through (docs/spec-eval-farm.md §3.1 FM-17): the
	// environment first, then — only once ZCP_FARM_PROJECT_ID is set —
	// resolved from the farm/os services' env in that project. Built once
	// per invocation, not per verb, so the (at most two) service-env
	// fetches it may trigger happen at most once even though several
	// verbs below read config through it.
	envr := resolvedFarmEnv()
	switch args[0] {
	case farmVerbPush:
		return runFarmPush(args[1:], envr)
	case farmVerbPull:
		return runFarmPull(args[1:], envr)
	case farmVerbCoverage:
		return runFarmCoverage(args[1:])
	case farmVerbReport:
		return runFarmReport(args[1:])
	case farmVerbRun:
		return runFarmRun(args[1:], envr)
	case farmVerbStatus:
		return runFarmStatus(args[1:], envr)
	case farmVerbGC:
		return runFarmGC(args[1:], envr)
	case farmVerbObserve:
		return runFarmObserve(args[1:])
	case farmVerbConsole:
		return runFarmConsole(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown farm subcommand: %s\n", args[0])
		printEvalFarmUsage()
		return 1
	}
}

// resolvedFarmEnv builds the ZCP_FARM_* overlay lookup (docs/spec-eval-farm.md
// §3.1 FM-17): os.Getenv first, then — only when ZCP_FARM_PROJECT_ID is
// set — resolved once from the farm/os services' env in that project via
// an account client built from ZCP_FARM_ACCOUNT_TOKEN + ZCP_API_HOST (the
// same construction eval_farm_run.go's runFarmRun uses for its own account
// client, minus the admin/org scoping run/gc additionally need). No
// network call happens here — platform.NewZeropsClient only builds the
// client; the farm/os service-env fetches are lazy, inside
// farm.EnvResolver.Lookup, and run at most once each.
func resolvedFarmEnv() *farm.EnvResolver {
	projectID := os.Getenv("ZCP_FARM_PROJECT_ID")
	var client platform.Client
	if projectID != "" {
		// platform.NewZeropsClient never fails on its current
		// implementation (host/token normalization only, no network
		// call) — a construction error leaves client nil, and
		// EnvResolver.Lookup then answers "" with an explicit
		// "no account client available" Err() instead of touching a
		// nil client.
		if c, err := platform.NewZeropsClient(os.Getenv("ZCP_FARM_ACCOUNT_TOKEN"), os.Getenv("ZCP_API_HOST")); err == nil {
			client = c
		}
	}
	return farm.NewEnvResolver(context.Background(), client, projectID, os.Getenv)
}

// farmConfigFromResolver builds the sink config by reading through envr —
// the ZCP_FARM_* overlay resolvedFarmEnv built once at dispatch. It
// prefers envr.Err() (a specific resolution failure: an unsupported
// reference, or a farm/os service that couldn't be read) over
// ConfigFromLookup's generic "missing env var(s)" message when both are
// non-nil, since the specific failure is the more useful one to report.
func farmConfigFromResolver(envr *farm.EnvResolver) (farm.Config, error) {
	cfg, err := farm.ConfigFromLookup(envr.Lookup)
	if err != nil {
		if rErr := envr.Err(); rErr != nil {
			return farm.Config{}, rErr
		}
		return farm.Config{}, err
	}
	return cfg, nil
}

// runFarmPush uploads one or more parts to the farm bucket, under the
// layout of docs/spec-eval-farm.md §1.1, and prints the digest of each part
// it uploaded.
func runFarmPush(args []string, envr *farm.EnvResolver) int {
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

	cfg, err := farmConfigFromResolver(envr)
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

		info, err := pushCandidateInfo(ctx, client, candidate, digest)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: push candidate info: %v\n", err)
			return 1
		}
		fmt.Fprintln(os.Stdout, candidateInfoLine(info))
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
		gateSetPath, err := resolveGateSetPath(scenarios, gateSet)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: push gate set: %v\n", err)
			return 1
		}
		gateSetBody, err := os.ReadFile(gateSetPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: push gate set: read %s: %v\n", gateSetPath, err)
			return 1
		}
		digest, err := pushScenarioTree(ctx, client, scenarios, gateSetBody)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: push scenarios: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stdout, "scenarios: %s\n", digest)

		gateKey, err := pushGateSet(ctx, client, digest, gateSetBody)
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

// pushCandidateInfo reads path's embedded Go build info and, when it
// carries a VCS revision, uploads it as digest's candidate-info object
// (docs/spec-eval-farm.md §3.3) and returns the mapped
// farm.CandidateInfo. A binary with no VCS stamping, or one
// debug/buildinfo.ReadFile cannot read at all (never a Go binary), returns
// a nil CandidateInfo and a nil error: the candidate binary itself already
// pushed successfully, so a missing build-info blob is never fatal.
func pushCandidateInfo(ctx context.Context, client *farm.SinkClient, path, digest string) (*farm.CandidateInfo, error) {
	bi, err := buildinfo.ReadFile(path)
	if err != nil {
		return nil, nil //nolint:nilerr,nilnil // path is not a Go binary debug/buildinfo can parse — "without VCS info it uploads nothing" (§3.3), never a push failure
	}
	info := farm.CandidateInfoFromBuildInfo(bi)
	if info == nil {
		return nil, nil //nolint:nilnil // no vcs.revision setting — the same "uploads nothing" case, just reached via a binary buildinfo.ReadFile could parse
	}
	body, err := json.Marshal(info)
	if err != nil {
		return nil, fmt.Errorf("marshal candidate info: %w", err)
	}
	if err := client.Put(ctx, farm.CandidateInfoKey(digest), body); err != nil {
		return nil, err
	}
	return info, nil
}

// candidateInfoLine formats pushCandidateInfo's result for stdout (§3.3):
// "candidate-info: <revision[:12]>[ modified]", or "candidate-info: none
// (built without VCS stamping)" when info is nil.
func candidateInfoLine(info *farm.CandidateInfo) string {
	if info == nil {
		return "candidate-info: none (built without VCS stamping)"
	}
	rev := info.Revision
	if len(rev) > 12 {
		rev = rev[:12]
	}
	line := "candidate-info: " + rev
	if info.Modified {
		line += " modified"
	}
	return line
}

const boundGateSetFile = ".farm-gate-set.txt"

// pushScenarioTree snapshots every file under dir together with gateSet as
// one content-addressed tree. Staging first makes the bytes hashed exactly the
// bytes uploaded even if the checkout changes during the push.
func pushScenarioTree(ctx context.Context, client *farm.SinkClient, dir string, gateSet []byte) (digest string, err error) {
	staged, err := os.MkdirTemp("", "farm-scenarios-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(staged) }()

	err = filepath.WalkDir(dir, func(source string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, source)
		if err != nil {
			return fmt.Errorf("rel %s: %w", source, err)
		}
		if filepath.ToSlash(rel) == boundGateSetFile {
			return fmt.Errorf("scenario tree reserves %s for the digest-bound gate set", boundGateSetFile)
		}
		body, err := os.ReadFile(source)
		if err != nil {
			return fmt.Errorf("read %s: %w", source, err)
		}
		dest := filepath.Join(staged, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("stage %s: %w", source, err)
		}
		if err := os.WriteFile(dest, body, 0o600); err != nil {
			return fmt.Errorf("stage %s: %w", source, err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(staged, boundGateSetFile), gateSet, 0o600); err != nil {
		return "", fmt.Errorf("stage gate set: %w", err)
	}
	digest, err = farm.TreeDigest(staged)
	if err != nil {
		return "", err
	}
	err = filepath.WalkDir(staged, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(staged, path)
		if err != nil {
			return fmt.Errorf("rel %s: %w", path, err)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		return client.Put(ctx, fmt.Sprintf("scenarios/%s/%s", digest, filepath.ToSlash(rel)), body)
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

// pushGateSet writes the legacy sets/<scenariosDigest>/gate.txt compatibility
// copy. New farm runs trust only boundGateSetFile inside the verified scenario
// tree; this object remains for older binaries during rollout.
func pushGateSet(ctx context.Context, client *farm.SinkClient, scenariosDigest string, body []byte) (key string, err error) {
	key = fmt.Sprintf("sets/%s/gate.txt", scenariosDigest)
	if err := client.Put(ctx, key, body); err != nil {
		return "", err
	}
	return key, nil
}

// runFarmPull downloads runs/<runId>/** (or every run a batch's manifest
// lists) to <out>/<runId>/, and prints each run's bundle completeness
// (docs/spec-eval-farm.md §5: "bundle: complete|partial|missing").
func runFarmPull(args []string, envr *farm.EnvResolver) int {
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

	cfg, err := farmConfigFromResolver(envr)
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
//
// A bucket key is an opaque string, not a filesystem path — FM-8 means a
// compromised run's own write-capable bucket key can plant an object key
// like "runs/<id>/../../.ssh/authorized_keys", and naively joining its
// relative part onto <out> would write there on the operator's machine
// (R4a, LAND review). Every key's relative part is checked with
// safeRelPath before it is ever joined onto out; an escaping key is
// skipped (never written) and named on stderr, and the run's bundle is
// still reported as an error so `farm pull` exits nonzero rather than
// silently reporting a bundle that is missing exactly the poisoned object.
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
	escaped := 0
	for _, key := range keys {
		rel := strings.TrimPrefix(key, prefix)
		relPath, ok := safeRelPath(rel)
		if !ok {
			fmt.Fprintf(os.Stderr, "pull %s: refusing key %s: relative path escapes --out\n", runID, key)
			escaped++
			continue
		}

		body, err := client.Get(ctx, key)
		if err != nil {
			return "", fmt.Errorf("get %s: %w", key, err)
		}
		dest := filepath.Join(out, runID, relPath)
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
	if escaped > 0 {
		return "", fmt.Errorf("%d object key(s) under %s escaped --out and were refused", escaped, prefix)
	}
	if hasDone {
		return bundleComplete, nil
	}
	return bundlePartial, nil
}

// safeRelPath cleans a bucket key's relative part (rel, already stripped
// of its "runs/<runId>/" prefix) and rejects it — ok=false — when the
// cleaned path is absolute or starts with a ".." segment, i.e. it would
// resolve outside whatever directory it gets joined onto.
func safeRelPath(rel string) (cleaned string, ok bool) {
	cleaned = filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(cleaned) {
		return "", false
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", false
	}
	return cleaned, true
}

func printEvalFarmUsage() {
	fmt.Fprintln(os.Stderr, `Usage: zcp eval farm <command>

Commands (ZCP_AUTHORING=1 required):
  push     --candidate <file> | --evaluator <file> | --scenarios <dir> [--gate-set <path>] | --wrapper <file>
                                               Upload one part to the farm bucket, print its digest.
                                               --gate-set overrides the gate scenario list read alongside
                                               --scenarios (default: <scenariosDir>/../../farm/gate-set.txt)
  pull     <runId>|--batch <batch> --out <dir> Download a run's (or a batch's) bundle
  run      --candidate <sha256> --scenarios <digest> --set gate|all|<ids> [--evaluator <sha256>] [--wrapper <sha256>]
           [--batch <id>] [--run-budget 45m] [--detach] [--observer <model>|off] [--note <text, max 200 chars>]
                                               (pins default to evaluators/current, farm/wrapper/current)
                                               Create zcp-farm-<runId> projects, watch the bucket, delete after done.json
  status   [<batch>]                           Recompute batch/run state from the bucket and the project list
  report   [--evaluator-sha256 <sha>] <dir>    Report over a pulled batch or run dir (no network)
  coverage <dir> [--since <batch>]           Derive (scenario, step, decision) coverage cells from pulled bundles
  gc       [--older-than <duration>] [--yes]   Delete zcp-farm-* projects no running batch references
  observe  <run-dir> [--model <m>] [--claude <path>]   Advisory evaluation of a pulled run; writes
                                               <run-dir>/observer/<obsId>.json, prints the rendering, never touches the bucket
  console  --listen :8080                       Serve the hosted console (§8): auth, read model, agent API over the farm bucket
                                               (env ZCP_FARM_CONSOLE_TOKEN required, ZCP_FARM_S3_*, optional ZCP_FARM_OBSERVER=off)`)
}
