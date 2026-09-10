package farm

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
// binary the wrapper downloads and execs. It never parses its own argv
// (the real binding's exact flags are covered by the CLI-layer tests in
// cmd/zcp; this stub only needs to behave like the runner's process
// lifecycle and its printed dimension lines, spec-testing-architecture.md
// §10.1) — it reads the run descriptor straight from its inherited
// environment instead.
//
// Sequence, unconditionally: drop a side-effect marker (so a test can prove
// this script never ran), write results/<scenario>/{meta.json,
// verification.json} and capture/manifest.json — each containing a literal
// secret value, to prove redaction — keep an unredacted copy outside
// results/capture for the test to diff against, print the three dimension
// lines, then end per STUB_MODE: "ok" (default) exits 0, "fail" prints a
// failed Task line and exits 1, "hang" sleeps 30s so a test can kill -9 it.
const stubEvaluatorScript = `#!/bin/sh
set -eu
: >./ran.marker
mkdir -p "results/$ZCP_FARM_SCENARIO" capture
cat >"results/$ZCP_FARM_SCENARIO/meta.json" <<EOF
{"scenarioId":"$ZCP_FARM_SCENARIO","secret1":"$ANTHROPIC_API_KEY","secret2":"$ZCP_FARM_S3_SECRET"}
EOF
cat >"results/$ZCP_FARM_SCENARIO/verification.json" <<'EOF'
{"formatVersion":"zcp-eval-verification-2","mode":"required","result":"passed"}
EOF
cat >capture/manifest.json <<EOF
{"status":"complete","secretKey":"$ZCP_FARM_S3_KEY"}
EOF
mkdir -p pristine
cp "results/$ZCP_FARM_SCENARIO/meta.json" pristine/meta.json
mode="${STUB_MODE:-ok}"
case "$mode" in
fail)
	echo "Execution:    ok"
	echo "Task:         required failed"
	echo "Task-end evidence: persisted, settled"
	exit 1
	;;
hang)
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
		runID:           "run-" + strings.ReplaceAll(t.Name(), "/", "-"),
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
// own ZCP_FARM_RUNDIR test seam.
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
		"ANTHROPIC_API_KEY":         "sk-ant-farm-secret-value",
		"projectId":                 h.projectID,
		"ZCP_FARM_RUNDIR":           h.rundir,
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
	if done.CredentialMode != "api-key" {
		t.Errorf("credentialMode = %q, want %q", done.CredentialMode, "api-key")
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
// value the wrapper holds is redacted from results/ and capture/ before
// upload. The stub evaluator writes every secret value into
// results/<scenario>/meta.json and capture/manifest.json, and keeps an
// unredacted copy outside results/capture (RUNDIR/pristine/meta.json) — so the
// test can prove the values were genuinely present pre-redaction, not just
// absent because the stub never wrote them.
func TestWrapper_Redaction_NoSecretValueInBundle(t *testing.T) {
	requireShAndCurl(t)

	h := newWrapperHarness(t)
	overrides := map[string]string{
		"ZCP_FARM_S3_KEY":    "AKIDEXAMPLE-redact-me",
		"ZCP_FARM_S3_SECRET": "s3-secret-redact-me",
		"ANTHROPIC_API_KEY":  "sk-ant-redact-me",
	}
	cmd := h.start(t, overrides)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("supervisor exited with error: %v", err)
	}

	secrets := []string{overrides["ZCP_FARM_S3_KEY"], overrides["ZCP_FARM_S3_SECRET"], overrides["ANTHROPIC_API_KEY"]}

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

// TestWrapper_TwoCredentials_Refused pins the credential-mode gate: two
// kinds of agent credential present (ANTHROPIC_API_KEY and
// CLAUDE_CODE_OAUTH_TOKEN) refuses before the child is even forked — an API
// key can shadow an OAuth profile, so carrying both makes the credential
// mode ambiguous (docs/spec-eval-farm.md §2.4).
func TestWrapper_TwoCredentials_Refused(t *testing.T) {
	requireShAndCurl(t)

	h := newWrapperHarness(t)
	cmd := h.start(t, map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "oauth-tok-value"})
	_ = cmd.Wait()

	doneBytes, ok := h.fake.get("runs/" + h.runID + "/done.json")
	if !ok {
		t.Fatalf("done.json missing from bucket")
	}
	var done wrapperDoneJSON
	if err := json.Unmarshal(doneBytes, &done); err != nil {
		t.Fatalf("done.json parse: %v (body: %s)", err, doneBytes)
	}
	if done.RunnerDimensions.Execution != "refused: two credentials" {
		t.Errorf("runnerDimensions.execution = %q, want %q", done.RunnerDimensions.Execution, "refused: two credentials")
	}

	// The child must never have been forked at all: no started.json PUT.
	if _, ok := h.fake.get("runs/" + h.runID + "/started.json"); ok {
		t.Errorf("started.json was uploaded — the child ran despite two credentials being present")
	}
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
