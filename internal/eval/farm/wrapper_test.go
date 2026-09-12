package farm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// This file drives the real eval/farm/wrapper.sh (POSIX sh) against a real
// curl over a real httptest server (the fakeS3 double from fakes3_test.go),
// entirely offline (docs/spec-eval-farm.md §2.3, FM-13). It never asserts on
// the script's internals — only on the fake S3's recorded requests, the
// bytes it stored, and the bundle's own done.json.

// wrapperDoneJSON is the FM-4 shape done.json carries.
type wrapperDoneJSON struct {
	RunID            string `json:"runId"`
	ScenarioID       string `json:"scenarioId"`
	RunnerDimensions struct {
		Execution string `json:"execution"`
		Task      string `json:"task"`
		TaskEnd   string `json:"taskEnd"`
	} `json:"runnerDimensions"`
	Parts struct {
		Results struct {
			TreeDigest string `json:"treeDigest"`
		} `json:"results"`
		Capture struct {
			TreeDigest string `json:"treeDigest"`
		} `json:"capture"`
	} `json:"parts"`
	EvaluatorSha256 string `json:"evaluatorSha256"`
	CandidateSha256 string `json:"candidateSha256"`
	CredentialMode  string `json:"credentialMode"`
}

// requireShAndCurl skips the whole offline rig when either real binary this
// file shells out to is unavailable (both are present on the Mac and on
// every zcp@1 container per the brief).
func requireShAndCurl(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not on PATH")
	}
}

// wrapperScriptPath locates eval/farm/wrapper.sh relative to this test
// file's own path, independent of the test binary's working directory.
func wrapperScriptPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0): not ok")
	}
	// internal/eval/farm/wrapper_test.go -> repo root is three levels up.
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	path := filepath.Join(root, "eval", "farm", "wrapper.sh")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("eval/farm/wrapper.sh: %v", err)
	}
	return path
}

// stubEvaluatorScript is the offline double for the pinned evaluator
// binary the wrapper downloads and execs. It records its own argv (so
// TestWrapper_PassesWorkDirUnderRunDir can assert on --work-dir and
// TestWrapper_PassesCaptureDirUnderRunDir can assert on --capture-dir; the
// real binding's exact flags are otherwise covered by the CLI-layer tests
// in cmd/zcp) and reads the run descriptor from its inherited environment.
//
// Sequence, unconditionally: drop a side-effect marker (so a test can prove
// this script never ran), record argv, touch a marker under --work-dir if
// one was passed (D3), write results/<scenario>/{meta.json,
// verification.json} and capture/manifest.json — each containing a literal
// secret value, to prove redaction — keep an unredacted copy outside
// results/capture for the test to diff against, write a fake capture
// window under a capture-<id> subdirectory of --capture-dir (the way the
// real evaluator's `--capture raw --capture-dir <dir>` now does — the
// product seam S5b landed; cmd/zcp/eval_capture.go), then print the three
// dimension lines, then end per STUB_MODE: "ok" (default) exits 0, "fail"
// prints a failed Task line and exits 1, "hang" spawns a `sleep 300`
// grandchild IN ITS OWN NEW PROCESS GROUP (mirroring the real
// sub-evaluator's observed behavior, D6 — via a one-line perl
// setpgrp(0,0)+exec, falling back to plain backgrounding if perl is
// unavailable), recording its pid to ./grandchild.pid, then sleeps 30s
// itself so a test can kill -9 the main stub process and observe the
// grandchild die too even though a pgid-only signal would miss it.
const stubEvaluatorScript = `#!/bin/sh
set -eu
: >./ran.marker
printf '%s\n' "$@" >./argv.log

work_dir=""
capture_dir=""
prev=""
for arg in "$@"; do
	if [ "$prev" = "--work-dir" ]; then
		work_dir="$arg"
	fi
	if [ "$prev" = "--capture-dir" ]; then
		capture_dir="$arg"
	fi
	prev="$arg"
done
if [ -n "$work_dir" ]; then
	mkdir -p "$work_dir"
	: >"$work_dir/workdir.marker"
fi

mkdir -p "results/$ZCP_FARM_SCENARIO" capture
cat >"results/$ZCP_FARM_SCENARIO/meta.json" <<EOF
{"scenarioId":"$ZCP_FARM_SCENARIO","secret1":"$CLAUDE_CODE_OAUTH_TOKEN","secret2":"$ZCP_FARM_S3_SECRET","secret3":"${ZCP_API_KEY:-}","secret4":"${ZCP_E2E_LAUNCH_KEY:-}"}
EOF
cat >"results/$ZCP_FARM_SCENARIO/verification.json" <<'EOF'
{"formatVersion":"zcp-eval-verification-2","mode":"required","result":"passed"}
EOF
cat >capture/manifest.json <<EOF
{"status":"complete","secretKey":"$ZCP_FARM_S3_KEY"}
EOF
mkdir -p pristine
cp "results/$ZCP_FARM_SCENARIO/meta.json" pristine/meta.json

if [ -n "$capture_dir" ]; then
	window_dir="$capture_dir/capture-1"
	mkdir -p "$window_dir"
	printf '{"line":"provider record","secretKey":"%s"}\n' "$ZCP_FARM_S3_KEY" >"$window_dir/provider.jsonl"
	provider_size=$(wc -c <"$window_dir/provider.jsonl" | tr -d ' ')
	provider_sha=$(sha256sum "$window_dir/provider.jsonl" | awk '{print $1}')
	cp "$window_dir/provider.jsonl" pristine/provider.jsonl
	cat >"$window_dir/manifest.json" <<EOF
{"formatVersion":"zcp-capture-1","sessionId":"stub-window-1","plaintext":true,"status":"complete","files":[{"kind":"provider","path":"provider.jsonl","sizeBytes":$provider_size,"sha256":"$provider_sha"}]}
EOF
fi

mode="${STUB_MODE:-ok}"
case "$mode" in
escape-session)
	setsid sh -c '
		trap "" TERM
		echo $$ >./escaped-session.pid
		while :; do
			printf x >>./escaped-session-writes
			sleep 0.02
		done
	' &
	while [ ! -s ./escaped-session.pid ] || [ ! -s ./escaped-session-writes ]; do
		sleep 0.01
	done
	echo "Execution:    ok"
	echo "Task:         required passed"
	echo "Task-end evidence: persisted, settled"
	exit 0
	;;
fail)
	echo "Execution:    ok"
	echo "Task:         required failed"
	echo "Task-end evidence: persisted, settled"
	exit 1
	;;
hang)
	if command -v perl >/dev/null 2>&1; then
		perl -e 'setpgrp(0, 0); exec { $ARGV[0] } @ARGV' sleep 300 &
	else
		sleep 300 &
	fi
	echo $! >./grandchild.pid
	echo "Execution:    ok"
	echo "Task:         required passed"
	echo "Task-end evidence: persisted, settled"
	sleep 30
	;;
*)
	echo "Execution:    ok"
	echo "Task:         required passed"
	echo "Task-end evidence: persisted, settled"
	exit 0
	;;
esac
`

const candidateStubScript = "#!/bin/sh\necho candidate stub, never executed by the wrapper\n"

// wrapperHarness wires one fakeS3 + one seeded evaluator/candidate pair for

// wrapperRunID names one harness's run. It carries the test's own name (so
// a failure points at its test) and a per-process unique suffix: one test
// writes under the real user's home (TestWrapper_NoHome_FallsBackToUserHome
// — the fallback it pins is to that home), so two test binaries running at
// once on one machine would otherwise share ~/.zcp-farm/<runId> and delete
// each other's done.json.
func wrapperRunID(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("run-%s-%d-%d", strings.ReplaceAll(t.Name(), "/", "-"), os.Getpid(), wrapperRunSeq.Add(1))
}

// wrapperRunSeq keeps two harnesses in one process apart.
var wrapperRunSeq atomic.Int64

// wrapper.sh to run against.
type wrapperHarness struct {
	server     *httptest.Server
	fake       *fakeS3
	scriptPath string

	runID           string
	scenarioID      string
	batchID         string
	scenariosDigest string
	projectID       string
	evaluatorSHA    string
	candidateSHA    string

	rundir string
}

