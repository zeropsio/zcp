// Handler round-trip proofs for the JSON envelope carrier. The wire
// contract test (envelope_json_carrier_test.go) pins the SHAPE of each
// response type; these pin that the handlers actually populate it, and
// that the tool's own payload keeps parsing as it always did.
package tools

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestImportTool_CarriesEnvelope(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "z3-eval"}).
		WithImportResult(&platform.ImportResult{
			ProjectID:   "proj-1",
			ProjectName: "myproject",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-1", Name: "api", Processes: []platform.Process{
					{ID: "p-1", ActionName: "serviceStackImport", Status: serviceStatusRunning},
				}},
			},
		}).
		WithProcess(&platform.Process{ID: "p-1", ActionName: "serviceStackImport", Status: statusFinished})

	engine := testEngine(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterImport(srv, mock, "proj-1", engine, engine.StateDir(), nil, runtime.Info{})

	result := callTool(t, srv, "zerops_import",
		map[string]any{"content": "services:\n  - hostname: api\n    type: nodejs@20\n"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	env, ok := workflow.ExtractEnvelope(text)
	if !ok {
		t.Fatalf("import result carries no envelope:\n%s", text)
	}
	if env.Project.ID != "proj-1" {
		t.Errorf("envelope project: got %q want proj-1", env.Project.ID)
	}

	// The tool's own payload must still parse as the import result it always
	// was — the envelope is a sibling key, not a reshape.
	var parsed ops.ImportResult
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("import result no longer parses as ops.ImportResult: %v", err)
	}
	if len(parsed.Processes) != 1 || parsed.Summary == "" {
		t.Errorf("import payload changed shape: %+v", parsed)
	}
}

func TestVerifyTool_CarriesEnvelope(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "z3-eval"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				}},
		})

	engine := testEngine(t)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, nil, "proj-1", engine.StateDir(), runtime.Info{})

	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "api"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", getTextContent(t, result))
	}

	text := getTextContent(t, result)
	env, ok := workflow.ExtractEnvelope(text)
	if !ok {
		t.Fatalf("verify result carries no envelope:\n%s", text)
	}
	if env.Project.ID != "proj-1" {
		t.Errorf("envelope project: got %q want proj-1", env.Project.ID)
	}
}
