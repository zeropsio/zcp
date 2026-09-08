package eval

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
	"github.com/zeropsio/zcp/internal/platform"
)

// behavioralHarness wires up the offline, no-network, no-credentials test
// rig documented in the S1a brief: a private HOME/PATH/WorkDir triple, an
// isolated results dir, and the eval sentinel so CleanupProject is willing
// to run against the fake work dir.
type behavioralHarness struct {
	root, home, work, bin, resultsDir string
}

// non-parallel: t.Setenv mutates process environment; every test using this
// harness must NOT call t.Parallel().
func newBehavioralHarness(t *testing.T) *behavioralHarness {
	t.Helper()
	root := t.TempDir()
	h := &behavioralHarness{
		root:       root,
		home:       filepath.Join(root, "home"),
		work:       filepath.Join(root, "work"),
		bin:        filepath.Join(root, "bin"),
		resultsDir: filepath.Join(root, "results"),
	}
	for _, dir := range []string{h.home, h.work, h.bin, h.resultsDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", h.home)
	t.Setenv("PATH", h.bin)
	t.Setenv("serviceId", "")
	t.Setenv("ZCP_EVAL_SENTINEL_FILE", ".zcp-eval-workdir")
	if err := os.WriteFile(filepath.Join(h.work, ".zcp-eval-workdir"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *behavioralHarness) writeClaudeScript(t *testing.T, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(h.bin, "claude"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}

func (h *behavioralHarness) writeScenario(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(h.root, "scenario.md")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func (h *behavioralHarness) config() RunnerConfig {
	return RunnerConfig{
		ResultsDir: h.resultsDir, WorkDir: h.work, ClaudeHome: h.home,
		Model: "fake-offline", Timeout: 5 * time.Second, TaskEndSettle: 200 * time.Millisecond,
	}
}

func (h *behavioralHarness) outDir(scenarioID string) string {
	return filepath.Join(h.resultsDir, "suite", scenarioID)
}

// defaultFakeClaudeDone is the offline claude stand-in: it emits one
// assistant "Done" turn on the initial invocation and, on --resume, touches
// a marker file (so tests can order the retrospective against a platform
// read) before emitting an assistant retrospective turn.
const defaultFakeClaudeDone = `#!/bin/sh
if [ "$1" = "--resume" ]; then
    : > "$RETRO_MARKER"
fi
printf '%s\n' '{"type":"system","subtype":"init","session_id":"offline-probe","model":"fake-offline"}'
printf '%s\n' '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Done. The application is ready."}]}}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"offline-probe","result":"Done. The application is ready."}'
`

// noHTTPGuard fails the test if Do is ever called — used to assert a
// required-mode probe row never fires an HTTP request.
type noHTTPGuard struct {
	t     *testing.T
	calls int
}

func (g *noHTTPGuard) Do(*http.Request) (*http.Response, error) {
	g.calls++
	g.t.Error("unexpected HTTP call")
	return nil, fmt.Errorf("unexpected HTTP call in offline harness")
}

func requiredExpectedAppScenario(id string) string {
	return fmt.Sprintf(`---
id: %s
seed: empty
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  expectedServices:
    - hostname: app
      status: [ACTIVE]
---
Finish the offline fixture application.
`, id)
}

func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func readVerificationDocument(t *testing.T, outDir string) VerificationDocument {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outDir, "verification.json"))
	if err != nil {
		t.Fatalf("read verification.json: %v", err)
	}
	var doc VerificationDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode verification.json: %v", err)
	}
	return doc
}

// TestBehavioralOutcome_RequiredSharedCapture_RefusedBeforeMutation pins
// docs/spec-capture-inspector.md §6: a required scenario refuses to start
// without its own owned capture window, before any platform call.
func TestBehavioralOutcome_RequiredSharedCapture_RefusedBeforeMutation(t *testing.T) { // non-parallel: process environment
	cases := []struct {
		name    string
		capture *capture.Connection
		owned   bool
	}{
		{"no capture at all", nil, false},
		{"capture set but not owned", &capture.Connection{CaptureID: "inherited", ProxyURL: "http://127.0.0.1:1", SessionDir: t.TempDir()}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newBehavioralHarness(t)
			h.writeClaudeScript(t, defaultFakeClaudeDone)
			scenarioPath := h.writeScenario(t, requiredExpectedAppScenario("required-refused"))
			client := platform.NewMock()
			cfg := h.config()
			cfg.Capture = tc.capture
			cfg.CaptureOwned = tc.owned
			runner := NewRunner(cfg, nil, client, "offline-project")
			result, err := runner.RunBehavioralScenario(context.Background(), scenarioPath, "suite")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(result.Error, "capture") {
				t.Errorf("result.Error = %q, want mention of capture", result.Error)
			}
			for method, count := range client.CallCounts {
				if count != 0 {
					t.Errorf("platform call %s made %d times, want 0", method, count)
				}
			}
			outDir := h.outDir("required-refused")
			if got := listFiles(t, outDir); len(got) != 1 || got[0] != "meta.json" {
				t.Fatalf("result dir files = %v, want only meta.json", got)
			}
		})
	}
}

// TestBehavioralOutcome_RequiredFalseAssertion_Failed pins
// docs/spec-testing-architecture.md §10.1: a proven-false assertion is a
// failed row and a failed task result, even though the agent claimed
// success and exited 0.
func TestBehavioralOutcome_RequiredFalseAssertion_Failed(t *testing.T) { // non-parallel: process environment
	h := newBehavioralHarness(t)
	h.writeClaudeScript(t, defaultFakeClaudeDone)
	t.Setenv("RETRO_MARKER", filepath.Join(h.root, "retro-marker"))
	scenarioPath := h.writeScenario(t, requiredExpectedAppScenario("required-false-assertion"))
	client := platform.NewMock().WithServicesDirect([]platform.ServiceStack{{ID: "app-1", Name: "app", Status: "FAILED"}})
	cfg := h.config()
	cfg.Capture = &capture.Connection{CaptureID: "owned", ProxyURL: "http://127.0.0.1:1", SessionDir: t.TempDir()}
	cfg.CaptureOwned = true
	runner := NewRunner(cfg, nil, client, "offline-project")
	result, err := runner.RunBehavioralScenario(context.Background(), scenarioPath, "suite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty", result.Error)
	}
	if result.Task == nil || result.Task.Result != CheckFailed {
		t.Fatalf("Task = %+v, want failed", result.Task)
	}

	outDir := h.outDir("required-false-assertion")
	doc := readVerificationDocument(t, outDir)
	if doc.Result != CheckFailed {
		t.Fatalf("verification.json result = %q, want failed", doc.Result)
	}
	var found *RequiredCheck
	for i := range doc.Checks {
		if doc.Checks[i].ID == "expected_service/app/status" {
			found = &doc.Checks[i]
		}
	}
	if found == nil {
		t.Fatalf("checks = %+v, want a row for expected_service/app/status", doc.Checks)
	}
	if found.Result != CheckFailed || found.Expected != "[ACTIVE]" || found.Observed != "FAILED" {
		t.Errorf("row = %+v, want failed [ACTIVE] vs FAILED", found)
	}
	if client.CallCounts["DeleteService"] != 0 {
		t.Errorf("DeleteService called %d times, want 0 (required mode retains the project)", client.CallCounts["DeleteService"])
	}
}