func newWrapperHarness(t *testing.T) *wrapperHarness {
	t.Helper()

	fake := newFakeS3()
	server := httptest.NewServer(fake.handler(t))
	t.Cleanup(server.Close)

	tmp := t.TempDir()
	evaluatorPath := filepath.Join(tmp, "evaluator-src")
	candidatePath := filepath.Join(tmp, "candidate-src")
	if err := os.WriteFile(evaluatorPath, []byte(stubEvaluatorScript), 0o755); err != nil {
		t.Fatalf("write evaluator stub: %v", err)
	}
	if err := os.WriteFile(candidatePath, []byte(candidateStubScript), 0o755); err != nil {
		t.Fatalf("write candidate stub: %v", err)
	}

	evaluatorSHA, err := FileDigest(evaluatorPath)
	if err != nil {
		t.Fatalf("FileDigest(evaluator): %v", err)
	}
	candidateSHA, err := FileDigest(candidatePath)
	if err != nil {
		t.Fatalf("FileDigest(candidate): %v", err)
	}

	evaluatorBytes, err := os.ReadFile(evaluatorPath)
	if err != nil {
		t.Fatalf("read evaluator stub: %v", err)
	}
	candidateBytes, err := os.ReadFile(candidatePath)
	if err != nil {
		t.Fatalf("read candidate stub: %v", err)
	}

	h := &wrapperHarness{
		server:          server,
		fake:            fake,
		scriptPath:      wrapperScriptPath(t),
		runID:           wrapperRunID(t),
		scenarioID:      "scenario1",
		batchID:         "batch1",
		scenariosDigest: "scendigest1",
		projectID:       "projABCDEF",
		evaluatorSHA:    evaluatorSHA,
		candidateSHA:    candidateSHA,
		rundir:          t.TempDir(),
	}

	fake.mu.Lock()
	fake.objects["evaluators/"+evaluatorSHA+"/zcp"] = evaluatorBytes
	fake.objects["candidates/"+candidateSHA+"/zcp"] = candidateBytes
	fake.objects["scenarios/"+h.scenariosDigest+"/"+h.scenarioID+".md"] = []byte("# stub scenario\n")
	fake.mu.Unlock()

	return h
}

// env builds the wrapper's process environment: the real run-descriptor
// contract (docs/spec-eval-farm.md §2.2 FM-12, plus ZCP_FARM_SCENARIOS_DIGEST)
// plus overrides ("STUB_MODE", extra credentials, etc.) and the harness's
// own ZCP_FARM_RUNDIR/ZCP_FARM_WORK_DIR test seams. ZCP_FARM_WORK_DIR keeps
// this offline rig from ever touching the real /var/www a farm run's
// wrapper defaults to (D21) — every test that wants that production
// default deletes the override instead (empty-string convention below).
func (h *wrapperHarness) env(overrides map[string]string) []string {
	base := map[string]string{
		"PATH":                      os.Getenv("PATH"),
		"HOME":                      os.Getenv("HOME"),
		"ZCP_FARM_BATCH":            h.batchID,
		"ZCP_FARM_RUN":              h.runID,
		"ZCP_FARM_SCENARIO":         h.scenarioID,
		"ZCP_FARM_EVALUATOR_SHA":    h.evaluatorSHA,
		"ZCP_FARM_CANDIDATE_SHA":    h.candidateSHA,
		"ZCP_FARM_SCENARIOS_DIGEST": h.scenariosDigest,
		"ZCP_FARM_S3_URL":           h.server.URL,
		"ZCP_FARM_S3_BUCKET":        h.fake.bucket,
		"ZCP_FARM_S3_KEY":           "AKIDEXAMPLE",
		"ZCP_FARM_S3_SECRET":        "s3-secret-value-xyz",
		"CLAUDE_CODE_OAUTH_TOKEN":   "oauth-tok-default-value",
		"projectId":                 h.projectID,
		"ZCP_FARM_RUNDIR":           h.rundir,
		"ZCP_FARM_WORK_DIR":         filepath.Join(h.rundir, "work"),
	}
	for k, v := range overrides {
		if v == "" {
			delete(base, k)
			continue
		}
		base[k] = v
	}
	env := make([]string, 0, len(base))
	for k, v := range base {
		env = append(env, k+"="+v)
	}
	return env
}

