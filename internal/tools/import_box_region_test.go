// Tests for: import.go + workflow_bootstrap.go — a Zerops Box exists only in a
// project in region prg1, so zerops_import and a classic bootstrap plan read
// the project's location once — only when they carry a Box — and refuse
// before anything is created or planned outside prg1 (docs/spec-workflows.md
// §8 PS-16).

package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// regionProject is a project record as the live API answers it
// (primaryInstanceLocation, live-verified 2026-10-24).
func regionProject(region string) *platform.Project {
	if region == "prg1" {
		return &platform.Project{ID: "proj-1", Name: "lab", LocationID: "eu-central", LocationName: "EU Central (prg1)"}
	}
	return &platform.Project{ID: "proj-1", Name: "lab", LocationID: "us-east-1", LocationName: "US East (ny1)"}
}

// regionImportMock answers an import of box + api with both creates FINISHED.
func regionImportMock(region string) *platform.Mock {
	return platform.NewMock().
		WithProject(regionProject(region)).
		WithImportResult(&platform.ImportResult{
			ProjectID: "proj-1", ProjectName: "lab",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-box", Name: "box", Processes: []platform.Process{{ID: "p-box", ActionName: "stack.create", Status: "PENDING"}}},
				{ID: "svc-api", Name: "api", Processes: []platform.Process{{ID: "p-api", ActionName: "stack.create", Status: "PENDING"}}},
			},
		}).
		WithProcess(&platform.Process{ID: "p-box", ActionName: "stack.create", Status: statusFinished}).
		WithProcess(&platform.Process{ID: "p-api", ActionName: "stack.create", Status: statusFinished})
}

func TestImportTool_BoxOutsidePrg1_RefusedBeforeImport(t *testing.T) {
	t.Parallel()
	for _, boxType := range []string{"ubuntu/zbox@1", "zbox@1"} {
		t.Run(boxType, func(t *testing.T) {
			t.Parallel()
			mock := regionImportMock("ny1")
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{})

			yaml := "services:\n  - hostname: box\n    type: " + boxType + "\n  - hostname: api\n    type: nodejs@22\n"
			result := callTool(t, srv, "zerops_import", map[string]any{"content": yaml})
			text := getTextContent(t, result)
			if !result.IsError {
				t.Fatalf("a Box import outside prg1 must be refused: %s", text)
			}
			for _, want := range []string{platform.ErrPrerequisiteMissing, "Nothing was imported", "box", "Zerops Box", "only in a project in region prg1", "US East (ny1)"} {
				if !strings.Contains(text, want) {
					t.Errorf("refusal missing %q: %s", want, text)
				}
			}
			if n := mock.CallCount("ImportServices"); n != 0 {
				t.Errorf("ImportServices called %d times; the refusal must come before anything is created", n)
			}
			if n := mock.CallCount("GetProject"); n != 1 {
				t.Errorf("GetProject reads = %d, want exactly one", n)
			}
		})
	}
}

// TestImportTool_BoxRegion_ReadOnlyWithABoxAndPassesInPrg1: an import with no
// Box never reads the project's region, even outside prg1, and a Box in prg1
// imports after exactly one read. The result's envelope reads the project
// too, so the region read is what a Box import reads beyond the same import
// without one.
func TestImportTool_BoxRegion_ReadOnlyWithABoxAndPassesInPrg1(t *testing.T) {
	t.Parallel()
	importIn := func(region, yaml string) int {
		t.Helper()
		mock := regionImportMock(region)
		srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
		RegisterImport(srv, mock, "proj-1", testEngine(t), "", nil, runtime.Info{})
		result := callTool(t, srv, "zerops_import", map[string]any{"content": yaml})
		if result.IsError {
			t.Fatalf("import in %s must proceed: %s", region, getTextContent(t, result))
		}
		if n := mock.CallCount("ImportServices"); n != 1 {
			t.Errorf("ImportServices called %d times, want 1", n)
		}
		return mock.CallCount("GetProject")
	}
	withoutBox := importIn("ny1", "services:\n  - hostname: api\n    type: nodejs@22\n")
	withBox := importIn("prg1", "services:\n  - hostname: box\n    type: ubuntu/zbox@1\n  - hostname: api\n    type: nodejs@22\n")
	if withBox != withoutBox+1 {
		t.Errorf("GetProject reads: %d with a Box in prg1, %d without one; want exactly one region read, and only with a Box", withBox, withoutBox)
	}
}