// TestBehavioralOutcome_RequiredObservationUnavailable_Blocked pins
// docs/spec-testing-architecture.md §10.1/§10.2: evidence the runner
// couldn't obtain is a blocked row, never a silent pass, and a failed row
// next to a blocked one still yields a failed result.
func TestBehavioralOutcome_RequiredObservationUnavailable_Blocked(t *testing.T) { // non-parallel: process environment
	h := newBehavioralHarness(t)
	h.writeClaudeScript(t, defaultFakeClaudeDone)
	t.Setenv("RETRO_MARKER", filepath.Join(h.root, "retro-marker"))

	scenario := `---
id: required-blocked
seed: empty
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  expectedServices:
    - hostname: probeapp
      status: [ACTIVE]
      subdomainProbe:
        path: /health
        expectStatus: "200"
---
Finish the offline fixture application.
`
	scenarioPath := h.writeScenario(t, scenario)
	client := platform.NewMock()
	client.WithError("ListServicesDirect", fmt.Errorf("network timeout"))
	cfg := h.config()
	cfg.Capture = &capture.Connection{CaptureID: "owned", ProxyURL: "http://127.0.0.1:1", SessionDir: t.TempDir()}
	cfg.CaptureOwned = true
	runner := NewRunner(cfg, nil, client, "offline-project")
	guard := &noHTTPGuard{t: t}
	runner.httpDoer = guard
	result, err := runner.RunBehavioralScenario(context.Background(), scenarioPath, "suite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if guard.calls != 0 {
		t.Errorf("HTTP probe fired %d times, want 0 (ListServicesDirect failed — probe URL can't be resolved)", guard.calls)
	}
	if result.Task == nil || result.Task.Result != CheckBlocked {
		t.Fatalf("Task = %+v, want blocked (ListServicesDirect failed)", result.Task)
	}
	outDir := h.outDir("required-blocked")
	doc := readVerificationDocument(t, outDir)
	if len(doc.Checks) != 1 || doc.Checks[0].Result != CheckBlocked || doc.Checks[0].Source != "ListServicesDirect" {
		t.Fatalf("checks = %+v, want one blocked row sourced from ListServicesDirect", doc.Checks)
	}
}