// start launches the supervisor (not the child directly) with the given
// env overrides and returns the running *exec.Cmd; the caller decides
// whether to Wait(), poll pidfiles, or send signals.
func (h *wrapperHarness) start(t *testing.T, overrides map[string]string) *exec.Cmd {
	t.Helper()
	// A 15s safety-net timeout, not a test-logic dependency: every named
	// test in this file completes in well under that (its own poll budgets
	// are much shorter); this only guards against a genuinely wedged
	// supervisor holding the test binary open.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, "sh", h.scriptPath) //nolint:gosec // h.scriptPath is this repo's own eval/farm/wrapper.sh, located via runtime.Caller, never external/user input
	cmd.Env = h.env(overrides)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatalf("start wrapper: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	return cmd
}

// waitForFile polls until path exists or the deadline passes.
func waitForFile(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// waitForS3Key polls the fake bucket until key appears or the deadline
// passes.
func waitForS3Key(fake *fakeS3, key string, timeout time.Duration) ([]byte, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if v, ok := fake.get(key); ok {
			return v, true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, false
}

// readPIDFile reads an int pid from a pidfile the wrapper wrote, waiting
// for it to appear first.
func readPIDFile(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()
	if !waitForFile(path, timeout) {
		t.Fatalf("pidfile %s never appeared", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pidfile %s: %v", path, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("pidfile %s content %q: %v", path, data, err)
	}
	return pid
}

// TestWrapper_Success_UploadsPartsThenDoneLast pins the full FM-3/FM-4
// success path: started.json first, every results/** and capture/** key
// before done.json, done.json shaped and digested against the independent
// farm.TreeDigest oracle (never the script's own output), and
// runnerDimensions carrying the stub's printed values.
func TestWrapper_Success_UploadsPartsThenDoneLast(t *testing.T) {
	requireShAndCurl(t)

	h := newWrapperHarness(t)
	cmd := h.start(t, nil)

	if err := cmd.Wait(); err != nil {
		t.Fatalf("supervisor exited with error: %v", err)
	}

	doneKey := "runs/" + h.runID + "/done.json"
	doneBytes, ok := h.fake.get(doneKey)
	if !ok {
		t.Fatalf("done.json missing from bucket")
	}

	var done wrapperDoneJSON
	if err := json.Unmarshal(doneBytes, &done); err != nil {
		t.Fatalf("done.json parse: %v (body: %s)", err, doneBytes)
	}

	if done.RunID != h.runID || done.ScenarioID != h.scenarioID {
		t.Errorf("done.json runId/scenarioId = %q/%q, want %q/%q", done.RunID, done.ScenarioID, h.runID, h.scenarioID)
	}
	if done.RunnerDimensions.Execution != "ok" {
		t.Errorf("runnerDimensions.execution = %q, want %q", done.RunnerDimensions.Execution, "ok")
	}
	if done.RunnerDimensions.Task != "required passed" {
		t.Errorf("runnerDimensions.task = %q, want %q", done.RunnerDimensions.Task, "required passed")
	}
	if done.RunnerDimensions.TaskEnd != "persisted, settled" {
		t.Errorf("runnerDimensions.taskEnd = %q, want %q", done.RunnerDimensions.TaskEnd, "persisted, settled")
	}
	if done.EvaluatorSha256 != h.evaluatorSHA || done.CandidateSha256 != h.candidateSHA {
		t.Errorf("done.json digests = %q/%q, want %q/%q", done.EvaluatorSha256, done.CandidateSha256, h.evaluatorSHA, h.candidateSHA)
	}
	if done.CredentialMode != "oauth-token" {
		t.Errorf("credentialMode = %q, want %q", done.CredentialMode, "oauth-token")
	}

	// Independent oracle: recompute the tree digest locally from what the
	// wrapper actually uploaded (via the fake's stored bytes), never trust
	// the script's own claimed digest.
	assertUploadedTreeDigest(t, h.fake, "runs/"+h.runID+"/results/", done.Parts.Results.TreeDigest)
	assertUploadedTreeDigest(t, h.fake, "runs/"+h.runID+"/capture/", done.Parts.Capture.TreeDigest)

	// PUT order: started.json first; every results/**+capture/** key
	// before done.json.
	order := h.fake.puts()
	if len(order) == 0 || order[0] != "runs/"+h.runID+"/started.json" {
		t.Fatalf("first PUT = %v, want started.json first", order)
	}
	doneIdx := -1
	for i, key := range order {
		if key == doneKey {
			doneIdx = i
			break
		}
	}
	if doneIdx == -1 {
		t.Fatalf("done.json never PUT (order: %v)", order)
	}
	for i, key := range order {
		if strings.HasPrefix(key, "runs/"+h.runID+"/results/") || strings.HasPrefix(key, "runs/"+h.runID+"/capture/") {
			if i > doneIdx {
				t.Errorf("part key %q PUT after done.json (order: %v)", key, order)
			}
		}
	}
}

// assertUploadedTreeDigest recomputes the FM-4 tree digest of every
// uploaded object under prefix using the package's own TreeDigest (the
// independent oracle: it is never told what the script claimed) and
// compares it against want.
func assertUploadedTreeDigest(t *testing.T, fake *fakeS3, prefix, want string) {
	t.Helper()
	dir := t.TempDir()
	fake.mu.Lock()
	for key, data := range fake.objects {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rel := strings.TrimPrefix(key, prefix)
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			fake.mu.Unlock()
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			fake.mu.Unlock()
			t.Fatalf("WriteFile: %v", err)
		}
	}
	fake.mu.Unlock()

	got, err := TreeDigest(dir)
	if err != nil {
		t.Fatalf("TreeDigest(%s): %v", prefix, err)
	}
	if got != want {
		t.Errorf("recomputed TreeDigest for %s = %q, want (done.json claim) %q", prefix, got, want)
	}
}

// TestWrapper_ChildKilled_StillUploadsDone pins FM-13: a kill -9 of the
// child still produces a bundle, because the supervisor's trap runs the
// upload regardless of how the child ended. The stub is told to hang
// (STUB_MODE=hang) so the test can kill it deterministically instead of
// racing a natural exit.
func TestWrapper_ChildKilled_StillUploadsDone(t *testing.T) {
	requireShAndCurl(t)

	h := newWrapperHarness(t)
	cmd := h.start(t, map[string]string{"STUB_MODE": "hang"})

	childPID := readPIDFile(t, filepath.Join(h.rundir, "child.pid"), 10*time.Second)

	if err := syscall.Kill(childPID, syscall.SIGKILL); err != nil {
		t.Fatalf("kill -9 child (pid %d): %v", childPID, err)
	}

	// The supervisor's own wait() unblocks as soon as the child dies; it
	// should exit cleanly (0) after finishing the upload.
	_ = cmd.Wait()

	doneKey := "runs/" + h.runID + "/done.json"
	doneBytes, ok := waitForS3Key(h.fake, doneKey, 10*time.Second)
	if !ok {
		t.Fatalf("done.json never appeared after the child was killed")
	}

	var done wrapperDoneJSON
	if err := json.Unmarshal(doneBytes, &done); err != nil {
		t.Fatalf("done.json parse: %v (body: %s)", err, doneBytes)
	}

	execution := done.RunnerDimensions.Execution
	if !strings.Contains(execution, "signal") && !strings.Contains(execution, "killed") {
		t.Errorf("runnerDimensions.execution = %q, want it to name signal/killed", execution)
	}
}

// TestWrapper_SupervisorKilled_NoDone pins the other half of FM-13: a
// SIGKILL of the supervisor itself is the one case nothing runs after — no
// trap fires (SIGKILL cannot be trapped), so no upload happens and
// done.json never appears. The now-orphaned child is still told to hang
// (STUB_MODE=hang); the test kills it too once both pidfiles are known, so
// it does not have to wait out the full 30s sleep to reap it.
func TestWrapper_SupervisorKilled_NoDone(t *testing.T) {
	requireShAndCurl(t)

	h := newWrapperHarness(t)
	cmd := h.start(t, map[string]string{"STUB_MODE": "hang"})

	supervisorPID := readPIDFile(t, filepath.Join(h.rundir, "supervisor.pid"), 10*time.Second)
	childPID := readPIDFile(t, filepath.Join(h.rundir, "child.pid"), 10*time.Second)

	if err := syscall.Kill(supervisorPID, syscall.SIGKILL); err != nil {
		t.Fatalf("kill -9 supervisor (pid %d): %v", supervisorPID, err)
	}
	// Reap the orphaned child quickly rather than waiting out its 30s
	// sleep; FM-13's guarantee is about the supervisor's trap, not about
	// how fast an orphaned child eventually dies on its own.
	if err := syscall.Kill(childPID, syscall.SIGKILL); err != nil {
		t.Fatalf("kill -9 child (pid %d): %v", childPID, err)
	}
	_ = cmd.Wait()

	if body, ok := waitForS3Key(h.fake, "runs/"+h.runID+"/done.json", 3*time.Second); ok {
		t.Fatalf("done.json was uploaded despite the supervisor being SIGKILLed (body: %s)", body)
	}
}

// TestWrapper_DigestMismatch_RefusesToRun pins FM-13 step 3: the child
// verifies both binary digests against ZCP_FARM_EVALUATOR_SHA/
// ZCP_FARM_CANDIDATE_SHA before running either — a mismatch refuses without
// ever exec'ing the evaluator, and done.json still gets written (by the
// supervisor's ordinary trap path) naming the refusal.
func TestWrapper_DigestMismatch_RefusesToRun(t *testing.T) {
	requireShAndCurl(t)

	h := newWrapperHarness(t)
	wrongSHA := strings.Repeat("0", 64)
	cmd := h.start(t, map[string]string{"ZCP_FARM_CANDIDATE_SHA": wrongSHA})

	_ = cmd.Wait()

	doneBytes, ok := h.fake.get("runs/" + h.runID + "/done.json")
	if !ok {
		t.Fatalf("done.json missing from bucket")
	}
	var done wrapperDoneJSON
	if err := json.Unmarshal(doneBytes, &done); err != nil {
		t.Fatalf("done.json parse: %v (body: %s)", err, doneBytes)
	}
	if done.RunnerDimensions.Execution != "refused: digest mismatch" {
		t.Errorf("runnerDimensions.execution = %q, want %q", done.RunnerDimensions.Execution, "refused: digest mismatch")
	}
	// evaluatorSha256/candidateSha256 record what was PINNED (the env
	// values), never what the downloaded file actually hashed to.
	if done.CandidateSha256 != wrongSHA {
		t.Errorf("done.json candidateSha256 = %q, want the pinned (wrong) value %q", done.CandidateSha256, wrongSHA)
	}

	// Independent oracle: parts digests must equal TreeDigest of a
	// genuinely empty directory — nothing was ever written to results/ or
	// capture/ because the stub never ran.
	wantEmpty, err := TreeDigest(t.TempDir())
	if err != nil {
		t.Fatalf("TreeDigest(empty): %v", err)
	}
	if done.Parts.Results.TreeDigest != wantEmpty || done.Parts.Capture.TreeDigest != wantEmpty {
		t.Errorf("parts digests = %q/%q, want both %q (empty dirs)", done.Parts.Results.TreeDigest, done.Parts.Capture.TreeDigest, wantEmpty)
	}

	// The stub evaluator's own side-effect marker (written as its very
	// first action, before anything else) must be absent: the wrapper
	// refused before ever exec'ing it. Checked directly on the local
	// RUNDIR — the marker is written outside results/capture, so it would
	// never reach the bucket even if the stub had run.
	if _, err := os.Stat(filepath.Join(h.rundir, "ran.marker")); err == nil {
		t.Errorf("stub evaluator's side-effect marker exists — it ran despite the digest mismatch")
	} else if !os.IsNotExist(err) {
		t.Errorf("stat ran.marker: %v", err)
	}
}

// TestWrapper_Redaction_NoSecretValueInBundle pins FM-7: every credential
// value the wrapper holds — ZCP_FARM_S3_KEY/SECRET, CLAUDE_CODE_OAUTH_TOKEN,
// ZCP_API_KEY, and ZCP_E2E_LAUNCH_KEY (redact_known_secrets' full set) — is
// redacted from results/ and capture/ before upload. The stub evaluator
// writes every secret value into results/<scenario>/meta.json and
// capture/manifest.json, and keeps an unredacted copy outside
// results/capture (RUNDIR/pristine/meta.json) — so the test can prove the
// values were genuinely present pre-redaction, not just absent because the
// stub never wrote them.
func TestWrapper_Redaction_NoSecretValueInBundle(t *testing.T) {
	requireShAndCurl(t)

	h := newWrapperHarness(t)
	overrides := map[string]string{
		"ZCP_FARM_S3_KEY":         "AKIDEXAMPLE-redact-me",
		"ZCP_FARM_S3_SECRET":      "s3-secret-redact-me",
		"CLAUDE_CODE_OAUTH_TOKEN": "oauth-tok-redact-me",
		"ZCP_API_KEY":             "zcp-api-key-redact-me",
		"ZCP_E2E_LAUNCH_KEY":      "launch-key-redact-me",
	}
	cmd := h.start(t, overrides)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("supervisor exited with error: %v", err)
	}

	secrets := []string{
		overrides["ZCP_FARM_S3_KEY"], overrides["ZCP_FARM_S3_SECRET"], overrides["CLAUDE_CODE_OAUTH_TOKEN"],
		overrides["ZCP_API_KEY"], overrides["ZCP_E2E_LAUNCH_KEY"],
	}

	// Pristine copy (outside results/capture, so the wrapper never touches
	// it) must actually contain at least one secret — proving the stub
	// really did write them, so their absence below is redaction, not
	// simply their never having been written.
	pristine, err := os.ReadFile(filepath.Join(h.rundir, "pristine", "meta.json"))
	if err != nil {
		t.Fatalf("read pristine copy: %v", err)
	}
	foundAny := false
	for _, s := range secrets {
		if strings.Contains(string(pristine), s) {
			foundAny = true
		}
	}
	if !foundAny {
		t.Fatalf("pristine copy %q contains none of the secret values — test fixture is broken", pristine)
	}

	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()
	runPrefix := "runs/" + h.runID + "/"
	checked := 0
	for key, body := range h.fake.objects {
		if !strings.HasPrefix(key, runPrefix+"results/") && !strings.HasPrefix(key, runPrefix+"capture/") {
			continue
		}
		checked++
		for _, s := range secrets {
			if strings.Contains(string(body), s) {
				t.Errorf("uploaded object %q contains a secret value %q", key, s)
			}
		}
	}
	if checked == 0 {
		t.Fatalf("no results/**+capture/** objects were uploaded at all")
	}
}

// TestWrapper_Redaction_MetacharValue_Redacted pins R8 (LAND review):
// redact_dir must treat the secret value as a literal string, never a
// sed/regex pattern. A sed BRE substitution only escapes backslash, "/",
// and "&" — a value containing any OTHER metacharacter ("[", "*", ".",
// "^", "$") was interpreted as a pattern, at best matching the wrong text
// and at worst making sed error out; under `set +e` in the
// finish_and_upload trap that error was swallowed and the file uploaded
// UNREDACTED. The fix (perl's \Q...\E fixed-string quoting) must fully
// redact a value carrying every one of those characters at once.
func TestWrapper_Redaction_MetacharValue_Redacted(t *testing.T) {
	requireShAndCurl(t)

	h := newWrapperHarness(t)
	const metaVal = "tok-a[b*c.d$^-end"
	overrides := map[string]string{
		"CLAUDE_CODE_OAUTH_TOKEN": metaVal,
	}
	cmd := h.start(t, overrides)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("supervisor exited with error: %v", err)
	}

	pristine, err := os.ReadFile(filepath.Join(h.rundir, "pristine", "meta.json"))
	if err != nil {
		t.Fatalf("read pristine copy: %v", err)
	}
	if !strings.Contains(string(pristine), metaVal) {
		t.Fatalf("pristine copy %q does not contain the metachar secret value — test fixture is broken", pristine)
	}

	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()
	runPrefix := "runs/" + h.runID + "/"
	sawRedactedMarker := false
	checked := 0
	for key, body := range h.fake.objects {
		if !strings.HasPrefix(key, runPrefix+"results/") && !strings.HasPrefix(key, runPrefix+"capture/") {
			continue
		}
		checked++
		if strings.Contains(string(body), metaVal) {
			t.Errorf("uploaded object %q still contains the metachar secret value %q (0 hits required)", key, metaVal)
		}
		if strings.Contains(string(body), "<redacted>") {
			sawRedactedMarker = true
		}
	}
	if checked == 0 {
		t.Fatalf("no results/**+capture/** objects were uploaded at all")
	}
	if !sawRedactedMarker {
		t.Errorf("no uploaded object under results/**+capture/** carries the \"<redacted>\" marker")
	}
}

// TestWrapper_RedactionFailure_BlocksEvidenceAndCompletion pins FM-7's
// fail-closed boundary. If a credential-bearing result cannot be rewritten,
// the wrapper may retain the local evidence and its non-secret started marker,
// but it must publish none of results/, capture/, or done.json and must not
// claim the attempt was uploaded.
func TestWrapper_RedactionFailure_BlocksEvidenceAndCompletion(t *testing.T) {
	requireShAndCurl(t)

	realPerl, err := exec.LookPath("perl")
	if err != nil {
		t.Skip("perl not on PATH")
	}
	h := newWrapperHarness(t)

	// On macOS the wrapper also uses perl to start the evaluator in a new
	// session. Delegate that invocation to the real binary and fail only the
	// in-place redaction invocation, so the fixture definitely writes a secret
	// before sanitization fails. Linux normally takes the setsid(1) branch and
	// reaches only the failing -pi invocation here.
	binDir := t.TempDir()
	perlStub := filepath.Join(binDir, "perl")
	stub := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"*'use POSIX qw(setsid)'*) exec " + shQuote(realPerl) + " \"$@\" ;;\n" +
		"*) exit 97 ;;\n" +
		"esac\n"
	if err := os.WriteFile(perlStub, []byte(stub), 0o755); err != nil {
		t.Fatalf("write failing perl stub: %v", err)
	}

	const secret = "oauth-redaction-must-fail-closed"
	cmd := h.start(t, map[string]string{
		"PATH":                    binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"CLAUDE_CODE_OAUTH_TOKEN": secret,
	})
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wrapper should retain local recovery evidence after redaction failure: %v", err)
	}

	localResult, err := os.ReadFile(filepath.Join(h.rundir, "results", h.scenarioID, "meta.json"))
	if err != nil {
		t.Fatalf("read retained local result: %v", err)
	}
	if !strings.Contains(string(localResult), secret) {
		t.Fatalf("fixture did not retain the secret after forced rewrite failure: %q", localResult)
	}

	startedKey := "runs/" + h.runID + "/started.json"
	if _, ok := h.fake.get(startedKey); !ok {
		t.Fatal("non-secret started.json was not published")
	}
	runPrefix := "runs/" + h.runID + "/"
	for _, key := range h.fake.puts() {
		if key == startedKey {
			continue
		}
		if strings.HasPrefix(key, runPrefix+"results/") ||
			strings.HasPrefix(key, runPrefix+"capture/") ||
			key == runPrefix+"done.json" {
			t.Errorf("published %q after redaction failed", key)
		}
	}
	if _, err := os.Stat(filepath.Join(h.rundir, ".uploaded")); err == nil {
		t.Fatal(".uploaded exists after redaction failed")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat .uploaded: %v", err)
	}
}

