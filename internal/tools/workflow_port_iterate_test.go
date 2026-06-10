package tools

import (
	"os"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// portTestServer registers the workflow tool against a fresh state dir and
// returns the server + dir. Non-parallel callers only: the PortSession is keyed
// by os.Getpid(), so two parallel tests in the SAME process sharing a stateDir
// would collide — each test uses its own t.TempDir() to stay isolated.
func portTestServer(t *testing.T) (*mcp.Server, string) {
	t.Helper()
	dir := t.TempDir()
	cache := schema.NewCache(15*time.Minute, "", nil)
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, nil, nil, "proj1", cache, engine, nil, dir, "", nil, nil, runtime.Info{InContainer: true})
	return srv, dir
}

func startPort(t *testing.T, srv *mcp.Server) {
	t.Helper()
	res := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "port",
		"portTarget": map[string]any{
			"name":            "strapi",
			"acquisitionHint": "source-repo",
			"dependencies":    []any{"postgresql"},
			"runtimes":        []any{"nodejs@22"},
		},
	})
	if res.IsError {
		t.Fatalf("port start failed: %s", getTextContent(t, res))
	}
}

// TestPortIterate_Failure_DerivesFixClassAndRecordsAttempt is the iterate
// round-trip: an agent-observed failure goes in, fix-class guidance comes out,
// and the PortSession attempt history records the class + signals + derived fix.
func TestPortIterate_Failure_DerivesFixClassAndRecordsAttempt(t *testing.T) {
	srv, dir := portTestServer(t)
	startPort(t, srv)

	res := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":           "iterate",
		"workflow":         "port",
		"portFailureClass": string(topology.FailureClassBuild),
		"portSignals":      []any{"build:command-not-found"},
	})
	if res.IsError {
		t.Fatalf("port iterate failed: %s", getTextContent(t, res))
	}
	body := decodePortJSON(t, res)
	if body["status"] != "port-iterate" {
		t.Errorf("status: got %v", body["status"])
	}
	fix, ok := body["fixClass"].(map[string]any)
	if !ok {
		t.Fatalf("expected fixClass object, got %T", body["fixClass"])
	}
	if fix["kind"] != string(workflow.FixAddPrepareCommands) {
		t.Errorf("fixClass.kind: got %v want %v", fix["kind"], workflow.FixAddPrepareCommands)
	}

	// The attempt must be persisted on the PortSession (Phase 2 reads this).
	ps, err := workflow.LoadPortSession(dir, os.Getpid())
	if err != nil || ps == nil {
		t.Fatalf("load port session: err=%v ps=%v", err, ps)
	}
	if len(ps.Attempts) != 1 {
		t.Fatalf("expected 1 recorded attempt, got %d", len(ps.Attempts))
	}
	a := ps.Attempts[0]
	if a.Class != topology.FailureClassBuild {
		t.Errorf("attempt class: got %v", a.Class)
	}
	if len(a.Signals) != 1 || a.Signals[0] != "build:command-not-found" {
		t.Errorf("attempt signals not persisted: %v", a.Signals)
	}
	if a.FixKind != workflow.FixAddPrepareCommands {
		t.Errorf("attempt fixKind: got %v", a.FixKind)
	}
	if a.Iteration != 1 {
		t.Errorf("attempt iteration: got %d want 1", a.Iteration)
	}
}

// TestPortIterate_OOMBuild_FlagsEscalation pins the §12 correction: a build OOM
// is an escalation trigger, not a fix.
func TestPortIterate_OOMBuild_FlagsEscalation(t *testing.T) {
	srv, dir := portTestServer(t)
	startPort(t, srv)

	res := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":           "iterate",
		"workflow":         "port",
		"portFailureClass": string(topology.FailureClassBuild),
		"portSignals":      []any{"build:oom-killed"},
	})
	if res.IsError {
		t.Fatalf("port iterate failed: %s", getTextContent(t, res))
	}
	body := decodePortJSON(t, res)
	fix := body["fixClass"].(map[string]any)
	if fix["kind"] != string(workflow.FixEscalate) {
		t.Errorf("oom build should escalate, got kind=%v", fix["kind"])
	}
	if fix["escalate"] != true {
		t.Errorf("oom build should set escalate=true, got %v", fix["escalate"])
	}

	ps, _ := workflow.LoadPortSession(dir, os.Getpid())
	if !ps.Attempts[0].Escalate {
		t.Errorf("recorded attempt must carry Escalate=true")
	}
}