// TestBehavioralOutcome_SimulatorOrLiveProcessUnsettled_NotPassed pins
// docs/spec-testing-architecture.md §10.2 step 3: a live process created
// after scenario start that never settles within TaskEndSettle blocks the
// result, and the simulator's termination is recorded regardless.
func TestBehavioralOutcome_SimulatorOrLiveProcessUnsettled_NotPassed(t *testing.T) { // non-parallel: process environment
	h := newBehavioralHarness(t)
	h.writeClaudeScript(t, defaultFakeClaudeDone)
	t.Setenv("RETRO_MARKER", filepath.Join(h.root, "retro-marker"))
	scenarioPath := h.writeScenario(t, requiredExpectedAppScenario("required-unsettled"))

	client := platform.NewMock().
		WithServicesDirect([]platform.ServiceStack{{ID: "app-1", Name: "app", Status: "ACTIVE"}}).
		WithProjectProcesses([]platform.Process{
			{ID: "p-live", ActionName: "stack.build", Status: platform.ProcessStatusRunning, Created: time.Now().Add(time.Minute).Format(time.RFC3339)},
		})
	cfg := h.config()
	cfg.Capture = &capture.Connection{CaptureID: "owned", ProxyURL: "http://127.0.0.1:1", SessionDir: t.TempDir()}
	cfg.CaptureOwned = true
	runner := NewRunner(cfg, nil, client, "offline-project")
	result, err := runner.RunBehavioralScenario(context.Background(), scenarioPath, "suite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TaskEnd == nil || result.TaskEnd.Settled {
		t.Fatalf("TaskEnd = %+v, want unsettled", result.TaskEnd)
	}
	found := false
	for _, id := range result.TaskEnd.LiveProcesses {
		if id == "p-live" {
			found = true
		}
	}
	if !found {
		t.Errorf("LiveProcesses = %v, want p-live", result.TaskEnd.LiveProcesses)
	}
	if result.Task == nil || result.Task.Result != CheckBlocked {
		t.Fatalf("Task = %+v, want blocked (unsettled)", result.Task)
	}
	if result.TaskEnd.Simulator.TerminatedBy == "" {
		t.Errorf("Simulator.TerminatedBy empty, want the user-sim loop's termination reason recorded")
	}
}