// TestWrapper_ApiKeyPresent_Refused pins the OAuth-only credential gate
// (owner decision 2026-09-10, docs/spec-eval-farm.md §2.3 step 2 + §2.4): an
// ANTHROPIC_API_KEY in the environment refuses before the child is even
// forked — an API key can shadow the farm's OAuth profile.
func TestWrapper_ApiKeyPresent_Refused(t *testing.T) {
	requireShAndCurl(t)

	h := newWrapperHarness(t)
	cmd := h.start(t, map[string]string{"ANTHROPIC_API_KEY": "sk-ant-should-refuse"})
	_ = cmd.Wait()

	doneBytes, ok := h.fake.get("runs/" + h.runID + "/done.json")
	if !ok {
		t.Fatalf("done.json missing from bucket")
	}
	var done wrapperDoneJSON
	if err := json.Unmarshal(doneBytes, &done); err != nil {
		t.Fatalf("done.json parse: %v (body: %s)", err, doneBytes)
	}
	if !strings.Contains(done.RunnerDimensions.Execution, "refused") {
		t.Errorf("runnerDimensions.execution = %q, want it to contain %q", done.RunnerDimensions.Execution, "refused")
	}
	if !strings.Contains(done.RunnerDimensions.Execution, "ANTHROPIC_API_KEY") {
		t.Errorf("runnerDimensions.execution = %q, want it to name ANTHROPIC_API_KEY", done.RunnerDimensions.Execution)
	}

	// The child must never have been forked at all: no started.json PUT.
	if _, ok := h.fake.get("runs/" + h.runID + "/started.json"); ok {
		t.Errorf("started.json was uploaded — the child ran despite ANTHROPIC_API_KEY being present")
	}
}

