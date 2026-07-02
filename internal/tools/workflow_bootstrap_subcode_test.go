// Tests for: workflow_bootstrap.go — bootstrapPlanSubcode conflation split
// (docs/spec-telemetry.md §4.2 error_subcode, telemetry-production-readiness
// plan S4). Each test drives the discover-step plan-completion failure to the
// wire response and asserts the JSON "subcode" key names the precise root
// cause instead of leaving three distinct failures indistinguishable under
// one INVALID_PARAMETER code.

package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestHandleBootstrapComplete_AdoptPairingAmbiguity_TagsAmbiguousScope: two
// same-type adoptable runtimes with no explicit plan trip
// workflow.ErrAdoptPairingChoice — the wire response must carry
// subcode=AMBIGUOUS_SCOPE so the pareto can distinguish this from every
// other INVALID_PARAMETER cause.
func TestHandleBootstrapComplete_AdoptPairingAmbiguity_TagsAmbiguousScope(t *testing.T) {
	t.Parallel()
	srv := adoptHarness(t, []platform.ServiceStack{
		svc("appdev", "nodejs@22"),
		svc("appstage", "nodejs@22"),
	})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"scope":  []any{"appdev", "appstage"},
		// no plan — auto-derive hits the same-type pairing refusal.
	})
	if !result.IsError {
		t.Fatalf("same-type pairing must refuse, got success: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, `"subcode":"`+platform.SubcodeAmbiguousScope+`"`) {
		t.Errorf("expected subcode=%s, got: %s", platform.SubcodeAmbiguousScope, text)
	}
}

// TestHandleBootstrapComplete_AdoptExplicitPlan_TagsWorkerPlanShape extends
// the existing PassesLiveServices scenario (a stage hostname the explicit
// plan names doesn't exist live — a ValidateBootstrapTargets shape failure)
// with the subcode assertion.
func TestHandleBootstrapComplete_AdoptExplicitPlan_TagsWorkerPlanShape(t *testing.T) {
	t.Parallel()
	srv := adoptHarness(t, []platform.ServiceStack{
		svc("appdev", "php-nginx@8.4"), // appstage intentionally absent
	})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"plan": []any{map[string]any{
			"runtime": map[string]any{
				"devHostname":   "appdev",
				"stageHostname": "appstage",
				"type":          "php-nginx@8.4",
				"bootstrapMode": "standard",
				"isExisting":    true,
			},
		}},
	})
	if !result.IsError {
		t.Fatalf("explicit plan naming a non-existent stage runtime must be rejected; got: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, `"subcode":"`+platform.SubcodeWorkerPlanShape+`"`) {
		t.Errorf("expected subcode=%s, got: %s", platform.SubcodeWorkerPlanShape, text)
	}
}

// TestHandleBootstrapComplete_ClassicPlan_InvalidHostname_TagsWorkerPlanShape
// drives the classic (non-adopt, non-recipe) route's own dispatch branch —
// distinct code path from the adopt-with-explicit-plan case above, same
// ValidateBootstrapTargets shape validator underneath.
func TestHandleBootstrapComplete_ClassicPlan_InvalidHostname_TagsWorkerPlanShape(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	if _, err := engine.BootstrapStart("proj1", "manual plan"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	client := platform.NewMock()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, client, nil, "proj1", nil, engine, nil, dir, "", nil, nil, runtime.Info{}, "")

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"plan": []any{map[string]any{
			"runtime": map[string]any{
				"devHostname":   "my-app", // invalid hostname shape (hyphen)
				"type":          "bun@1.2",
				"bootstrapMode": "dev",
			},
		}},
	})
	if !result.IsError {
		t.Fatalf("invalid hostname must be rejected; got: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, `"subcode":"`+platform.SubcodeWorkerPlanShape+`"`) {
		t.Errorf("expected subcode=%s, got: %s", platform.SubcodeWorkerPlanShape, text)
	}
}

// TestHandleBootstrapComplete_RecipePlanMismatch_TagsPlanTypeMismatch: a
// submitted recipe-route plan renaming a managed dependency doesn't match
// any dependency the recipe derives — workflow.ErrRecipePlanMismatch — and
// must surface subcode=PLAN_TYPE_MISMATCH.
func TestHandleBootstrapComplete_RecipePlanMismatch_TagsPlanTypeMismatch(t *testing.T) {
	t.Parallel()
	docs := map[string]*knowledge.Document{
		"zerops://recipes/dotnet-hello-world": {
			URI:        "zerops://recipes/dotnet-hello-world",
			Title:      "Dotnet Hello World",
			Languages:  []string{"dotnet"},
			ImportYAML: "services:\n  - hostname: appdev\n    type: dotnet@9\n    zeropsSetup: dev\n    buildFromGit: https://example.com/dotnet\n  - hostname: db\n    type: postgresql@16\n    mode: NON_HA\n",
		},
	}
	store, err := knowledge.NewStore(docs)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvLocal, store)
	if _, err := engine.BootstrapStartWithRoute("proj1", "upload service", workflow.BootstrapRouteRecipe, "dotnet-hello-world"); err != nil {
		t.Fatalf("BootstrapStartWithRoute(recipe): %v", err)
	}
	client := platform.NewMock()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, client, nil, "proj1", nil, engine, nil, dir, "", nil, nil, runtime.Info{}, "")

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"plan": []any{map[string]any{
			"runtime": map[string]any{
				"devHostname":   "appdev",
				"type":          "dotnet@9",
				"bootstrapMode": "dev",
			},
			"dependencies": []any{map[string]any{
				"hostname":   "mydb", // renames the fixed managed hostname — rejected
				"type":       "postgresql@16",
				"mode":       "NON_HA",
				"resolution": "CREATE",
			}},
		}},
	})
	if !result.IsError {
		t.Fatalf("managed-dep rename must be rejected; got: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, `"subcode":"`+platform.SubcodePlanTypeMismatch+`"`) {
		t.Errorf("expected subcode=%s, got: %s", platform.SubcodePlanTypeMismatch, text)
	}
}
