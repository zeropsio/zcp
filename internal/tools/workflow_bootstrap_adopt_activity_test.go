package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// adoptActivityHarness opens an adopt-route bootstrap session against a mock
// seeded with services (WITH ids), the DIRECT project process list (for
// ProjectActivity), and GetProcess records (for the gate's freshen). Returns the
// server + the state dir (to assert no meta was written on refusal).
func adoptActivityHarness(t *testing.T, services []platform.ServiceStack, projectProcs []platform.Process, getProcs []*platform.Process) (*mcp.Server, string) {
	t.Helper()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	if _, err := engine.BootstrapStartWithRoute("proj1", "adopt existing for a dashboard", workflow.BootstrapRouteAdopt, ""); err != nil {
		t.Fatalf("BootstrapStartWithRoute(adopt): %v", err)
	}
	client := platform.NewMock().
		WithServices(services).
		WithProjectProcesses(projectProcs)
	for _, p := range getProcs {
		client.WithProcess(p)
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, client, nil, "proj1", nil, engine, nil, dir, "", nil, nil, runtime.Info{}, "")
	return srv, dir
}

func svcID(id, name, typeVersion string) platform.ServiceStack {
	return platform.ServiceStack{
		ID:                   id,
		Name:                 name,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: typeVersion},
	}
}

// liveBuild returns the live-verified in-flight shape via the DIRECT source: a
// stack.build process RUNNING referencing the target + ephemeral build container,
// carrying the embedded BUILDING appVersion phase — plus the GetProcess record
// the gate's freshen confirms.
func liveBuild(targetID, hostname, procID string) ([]platform.Process, *platform.Process) {
	p := platform.Process{
		ID: procID,
		ServiceStacks: []platform.ServiceStackRef{
			{ID: targetID, Name: hostname},
			{ID: "build-ephemeral-" + targetID, Name: "build" + hostname},
		},
		ActionName: "stack.build",
		Status:     platform.ProcessStatusRunning,
		Created:    "2026-06-30T09:01:34Z",
		AppVersion: &platform.ProcessAppVersion{Status: platform.BuildStatusBuilding},
	}
	return []platform.Process{p}, &platform.Process{ID: procID, Status: platform.ProcessStatusRunning, ActionName: "stack.build"}
}

// TestAdoptGate_BusyScopeTarget_Refuses pins the gate on the scope path: an
// adopt target with a live build is refused, naming the processId + watch/cancel
// guidance, and NO ServiceMeta is written (the dispatch never ran).
func TestAdoptGate_BusyScopeTarget_Refuses(t *testing.T) {
	t.Parallel()
	procs, proc := liveBuild("id-appdev", "appdev", "build-proc-1")
	srv, dir := adoptActivityHarness(t,
		[]platform.ServiceStack{
			svcID("id-appdev", "appdev", "nodejs@22"),
			svcID("id-db", "db", "postgresql@16"),
		}, procs, []*platform.Process{proc})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"scope":  []any{"appdev"},
	})
	if !result.IsError {
		t.Fatalf("busy adopt target must be refused; got success: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	for _, want := range []string{"appdev", "build-proc-1", "zerops_events", "cancel"} {
		if !strings.Contains(text, want) {
			t.Errorf("busy refusal missing %q; got: %s", want, text)
		}
	}
	if metas, _ := workflow.ListServiceMetas(dir); len(metas) != 0 {
		t.Errorf("no ServiceMeta may be written on a refused adopt; got %d", len(metas))
	}
}

