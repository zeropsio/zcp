package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
)

// This file exercises the ACTUAL `zcp` binary (built once, run via
// exec.Command) against a loopback fake of the Zerops REST API and a fake
// `claude` on PATH — the real-binary CLI acceptance harness required by
// docs/spec-testing-architecture.md §10.1 "CLI acceptance" and
// docs/spec-capture-inspector.md §6. Every test in this file is
// non-parallel: it builds and runs the zcp binary with a private HOME.

// ---------------------------------------------------------------------------
// Binary build (once per test binary)
// ---------------------------------------------------------------------------

var (
	zcpBinaryOnce sync.Once
	zcpBinaryPath string
	errZCPBuild   error
)

// zcpRepoRoot resolves the module root from the test's working directory —
// `go test` runs a package's tests with cwd = the package directory, and
// cmd/zcp lives two levels below the root. Verified by the presence of go.mod
// so a moved checkout fails loudly instead of building a stranger's tree.
func zcpRepoRoot() (string, error) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		return "", fmt.Errorf("resolve module root: %w", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return "", fmt.Errorf("module root %s: %w", root, err)
	}
	return root, nil
}

func buildZCPBinary(t *testing.T) string {
	t.Helper()
	zcpBinaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "zcp-cli-test-build-") //nolint:usetesting // shared across every test via sync.Once; t.TempDir() would be removed after the first test
		if err != nil {
			errZCPBuild = fmt.Errorf("create build tempdir: %w", err)
			return
		}
		root, err := zcpRepoRoot()
		if err != nil {
			errZCPBuild = err
			return
		}
		out := filepath.Join(dir, "zcp")
		cmd := exec.CommandContext(context.Background(), "go", "build", "-o", out, "./cmd/zcp")
		cmd.Dir = root
		var stderr bytes.Buffer
		cmd.Stdout = &stderr
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			errZCPBuild = fmt.Errorf("go build ./cmd/zcp: %w\n%s", err, stderr.String())
			return
		}
		zcpBinaryPath = out
	})
	if errZCPBuild != nil {
		t.Fatalf("build zcp binary: %v", errZCPBuild)
	}
	return zcpBinaryPath
}

// ---------------------------------------------------------------------------
// Fake claude on PATH
// ---------------------------------------------------------------------------

// writeFakeClaude writes a shell script named "claude" into dir that emits a
// minimal valid stream-json transcript: one system/init event carrying a
// session_id, one assistant text turn containing a done-marker word (so the
// runner's usersim classifier resolves VerdictDone on the first pass without
// spawning a user-sim resume), and one successful result event. When invoked
// with --resume AND $ZCP_EVAL_FAKE_CLAUDE_BREAK_CAPTURE is set, it first
// chmods $ZCP_CAPTURE_SESSION_DIR read-only (0500) so the runtime cannot
// write its terminal manifest/lifecycle-end record on close — a
// deterministic close/manifest failure even though the task result already
// froze "passed" before the retrospective ran (docs/spec-testing-
// architecture.md §10.2 "only after step 5 does the optional retrospective
// run"). When invoked with --mcp-config, it also launches the configured
// "zerops" MCP server (the candidate binary's `serve`) in the background,
// mirroring what the real `claude` CLI does on startup — §10.4 "Observed
// process identity" needs a real candidate `serve` process alive under the
// capture window's env for the runner's /proc poll to find, which a claude
// stub that never starts it can never provide. The script then sleeps past
// the runner's identity poll interval so the poll has a chance to observe
// the server before this invocation returns; the server's stdin comes from a
// `sleep`-fed pipe, and the script `wait`s for it before exiting, so the
// server has already received EOF and flushed its capture file — never left
// running past this invocation for the capture window to close over an
// unflushed file.
func writeFakeClaude(t *testing.T, dir string) string {
	t.Helper()
	script := `#!/bin/sh
set -e
IS_RESUME=0
MCP_CONFIG=""
PREV=""
for a in "$@"; do
  if [ "$a" = "--resume" ]; then IS_RESUME=1; fi
  if [ "$PREV" = "--mcp-config" ]; then MCP_CONFIG="$a"; fi
  PREV="$a"
done
if [ "$IS_RESUME" = "1" ] && [ -n "$ZCP_EVAL_FAKE_CLAUDE_BREAK_CAPTURE" ] && [ -n "$ZCP_CAPTURE_SESSION_DIR" ]; then
  chmod 0500 "$ZCP_CAPTURE_SESSION_DIR"
fi
if [ -n "$MCP_CONFIG" ] && [ -f "$MCP_CONFIG" ]; then
  MCP_CMD=$(grep -o '"command": *"[^"]*"' "$MCP_CONFIG" | head -1 | sed 's/.*"command": *"//;s/"$//')
  if [ -n "$MCP_CMD" ]; then
    (sleep 1) | env projectId="` + fakeProjectID + `" serviceId="` + fakeZCPServiceID + `" "$MCP_CMD" serve >/dev/null 2>&1 &
    MCP_JOB=$!
    sleep 0.3
    wait "$MCP_JOB"
  fi
fi
SESSION_ID="fake-session-$$"
printf '{"type":"system","subtype":"init","session_id":"%s"}\n' "$SESSION_ID"
printf '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Task complete. Done."}]}}\n'
printf '{"type":"result","subtype":"success","is_error":false,"result":"Task complete. Done."}\n'
exit 0
`
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	return path
}