// classicPlanHarness opens a classic bootstrap session in a project in region
// and registers zerops_workflow over it.
func classicPlanHarness(t *testing.T, region string) (*mcp.Server, *workflow.Engine, *platform.Mock) {
	t.Helper()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	if _, err := engine.BootstrapStart("proj-1", "a playground that runs people's snippets"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	mock := platform.NewMock().WithProject(regionProject(region))
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, mock, nil, "proj-1", nil, engine, nil, dir, "", nil, nil, runtime.Info{}, "")
	return srv, engine, mock
}

// boxPlan is a classic plan: one simple runtime and a Box beside it with the
// given resolution.
func boxPlan(resolution string) []any {
	return []any{map[string]any{
		"runtime": map[string]any{"devHostname": "api", "type": "nodejs@22", "bootstrapMode": "simple"},
		"dependencies": []any{map[string]any{
			"hostname": "sandbox", "type": "ubuntu/zbox@1", "resolution": resolution,
		}},
	}}
}

func TestBootstrapPlan_BoxDependencyOutsidePrg1_RefusedBeforePlanning(t *testing.T) {
	t.Parallel()
	srv, engine, mock := classicPlanHarness(t, "ny1")
	result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "complete", "step": "discover", "plan": boxPlan("CREATE")})
	text := getTextContent(t, result)
	if !result.IsError {
		t.Fatalf("a plan creating a Box outside prg1 must be refused: %s", text)
	}
	for _, want := range []string{platform.ErrPrerequisiteMissing, "sandbox", "Zerops Box", "only in a project in region prg1", "US East (ny1)"} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal missing %q: %s", want, text)
		}
	}
	if n := mock.CallCount("GetProject"); n != 1 {
		t.Errorf("GetProject reads = %d, want exactly one", n)
	}
	state, err := engine.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if state.Bootstrap.Plan != nil || state.Bootstrap.CurrentStepName() != workflow.StepDiscover {
		t.Errorf("a refused plan must leave the session at discover with no plan; step %q plan %+v", state.Bootstrap.CurrentStepName(), state.Bootstrap.Plan)
	}
}

// TestBootstrapPlan_BoxDependency_AcceptedWhereItCanExist: a plan creating a
// Box in prg1 is taken after one region read, a plan naming a Box that exists
// is never region-checked — even outside prg1 — and either stores the Box as
// the runtime's dependency, with no mode. The response's envelope reads the
// project too, so the region read is what the CREATE plan reads beyond the
// EXISTS one.
func TestBootstrapPlan_BoxDependency_AcceptedWhereItCanExist(t *testing.T) {
	t.Parallel()
	planIn := func(region, resolution string) int {
		t.Helper()
		srv, engine, mock := classicPlanHarness(t, region)
		result := callTool(t, srv, "zerops_workflow", map[string]any{"action": "complete", "step": "discover", "plan": boxPlan(resolution)})
		if result.IsError {
			t.Fatalf("a %s Box plan in %s must be taken: %s", resolution, region, getTextContent(t, result))
		}
		state, err := engine.GetState()
		if err != nil {
			t.Fatalf("GetState: %v", err)
		}
		plan := state.Bootstrap.Plan
		if plan == nil || len(plan.Targets) != 1 || len(plan.Targets[0].Dependencies) != 1 {
			t.Fatalf("plan not stored with the Box as its dependency: %+v", plan)
		}
		if dep := plan.Targets[0].Dependencies[0]; dep.Hostname != "sandbox" || dep.Mode != "" {
			t.Errorf("Box dependency stored as %+v; a Box takes no mode", dep)
		}
		return mock.CallCount("GetProject")
	}
	existing := planIn("ny1", "EXISTS")
	created := planIn("prg1", "CREATE")
	if created != existing+1 {
		t.Errorf("GetProject reads: %d for a Box created in prg1, %d for one that exists; want exactly one region read, and only for a Box the plan creates", created, existing)
	}
}