// TestWrapper_NoOAuthToken_Refused pins the other half of the OAuth-only
// gate: an empty/absent CLAUDE_CODE_OAUTH_TOKEN refuses too — there is no
// API-key fallback.
func TestWrapper_NoOAuthToken_Refused(t *testing.T) {
	requireShAndCurl(t)

	h := newWrapperHarness(t)
	cmd := h.start(t, map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": ""})
	_ = cmd.Wait()

	doneBytes, ok := h.fake.get("runs/" + h.runID + "/done.json")
	if !ok {
		t.Fatalf("done.json missing from bucket")
	}
	var done wrapperDoneJSON
	if err := json.Unmarshal(doneBytes, &done); err != nil {
		t.Fatalf("done.json parse: %v (body: %s)", err, doneBytes)
	}
	if !strings.Contains(done.RunnerDimensions.Execution, "refused") {
		t.Errorf("runnerDimensions.execution = %q, want it to contain %q", done.RunnerDimensions.Execution, "refused")
	}
	if !strings.Contains(done.RunnerDimensions.Execution, "CLAUDE_CODE_OAUTH_TOKEN") {
		t.Errorf("runnerDimensions.execution = %q, want it to name CLAUDE_CODE_OAUTH_TOKEN", done.RunnerDimensions.Execution)
	}

	if _, ok := h.fake.get("runs/" + h.runID + "/started.json"); ok {
		t.Errorf("started.json was uploaded — the child ran despite no CLAUDE_CODE_OAUTH_TOKEN")
	}
}

// TestWrapper_PassesWorkDirUnderRunDir exercises the wrapper's
// ZCP_FARM_WORK_DIR test seam (mirroring ZCP_FARM_RUNDIR): this harness
// always sets it to $RUNDIR/work so the offline rig never touches a real
// /var/www, and asserts the wrapper forwards that value verbatim as
// --work-dir and that the directory exists. In production the env var is
// unset and the wrapper defaults to /var/www (D21,
// TestWrapper_WorkDirDefaultsToVarWww_WhenEnvUnset) — the agent's cwd in a
// real container. Before D21 the default itself was hardcoded to
// $RUNDIR/work because the evaluator's old private-bin derivation
// (filepath.Dir(workDir)/candidate-bin = /var/candidate-bin when
// workDir=/var/www) failed on every live tracer run: uid zerops cannot
// create /var/candidate-bin (D3). cmd/zcp/eval_behavioral.go now derives
// that path from the results dir instead, so /var/www is safe to use.
func TestWrapper_PassesWorkDirUnderRunDir(t *testing.T) {
	requireShAndCurl(t)

	h := newWrapperHarness(t)
	cmd := h.start(t, nil)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("supervisor exited with error: %v", err)
	}

	argvBytes, err := os.ReadFile(filepath.Join(h.rundir, "argv.log"))
	if err != nil {
		t.Fatalf("read argv.log: %v", err)
	}
	argv := strings.Split(strings.TrimRight(string(argvBytes), "\n"), "\n")

	wantWorkDir := filepath.Join(h.rundir, "work")
	gotWorkDir := ""
	for i, a := range argv {
		if a == "--work-dir" && i+1 < len(argv) {
			gotWorkDir = argv[i+1]
			break
		}
	}
	if gotWorkDir != wantWorkDir {
		t.Errorf("--work-dir = %q, want %q (argv: %v)", gotWorkDir, wantWorkDir, argv)
	}
	if info, err := os.Stat(wantWorkDir); err != nil || !info.IsDir() {
		t.Errorf("work dir %q does not exist as a directory: %v", wantWorkDir, err)
	}
	if _, err := os.Stat(filepath.Join(wantWorkDir, "workdir.marker")); err != nil {
		t.Errorf("stub's workdir.marker missing under %q: %v", wantWorkDir, err)
	}
}

// TestWrapper_WorkDirDefaultsToVarWww_WhenEnvUnset pins D21
// (docs/spec-eval-farm.md §6's removed gap): with ZCP_FARM_WORK_DIR unset —
// the real farm run's condition — the wrapper's work_dir default is
// literally "/var/www", matching internal/ops/mount.go's mountBase and
// internal/eval/runner.go's own WorkDir default, so the agent's `claude`
// and the MCP `zcp serve` child it spawns share the same cwd a real
// container uses. It extracts the actual assignment line out of the real
// script — rather than restating it — so an edit to the default breaks
// this test, not just the intent, and evaluates only that one line (never
// the mkdir/exec that follows it in the real script) so this never touches
// a real /var/www on the machine running go test.
func TestWrapper_WorkDirDefaultsToVarWww_WhenEnvUnset(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}

	src, err := os.ReadFile(wrapperScriptPath(t))
	if err != nil {
		t.Fatalf("read wrapper.sh: %v", err)
	}
	re := regexp.MustCompile(`(?m)^\s*work_dir="\$\{ZCP_FARM_WORK_DIR:-[^}]*\}"\s*$`)
	line := re.FindString(string(src))
	if line == "" {
		t.Fatalf(`wrapper.sh: no work_dir="${ZCP_FARM_WORK_DIR:-...}" default-assignment line found`)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "sh", "-c", "unset ZCP_FARM_WORK_DIR; "+line+"; printf '%s' \"$work_dir\"").Output() //nolint:gosec // line is extracted from this repo's own eval/farm/wrapper.sh via the regexp above, never external/user input
	if err != nil {
		t.Fatalf("evaluate extracted line %q: %v", line, err)
	}
	if string(out) != "/var/www" {
		t.Errorf("default work_dir = %q, want %q", out, "/var/www")
	}
}

// TestWrapper_PassesCaptureDirUnderRunDir pins D5's replacement seam: the
// wrapper execs the evaluator with --capture-dir $RUNDIR/capture, forwarded
// by the evaluator itself as `zcp capture raw --output-dir <dir>`
// (cmd/zcp/eval_capture.go, S5b) — replacing the old "records:" grep
// recovery this brief removes from eval/farm/wrapper.sh.
func TestWrapper_PassesCaptureDirUnderRunDir(t *testing.T) {
	requireShAndCurl(t)

	h := newWrapperHarness(t)
	cmd := h.start(t, nil)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("supervisor exited with error: %v", err)
	}

	argvBytes, err := os.ReadFile(filepath.Join(h.rundir, "argv.log"))
	if err != nil {
		t.Fatalf("read argv.log: %v", err)
	}
	argv := strings.Split(strings.TrimRight(string(argvBytes), "\n"), "\n")

	wantCaptureDir := filepath.Join(h.rundir, "capture")
	gotCaptureDir := ""
	for i, a := range argv {
		if a == "--capture-dir" && i+1 < len(argv) {
			gotCaptureDir = argv[i+1]
			break
		}
	}
	if gotCaptureDir != wantCaptureDir {
		t.Errorf("--capture-dir = %q, want %q (argv: %v)", gotCaptureDir, wantCaptureDir, argv)
	}
}

