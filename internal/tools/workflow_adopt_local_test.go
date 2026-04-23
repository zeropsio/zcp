// Tests for: workflow_adopt_local.go — the action="adopt-local" subaction
// that upgrades a local-only meta to local-stage by linking one runtime.
package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestHandleAdoptLocal_ContainerEnv_Refused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mock := platform.NewMock()

	result, _, err := handleAdoptLocal(
		context.Background(), mock, "p1", dir,
		WorkflowInput{TargetService: "api"},
		runtime.Info{InContainer: true},
	)
	if err != nil {
		t.Fatalf("handleAdoptLocal: %v", err)
	}
	if !result.IsError {
		t.Fatalf("container env should refuse adopt-local; got: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "local env") {
		t.Errorf("error should explain local-only scope; got:\n%s", text)
	}
}

func TestHandleAdoptLocal_MissingTarget_Refused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname: "myproject", Mode: workflow.PlanModeLocalOnly, BootstrappedAt: "2026-04-01",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	mock := platform.NewMock()

	result, _, _ := handleAdoptLocal(
		context.Background(), mock, "p1", dir,
		WorkflowInput{},
		runtime.Info{},
	)
	if !result.IsError {
		t.Fatalf("missing targetService should error; got: %s", getTextContent(t, result))
	}
}

func TestHandleAdoptLocal_NoLocalMeta_Refused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mock := platform.NewMock()

	result, _, _ := handleAdoptLocal(
		context.Background(), mock, "p1", dir,
		WorkflowInput{TargetService: "api"},
		runtime.Info{},
	)
	if !result.IsError {
		t.Fatalf("missing local meta should error; got: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "Auto-adopt") && !strings.Contains(text, "restart") {
		t.Errorf("error should hint at auto-adopt / restart; got:\n%s", text)
	}
}

func TestHandleAdoptLocal_AlreadyLinked_Refused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// local-stage already — can't re-link via adopt-local.
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname: "myproject", StageHostname: "existing", Mode: workflow.PlanModeLocalStage,
		BootstrappedAt: "2026-04-01",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	mock := platform.NewMock()

	result, _, _ := handleAdoptLocal(
		context.Background(), mock, "p1", dir,
		WorkflowInput{TargetService: "other"},
		runtime.Info{},
	)
	if !result.IsError {
		t.Fatalf("already-linked meta should error; got: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "already linked") {
		t.Errorf("error should say 'already linked'; got:\n%s", text)
	}
}

func TestHandleAdoptLocal_TargetNotARuntime_Refused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname: "myproject", Mode: workflow.PlanModeLocalOnly, BootstrappedAt: "2026-04-01",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	// The target is a managed service — reject, not a deploy target.
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{
			ID: "db-1", Name: "db", Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName:  "postgresql@16",
				ServiceStackTypeCategoryName: "USER",
			},
		},
	})

	result, _, _ := handleAdoptLocal(
		context.Background(), mock, "p1", dir,
		WorkflowInput{TargetService: "db"},
		runtime.Info{},
	)
	if !result.IsError {
		t.Fatalf("managed-service target should error; got: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "managed service") {
		t.Errorf("error should explain managed-service rejection; got:\n%s", text)
	}
}

func TestHandleAdoptLocal_TargetNotFound_Refused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname: "myproject", Mode: workflow.PlanModeLocalOnly, BootstrappedAt: "2026-04-01",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	mock := platform.NewMock() // no services at all

	result, _, _ := handleAdoptLocal(
		context.Background(), mock, "p1", dir,
		WorkflowInput{TargetService: "ghost"},
		runtime.Info{},
	)
	if !result.IsError {
		t.Fatalf("missing target should error; got: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "not found") {
		t.Errorf("error should say 'not found'; got:\n%s", text)
	}
}

func TestHandleAdoptLocal_LinksRuntime_UpgradesMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname: "myproject", Mode: workflow.PlanModeLocalOnly,
		BootstrappedAt: "2026-04-01",
		DeployStrategy: workflow.StrategyManual,
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{
			ID: "rt-1", Name: "apistage", Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName:  "nodejs@22",
				ServiceStackTypeCategoryName: "USER",
			},
		},
	})

	result, _, _ := handleAdoptLocal(
		context.Background(), mock, "p1", dir,
		WorkflowInput{TargetService: "apistage"},
		runtime.Info{},
	)
	if result.IsError {
		t.Fatalf("expected success; got: %s", getTextContent(t, result))
	}
	got, _ := workflow.ReadServiceMeta(dir, "myproject")
	if got.Mode != workflow.PlanModeLocalStage {
		t.Errorf("Mode = %q, want local-stage", got.Mode)
	}
	if got.StageHostname != "apistage" {
		t.Errorf("StageHostname = %q, want apistage", got.StageHostname)
	}
	// Previous forced-manual strategy reset because linkage expands options;
	// cleared to empty so on-disk shape matches a never-configured service.
	if got.DeployStrategy != "" {
		t.Errorf("DeployStrategy = %q, want empty after link (router prompts)", got.DeployStrategy)
	}
	// ACTIVE target stamps FirstDeployedAt.
	if got.FirstDeployedAt == "" {
		t.Error("FirstDeployedAt empty — ACTIVE target at link time must stamp")
	}
}
