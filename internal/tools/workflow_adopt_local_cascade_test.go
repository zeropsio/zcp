package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// Gate A hit on handleAdoptLocal: seeded GH integration drives the
// cascade to a clean answer; the meta lands with StageSetupName
// populated and the response is the success shape (not a blocker).
func TestHandleAdoptLocal_CascadeHit_PopulatesStageSetupName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:        "myproject",
		Mode:            topology.PlanModeLocalOnly,
		BootstrappedAt:  "2026-04-01",
		CloseDeployMode: topology.CloseModeUnset,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{
				ID: "rt-1", Name: "apistage", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
			},
		}).
		WithIntegrationStatus("rt-1", platform.IntegrationStatus{
			State:           platform.IntegrationConfigured,
			Provider:        platform.IntegrationProviderGitHub,
			ZeropsYamlSetup: "prod",
		})

	result, _, _ := handleAdoptLocal(
		context.Background(), mock, "p1", dir,
		WorkflowInput{TargetService: "apistage"},
		runtime.Info{},
	)
	if result.IsError {
		t.Fatalf("expected success; got: %s", getTextContent(t, result))
	}
	// Response shape: success path (not blocker).
	var resp map[string]any
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "linked" {
		t.Errorf("response status: got %v, want linked (cascade hit → success, not blocker)", resp["status"])
	}

	meta, _ := workflow.ReadServiceMeta(dir, "myproject")
	if meta == nil {
		t.Fatal("expected meta after adoption")
	}
	if meta.StageSetupName != "prod" {
		t.Errorf("StageSetupName: got %q, want prod (cascade-written)", meta.StageSetupName)
	}
	if got := meta.SetupNameFor("apistage"); got != "prod" {
		t.Errorf("SetupNameFor(apistage): got %q, want prod", got)
	}
}

// Cascade miss on handleAdoptLocal returns the structured
// requiresSetupInput blocker (status=blocked, reason=requiresSetupInput,
// recovery pointing at set-default-setup). Meta still written so the
// adoption itself isn't rolled back — just the setup name stays empty.
func TestHandleAdoptLocal_CascadeMiss_ReturnsRequiresSetupInput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:        "myproject",
		Mode:            topology.PlanModeLocalOnly,
		BootstrappedAt:  "2026-04-01",
		CloseDeployMode: topology.CloseModeUnset,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{
				ID: "rt-1", Name: "apistage", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
			},
		})
	// No integration / app-version seed → cascade exhausts at step 6.

	result, _, _ := handleAdoptLocal(
		context.Background(), mock, "p1", dir,
		WorkflowInput{TargetService: "apistage"},
		runtime.Info{},
	)
	if result.IsError {
		t.Fatalf("expected success-shaped tool result (blocker carried in body, not IsError); got: %s",
			getTextContent(t, result))
	}

	var resp RequiresSetupInputResponse
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &resp); err != nil {
		t.Fatalf("unmarshal: %v\nbody=%s", err, getTextContent(t, result))
	}
	if resp.Status != "blocked" {
		t.Errorf("Status: got %q, want blocked", resp.Status)
	}
	if resp.Reason != "requiresSetupInput" {
		t.Errorf("Reason: got %q, want requiresSetupInput", resp.Reason)
	}
	if resp.Service != "apistage" {
		t.Errorf("Service: got %q, want apistage", resp.Service)
	}
	if resp.Recovery.Action != "set-default-setup" {
		t.Errorf("Recovery.Action: got %q, want set-default-setup", resp.Recovery.Action)
	}

	// Meta itself was still written (adoption succeeds even on cascade
	// miss); StageSetupName stays empty.
	meta, _ := workflow.ReadServiceMeta(dir, "myproject")
	if meta == nil {
		t.Fatal("expected meta after adoption despite cascade miss")
	}
	if meta.Mode != topology.PlanModeLocalStage {
		t.Errorf("Mode: got %q, want local-stage (adoption itself succeeds)", meta.Mode)
	}
	if meta.StageSetupName != "" {
		t.Errorf("StageSetupName: got %q, want empty on cascade miss", meta.StageSetupName)
	}
}