// TestWrapper_CapturePart_NonEmptyWhenWindowWritten pins D5: the evaluator's
// capture window, written directly under the --capture-dir the wrapper
// passes ($RUNDIR/capture), ends up uploaded as a non-empty "capture" part —
// no separate recovery step needed, eval/farm/wrapper.sh.
func TestWrapper_CapturePart_NonEmptyWhenWindowWritten(t *testing.T) {
	requireShAndCurl(t)

	h := newWrapperHarness(t)
	cmd := h.start(t, nil)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("supervisor exited with error: %v", err)
	}

	capturePrefix := "runs/" + h.runID + "/capture/"
	h.fake.mu.Lock()
	var captureKeys []string
	for key := range h.fake.objects {
		if strings.HasPrefix(key, capturePrefix) {
			captureKeys = append(captureKeys, key)
		}
	}
	h.fake.mu.Unlock()
	if len(captureKeys) == 0 {
		t.Fatalf("no capture/** objects uploaded; want the stub's fake window (manifest.json/provider.jsonl) recovered and uploaded")
	}
	foundProviderJSONL := false
	for _, k := range captureKeys {
		if strings.HasSuffix(k, "provider.jsonl") {
			foundProviderJSONL = true
		}
	}
	if !foundProviderJSONL {
		t.Errorf("capture/** keys = %v, want one ending in provider.jsonl (the recovered window)", captureKeys)
	}
}

// TestWrapper_ChildKilled_NewProcessGroupGrandchildAlsoDead pins D6: kill -9
// of the wrapper child must not leave a grandchild process running, even
// when that grandchild put itself in a NEW process group (as the real
// sub-evaluator was observed to do live: `pid ppid pgid sid` showed the
// child's own pgid unchanged for itself but a fresh one for the
// sub-evaluator and further descendants, all sharing the child's session
// id). The supervisor's trap must therefore signal by SESSION, not process
// group, before redaction+upload — a bundle is final only once nothing is
// still running (docs/spec-eval-farm.md §2.3 FM-13). STUB_MODE=hang spawns
// a `sleep 300` grandchild in its own pgid (via perl setpgrp) and records
// its pid to ./grandchild.pid before itself sleeping.
func TestWrapper_ChildKilled_NewProcessGroupGrandchildAlsoDead(t *testing.T) {
	requireShAndCurl(t)

	h := newWrapperHarness(t)
	cmd := h.start(t, map[string]string{"STUB_MODE": "hang"})

	childPID := readPIDFile(t, filepath.Join(h.rundir, "child.pid"), 10*time.Second)
	if !waitForFile(filepath.Join(h.rundir, "grandchild.pid"), 10*time.Second) {
		t.Fatalf("grandchild.pid never appeared")
	}
	grandchildPID := readPIDFile(t, filepath.Join(h.rundir, "grandchild.pid"), time.Second)

	if err := syscall.Kill(grandchildPID, syscall.Signal(0)); err != nil {
		t.Fatalf("grandchild (pid %d) not alive before the test even started killing: %v", grandchildPID, err)
	}

	if err := syscall.Kill(childPID, syscall.SIGKILL); err != nil {
		t.Fatalf("kill -9 child (pid %d): %v", childPID, err)
	}
	_ = cmd.Wait()

	doneKey := "runs/" + h.runID + "/done.json"
	if _, ok := waitForS3Key(h.fake, doneKey, 10*time.Second); !ok {
		t.Fatalf("done.json never appeared after the child was killed")
	}

	// By the time done.json is uploaded, the supervisor's trap has already
	// run kill_child_group — the grandchild must be dead, not merely
	// orphaned-but-still-running.
	if err := syscall.Kill(grandchildPID, syscall.Signal(0)); err == nil {
		t.Errorf("grandchild (pid %d) is still alive after done.json was uploaded", grandchildPID)
	} else if !errors.Is(err, syscall.ESRCH) {
		t.Errorf("kill -0 grandchild (pid %d): unexpected error %v", grandchildPID, err)
	}
}

// TestWrapper_NormalExit_NewSessionDescendantCannotWriteAfterDone pins the
// stronger FM-13 finality boundary: a descendant that calls setsid(2) leaves
// both the evaluator's process group and its session. Publishing done.json is
// therefore allowed only after the wrapper has terminated that descendant and
// proved it can no longer mutate the evidence tree.
func TestWrapper_NormalExit_NewSessionDescendantCannotWriteAfterDone(t *testing.T) {
	requireShAndCurl(t)
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only: production cleanup proof uses Linux subreaper semantics")
	}
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("setsid not on PATH")
	}

	h := newWrapperHarness(t)
	cmd := h.start(t, map[string]string{"STUB_MODE": "escape-session"})
	escapedPID := readPIDFile(t, filepath.Join(h.rundir, "escaped-session.pid"), 10*time.Second)
	t.Cleanup(func() {
		// The RED implementation leaves a real session leader behind. Kill its
		// exact process group so a failing regression test never leaks it.
		_ = syscall.Kill(-escapedPID, syscall.SIGKILL)
	})
	if err := cmd.Wait(); err != nil {
		t.Fatalf("supervisor exited with error: %v", err)
	}

	if _, ok := waitForS3Key(h.fake, "runs/"+h.runID+"/done.json", 10*time.Second); !ok {
		t.Fatal("done.json never appeared")
	}
	writesPath := filepath.Join(h.rundir, "escaped-session-writes")
	before, err := os.Stat(writesPath)
	if err != nil {
		t.Fatalf("stat escaped-session-writes before stability check: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	after, err := os.Stat(writesPath)
	if err != nil {
		t.Fatalf("stat escaped-session-writes after stability check: %v", err)
	}

	if err := syscall.Kill(escapedPID, syscall.Signal(0)); err == nil {
		t.Errorf("setsid descendant (pid %d) is still alive after done.json was uploaded", escapedPID)
	} else if !errors.Is(err, syscall.ESRCH) {
		t.Errorf("kill -0 setsid descendant (pid %d): unexpected error %v", escapedPID, err)
	}
	if after.Size() != before.Size() {
		t.Errorf("setsid descendant mutated evidence after done.json: size grew from %d to %d", before.Size(), after.Size())
	}
}

// TestWrapper_RunDir_FixedRootUnderHome pins the RUNDIR fix: with
// ZCP_FARM_RUNDIR unset, the wrapper uses the fixed, discoverable root
// $HOME/.zcp-farm/<runId>/ instead of an untraceable `mktemp -d` path.
func TestWrapper_RunDir_FixedRootUnderHome(t *testing.T) {
	requireShAndCurl(t)

	home := t.TempDir()
	h := newWrapperHarness(t)
	overrides := map[string]string{
		"HOME":            home,
		"ZCP_FARM_RUNDIR": "",
	}
	wantRunDir := filepath.Join(home, ".zcp-farm", h.runID)
	t.Cleanup(func() { _ = os.RemoveAll(wantRunDir) })

	cmd := h.start(t, overrides)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("supervisor exited with error: %v", err)
	}

	if info, err := os.Stat(filepath.Join(wantRunDir, "supervisor.pid")); err != nil || info.IsDir() {
		t.Fatalf("supervisor.pid missing under fixed root %q: %v", wantRunDir, err)
	}
	if _, err := os.Stat(filepath.Join(wantRunDir, "done.json")); err != nil {
		t.Errorf("done.json missing under fixed root %q: %v", wantRunDir, err)
	}
}

// TestWrapper_NoHome_FallsBackToUserHome pins D7: `zcp@1` init commands run
// with NO $HOME in the environment at all — under `set -u`, the wrapper
// used to die immediately with "HOME: parameter not set" while computing
// its fixed RUNDIR, before started.json (or even execution-override) could
// ever be written. The wrapper must fall back to the current user's real
// home directory (never a hardcoded "/home/$(id -un)", which only holds on
// the run container's own image) so it still reaches a real bundle. The
// independent oracle here is os/user.Current().HomeDir — never anything
// read back from the script's own behavior.
func TestWrapper_NoHome_FallsBackToUserHome(t *testing.T) {
	requireShAndCurl(t)

	me, err := user.Current()
	if err != nil {
		t.Skipf("user.Current(): %v", err)
	}
	if me.HomeDir == "" {
		t.Skip("current user has no resolvable home directory")
	}

	h := newWrapperHarness(t)
	overrides := map[string]string{
		"HOME":            "",
		"ZCP_FARM_RUNDIR": "",
	}
	wantRunDir := filepath.Join(me.HomeDir, ".zcp-farm", h.runID)
	t.Cleanup(func() { _ = os.RemoveAll(wantRunDir) })

	cmd := h.start(t, overrides)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("supervisor exited with error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(wantRunDir, "done.json")); err != nil {
		t.Fatalf("done.json missing under the user-home fallback root %q: %v", wantRunDir, err)
	}

	doneBytes, ok := h.fake.get("runs/" + h.runID + "/done.json")
	if !ok {
		t.Fatalf("done.json missing from bucket")
	}
	var done wrapperDoneJSON
	if err := json.Unmarshal(doneBytes, &done); err != nil {
		t.Fatalf("done.json parse: %v (body: %s)", err, doneBytes)
	}
	if done.RunnerDimensions.Execution != "ok" {
		t.Errorf("runnerDimensions.execution = %q, want %q (the run must actually complete, not merely avoid crashing)", done.RunnerDimensions.Execution, "ok")
	}
}

