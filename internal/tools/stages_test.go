package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// threeStagePairState writes a bootstrapped appdev pair with stages webstage
// (setup web), apistage (setup api) and workerstage (no setup yet), and a
// zerops.yaml on appdev's mount declaring every setup plus a `prod` block a
// hostname-suffix guess would pick for any stage.
func threeStagePairState(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	block := func(name string) string {
		return "  - setup: " + name + "\n    build:\n      base: nodejs@22\n      deployFiles: [.]\n    run:\n      start: node main.js\n      ports:\n        - port: 3000\n"
	}
	scaffoldServiceYaml(t, dir, "appdev", "zerops:\n"+block("dev")+block("prod")+block("web")+block("api")+block("worker"))
	m := workflow.NewServiceMeta("appdev", topology.PlanModeStandard)
	m.PrimarySetupName = "dev"
	m.Stages = []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}, {Hostname: "workerstage"}}
	m.BootstrapSession = "s1"
	m.BootstrappedAt = "2026-10-14T00:00:00Z"
	if err := workflow.WriteServiceMeta(stateDir, m); err != nil {
		t.Fatal(err)
	}
	return stateDir
}

func TestDeployPreFlight_StageOfThreeStagePair_TakesItsSetupFromStages(t *testing.T) {
	t.Parallel()
	stateDir := threeStagePairState(t)
	for _, tt := range []struct{ stage, want string }{
		{"webstage", "web"},
		{"apistage", "api"},
	} {
		resolved, result, err := deployPreFlight(context.Background(), platform.NewMock(), "proj-1", stateDir, "appdev", tt.stage, "", "", true)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.stage, err)
		}
		if result == nil || !result.Passed {
			t.Fatalf("%s: expected preflight pass; got %+v", tt.stage, result)
		}
		if resolved.setup != tt.want {
			t.Errorf("%s: resolved setup = %q, want %q (its Stages entry, not the prod a suffix guess picks)", tt.stage, resolved.setup, tt.want)
		}
	}
}

func TestDeployPreFlight_StageWithoutRecordedSetup_RecordsTheResolvedOneOnThatStage(t *testing.T) {
	t.Parallel()
	stateDir := threeStagePairState(t)
	resolved, result, err := deployPreFlight(context.Background(), platform.NewMock(), "proj-1", stateDir, "appdev", "workerstage", "worker", "", true)
	if err != nil || result == nil || !result.Passed || resolved.setup != "worker" {
		t.Fatalf("preflight = %q, %+v, %v; want worker passing", resolved.setup, result, err)
	}
	meta, err := workflow.ReadServiceMeta(stateDir, "appdev")
	if err != nil {
		t.Fatal(err)
	}
	want := []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}, {Hostname: "workerstage", SetupName: "worker"}}
	if !slices.Equal(meta.Stages, want) || meta.PrimarySetupName != "dev" {
		t.Errorf("meta = primary %q, stages %+v; want dev and %+v", meta.PrimarySetupName, meta.Stages, want)
	}
}

func TestValidateDevelopScope_DevHalfOfThreeStagePair_IncludesEveryStage(t *testing.T) {
	t.Parallel()
	m := &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeStandard, BootstrappedAt: "2026-10-14",
		Stages: []topology.StageRef{{Hostname: "webstage"}, {Hostname: "apistage"}, {Hostname: "workerstage"}},
	}
	runtimeMetas := workflow.ManagedRuntimeIndex([]*workflow.ServiceMeta{m})
	live := map[string]bool{"appdev": true, "webstage": true, "apistage": true, "workerstage": true}

	scope, err := validateDevelopScope([]string{"appdev"}, runtimeMetas, live)
	if err != nil {
		t.Fatalf("validateDevelopScope: %v", err)
	}
	if want := []string{"apistage", "appdev", "webstage", "workerstage"}; !slices.Equal(scope, want) {
		t.Errorf("scope = %v, want %v", scope, want)
	}

	// A stage named directly stays the only half in scope, as today.
	scope, err = validateDevelopScope([]string{"apistage"}, runtimeMetas, live)
	if err != nil || !slices.Equal(scope, []string{"apistage"}) {
		t.Errorf("scope = %v, %v; want [apistage]", scope, err)
	}

	// A stage that is gone from the project blocks the dev half's scope.
	delete(live, "workerstage")
	if _, err := validateDevelopScope([]string{"appdev"}, runtimeMetas, live); err == nil || !strings.Contains(err.Error(), "workerstage") {
		t.Errorf("err = %v, want the missing workerstage named", err)
	}
}

func TestHandleSetDefaultSetup_ThreeStagePair_WritesTheNamedStage(t *testing.T) {
	t.Parallel()
	stateDir := threeStagePairState(t)

	result, _, _ := handleSetDefaultSetup(context.Background(), platform.NewMock(), "p1",
		WorkflowInput{TargetService: "workerstage", Setup: "dev", StageSetup: "worker"}, stateDir)
	if result.IsError {
		t.Fatalf("expected success; got: %s", getTextContent(t, result))
	}
	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	want := []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}, {Hostname: "workerstage", SetupName: "worker"}}
	if !slices.Equal(meta.Stages, want) {
		t.Errorf("Stages = %+v, want %+v", meta.Stages, want)
	}

	// Naming the dev half leaves which stage was meant open: refused, nothing written.
	result, _, _ = handleSetDefaultSetup(context.Background(), platform.NewMock(), "p1",
		WorkflowInput{TargetService: "appdev", Setup: "dev", StageSetup: "api"}, stateDir)
	if !result.IsError || !strings.Contains(getTextContent(t, result), "webstage, apistage, workerstage") {
		t.Errorf("expected a refusal naming the stages; got: %s", getTextContent(t, result))
	}
	after, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if !slices.Equal(after.Stages, want) {
		t.Errorf("refused call changed Stages to %+v", after.Stages)
	}
}

func TestDeployPreFlight_StageSetupMissingFromZeropsYAML_IsRefusedNamingTheStage(t *testing.T) {
	t.Parallel()
	stateDir := threeStagePairState(t)
	if err := workflow.UpdateServiceMeta(stateDir, "appdev", func(m *workflow.ServiceMeta) error {
		m.SetStageSetup("apistage", "backend")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, _, err := deployPreFlight(context.Background(), platform.NewMock(), "proj-1", stateDir, "appdev", "apistage", "", "", true)
	var blocker *workflow.ErrRequiresSetupInput
	if !errors.As(err, &blocker) {
		t.Fatalf("err = %v, want a requiresSetupInput blocker", err)
	}
	if !strings.Contains(blocker.Reason, `apistage records setup "backend"`) || !slices.Contains(blocker.AvailableSetups, "api") {
		t.Errorf("blocker = %+v, want the stage's recorded setup named and the yaml's setups offered", blocker)
	}
}