// TestBehavioralOutcome_TaskEndPrecedesRetrospective_Frozen pins
// docs/spec-testing-architecture.md §10.2: the deciding platform read
// happens before the retrospective's --resume call, and verification.json's
// digest is unchanged by the retrospective.
func TestBehavioralOutcome_TaskEndPrecedesRetrospective_Frozen(t *testing.T) { // non-parallel: process environment
	h := newBehavioralHarness(t)
	retroMarker := filepath.Join(h.root, "retro-marker")
	t.Setenv("RETRO_MARKER", retroMarker)
	h.writeClaudeScript(t, defaultFakeClaudeDone)
	scenarioPath := h.writeScenario(t, requiredExpectedAppScenario("required-order"))

	client := &orderProbeClient{Mock: platform.NewMock().WithServicesDirect([]platform.ServiceStack{{ID: "app-1", Name: "app", Status: "ACTIVE"}}), retroMarker: retroMarker}
	cfg := h.config()
	cfg.Capture = &capture.Connection{CaptureID: "owned", ProxyURL: "http://127.0.0.1:1", SessionDir: t.TempDir()}
	cfg.CaptureOwned = true
	runner := NewRunner(cfg, nil, client, "offline-project")
	result, err := runner.RunBehavioralScenario(context.Background(), scenarioPath, "suite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.directReads != 1 || client.readBeforeRetroMarker != true {
		t.Fatalf("directReads=%d readBeforeRetroMarker=%v, want exactly one read that happened before --resume wrote its marker", client.directReads, client.readBeforeRetroMarker)
	}

	outDir := h.outDir("required-order")
	frozen, err := os.ReadFile(filepath.Join(outDir, "verification.json"))
	if err != nil {
		t.Fatalf("read verification.json: %v", err)
	}
	// The retrospective already ran by the time RunBehavioralScenario
	// returned. Re-reading verification.json now must still match the bytes
	// this call already returned — nothing rewrites it after the freeze.
	sum := sha256.Sum256(frozen)
	again, err := os.ReadFile(filepath.Join(outDir, "verification.json"))
	if err != nil {
		t.Fatalf("re-read verification.json: %v", err)
	}
	if sha256.Sum256(again) != sum {
		t.Fatalf("verification.json bytes changed across reads")
	}
	if result.Task == nil || result.Task.Result != CheckPassed {
		t.Fatalf("Task = %+v, want passed", result.Task)
	}
}

// orderProbeClient records whether its one direct ListServicesDirect call
// happened before the retrospective's --resume marker file existed.
type orderProbeClient struct {
	*platform.Mock
	retroMarker           string
	directReads           int
	readBeforeRetroMarker bool
}

func (c *orderProbeClient) ListServicesDirect(ctx context.Context, projectID string) ([]platform.ServiceStack, error) {
	c.directReads++
	_, err := os.Stat(c.retroMarker)
	c.readBeforeRetroMarker = os.IsNotExist(err)
	return c.Mock.ListServicesDirect(ctx, projectID)
}

// TestBehavioralOutcome_OptionalRetrospectiveFailure_DoesNotRewriteTask pins
// docs/spec-testing-architecture.md §10.2: a retrospective failure lands in
// Error only — Task.Result and the frozen verification.json are untouched.
func TestBehavioralOutcome_OptionalRetrospectiveFailure_DoesNotRewriteTask(t *testing.T) { // non-parallel: process environment
	h := newBehavioralHarness(t)
	script := `#!/bin/sh
if [ "$1" = "--resume" ]; then
    exit 1
fi
printf '%s\n' '{"type":"system","subtype":"init","session_id":"offline-probe","model":"fake-offline"}'
printf '%s\n' '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Done."}]}}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"offline-probe","result":"Done."}'
`
	h.writeClaudeScript(t, script)
	scenarioPath := h.writeScenario(t, requiredExpectedAppScenario("required-retro-fails"))
	client := platform.NewMock().WithServicesDirect([]platform.ServiceStack{{ID: "app-1", Name: "app", Status: "ACTIVE"}})
	cfg := h.config()
	cfg.Capture = &capture.Connection{CaptureID: "owned", ProxyURL: "http://127.0.0.1:1", SessionDir: t.TempDir()}
	cfg.CaptureOwned = true
	runner := NewRunner(cfg, nil, client, "offline-project")
	result, err := runner.RunBehavioralScenario(context.Background(), scenarioPath, "suite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Error, "retrospective") {
		t.Errorf("result.Error = %q, want mention of retrospective", result.Error)
	}
	if result.Task == nil || result.Task.Result != CheckPassed {
		t.Fatalf("Task = %+v, want passed (untouched by retrospective failure)", result.Task)
	}

	outDir := h.outDir("required-retro-fails")
	frozenBytes, err := os.ReadFile(filepath.Join(outDir, "verification.json"))
	if err != nil {
		t.Fatalf("read verification.json: %v", err)
	}
	metaData, err := os.ReadFile(filepath.Join(outDir, "meta.json"))
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	var meta BehavioralResult
	if err := json.Unmarshal(metaData, &meta); err != nil {
		t.Fatalf("decode meta.json: %v", err)
	}
	if meta.Task == nil || meta.Task.Result != CheckPassed {
		t.Fatalf("meta.json task = %+v, want passed", meta.Task)
	}
	var frozenDoc VerificationDocument
	if err := json.Unmarshal(frozenBytes, &frozenDoc); err != nil {
		t.Fatalf("decode verification.json: %v", err)
	}
	if frozenDoc.Result != CheckPassed {
		t.Fatalf("verification.json result = %q, want passed", frozenDoc.Result)
	}
}