// wrapperDoneJSONWithRedacted extends wrapperDoneJSON with the D8
// "redacted" field done.json now carries.
type wrapperDoneJSONWithRedacted struct {
	wrapperDoneJSON
	Redacted []string `json:"redacted"`
}

// captureManifestFile mirrors internal/capture.ManifestFile's JSON shape —
// this test never imports internal/capture (out of this slice's write-set),
// it only decodes the same wire shape.
type captureManifestFile struct {
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
}

type captureManifestDoc struct {
	Files []captureManifestFile `json:"files"`
}

// TestWrapper_Redaction_UpdatesCaptureManifestAndListsRedacted pins D8: the
// FM-7 redaction pass rewrites capture/capture-1/provider.jsonl (it carries
// the stub's $ZCP_FARM_S3_KEY value, and capture-1/ is the capture WINDOW
// subdirectory the real evaluator writes under --capture-dir), which
// changes its size and sha256 out from under that window's own
// manifest.json (capture/capture-1/manifest.json) recorded entry for that
// file — exactly the "manifest size mismatch" failure observed live. The
// wrapper must patch that entry to match the post-redaction bytes and name
// the changed path in done.json's "redacted" array.
func TestWrapper_Redaction_UpdatesCaptureManifestAndListsRedacted(t *testing.T) {
	requireShAndCurl(t)

	h := newWrapperHarness(t)
	overrides := map[string]string{
		"ZCP_FARM_S3_KEY": "AKID-redact-manifest-me",
	}
	cmd := h.start(t, overrides)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("supervisor exited with error: %v", err)
	}

	// Pristine copy (outside results/capture) proves the secret was really
	// written into provider.jsonl pre-redaction.
	pristine, err := os.ReadFile(filepath.Join(h.rundir, "pristine", "provider.jsonl"))
	if err != nil {
		t.Fatalf("read pristine copy: %v", err)
	}
	if !strings.Contains(string(pristine), overrides["ZCP_FARM_S3_KEY"]) {
		t.Fatalf("pristine copy %q does not contain the secret value — test fixture is broken", pristine)
	}

	manifestBytes, ok := h.fake.get("runs/" + h.runID + "/capture/capture-1/manifest.json")
	if !ok {
		t.Fatalf("capture/capture-1/manifest.json missing from bucket")
	}
	providerBytes, ok := h.fake.get("runs/" + h.runID + "/capture/capture-1/provider.jsonl")
	if !ok {
		t.Fatalf("capture/capture-1/provider.jsonl missing from bucket")
	}
	if strings.Contains(string(providerBytes), overrides["ZCP_FARM_S3_KEY"]) {
		t.Fatalf("uploaded provider.jsonl still contains the secret value")
	}

	// Independent oracle: recompute size/sha256 of the actually-uploaded
	// (post-redaction) provider.jsonl and compare against what the
	// manifest claims for it.
	wantSize := int64(len(providerBytes))
	sum := sha256.Sum256(providerBytes)
	wantSHA := hex.EncodeToString(sum[:])

	var manifest captureManifestDoc
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("capture/capture-1/manifest.json parse: %v (body: %s)", err, manifestBytes)
	}
	var providerEntry *captureManifestFile
	for i := range manifest.Files {
		if manifest.Files[i].Path == "provider.jsonl" {
			providerEntry = &manifest.Files[i]
		}
	}
	if providerEntry == nil {
		t.Fatalf("manifest.files has no entry for provider.jsonl: %+v", manifest.Files)
	}
	if providerEntry.SizeBytes != wantSize {
		t.Errorf("manifest provider.jsonl sizeBytes = %d, want %d (the actual uploaded size)", providerEntry.SizeBytes, wantSize)
	}
	if providerEntry.SHA256 != wantSHA {
		t.Errorf("manifest provider.jsonl sha256 = %q, want %q (the actual uploaded digest)", providerEntry.SHA256, wantSHA)
	}

	doneBytes, ok := h.fake.get("runs/" + h.runID + "/done.json")
	if !ok {
		t.Fatalf("done.json missing from bucket")
	}
	var done wrapperDoneJSONWithRedacted
	if err := json.Unmarshal(doneBytes, &done); err != nil {
		t.Fatalf("done.json parse: %v (body: %s)", err, doneBytes)
	}
	foundProvider := false
	for _, r := range done.Redacted {
		if r == "capture/capture-1/provider.jsonl" {
			foundProvider = true
		}
	}
	if !foundProvider {
		t.Errorf("done.json redacted = %v, want it to list %q", done.Redacted, "capture/capture-1/provider.jsonl")
	}

	// Independent oracle: the part digest done.json claims for "capture"
	// must equal TreeDigest recomputed from what was actually uploaded
	// (post-redaction, post-manifest-patch) — proving the manifest fix
	// does not itself break FM-4/FM-5's own digest promise.
	var withParts wrapperDoneJSON
	if err := json.Unmarshal(doneBytes, &withParts); err != nil {
		t.Fatalf("done.json parse (parts): %v", err)
	}
	assertUploadedTreeDigest(t, h.fake, "runs/"+h.runID+"/capture/", withParts.Parts.Capture.TreeDigest)
}

// TestWrapper_Redaction_ManifestRewrite_FlatLayoutStillWorks guards the D8
// fallback kept "for safety" alongside the capture-<id>/ window layout:
// eval/farm/wrapper.sh's update_capture_manifest must still patch a legacy
// flat $CAPTURE_DIR/manifest.json (files[].path relative to $CAPTURE_DIR
// itself, no window subdirectory) after a redaction. Exercises
// update_capture_manifest directly — every function definition in
// wrapper.sh up to its trailing `main "$@"` — rather than running the
// whole supervisor, so this stays a fast unit test.
func TestWrapper_Redaction_ManifestRewrite_FlatLayoutStillWorks(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("perl not on PATH")
	}

	src, err := os.ReadFile(wrapperScriptPath(t))
	if err != nil {
		t.Fatalf("read wrapper.sh: %v", err)
	}
	const trailer = "main \"$@\"\n"
	body := strings.TrimSuffix(string(src), trailer)
	if body == string(src) {
		t.Fatalf("wrapper.sh does not end with %q — test fixture assumption broke", trailer)
	}

	rundir := t.TempDir()
	captureDir := filepath.Join(rundir, "capture")
	if err := os.MkdirAll(captureDir, 0o755); err != nil {
		t.Fatalf("mkdir capture: %v", err)
	}

	providerPath := filepath.Join(captureDir, "provider.jsonl")
	postRedaction := []byte(`{"line":"provider record"}` + "\n")
	if err := os.WriteFile(providerPath, postRedaction, 0o644); err != nil {
		t.Fatalf("write provider.jsonl: %v", err)
	}

	manifestPath := filepath.Join(captureDir, "manifest.json")
	manifestJSON := `{"formatVersion":"zcp-capture-1","sessionId":"flat-1","plaintext":true,"status":"complete","files":[{"kind":"provider","path":"provider.jsonl","sizeBytes":999,"sha256":"` + strings.Repeat("0", 64) + `"}]}`
	if err := os.WriteFile(manifestPath, []byte(manifestJSON), 0o644); err != nil {
		t.Fatalf("write manifest.json: %v", err)
	}

	if err := os.WriteFile(filepath.Join(rundir, "redacted.log"), []byte(providerPath+"\n"), 0o644); err != nil {
		t.Fatalf("write redacted.log: %v", err)
	}

	driver := body + "\n" +
		"RUNDIR=" + shQuote(rundir) + "\n" +
		"CAPTURE_DIR=" + shQuote(captureDir) + "\n" +
		"update_capture_manifest\n"

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "sh", "-c", driver).CombinedOutput()
	if err != nil {
		t.Fatalf("update_capture_manifest: %v\n%s", err, out)
	}

	// Independent oracle: recompute size/sha256 of the post-redaction bytes
	// on disk and compare against what the rewritten manifest claims.
	wantSize := int64(len(postRedaction))
	sum := sha256.Sum256(postRedaction)
	wantSHA := hex.EncodeToString(sum[:])

	rewritten, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read rewritten manifest.json: %v", err)
	}
	var manifest captureManifestDoc
	if err := json.Unmarshal(rewritten, &manifest); err != nil {
		t.Fatalf("manifest.json parse: %v (body: %s)", err, rewritten)
	}
	var providerEntry *captureManifestFile
	for i := range manifest.Files {
		if manifest.Files[i].Path == "provider.jsonl" {
			providerEntry = &manifest.Files[i]
		}
	}
	if providerEntry == nil {
		t.Fatalf("manifest.files has no entry for provider.jsonl: %+v", manifest.Files)
	}
	if providerEntry.SizeBytes != wantSize {
		t.Errorf("manifest provider.jsonl sizeBytes = %d, want %d (the actual post-redaction size)", providerEntry.SizeBytes, wantSize)
	}
	if providerEntry.SHA256 != wantSHA {
		t.Errorf("manifest provider.jsonl sha256 = %q, want %q (the actual post-redaction digest)", providerEntry.SHA256, wantSHA)
	}
}