// TestPortIterate_ImportOverride_FlagsTax pins the §11 budget: an import edit to
// an existing hostname surfaces the override tax.
func TestPortIterate_ImportOverride_FlagsTax(t *testing.T) {
	srv, _ := portTestServer(t)
	startPort(t, srv)

	res := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":             "iterate",
		"workflow":           "port",
		"portFailureClass":   string(topology.FailureClassStart),
		"portSignals":        []any{"init:db-connection-refused"},
		"portImportOverride": true,
	})
	if res.IsError {
		t.Fatalf("port iterate failed: %s", getTextContent(t, res))
	}
	body := decodePortJSON(t, res)
	fix := body["fixClass"].(map[string]any)
	if fix["requiresImportOverride"] != true {
		t.Errorf("import-override iterate must flag requiresImportOverride, got %v", fix["requiresImportOverride"])
	}
}

// TestPortIterate_Success_RecordsAndAdvances covers the success branch.
func TestPortIterate_Success_RecordsAndAdvances(t *testing.T) {
	srv, dir := portTestServer(t)
	startPort(t, srv)

	res := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":              "iterate",
		"workflow":            "port",
		"portDeploySucceeded": true,
		"targetService":       "strapi",
	})
	if res.IsError {
		t.Fatalf("port iterate (success) failed: %s", getTextContent(t, res))
	}
	body := decodePortJSON(t, res)
	if body["status"] != "port-iterate" {
		t.Errorf("status: got %v", body["status"])
	}

	ps, _ := workflow.LoadPortSession(dir, os.Getpid())
	if len(ps.Attempts) != 1 || !ps.Attempts[0].Succeeded {
		t.Fatalf("expected one succeeded attempt, got %+v", ps.Attempts)
	}
}

// TestPortIterate_MissingClass_Errors guards the failure-path precondition.
func TestPortIterate_MissingClass_Errors(t *testing.T) {
	srv, _ := portTestServer(t)
	startPort(t, srv)

	res := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "iterate",
		"workflow": "port",
	})
	if !res.IsError {
		t.Fatalf("iterate without failure class or success flag must error")
	}
}

// TestPortIterate_NoSession_Errors guards loading a non-existent session.
func TestPortIterate_NoSession_Errors(t *testing.T) {
	srv, _ := portTestServer(t)
	// No startPort — no session for this PID/stateDir.
	res := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":           "iterate",
		"workflow":         "port",
		"portFailureClass": string(topology.FailureClassBuild),
	})
	if !res.IsError {
		t.Fatalf("iterate without an active port session must error")
	}
}

// TestPortIterate_IterationCap_ClosesAndStops drives the EASY cap (4): the
// fourth failing turn returns a stop decision with the iteration-cap terminator
// and closes the wrapped work session with CloseReasonIterationCap.
func TestPortIterate_IterationCap_ClosesAndStops(t *testing.T) {
	srv, dir := portTestServer(t)
	startPort(t, srv)

	var body map[string]any
	for range 4 {
		res := callTool(t, srv, "zerops_workflow", map[string]any{
			"action":           "iterate",
			"workflow":         "port",
			"portFailureClass": string(topology.FailureClassVerify),
		})
		if res.IsError {
			t.Fatalf("iterate failed: %s", getTextContent(t, res))
		}
		body = decodePortJSON(t, res)
	}

	if body["stop"] != true {
		t.Fatalf("expected stop=true at cap, got %v (body=%v)", body["stop"], body)
	}
	if body["terminator"] != string(workflow.PortTermIterationCap) {
		t.Errorf("terminator: got %v want %v", body["terminator"], workflow.PortTermIterationCap)
	}

	ps, _ := workflow.LoadPortSession(dir, os.Getpid())
	if ps.WorkSession == nil || ps.WorkSession.CloseReason != workflow.CloseReasonIterationCap {
		t.Errorf("wrapped work session must close with CloseReasonIterationCap, got %+v", ps.WorkSession)
	}
}