// blockedWritesClient is unused directly; verification.json/platform-
// snapshot.json/meta.json are pre-created as directories by the test so the
// atomic-rename writer fails.

// TestBehavioralOutcome_PersistenceFailure_RetainsEvidenceAndNoCleanup pins
// docs/spec-testing-architecture.md §10.2 step 5: a persistence failure
// downgrades a would-be passed result to blocked, retains whatever was
// already written, and never triggers cleanup.
func TestBehavioralOutcome_PersistenceFailure_RetainsEvidenceAndNoCleanup(t *testing.T) { // non-parallel: process environment
	h := newBehavioralHarness(t)
	h.writeClaudeScript(t, defaultFakeClaudeDone)
	t.Setenv("RETRO_MARKER", filepath.Join(h.root, "retro-marker"))
	scenarioPath := h.writeScenario(t, requiredExpectedAppScenario("required-persist-fails"))
	client := platform.NewMock().WithServicesDirect([]platform.ServiceStack{{ID: "app-1", Name: "app", Status: "ACTIVE"}})
	cfg := h.config()
	cfg.Capture = &capture.Connection{CaptureID: "owned", ProxyURL: "http://127.0.0.1:1", SessionDir: t.TempDir()}
	cfg.CaptureOwned = true
	runner := NewRunner(cfg, nil, client, "offline-project")

	outDir := h.outDir("required-persist-fails")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-create verification.json as a directory so the atomic writer's
	// rename fails — platform-snapshot.json is left as a normal file so we
	// can assert it survives the verification.json failure.
	if err := os.Mkdir(filepath.Join(outDir, "verification.json"), 0o700); err != nil {
		t.Fatal(err)
	}

	result, err := runner.RunBehavioralScenario(context.Background(), scenarioPath, "suite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TaskEnd == nil || result.TaskEnd.Persisted {
		t.Fatalf("TaskEnd = %+v, want Persisted=false", result.TaskEnd)
	}
	if result.TaskEnd.PersistError == "" {
		t.Error("PersistError empty, want a message")
	}
	if result.Task == nil || result.Task.Result != CheckBlocked {
		t.Fatalf("Task = %+v, want blocked (would-be passed downgraded by persistence failure)", result.Task)
	}
	if _, err := os.Stat(filepath.Join(outDir, "platform-snapshot.json")); err != nil {
		t.Errorf("platform-snapshot.json missing after verification.json persist failure: %v", err)
	}
	if client.CallCounts["DeleteService"] != 0 {
		t.Errorf("DeleteService called %d times, want 0", client.CallCounts["DeleteService"])
	}
}