// TestAdoptGate_BusyPlanTarget_Refuses pins that the gate covers the EXPLICIT
// plan=[...] dispatch path (the reported recipe pair flow), not just scope: a
// busy STAGE half named only in the plan must refuse.
func TestAdoptGate_BusyPlanTarget_Refuses(t *testing.T) {
	t.Parallel()
	procs, proc := liveBuild("id-appstage", "appstage", "build-proc-stage")
	srv, dir := adoptActivityHarness(t,
		[]platform.ServiceStack{
			svcID("id-appdev", "appdev", "nodejs@22"),
			svcID("id-appstage", "appstage", "nodejs@22"),
		}, procs, []*platform.Process{proc})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"plan": []any{map[string]any{
			"runtime": map[string]any{
				"devHostname":   "appdev",
				"stageHostname": "appstage",
				"type":          "nodejs@22",
				"bootstrapMode": "standard",
				"isExisting":    true,
			},
		}},
	})
	if !result.IsError {
		t.Fatalf("busy plan stage target must be refused; got: %s", getTextContent(t, result))
	}
	if text := getTextContent(t, result); !strings.Contains(text, "appstage") || !strings.Contains(text, "build-proc-stage") {
		t.Errorf("plan-path refusal must name the busy stage + its processId; got: %s", text)
	}
	if metas, _ := workflow.ListServiceMetas(dir); len(metas) != 0 {
		t.Errorf("no ServiceMeta may be written on a refused adopt; got %d", len(metas))
	}
}

// TestAdoptGate_TerminalTarget_Proceeds pins the loop-safety invariant: a target
// whose build FAILED (process terminal, appVersion frozen WAITING_TO_BUILD) is
// NOT busy, so adopt + corrective deploy proceed — recovery is never gated.
func TestAdoptGate_TerminalTarget_Proceeds(t *testing.T) {
	t.Parallel()
	procs := []platform.Process{{
		ID:            "failed-build",
		ServiceStacks: []platform.ServiceStackRef{{ID: "id-appdev", Name: "appdev"}},
		ActionName:    "stack.build",
		Status:        platform.ProcessStatusFailed,
		Created:       "2026-06-30T09:04:27Z",
		AppVersion:    &platform.ProcessAppVersion{Status: "WAITING_TO_BUILD"}, // frozen
	}}
	srv, _ := adoptActivityHarness(t,
		[]platform.ServiceStack{svcID("id-appdev", "appdev", "nodejs@22")},
		procs, nil)

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover", "scope": []any{"appdev"},
	})
	if result.IsError {
		t.Fatalf("a FAILED/terminal target must NOT be gated (recovery never gated); got: %s", getTextContent(t, result))
	}
	if text := getTextContent(t, result); !strings.Contains(text, "Auto-derived") {
		t.Errorf("terminal target should adopt (auto-derive); got: %s", text)
	}
}

// TestAdoptGate_StaleSearchRow_Proceeds pins the F4 fix: SearchProcesses returns
// a stale RUNNING row, but GetProcess (authoritative + current) reports the
// process FINISHED — the gate freshens and opens, so adopt is not deadlocked.
func TestAdoptGate_StaleSearchRow_Proceeds(t *testing.T) {
	t.Parallel()
	procs, _ := liveBuild("id-appdev", "appdev", "stale-proc")
	// GetProcess says the process is actually FINISHED (search row is stale).
	freshTerminal := &platform.Process{ID: "stale-proc", Status: platform.ProcessStatusFinished, ActionName: "stack.build"}
	srv, _ := adoptActivityHarness(t,
		[]platform.ServiceStack{svcID("id-appdev", "appdev", "nodejs@22")},
		procs, []*platform.Process{freshTerminal})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover", "scope": []any{"appdev"},
	})
	if result.IsError {
		t.Fatalf("stale RUNNING search row + FINISHED GetProcess must NOT deadlock adopt; got: %s", getTextContent(t, result))
	}
}

// TestAdoptGate_IdleTarget_Proceeds pins that an idle target is not gated (no
// activity → normal auto-derive).
func TestAdoptGate_IdleTarget_Proceeds(t *testing.T) {
	t.Parallel()
	srv, _ := adoptActivityHarness(t,
		[]platform.ServiceStack{
			svcID("id-appdev", "appdev", "nodejs@22"),
			svcID("id-db", "db", "postgresql@16"),
		}, nil, nil)

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover", "scope": []any{"appdev"},
	})
	if result.IsError {
		t.Fatalf("idle target must adopt normally; got: %s", getTextContent(t, result))
	}
	if text := getTextContent(t, result); !strings.Contains(text, "Auto-derived") {
		t.Errorf("idle target should auto-derive; got: %s", text)
	}
}