// ---------------------------------------------------------------------------
// Fake loopback Zerops REST API
// ---------------------------------------------------------------------------

// goosLinux names the one platform where the §10.4 process-identity
// observation is implemented; used by every GOOS-gated assertion in this
// file so the literal appears once (goconst).
const goosLinux = "linux"

// fakeRequest records one HTTP request the fake server observed.
type fakeRequest struct {
	Method string
	Path   string
	At     time.Time
}

// fakeZeropsServer is a loopback httptest server answering exactly the REST
// paths the real platform.ZeropsClient hits in the CLI acceptance path:
// GET /api/rest/public/user/info, POST /api/rest/public/project/search, GET
// /api/rest/public/project/{id} (the candidate `serve` process's own
// runtime-context lookup on Linux CLI acceptance runs), GET
// /api/rest/public/project/{id}/service-stack (direct), POST
// /api/rest/public/service-stack/search (Elasticsearch-backed, used by
// SeedEmpty/CleanupProject), GET /api/rest/public/project/{id}/process
// (direct). Any other path is logged (test failure via t.Errorf, not a
// crash) and answered 404, per the brief's "discover empirically" method.
type fakeZeropsServer struct {
	t           *testing.T
	srv         *httptest.Server
	mu          sync.Mutex
	requests    []fakeRequest
	appStatus   string // "" omits the "app" service entirely
	appCategory string
	// directServiceCalls counts GET /project/{id}/service-stack (direct)
	// requests seen so far. appHiddenForCalls, when > 0, makes the "app"
	// service invisible on direct reads until directServiceCalls exceeds
	// it — used by §10.4 binding tests where the same fake project must
	// look fresh (no app) at preflight time and populated (app present) at
	// the task-end freeze, both direct-GET reads against this same server.
	directServiceCalls int
	appHiddenForCalls  int
}

func newFakeZeropsServer(t *testing.T, appStatus string) *fakeZeropsServer {
	t.Helper()
	f := &fakeZeropsServer{t: t, appStatus: appStatus, appCategory: "CORE"}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeZeropsServer) URL() string { return f.srv.URL }

// hideAppForFirstDirectCall makes the "app" service invisible on the first
// direct GET /project/{id}/service-stack read (the preflight fresh-target
// check), then visible from the second call onward (the task-end freeze
// read) — see the directServiceCalls field doc.
func (f *fakeZeropsServer) hideAppForFirstDirectCall() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appHiddenForCalls = 1
}

func (f *fakeZeropsServer) record(r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, fakeRequest{Method: r.Method, Path: r.URL.Path, At: time.Now()})
}

func (f *fakeZeropsServer) esSearchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, req := range f.requests {
		if req.Method == http.MethodPost && req.Path == "/api/rest/public/service-stack/search" {
			n++
		}
	}
	return n
}

// requestPaths returns every request path this server has observed, in
// order — used by tests asserting a preflight refusal made zero platform
// reads beyond the auth handshake.
func (f *fakeZeropsServer) requestPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.requests))
	for i, req := range f.requests {
		out[i] = req.Path
	}
	return out
}

func (f *fakeZeropsServer) deleteCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, req := range f.requests {
		if req.Method == http.MethodDelete {
			n++
		}
	}
	return n
}

const (
	fakeProjectID    = "project-cli-test-0000000000"
	fakeClientID     = "client-cli-test-000000000000"
	fakeUserID       = "user-cli-test-0000000000000"
	fakeZCPServiceID = "svc-zcp-000000000000000000000"
	fakeAppServiceID = "svc-app-000000000000000000000"
)