// shQuote wraps s in single quotes for embedding as a literal sh word,
// escaping any single quote it contains (POSIX sh has no other portable
// quoting mechanism for arbitrary bytes).
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// TestWrapper_NoBashisms pins the brief's "#!/bin/sh, no bashisms"
// requirement: `sh -n` must accept the script as valid POSIX sh syntax, and
// when shellcheck is on PATH, `shellcheck -s sh` must report zero findings
// (any finding — including an "info"-level one — fails this test; the
// script's own trap-reachability/env-naming false positives are silenced
// in-file via `# shellcheck disable=...` directives, not by loosening this
// check).
func TestWrapper_NoBashisms(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	path := wrapperScriptPath(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "sh", "-n", path).CombinedOutput()
	if err != nil {
		t.Fatalf("sh -n %s: %v\n%s", path, err, out)
	}

	shellcheckPath, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Logf("shellcheck not on PATH; skipping the shellcheck half of this check (sh -n passed)")
		return
	}
	out, err = exec.CommandContext(ctx, shellcheckPath, "-s", "sh", path).CombinedOutput()
	if err != nil {
		t.Fatalf("shellcheck -s sh %s: %v\n%s", path, err, out)
	}
}

// TestWrapper_RepeatedStart_DoesNotLaunchEvaluator proves the run-directory
// claim is durable: a second invocation refuses before it can touch the
// winner's evidence or start a second paid evaluator.
func TestWrapper_RepeatedStart_DoesNotLaunchEvaluator(t *testing.T) {
	requireShAndCurl(t)
	h := newWrapperHarness(t)
	first := h.start(t, nil)
	if err := first.Wait(); err != nil {
		t.Fatalf("first wrapper: %v", err)
	}
	putsBefore := len(h.fake.puts())
	second := h.start(t, nil)
	if err := second.Wait(); err == nil {
		t.Fatal("repeated wrapper start succeeded; want refusal")
	}
	if got := len(h.fake.puts()); got != putsBefore {
		t.Errorf("repeated start added %d sink PUTs; winner had %d before and %d after", got-putsBefore, putsBefore, got)
	}
}

// TestWrapper_ConcurrentStart_OneChild exercises mkdir's atomic claim with
// two supervisors racing on one empty run directory.
func TestWrapper_ConcurrentStart_OneChild(t *testing.T) {
	requireShAndCurl(t)
	h := newWrapperHarness(t)
	first := h.start(t, nil)
	second := h.start(t, nil)
	err1 := first.Wait()
	err2 := second.Wait()
	if (err1 == nil) == (err2 == nil) {
		t.Fatalf("concurrent starts had errors (%v, %v); want exactly one winner", err1, err2)
	}
	puts := h.fake.puts()
	started := 0
	for _, key := range puts {
		if key == "runs/"+h.runID+"/started.json" {
			started++
		}
	}
	if started != 1 {
		t.Errorf("started.json PUT count = %d, want exactly one", started)
	}
}

// TestWrapper_ExistingInterruptedAttempt_PreservesEvidence ensures a stale
// claim is a refusal, including when the prior attempt left only a pid and
// partial evidence behind.
func TestWrapper_ExistingInterruptedAttempt_PreservesEvidence(t *testing.T) {
	requireShAndCurl(t)
	h := newWrapperHarness(t)
	prior := []byte("prior evidence")
	if err := os.WriteFile(filepath.Join(h.rundir, "supervisor.pid"), []byte("99999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.rundir, "partial.log"), prior, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := h.start(t, nil)
	if err := cmd.Wait(); err == nil {
		t.Fatal("existing interrupted attempt was launched")
	}
	got, err := os.ReadFile(filepath.Join(h.rundir, "partial.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(prior) {
		t.Errorf("prior evidence changed to %q", got)
	}
	if _, ok := h.fake.get("runs/" + h.runID + "/started.json"); ok {
		t.Fatal("refused attempt uploaded started.json")
	}
}

// TestWrapper_UploadFailure_DoesNotMarkUploaded makes the final done upload
// fail and checks that the local success marker is absent. A later invocation
// must still refuse on the durable attempt claim, preserving retry/debug
// evidence for the operator rather than pretending the bundle was uploaded.
func TestWrapper_UploadFailure_DoesNotMarkUploaded(t *testing.T) {
	requireShAndCurl(t)
	h := newWrapperHarness(t)
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/runs/"+h.runID+"/done.json") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		proxy, err := http.NewRequestWithContext(r.Context(), r.Method, h.server.URL+r.URL.RequestURI(), bytes.NewReader(body))
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		proxy.Header = r.Header.Clone()
		resp, err := http.DefaultClient.Do(proxy)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	t.Cleanup(failing.Close)
	cmd := h.start(t, map[string]string{"ZCP_FARM_S3_URL": failing.URL})
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wrapper should finish its local trap after upload failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.rundir, ".uploaded")); err == nil {
		t.Fatal(".uploaded exists although done.json upload failed")
	}
}

// TestWrapper_PartUploadFailure_DoesNotPublishDoneOrMarkUploaded ensures a
// partial results upload cannot be advertised as a complete bundle. The
// proxy fails one results object and forwards all later requests normally.
func TestWrapper_PartUploadFailure_DoesNotPublishDoneOrMarkUploaded(t *testing.T) {
	requireShAndCurl(t)
	h := newWrapperHarness(t)
	failedPart := false
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && !failedPart && strings.Contains(r.URL.Path, "/runs/"+h.runID+"/results/") {
			failedPart = true
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		proxy, err := http.NewRequestWithContext(r.Context(), r.Method, h.server.URL+r.URL.RequestURI(), bytes.NewReader(body))
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		proxy.Header = r.Header.Clone()
		resp, err := http.DefaultClient.Do(proxy)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	t.Cleanup(failing.Close)
	cmd := h.start(t, map[string]string{"ZCP_FARM_S3_URL": failing.URL})
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wrapper should finish local cleanup after part upload failure: %v", err)
	}
	if !failedPart {
		t.Fatal("proxy did not observe a results PUT to fail")
	}
	if _, err := os.Stat(filepath.Join(h.rundir, "done.json")); err != nil {
		t.Fatalf("local done.json was not retained: %v", err)
	}
	if _, ok := h.fake.get("runs/" + h.runID + "/done.json"); ok {
		t.Fatal("remote done.json was published after a failed part upload")
	}
	if _, err := os.Stat(filepath.Join(h.rundir, ".uploaded")); err == nil {
		t.Fatal(".uploaded exists after a failed part upload")
	}
}
