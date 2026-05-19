package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestResolveBuildTargetForHost_StandardPair_GitPush pins the dev → stage
// redirect for record-deploy + verify under standard mode WHEN the meta's
// actual closeMode is git-push + configured (= the only delivery shape
// where dev pushes and stage builds remotely). For auto / manual / unset
// the redirect must NOT fire — that branch is covered by
// TestResolveBuildTargetForHost_StandardPair_AutoNoRedirect below.
func TestResolveBuildTargetForHost_StandardPair_GitPush(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		CloseDeployMode:  topology.CloseModeGitPush,
		GitPushState:     topology.GitPushConfigured,
		BootstrapSession: "test",
		BootstrappedAt:   "2026-05-19",
		FirstDeployedAt:  "2026-05-19",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	host, setup := resolveBuildTargetForHost(dir, "appdev")
	if host != "appstage" {
		t.Errorf("dev half (git-push): build target = %q, want appstage", host)
	}
	if setup != "prod" {
		t.Errorf("dev half (git-push): build setup = %q, want prod", setup)
	}

	// Stage half under git-push: helper may return "appstage" (identity)
	// or empty — both are no-op at the caller (`if buildHost != "" &&
	// buildHost != input.TargetService`). Either way no swap fires;
	// assertion captures "no observable redirect for the caller".
	host, _ = resolveBuildTargetForHost(dir, "appstage")
	if host != "" && host != "appstage" {
		t.Errorf("stage half (git-push): build target = %q, want empty or appstage (no redirect)", host)
	}
}

// TestResolveBuildTargetForHost_StandardPair_AutoNoRedirect pins the
// regression that broke the greenfield-node-postgres-dev-stage flow-eval
// run on 2026-05-19: under closeMode=auto, dev half deploys to itself
// (direct delivery), so verify + record-deploy on appdev MUST NOT
// redirect to appstage. The fix made resolveBuildTargetForHost read the
// meta's actual closeMode + gitPushState (not synthesize git-push).
func TestResolveBuildTargetForHost_StandardPair_AutoNoRedirect(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		CloseDeployMode:  topology.CloseModeAuto,
		BootstrapSession: "test",
		BootstrappedAt:   "2026-05-19",
		FirstDeployedAt:  "2026-05-19",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	host, _ := resolveBuildTargetForHost(dir, "appdev")
	if host != "" {
		t.Errorf("dev half (auto): build target = %q, want empty (no redirect — auto self-deploys)", host)
	}
	host, _ = resolveBuildTargetForHost(dir, "appstage")
	if host != "" {
		t.Errorf("stage half (auto): build target = %q, want empty (no redirect)", host)
	}
}

// TestResolveBuildTargetForHost_StandardPair_GitPushUnconfigured pins the
// "capability gap" branch: closeMode=git-push but GitPushState=unconfigured
// means the resolver falls back to direct delivery, so no redirect.
func TestResolveBuildTargetForHost_StandardPair_GitPushUnconfigured(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		CloseDeployMode:  topology.CloseModeGitPush,
		GitPushState:     topology.GitPushUnconfigured,
		BootstrapSession: "test",
		BootstrappedAt:   "2026-05-19",
		FirstDeployedAt:  "2026-05-19",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	host, _ := resolveBuildTargetForHost(dir, "appdev")
	if host != "" {
		t.Errorf("git-push unconfigured: build target = %q, want empty (falls back to direct)", host)
	}
}

// TestResolveBuildTargetForHost_Simple verifies that simple-mode (single
// service) does NOT redirect — build target equals input even under
// git-push (push source == build target for simple modes).
func TestResolveBuildTargetForHost_Simple(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "api",
		Mode:             topology.PlanModeSimple,
		CloseDeployMode:  topology.CloseModeGitPush,
		GitPushState:     topology.GitPushConfigured,
		BootstrapSession: "test",
		BootstrappedAt:   "2026-05-19",
		FirstDeployedAt:  "2026-05-19",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	host, _ := resolveBuildTargetForHost(dir, "api")
	if host != "api" {
		t.Errorf("simple (git-push): build target = %q, want api (push source == build target)", host)
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
// passes `targetService=appdev` on a standard pair UNDER git-push gets the
// stamp routed to the stage half (build target), with a warning explaining
// the redirect.
func TestHandleRecordDeploy_RedirectsDevToStage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		CloseDeployMode:  topology.CloseModeGitPush,
		GitPushState:     topology.GitPushConfigured,
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