// TestBehavioralOutcome_ObserveMode_KeepsExecutionOnlyResultAndCleanup pins
// docs/spec-testing-architecture.md §4/§10.1 point 8: a legacy scenario (no
// verification.mode) keeps the execution-only Error semantics and its
// post-retrospective cleanup, while still recording the observe-mode
// Task/TaskEnd dimensions and the new single-object verification.json.
func TestBehavioralOutcome_ObserveMode_KeepsExecutionOnlyResultAndCleanup(t *testing.T) { // non-parallel: process environment
	h := newBehavioralHarness(t)
	retroMarker := filepath.Join(h.root, "retro-marker")
	t.Setenv("RETRO_MARKER", retroMarker)
	h.writeClaudeScript(t, defaultFakeClaudeDone)

	scenario := `---
id: observe-legacy
seed: empty
retrospective:
  promptStyle: briefing-future-agent
verification:
  expectedServices:
    - hostname: app
      status: [ACTIVE]
---
Finish the offline fixture application.
`
	scenarioPath := h.writeScenario(t, scenario)
	client := &deleteTrackingClient{Mock: platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "app-1", Name: "app", Status: "FAILED"}}).
		WithServicesDirect([]platform.ServiceStack{{ID: "app-1", Name: "app", Status: "FAILED"}}).
		WithProcess(&platform.Process{ID: "proc-delete-app-1", ActionName: "delete", Status: platform.ProcessStatusFinished}).
		WithDeleteRemovesService(true), retroMarker: retroMarker}
	cfg := h.config()
	runner := NewRunner(cfg, nil, client, "offline-project")
	result, err := runner.RunBehavioralScenario(context.Background(), scenarioPath, "suite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("result.Error = %q, want empty (observe mode never gates on rows)", result.Error)
	}
	if result.Task == nil || result.Task.Mode != VerificationObserve {
		t.Fatalf("Task = %+v, want Mode=observe", result.Task)
	}

	outDir := h.outDir("observe-legacy")
	doc := readVerificationDocument(t, outDir)
	if doc.Result != CheckFailed {
		t.Fatalf("verification.json result = %q, want failed", doc.Result)
	}
	foundAdvisory := false
	for _, f := range doc.Advisory {
		if f.Severity == "fail" && f.Check == "service_status" {
			foundAdvisory = true
		}
	}
	if !foundAdvisory {
		t.Errorf("advisory = %+v, want a fail service_status finding", doc.Advisory)
	}
	if !client.listServicesCalledAfterMarker {
		t.Fatal("cleanup (ListServices) never ran after the retrospective marker existed")
	}
	if _, err := os.Stat(retroMarker); err != nil {
		t.Fatal("retrospective marker missing — --resume never ran")
	}
}

// deleteTrackingClient records whether ListServices ran after the
// retrospective marker existed, so observe-mode's "cleanup after
// retrospective" ordering is independently verifiable (CleanupProject lists
// services before it deletes any).
type deleteTrackingClient struct {
	*platform.Mock
	retroMarker                   string
	listServicesCalledAfterMarker bool
}

func (c *deleteTrackingClient) ListServices(ctx context.Context, projectID string) ([]platform.ServiceStack, error) {
	if _, err := os.Stat(c.retroMarker); err == nil {
		c.listServicesCalledAfterMarker = true
	}
	return c.Mock.ListServices(ctx, projectID)
}

// TestBehavioralOutcome_MetaPersistenceFailure_Blocked pins the meta.json
// half of docs/spec-testing-architecture.md §10.2 step 5: meta.json is a
// persisted task-end artifact, so a failure to write it is a persistence
// failure — Persisted=false, a would-be passed result reads blocked, and the
// already-written verification.json / platform-snapshot.json are retained.
func TestBehavioralOutcome_MetaPersistenceFailure_Blocked(t *testing.T) { // non-parallel: process environment
	h := newBehavioralHarness(t)
	h.writeClaudeScript(t, defaultFakeClaudeDone)
	t.Setenv("RETRO_MARKER", filepath.Join(h.root, "retro-marker"))
	scenarioPath := h.writeScenario(t, requiredExpectedAppScenario("required-meta-fails"))
	client := platform.NewMock().WithServicesDirect([]platform.ServiceStack{{ID: "app-1", Name: "app", Status: "ACTIVE"}})
	cfg := h.config()
	cfg.Capture = &capture.Connection{CaptureID: "owned", ProxyURL: "http://127.0.0.1:1", SessionDir: t.TempDir()}
	cfg.CaptureOwned = true
	runner := NewRunner(cfg, nil, client, "offline-project")

	outDir := h.outDir("required-meta-fails")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(outDir, "meta.json"), 0o700); err != nil {
		t.Fatal(err)
	}

	result, err := runner.RunBehavioralScenario(context.Background(), scenarioPath, "suite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TaskEnd == nil || result.TaskEnd.Persisted || result.TaskEnd.PersistError == "" {
		t.Fatalf("TaskEnd = %+v, want Persisted=false with a PersistError naming meta.json", result.TaskEnd)
	}
	if result.Task == nil || result.Task.Result != CheckBlocked {
		t.Fatalf("Task = %+v, want blocked", result.Task)
	}
	for _, name := range []string{"verification.json", "platform-snapshot.json"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Errorf("%s missing after meta.json persist failure: %v", name, err)
		}
	}
	if client.CallCounts["DeleteService"] != 0 {
		t.Errorf("DeleteService called %d times, want 0", client.CallCounts["DeleteService"])
	}
}