func (f *fakeZeropsServer) handle(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	w.Header().Set("Content-Type", "application/json")

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/rest/public/user/info":
		fmt.Fprintf(w, `{"id":%q,"email":"cli-test@example.com","fullName":"CLI Test","clientUserList":[{"id":"cu1","clientId":%q,"userId":%q}]}`,
			fakeUserID, fakeClientID, fakeUserID)
		return

	case r.Method == http.MethodPost && r.URL.Path == "/api/rest/public/project/search":
		fmt.Fprintf(w, `{"limit":100,"offset":0,"totalHits":1,"items":[{"id":%q,"clientId":%q,"name":"cli-test-project","status":"ACTIVE"}]}`,
			fakeProjectID, fakeClientID)
		return

	case r.Method == http.MethodGet && r.URL.Path == "/api/rest/public/project/"+fakeProjectID:
		fmt.Fprintf(w, `{"id":%q,"name":"cli-test-project","status":"ACTIVE"}`, fakeProjectID)
		return

	case r.Method == http.MethodGet && r.URL.Path == "/api/rest/public/project/"+fakeProjectID+"/service-stack":
		appVisible := f.directServiceStackAppVisible()
		fmt.Fprint(w, `{"list":[`+f.serviceStackItemsJSON(appVisible)+`],"totalCount":`+fmt.Sprint(f.serviceStackCount(appVisible))+`}`)
		return

	case r.Method == http.MethodPost && r.URL.Path == "/api/rest/public/service-stack/search":
		fmt.Fprint(w, `{"limit":1000,"offset":0,"totalHits":`+fmt.Sprint(f.serviceStackCount(true))+`,"items":[`+f.serviceStackItemsJSON(true)+`]}`)
		return

	case r.Method == http.MethodGet && r.URL.Path == "/api/rest/public/project/"+fakeProjectID+"/process":
		fmt.Fprint(w, `{"list":[],"totalCount":0}`)
		return

	default:
		f.t.Logf("fake zerops server: unmatched request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"unmatched fake route"}`)
	}
}

// directServiceStackAppVisible decides, for THIS direct-GET call, whether
// the "app" service should appear — see the directServiceCalls field doc.
func (f *fakeZeropsServer) directServiceStackAppVisible() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.directServiceCalls++
	if f.appHiddenForCalls > 0 && f.directServiceCalls <= f.appHiddenForCalls {
		return false
	}
	return true
}

func (f *fakeZeropsServer) serviceStackCount(appVisible bool) int {
	if f.appStatus == "" || !appVisible {
		return 1
	}
	return 2
}

// serviceStackItemsJSON renders the "zcp" controlling service (always
// present, category CORE so IsSystem() is true and it is never a cleanup
// candidate) plus, when appStatus is set, an "app" service in the same
// category — CORE keeps it out of CleanupProject's delete-candidate set too,
// so this harness never needs to model the delete-and-poll lifecycle to get
// a stable pre-task platform state.
func (f *fakeZeropsServer) serviceStackItemsJSON(appVisible bool) string {
	items := []string{f.serviceStackJSON(fakeZCPServiceID, "zcp", "ACTIVE", "CORE")}
	if f.appStatus != "" && appVisible {
		items = append(items, f.serviceStackJSON(fakeAppServiceID, "app", f.appStatus, f.appCategory))
	}
	return strings.Join(items, ",")
}

func (f *fakeZeropsServer) serviceStackJSON(id, name, status, category string) string {
	return fmt.Sprintf(
		`{"id":%q,"name":%q,"projectId":%q,"serviceStackTypeId":"runtime@1","serviceStackTypeInfo":{"serviceStackTypeVersionName":"v1","serviceStackTypeCategory":%q},"status":%q,"subdomainAccess":false,"ports":[],"created":"2026-01-01T00:00:00Z","lastUpdate":"2026-01-01T00:00:00Z"}`,
		id, name, fakeProjectID, category, status,
	)
}

// ---------------------------------------------------------------------------
// CLI harness scaffolding
// ---------------------------------------------------------------------------

// cliHarness bundles one isolated run's HOME, work dir, and environment.
type cliHarness struct {
	t          *testing.T
	home       string
	workDir    string
	resultsDir string
	binDir     string
	server     *fakeZeropsServer
	env        []string
}

// newCLIHarness sets up a private HOME/work dir, a fake claude on PATH, and
// a fake Zerops REST server, and returns the environment for exec.Command.
// appStatus configures the "app" service's platform-reported status ("" =
// service does not exist at all).
func newCLIHarness(t *testing.T, appStatus string) *cliHarness {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	workDir := filepath.Join(root, "work")
	resultsDir := filepath.Join(root, "results")
	claudeHome := filepath.Join(root, "claude-home")
	binDir := filepath.Join(root, "bin")
	for _, dir := range []string{home, workDir, resultsDir, claudeHome, binDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(workDir, ".zcp-eval-workdir"), []byte(""), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	mcpConfig := filepath.Join(root, "mcp.json")
	if err := os.WriteFile(mcpConfig, []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatalf("write mcp config: %v", err)
	}
	writeFakeClaude(t, binDir)
	server := newFakeZeropsServer(t, appStatus)

	env := []string{
		"HOME=" + home,
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"ZCP_API_KEY=fake-token",
		"ZCP_API_HOST=" + server.URL(),
		"ZCP_EVAL_WORK_DIR=" + workDir,
		"ZCP_EVAL_SENTINEL_FILE=.zcp-eval-workdir",
		"ZCP_EVAL_RESULTS_DIR=" + resultsDir,
		"ZCP_EVAL_CLAUDE_HOME=" + claudeHome,
		"ZCP_EVAL_MCP_CONFIG=" + mcpConfig,
		"ZCP_AUTO_UPDATE=0",
		"ZCP_TELEMETRY=0",
		"TMPDIR=" + os.TempDir(),
	}
	return &cliHarness{t: t, home: home, workDir: workDir, resultsDir: resultsDir, binDir: binDir, server: server, env: env}
}

// run executes the built zcp binary with args, appending extraEnv, and
// returns exit code + combined stdout/stderr (stderr is where every
// behavioral CLI diagnostic is printed).
func (h *cliHarness) run(t *testing.T, extraEnv []string, args ...string) (exitCode int, stderr string) {
	t.Helper()
	bin := buildZCPBinary(t)
	cmd := exec.CommandContext(context.Background(), bin, args...)
	cmd.Env = append(append([]string{}, h.env...), extraEnv...)
	var errBuf bytes.Buffer
	cmd.Stdout = &errBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err == nil {
		return 0, errBuf.String()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), errBuf.String()
	}
	t.Fatalf("run zcp %v: %v\noutput:\n%s", args, err, errBuf.String())
	return -1, errBuf.String()
}

