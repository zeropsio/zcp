package tools

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// adoptHarness opens a fresh adopt-route bootstrap session on a shared engine and
// registers the zerops_workflow tool against a mock with the given services.
func adoptHarness(t *testing.T, services []platform.ServiceStack) *mcp.Server {
	t.Helper()
	dir := t.TempDir()
	engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	if _, err := engine.BootstrapStartWithRoute("proj1", "adopt existing for a dashboard", workflow.BootstrapRouteAdopt, ""); err != nil {
		t.Fatalf("BootstrapStartWithRoute(adopt): %v", err)
	}
	client := platform.NewMock().WithServices(services)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, client, nil, "proj1", nil, engine, nil, dir, "", nil, nil, runtime.Info{}, "")
	return srv
}

func svc(name, typeVersion string) platform.ServiceStack {
	return platform.ServiceStack{
		Name:                 name,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: typeVersion},
	}
}

// TestHandleBootstrapComplete_AdoptEmptyPlanWithScope_AutoDerives —
// route=adopt, complete step=discover with NO plan and a named scope
// auto-derives from exactly that live service (no "attestation required", no
// hand-authored plan). One scoped adoptable runtime → frictionless commit.
func TestHandleBootstrapComplete_AdoptEmptyPlanWithScope_AutoDerives(t *testing.T) {
	t.Parallel()
	srv := adoptHarness(t, []platform.ServiceStack{
		svc("appdev", "php-nginx@8.4"),
		svc("db", "postgresql@16"),
	})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"scope":  []any{"appdev"},
		// no plan, no attestation
	})
	if result.IsError {
		t.Fatalf("auto-derive should succeed, got error: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "Auto-derived") || !strings.Contains(text, "appdev") {
		t.Errorf("expected auto-derived adoption message naming appdev, got: %s", text)
	}
}

// TestHandleBootstrapComplete_AdoptEmptyArrayWithScope_AutoDerives — Codex rev
// 2: an empty JSON array plan must take the scoped auto-derive path too, not the
// zero-target commit that would advance discover with no metas.
func TestHandleBootstrapComplete_AdoptEmptyArrayWithScope_AutoDerives(t *testing.T) {
	t.Parallel()
	srv := adoptHarness(t, []platform.ServiceStack{
		svc("appdev", "php-nginx@8.4"),
		svc("db", "postgresql@16"),
	})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"scope":  []any{"appdev"},
		"plan":   []any{}, // empty array — must still auto-derive
	})
	if result.IsError {
		t.Fatalf("empty-array plan should auto-derive, got error: %s", getTextContent(t, result))
	}
	if text := getTextContent(t, result); !strings.Contains(text, "Auto-derived") {
		t.Errorf("empty-array plan should take the auto-derive path, got: %s", text)
	}
}

func TestHandleBootstrapComplete_AdoptEmptyScopeListsCandidates(t *testing.T) {
	t.Parallel()
	srv := adoptHarness(t, []platform.ServiceStack{
		svc("appdev", "php-nginx@8.4"),
		svc("api", "go@1"),
		svc("db", "postgresql@16"),
	})

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		// no plan, no scope
	})
	if !result.IsError {
		t.Fatalf("empty adopt scope must return diagnostic, got success: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	for _, needle := range []string{"adopt scope is required", "available adoptable runtime services", "appdev", "api", "scope"} {
		if !strings.Contains(text, needle) {
			t.Errorf("empty-scope diagnostic missing %q; got: %s", needle, text)
		}
	}
	if strings.Contains(text, "db") {
		t.Errorf("managed service must not be listed as adoptable; got: %s", text)
	}
}

// TestHandleBootstrapComplete_AdoptExplicitPlan_PassesLiveServices — Codex rev 3:
// an explicit adopt plan is validated against live services, so a standard-pair plan
// naming a stage runtime that does not exist is rejected (pre-fix the handler passed
// nil live services and this slipped through to fail later at deploy).
func TestHandleBootstrapComplete_AdoptExplicitPlan_PassesLiveServices(t *testing.T) {
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
	if text := getTextContent(t, result); !strings.Contains(text, "appstage") {
		t.Errorf("rejection should name the missing stage runtime appstage, got: %s", text)
	}
}

// TestHandleBootstrapComplete_AdoptVariantBBesideAnotherRepository_OnePairWithItsStages
// (docs/spec-mate.md §2.8a.4a; live S2): a scope naming a variant-B
// repository's dev services and stages, in a project whose first runtime is
// an unrelated app with its own zerops.yaml, is recorded as one standard pair
// holding the code volume again — the setups read through the scoped dev
// services, each stage with the setup named after it — never as four
// independent dev services with the volume a dependency. The owner's app is
// neither adopted nor reached.
func TestHandleBootstrapComplete_AdoptVariantBBesideAnotherRepository_OnePairWithItsStages(t *testing.T) {
	t.Parallel()
	existing, ssh := s2Project()
	stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
	engine := workflow.NewEngine(stateDir, workflow.EnvContainer, nil)
	if _, err := engine.BootstrapStartWithRoute("proj1", "adopt the code-volume pair", workflow.BootstrapRouteAdopt, ""); err != nil {
		t.Fatalf("BootstrapStartWithRoute(adopt): %v", err)
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, platform.NewMock().WithServices(existing), nil, "proj1", nil, engine, nil, stateDir, "", nil, ssh, runtime.Info{}, "")

	result := callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete",
		"step":   "discover",
		"scope":  []any{"lvmwebdev", "lvmapidev", "lvmwebstage", "lvmapistage"},
	})
	if result.IsError {
		t.Fatalf("adopt failed: %s", getTextContent(t, result))
	}
	state, err := engine.GetState()
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	targets := state.Bootstrap.Plan.Targets
	if len(targets) != 1 {
		t.Fatalf("want one variant-B pair, got %+v", targets)
	}
	rt := targets[0].Runtime
	wantStages := []topology.StageRef{{Hostname: "lvmwebstage", SetupName: "lvmwebstage"}, {Hostname: "lvmapistage", SetupName: "lvmapistage"}}
	wantDevServices := []topology.DevServiceRef{{Hostname: "lvmapidev", SetupName: "lvmapidev"}}
	if rt.DevHostname != "lvmwebdev" || rt.EffectiveMode() != topology.PlanModeStandard || !slices.Equal(rt.StageRefs(), wantStages) ||
		rt.CodeVolume == nil || rt.CodeVolume.Hostname != "lvmvol" || rt.CodeVolume.MountPath != "/var/www/ws" || !slices.Equal(rt.CodeVolume.DevServices, wantDevServices) {
		t.Errorf("pair = %+v codeVolume = %+v", rt, rt.CodeVolume)
	}
	for _, d := range targets[0].Dependencies {
		if d.Hostname == "lvmvol" {
			t.Errorf("the code volume is no dependency: %+v", targets[0].Dependencies)
		}
	}
	for _, h := range ssh.hosts {
		if h == "appstage" || h == "appdev" {
			t.Errorf("SSH reached the owner's %q; hosts = %v", h, ssh.hosts)
		}
	}
}
