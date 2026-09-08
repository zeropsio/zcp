package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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

// zcpRepoRoot is this worktree's module root — cmd/zcp/eval_behavioral_cli_test.go
// lives two levels below it.
const zcpRepoRoot = "/Users/macbook/Documents/Zerops-MCP/zcp-wt/zcp-evolution"

func buildZCPBinary(t *testing.T) string {
	t.Helper()
	zcpBinaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "zcp-cli-test-build-") //nolint:usetesting // shared across every test via sync.Once; t.TempDir() would be removed after the first test
		if err != nil {
			errZCPBuild = fmt.Errorf("create build tempdir: %w", err)
			return
		}
		out := filepath.Join(dir, "zcp")
		cmd := exec.CommandContext(context.Background(), "go", "build", "-o", out, "./cmd/zcp")
		cmd.Dir = zcpRepoRoot
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
// deletes $ZCP_CAPTURE_SESSION_DIR/lifecycle.jsonl — used to force the owned
// capture window to fail finalization/inspection after a task already
// passed (docs/spec-capture-inspector.md §8 "task result and capture status
// are different facts").
func writeFakeClaude(t *testing.T, dir string) string {
	t.Helper()
	script := `#!/bin/sh
set -e
IS_RESUME=0
for a in "$@"; do
  if [ "$a" = "--resume" ]; then IS_RESUME=1; fi
done
if [ "$IS_RESUME" = "1" ] && [ -n "$ZCP_EVAL_FAKE_CLAUDE_BREAK_CAPTURE" ] && [ -n "$ZCP_CAPTURE_SESSION_DIR" ]; then
  rm -f "$ZCP_CAPTURE_SESSION_DIR/lifecycle.jsonl"
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

// fakeRequest records one HTTP request the fake server observed.
type fakeRequest struct {
	Method string
	Path   string
	At     time.Time
}

// fakeZeropsServer is a loopback httptest server answering exactly the REST
// paths the real platform.ZeropsClient hits in the CLI acceptance path:
// GET /api/rest/public/user/info, POST /api/rest/public/project/search, GET
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
}

func newFakeZeropsServer(t *testing.T, appStatus string) *fakeZeropsServer {
	t.Helper()
	f := &fakeZeropsServer{t: t, appStatus: appStatus, appCategory: "CORE"}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeZeropsServer) URL() string { return f.srv.URL }

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

	case r.Method == http.MethodGet && r.URL.Path == "/api/rest/public/project/"+fakeProjectID+"/service-stack":
		fmt.Fprint(w, `{"list":[`+f.serviceStackItemsJSON()+`],"totalCount":`+fmt.Sprint(f.serviceStackCount())+`}`)
		return

	case r.Method == http.MethodPost && r.URL.Path == "/api/rest/public/service-stack/search":
		fmt.Fprint(w, `{"limit":1000,"offset":0,"totalHits":`+fmt.Sprint(f.serviceStackCount())+`,"items":[`+f.serviceStackItemsJSON()+`]}`)
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

func (f *fakeZeropsServer) serviceStackCount() int {
	if f.appStatus == "" {
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
func (f *fakeZeropsServer) serviceStackItemsJSON() string {
	items := []string{f.serviceStackJSON(fakeZCPServiceID, "zcp", "ACTIVE", "CORE")}
	if f.appStatus != "" {
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
	scenarioDir := t.TempDir()
	scenarioPath := writeRequiredScenario(t, scenarioDir, "cli-required-fail", "required")

	exitCode, stderr := h.run(t, nil, "eval", "behavioral", "run", "--file", scenarioPath, "--capture", "raw")

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
	scenarioDir := t.TempDir()
	scenarioPath := writeRequiredScenario(t, scenarioDir, "cli-required-fail-capture", "required")

	exitCode, stderr := h.run(t, nil, "eval", "behavioral", "run", "--file", scenarioPath, "--capture", "raw")
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