// writeRequiredScenario writes a behavioral scenario asserting hostname
// "app" is ACTIVE, in the given verification mode ("" omits the
// verification block entirely, defaulting to observe).
func writeRequiredScenario(t *testing.T, dir, id, mode string) string {
	t.Helper()
	var verification string
	switch mode {
	case "":
		verification = "verification:\n  expectedServices:\n    - hostname: app\n      status: [ACTIVE]\n"
	default:
		verification = "verification:\n  mode: " + mode + "\n  expectedServices:\n    - hostname: app\n      status: [ACTIVE]\n"
	}
	content := "---\n" +
		"id: " + id + "\n" +
		"description: CLI acceptance smoke scenario\n" +
		"seed: empty\n" +
		"retrospective:\n  promptStyle: briefing-future-agent\n" +
		verification +
		"---\n" +
		"Say the task is done.\n"
	path := filepath.Join(dir, id+".md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	return path
}

// ---------------------------------------------------------------------------
// TestBehavioralCLI_RequiredFailure_NonzeroWithExplicitDimensions
// ---------------------------------------------------------------------------

// non-parallel: builds and runs the zcp binary with a private HOME.
func TestBehavioralCLI_RequiredFailure_NonzeroWithExplicitDimensions(t *testing.T) {
	h := newCLIHarness(t, "FAILED")
	h.server.hideAppForFirstDirectCall() // preflight fresh-target read sees an empty project; the task-end freeze sees the seeded FAILED "app"
	scenarioDir := t.TempDir()
	scenarioPath := writeRequiredScenario(t, scenarioDir, "cli-required-fail", "required")

	args := append([]string{"eval", "behavioral", "run", "--file", scenarioPath, "--capture", "raw"}, cliBindingArgs(t)...)
	exitCode, stderr := h.run(t, nil, args...)

	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1\nstderr:\n%s", exitCode, stderr)
	}
	for _, want := range []string{"Execution:    ok", "Task:         required failed", "Task-end evidence: persisted"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q\nstderr:\n%s", want, stderr)
		}
	}
	if n := h.server.deleteCount(); n != 0 {
		t.Errorf("DELETE requests to fake server = %d, want 0", n)
	}

	metaPath := findResultFile(t, h.resultsDir, "cli-required-fail", "meta.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	var meta struct {
		Task struct {
			Result string `json:"result"`
		} `json:"task"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("parse meta.json: %v\n%s", err, data)
	}
	if meta.Task.Result != "failed" {
		t.Errorf("meta.json task.result = %q, want failed", meta.Task.Result)
	}
	if meta.Error != "" {
		t.Errorf("meta.json error = %q, want empty", meta.Error)
	}
}

// findResultFile globs <resultsDir>/<suiteID>/<scenarioID>/<name> across every
// suite directory (suite IDs are timestamp-generated, unknown to the caller)
// and returns the single match.
func findResultFile(t *testing.T, resultsDir, scenarioID, name string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(resultsDir, "*", scenarioID, name))
	if err != nil {
		t.Fatalf("glob result file %s: %v", name, err)
	}
	if len(matches) != 1 {
		t.Fatalf("result file %s under %s/*/%s: got %d matches, want 1: %v", name, resultsDir, scenarioID, len(matches), matches)
	}
	return matches[0]
}

// ---------------------------------------------------------------------------
// TestBehavioralCLI_ObserveMode_PreservesExecutionOnlyExit
// ---------------------------------------------------------------------------

// non-parallel: builds and runs the zcp binary with a private HOME.
func TestBehavioralCLI_ObserveMode_PreservesExecutionOnlyExit(t *testing.T) {
	h := newCLIHarness(t, "FAILED")
	scenarioDir := t.TempDir()
	scenarioPath := writeRequiredScenario(t, scenarioDir, "cli-observe-fail", "")

	exitCode, stderr := h.run(t, nil, "eval", "behavioral", "run", "--file", scenarioPath)

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr:\n%s", exitCode, stderr)
	}
	if !strings.Contains(stderr, "Task:         observe failed (advisory)") {
		t.Errorf("stderr missing observe-mode advisory task line\nstderr:\n%s", stderr)
	}

	// Legacy cleanup runs the ES service-stack search at least twice: once
	// during SeedEmpty (before task work) and once more from the deferred
	// post-scenario CleanupProject (after the run) — the execution-only exit
	// and legacy cleanup path observe mode keeps.
	if n := h.server.esSearchCount(); n < 2 {
		t.Errorf("POST /service-stack/search count = %d, want >= 2 (seed + post-run cleanup)", n)
	}
}

// ---------------------------------------------------------------------------
// TestBehavioralCLI_FailedTask_CompleteCaptureRemainsReadable
// ---------------------------------------------------------------------------

// non-parallel: builds and runs the zcp binary with a private HOME.
func TestBehavioralCLI_FailedTask_CompleteCaptureRemainsReadable(t *testing.T) {
	h := newCLIHarness(t, "FAILED")
	h.server.hideAppForFirstDirectCall()
	scenarioDir := t.TempDir()
	scenarioPath := writeRequiredScenario(t, scenarioDir, "cli-required-fail-capture", "required")

	args := append([]string{"eval", "behavioral", "run", "--file", scenarioPath, "--capture", "raw"}, cliBindingArgs(t)...)
	exitCode, stderr := h.run(t, nil, args...)
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1\nstderr:\n%s", exitCode, stderr)
	}

	sessionDir := findCaptureSessionDir(t, h.home)
	manifest, err := capture.ReadSessionManifest(filepath.Join(sessionDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if manifest.Status != "complete" {
		t.Fatalf("manifest status = %q, want complete", manifest.Status)
	}

	report, err := capture.InspectSession(sessionDir)
	if err != nil {
		t.Fatalf("InspectSession: %v", err)
	}
	if !report.Integrity.Valid || !report.Integrity.Complete {
		t.Fatalf("Integrity = %+v, want Valid && Complete", report.Integrity)
	}
	// A failed task is a normal, complete capture (spec-capture-inspector §8):
	// the eval-run lifecycle end must say complete, not partial.
	records, err := capture.ReadLifecycleRecords(filepath.Join(sessionDir, "lifecycle.jsonl"))
	if err != nil {
		t.Fatalf("read lifecycle: %v", err)
	}
	sawRunEnd := false
	for _, record := range records {
		if record.Kind == capture.LifecycleEvalRunEnd {
			sawRunEnd = true
			if record.Status != capture.CaptureComplete || record.Error != "" {
				t.Fatalf("eval_run.end = status %q error %q, want complete with no error for a failed task", record.Status, record.Error)
			}
		}
	}
	if !sawRunEnd {
		t.Fatal("no eval_run.end lifecycle record")
	}

	bundled := findResultFile(t, filepath.Join(sessionDir, "eval"), "cli-required-fail-capture", "verification.json")
	bundledBytes, err := os.ReadFile(bundled)
	if err != nil {
		t.Fatalf("read bundled verification.json: %v", err)
	}
	resultVerification := findResultFile(t, h.resultsDir, "cli-required-fail-capture", "verification.json")
	resultBytes, err := os.ReadFile(resultVerification)
	if err != nil {
		t.Fatalf("read result-dir verification.json: %v", err)
	}
	if string(bundledBytes) != string(resultBytes) {
		t.Fatalf("bundled verification.json differs from result-dir copy\nbundled:\n%s\nresult:\n%s", bundledBytes, resultBytes)
	}
	var doc struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(bundledBytes, &doc); err != nil {
		t.Fatalf("parse bundled verification.json: %v", err)
	}
	if doc.Result != "failed" {
		t.Errorf("bundled verification.json result = %q, want failed", doc.Result)
	}
}

// findCaptureSessionDir returns the single capture session directory created
// under $HOME/.local/state/zcp/captures.
func findCaptureSessionDir(t *testing.T, home string) string {
	t.Helper()
	root := filepath.Join(home, ".local", "state", "zcp", "captures")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read capture root %s: %v", root, err)
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("capture root entries = %v, want exactly one session directory", entries)
	}
	return filepath.Join(root, entries[0].Name())
}

// ---------------------------------------------------------------------------
// TestBehavioralCLI_SharedCapture_RefusedBeforeMutation
// ---------------------------------------------------------------------------

// non-parallel: builds and runs the zcp binary with a private HOME.
func TestBehavioralCLI_SharedCapture_RefusedBeforeMutation(t *testing.T) {
	t.Run("no capture window at all", func(t *testing.T) {
		h := newCLIHarness(t, "ACTIVE")
		scenarioDir := t.TempDir()
		scenarioPath := writeRequiredScenario(t, scenarioDir, "cli-required-no-window", "required")

		exitCode, stderr := h.run(t, nil, "eval", "behavioral", "run", "--file", scenarioPath)
		if exitCode == 0 {
			t.Fatalf("exit code = 0, want nonzero\nstderr:\n%s", stderr)
		}
		if !strings.Contains(stderr, "capture: required mode needs this invocation's own scoped capture window") {
			t.Errorf("stderr does not name the capture refusal\nstderr:\n%s", stderr)
		}
		assertOnlyAuthAndDiscoveryRequests(t, h.server)
	})

	t.Run("inherited outer capture window", func(t *testing.T) {
		h := newCLIHarness(t, "ACTIVE")
		scenarioDir := t.TempDir()
		scenarioPath := writeRequiredScenario(t, scenarioDir, "cli-required-inherited", "required")
		outerCaptureRoot := t.TempDir()
		bin := buildZCPBinary(t)

		exitCode, stderr := h.run(t, nil,
			"capture", "raw", "--output-dir", outerCaptureRoot, "--",
			bin, "eval", "behavioral", "run", "--file", scenarioPath, "--capture", "raw",
		)
		if exitCode == 0 {
			t.Fatalf("exit code = 0, want nonzero\nstderr:\n%s", stderr)
		}
		if !strings.Contains(stderr, "capture: required mode needs this invocation's own scoped capture window") {
			t.Errorf("stderr does not name the capture refusal\nstderr:\n%s", stderr)
		}
		assertOnlyAuthAndDiscoveryRequests(t, h.server)
	})
}

// assertOnlyAuthAndDiscoveryRequests asserts the fake server saw at most the
// auth/project-discovery reads (user/info, project/search) and nothing that
// would mutate or read project-scoped state — i.e. the refused scenario made
// zero platform calls beyond identity resolution.
func assertOnlyAuthAndDiscoveryRequests(t *testing.T, server *fakeZeropsServer) {
	t.Helper()
	server.mu.Lock()
	defer server.mu.Unlock()
	for _, req := range server.requests {
		switch {
		case req.Method == http.MethodGet && req.Path == "/api/rest/public/user/info":
		case req.Method == http.MethodPost && req.Path == "/api/rest/public/project/search":
		default:
			t.Errorf("unexpected platform request after refusal: %s %s", req.Method, req.Path)
		}
	}
}

// ---------------------------------------------------------------------------
// TestBehavioralCLI_OwnedScopedChild_FinalizesThenReturnsAcceptance
// ---------------------------------------------------------------------------

// non-parallel: builds and runs the zcp binary with a private HOME.
func TestBehavioralCLI_OwnedScopedChild_FinalizesThenReturnsAcceptance(t *testing.T) {
	h := newCLIHarness(t, "ACTIVE")
	h.server.hideAppForFirstDirectCall()
	scenarioDir := t.TempDir()
	scenarioPath := writeRequiredScenario(t, scenarioDir, "cli-required-pass", "required")

	args := append([]string{"eval", "behavioral", "run", "--file", scenarioPath, "--capture", "raw"}, cliBindingArgs(t)...)
	exitCode, stderr := h.run(t, nil, args...)
	// The task itself passes regardless of platform (asserted below via
	// stderr + meta.json), but §10.4's fourth acceptance dimension (process
	// identity) is always "unsupported" on a non-Linux machine — a bound
	// required run can only exit 0 here on Linux (see
	// TestBehavioralCLI_Bound_RequiredRefusesOnUnsupportedOS_OrAcceptsOnLinux).
	if runtime.GOOS == goosLinux {
		if exitCode != 0 {
			t.Fatalf("exit code = %d, want 0 on linux\nstderr:\n%s", exitCode, stderr)
		}
		if !strings.Contains(stderr, "Process identity: ok") {
			t.Errorf("stderr missing 'Process identity: ok' on linux\nstderr:\n%s", stderr)
		}
	} else {
		if exitCode == 0 {
			t.Fatalf("exit code = 0, want nonzero on %s (process identity is always unsupported)\nstderr:\n%s", runtime.GOOS, stderr)
		}
		if !strings.Contains(stderr, "Process identity: blocked") {
			t.Errorf("stderr missing 'Process identity: blocked' on %s\nstderr:\n%s", runtime.GOOS, stderr)
		}
		if !strings.Contains(stderr, "rejected:") || !strings.Contains(stderr, "process identity") {
			t.Errorf("stderr missing a 'rejected:' line naming process identity on %s\nstderr:\n%s", runtime.GOOS, stderr)
		}
	}
	// Asserted on both OSes: the task itself still passes, and finalization
	// still completes, even when the fourth dimension blocks acceptance.
	if !strings.Contains(stderr, "Task:         required passed") {
		t.Errorf("stderr missing required-pass task line\nstderr:\n%s", stderr)
	}

	resultIdx := strings.Index(stderr, "=== Behavioral cli-required-pass ===")
	completeIdx := strings.Index(stderr, "capture: complete")
	if resultIdx < 0 || completeIdx < 0 || completeIdx < resultIdx {
		t.Fatalf("wrapper's 'capture: complete' line did not appear after the runner's result block\nstderr:\n%s", stderr)
	}

	sessionDir := findCaptureSessionDir(t, h.home)
	manifest, err := capture.ReadSessionManifest(filepath.Join(sessionDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if manifest.Status != "complete" {
		t.Fatalf("manifest status = %q, want complete", manifest.Status)
	}

	metaPath := findResultFile(t, h.resultsDir, "cli-required-pass", "meta.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	var meta struct {
		TaskEnd struct {
			Persisted bool `json:"persisted"`
		} `json:"taskEnd"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("parse meta.json: %v\n%s", err, data)
	}
	if !meta.TaskEnd.Persisted {
		t.Errorf("meta.json taskEnd.persisted = false, want true")
	}
}