// TestPortIterate_BuildStallEscalates: three build-class failures with a
// prebuilt URL present escalate the acquisition strategy to prebuilt-binary and
// re-budget the loop.
func TestPortIterate_BuildStallEscalates(t *testing.T) {
	srv, dir := portTestServer(t)
	// Start with a prebuilt URL so T1 is available.
	res := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "start",
		"workflow": "port",
		"portTarget": map[string]any{
			"name":            "strapi",
			"acquisitionHint": "source-repo",
			"prebuiltUrl":     "https://example.com/app.tar.gz",
			"dependencies":    []any{"postgresql"},
			"runtimes":        []any{"nodejs@22"},
		},
	})
	if res.IsError {
		t.Fatalf("port start failed: %s", getTextContent(t, res))
	}

	var body map[string]any
	for range 3 {
		res := callTool(t, srv, "zerops_workflow", map[string]any{
			"action":           "iterate",
			"workflow":         "port",
			"portFailureClass": string(topology.FailureClassBuild),
			"portSignals":      []any{"build:linker-error"},
		})
		if res.IsError {
			t.Fatalf("iterate failed: %s", getTextContent(t, res))
		}
		body = decodePortJSON(t, res)
	}

	esc, ok := body["escalation"].(map[string]any)
	if !ok {
		t.Fatalf("expected escalation object on build-stall turn, got %T (%v)", body["escalation"], body)
	}
	if esc["tier"] != string(workflow.PortEscalateT1) {
		t.Errorf("escalation tier: got %v want %v", esc["tier"], workflow.PortEscalateT1)
	}

	ps, _ := workflow.LoadPortSession(dir, os.Getpid())
	if ps.Plan.Acquisition != workflow.AcquirePrebuiltBinary {
		t.Errorf("plan acquisition should switch to prebuilt-binary, got %v", ps.Plan.Acquisition)
	}
	if ps.RebudgetOrigin == 0 {
		t.Errorf("T1 escalation must set RebudgetOrigin, got 0")
	}
}

// TestPortIterate_BuildStallNoPrebuilt_Bails: three build-class failures with NO
// prebuilt URL bail (T2) with a stop decision.
func TestPortIterate_BuildStallNoPrebuilt_Bails(t *testing.T) {
	srv, _ := portTestServer(t)
	startPort(t, srv) // no prebuilt URL

	var body map[string]any
	for range 3 {
		res := callTool(t, srv, "zerops_workflow", map[string]any{
			"action":           "iterate",
			"workflow":         "port",
			"portFailureClass": string(topology.FailureClassBuild),
			"portSignals":      []any{"build:linker-error"},
		})
		if res.IsError {
			t.Fatalf("iterate failed: %s", getTextContent(t, res))
		}
		body = decodePortJSON(t, res)
	}

	if body["stop"] != true {
		t.Fatalf("build stall with no prebuilt should bail (stop=true), got %v", body)
	}
	if body["terminator"] != string(workflow.PortTermEscalationBail) {
		t.Errorf("terminator: got %v want %v", body["terminator"], workflow.PortTermEscalationBail)
	}
}

// TestPortStatus_ReturnsActiveEnvelope covers OQ-5: status on a live
// PortSession returns a coherent port-active recovery envelope (not idle).
func TestPortStatus_ReturnsActiveEnvelope(t *testing.T) {
	srv, _ := portTestServer(t)
	startPort(t, srv)

	// Record one failing turn so the envelope surfaces a last attempt.
	res := callTool(t, srv, "zerops_workflow", map[string]any{
		"action":           "iterate",
		"workflow":         "port",
		"portFailureClass": string(topology.FailureClassStart),
		"portSignals":      []any{"init:missing-env-var"},
	})
	if res.IsError {
		t.Fatalf("iterate failed: %s", getTextContent(t, res))
	}

	res = callTool(t, srv, "zerops_workflow", map[string]any{
		"action":   "status",
		"workflow": "port",
	})
	if res.IsError {
		t.Fatalf("port status failed: %s", getTextContent(t, res))
	}
	body := decodePortJSON(t, res)
	if body["kind"] != "port-active" {
		t.Errorf("status kind: got %v want port-active", body["kind"])
	}
	if body["target"] != "strapi" {
		t.Errorf("status target: got %v", body["target"])
	}
	if body["phase"] != string(workflow.PhasePortActive) {
		t.Errorf("status phase: got %v", body["phase"])
	}
	last, ok := body["lastAttempt"].(map[string]any)
	if !ok {
		t.Fatalf("expected lastAttempt in envelope, got %T", body["lastAttempt"])
	}
	if last["fixKind"] != string(workflow.FixSetEnv) {
		t.Errorf("lastAttempt.fixKind: got %v want %v", last["fixKind"], workflow.FixSetEnv)
	}
	if body["nextCall"] == "" {
		t.Errorf("status must carry a nextCall")
	}
}
