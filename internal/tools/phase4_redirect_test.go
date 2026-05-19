package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestResolveBuildTargetForHost_StandardPair pins the dev → stage redirect
// for record-deploy + verify under standard mode. A standard pair always
// resolves to the stage half regardless of the meta's current closeMode
// (Phase 4 design: build target is a topology fact, not a delivery
// decision — record-deploy / verify run AFTER deploy landed and the
// target is decided by Mode + StageHostname).
func TestResolveBuildTargetForHost_StandardPair(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-05-19",
		FirstDeployedAt:  "2026-05-19",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	host, setup := resolveBuildTargetForHost(dir, "appdev")
	if host != "appstage" {
		t.Errorf("dev half: build target = %q, want appstage", host)
	}
	if setup != "prod" {
		t.Errorf("dev half: build setup = %q, want prod", setup)
	}

	// Stage half: resolves to itself.
	host, setup = resolveBuildTargetForHost(dir, "appstage")
	if host != "appstage" {
		t.Errorf("stage half: build target = %q, want appstage", host)
	}
	if setup != "prod" {
		t.Errorf("stage half: build setup = %q, want prod", setup)
	}
}

// TestResolveBuildTargetForHost_Simple verifies that simple-mode (single
// service) does NOT redirect — build target equals input.
func TestResolveBuildTargetForHost_Simple(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "api",
		Mode:             topology.PlanModeSimple,
		BootstrapSession: "test",
		BootstrappedAt:   "2026-05-19",
		FirstDeployedAt:  "2026-05-19",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	host, _ := resolveBuildTargetForHost(dir, "api")
	if host != "api" {
		t.Errorf("simple: build target = %q, want api", host)
	}
}

// TestResolveBuildTargetForHost_NoMeta returns ("","") for unknown hosts so
// callers fall back to the input without warning.
func TestResolveBuildTargetForHost_NoMeta(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	host, setup := resolveBuildTargetForHost(dir, "unknown")
	if host != "" || setup != "" {
		t.Errorf("missing meta: got (%q, %q), want both empty", host, setup)
	}
}

// TestHandleRecordDeploy_RedirectsDevToStage pins Phase 4: an agent that
// passes `targetService=appdev` on a standard pair gets the stamp routed
// to the stage half (build target), with a warning explaining the redirect.
func TestHandleRecordDeploy_RedirectsDevToStage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-05-19",
		FirstDeployedAt:  "2026-05-19",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	// nil client skips the build-status gate (test runs without platform).
	result, _, err := handleRecordDeploy(context.Background(), nil, nil, "", dir, WorkflowInput{TargetService: "appdev"})
	if err != nil {
		t.Fatalf("handleRecordDeploy: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", getTextContent(t, result))
	}
	body := getTextContent(t, result)
	// Response must record on appstage, not appdev.
	if !strings.Contains(body, `"hostname":"appstage"`) {
		t.Errorf("response should stamp appstage, got: %s", body)
	}
	// Warning must explain the redirect.
	if !strings.Contains(body, "is a push source") || !strings.Contains(body, "build target") {
		t.Errorf("response missing redirect warning, got: %s", body)
	}
}

// TestHandleRecordDeploy_SimpleNoRedirect verifies simple/single-runtime
// hosts stamp themselves with no warning.
func TestHandleRecordDeploy_SimpleNoRedirect(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "api",
		Mode:             topology.PlanModeSimple,
		BootstrapSession: "test",
		BootstrappedAt:   "2026-05-19",
		FirstDeployedAt:  "2026-05-19",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	result, _, err := handleRecordDeploy(context.Background(), nil, nil, "", dir, WorkflowInput{TargetService: "api"})
	if err != nil {
		t.Fatalf("handleRecordDeploy: %v", err)
	}
	body := getTextContent(t, result)
	if !strings.Contains(body, `"hostname":"api"`) {
		t.Errorf("response should stamp api, got: %s", body)
	}
	if strings.Contains(body, "is a push source") {
		t.Errorf("simple mode should not surface a redirect warning, got: %s", body)
	}
}