// ---------------------------------------------------------------------------
// TestBehavioralCLI_CaptureCloseFailure_NonzeroEvenIfTaskPassed
// ---------------------------------------------------------------------------

// non-parallel: builds and runs the zcp binary with a private HOME.
func TestBehavioralCLI_CaptureCloseFailure_NonzeroEvenIfTaskPassed(t *testing.T) {
	h := newCLIHarness(t, "ACTIVE")
	h.server.hideAppForFirstDirectCall()
	scenarioDir := t.TempDir()
	scenarioPath := writeRequiredScenario(t, scenarioDir, "cli-required-broken-capture", "required")

	args := append([]string{"eval", "behavioral", "run", "--file", scenarioPath, "--capture", "raw"}, cliBindingArgs(t)...)
	exitCode, stderr := h.run(t, []string{"ZCP_EVAL_FAKE_CLAUDE_BREAK_CAPTURE=1"}, args...)

	if exitCode == 0 {
		t.Fatalf("exit code = 0, want nonzero even though the task passed\nstderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "Task:         required passed") {
		t.Errorf("stderr missing required-pass task line (task result must stay passed)\nstderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "capture:") {
		t.Errorf("stderr does not name the capture dimension\nstderr:\n%s", stderr)
	}

	metaPath := findResultFile(t, h.resultsDir, "cli-required-broken-capture", "meta.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	var meta struct {
		Task struct {
			Result string `json:"result"`
		} `json:"task"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("parse meta.json: %v\n%s", err, data)
	}
	if meta.Task.Result != "passed" {
		t.Fatalf("meta.json task.result = %q, want passed (capture failure must not downgrade an already-frozen task result)", meta.Task.Result)
	}

	sessionDir := findCaptureSessionDir(t, h.home)
	t.Cleanup(func() { _ = os.Chmod(sessionDir, 0o700) }) // restore before TempDir's own removal cleanup runs (LIFO)

	info, statErr := os.Stat(sessionDir)
	if statErr != nil {
		t.Fatalf("stat session dir: %v", statErr)
	}
	if info.Mode().Perm()&0o200 != 0 {
		t.Fatalf("session dir %s is still writable — fake claude's break-capture step did not fire", sessionDir)
	}
	manifestErr := errOrNil(capture.ReadSessionManifest(filepath.Join(sessionDir, "manifest.json")))
	inspectErr := errOrNil2(capture.InspectSession(sessionDir))
	if manifestErr == nil && inspectErr == nil {
		t.Fatalf("both manifest read and InspectSession(%s) succeeded, want at least one failure (session dir is read-only)", sessionDir)
	}
}

func errOrNil(_ *capture.SessionManifestDocument, err error) error { return err }
func errOrNil2(_ *capture.InspectionReport, err error) error       { return err }

// ---------------------------------------------------------------------------
// TestExecutionBinding_WrongProject_RefusedBeforeRunner
// ---------------------------------------------------------------------------

// cliBindingArgs builds a valid §10.4 binding flag set for the CLI harness:
// a candidate that is a byte-identical copy of the binary under test, bound
// to fakeProjectID. h.server.hideAppForFirstDirectCall() must be called
// separately by tests whose fake server pre-populates a non-system service,
// so the preflight's fresh-target read (the 1st direct GET) sees an empty
// project while the task-end freeze (the 2nd) sees the seeded state.
func cliBindingArgs(t *testing.T) []string {
	t.Helper()
	bin := buildZCPBinary(t)
	candidate := filepath.Join(t.TempDir(), "zcp-candidate")
	copyExecutableFile(t, bin, candidate)
	sha := sha256HexFile(t, candidate)
	return []string{
		"--candidate", candidate,
		"--candidate-sha256", sha,
		"--project-id", fakeProjectID,
		"--ack-disposable-project", "yes",
	}
}

// sha256HexFile hashes a file for use as --candidate-sha256 in tests.
func sha256HexFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// non-parallel: builds and runs the zcp binary with a private HOME.
//
// TestExecutionBinding_WrongProject_RefusedBeforeRunner pins
// docs/spec-testing-architecture.md §10.4: a binding whose --project-id
// does not match the project the credentials resolve to is refused before
// the runner is built — zero service/process/DELETE requests against the
// fake Zerops API.
func TestExecutionBinding_WrongProject_RefusedBeforeRunner(t *testing.T) {
	h := newCLIHarness(t, "")
	scenarioDir := t.TempDir()
	scenarioPath := writeRequiredScenario(t, scenarioDir, "cli-wrong-project", "required")

	bin := buildZCPBinary(t)
	sha := sha256HexFile(t, bin)

	exitCode, stderr := h.run(t, nil,
		"eval", "behavioral", "run", "--file", scenarioPath,
		"--candidate", bin,
		"--candidate-sha256", sha,
		"--project-id", "not-"+fakeProjectID,
		"--ack-disposable-project", "yes",
	)

	if exitCode == 0 {
		t.Fatalf("exit code = 0, want nonzero\nstderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "binding: wrong project") {
		t.Errorf("stderr missing 'binding: wrong project'\nstderr:\n%s", stderr)
	}
	for _, req := range h.server.requestPaths() {
		if strings.Contains(req, "service-stack") || strings.Contains(req, "/process") {
			t.Errorf("unexpected platform request %s — wrong-project binding must refuse before any service/process read", req)
		}
	}
	if n := h.server.deleteCount(); n != 0 {
		t.Errorf("DELETE requests = %d, want 0", n)
	}
}

// ---------------------------------------------------------------------------
// TestBehavioralCLI_Bound_RequiredRefusesOnUnsupportedOS_OrAcceptsOnLinux
// ---------------------------------------------------------------------------

// non-parallel: builds and runs the zcp binary with a private HOME.
//
// TestBehavioralCLI_Bound_RequiredRefusesOnUnsupportedOS_OrAcceptsOnLinux
// pins docs/spec-testing-architecture.md §10.4's fourth acceptance
// dimension end to end: on a non-Linux CI/dev machine the process-identity
// observation is always "unsupported", so a required run with a binding
// must exit nonzero and print the exact blocked line; on Linux the same
// scenario must actually observe the candidate `serve` process and accept.
// Both branches are real assertions (runtime.GOOS decides which), not a
// skip.
func TestBehavioralCLI_Bound_RequiredRefusesOnUnsupportedOS_OrAcceptsOnLinux(t *testing.T) {
	// appStatus "ACTIVE" + hideAppForFirstDirectCall: the scenario's
	// required check (hostname app ACTIVE) must actually be able to pass —
	// on Linux, exit 0 requires a passed task AND accepted process
	// identity, not process identity alone (behavioralAccepted checks task
	// result first). An absent "app" would fail the task regardless of
	// platform and made the "OrAcceptsOnLinux" branch untestable.
	h := newCLIHarness(t, "ACTIVE")
	h.server.hideAppForFirstDirectCall()
	scenarioDir := t.TempDir()
	scenarioPath := writeRequiredScenario(t, scenarioDir, "cli-bound-identity", "required")

	bin := buildZCPBinary(t)
	candidate := filepath.Join(t.TempDir(), "zcp-candidate")
	copyExecutableFile(t, bin, candidate)
	sha := sha256HexFile(t, candidate)

	exitCode, stderr := h.run(t, nil,
		"eval", "behavioral", "run", "--file", scenarioPath, "--capture", "raw",
		"--candidate", candidate,
		"--candidate-sha256", sha,
		"--project-id", fakeProjectID,
		"--ack-disposable-project", "yes",
	)

	if runtime.GOOS != goosLinux {
		if exitCode == 0 {
			t.Fatalf("exit code = 0, want nonzero on %s\nstderr:\n%s", runtime.GOOS, stderr)
		}
		if !strings.Contains(stderr, "Process identity: blocked: unsupported OS") {
			t.Errorf("stderr missing 'Process identity: blocked: unsupported OS'\nstderr:\n%s", stderr)
		}
		return
	}
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0 on linux\nstderr:\n%s", exitCode, stderr)
	}
	if !strings.Contains(stderr, "Process identity: ok") {
		t.Errorf("stderr missing 'Process identity: ok'\nstderr:\n%s", stderr)
	}
}

// copyExecutableFile copies src to dst preserving the executable bit — used
// to give a CLI test a "candidate" binary that is byte-identical to (but a
// different path from) the binary under test.
func copyExecutableFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o700); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}
