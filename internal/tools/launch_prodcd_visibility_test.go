package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestProdCDActionsBlock pins FP-5 (the F7 item that was silently cut):
// a source pair with BuildIntegration=actions gets the tag→prod Actions
// track on the launched response — complete workflow file with CONCRETE
// prod service IDs from the import result, the ZEROPS_TOKEN_PROD secret
// command (user-held value, GH_TOKEN conveyance, never the launch key),
// and the release-act pointer. Webhook/none sources get nil (the
// pipeline atoms own the dashboard story).
func TestProdCDActionsBlock(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "weather",
		Mode:             topology.PlanModeSimple,
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/krls2020/xy3",
		BuildIntegration: topology.BuildIntegrationActions,
		FirstDeployedAt:  "2026-06-10T09:00:00Z",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-06-10",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	state := &launchState{
		TargetServiceHostname: "weather",
		TargetProjectID:       "prod-1",
		ImportedServices: []importedServiceEntry{
			{ID: "prod-svc-9", Name: "weather"},
		},
		RuntimeProds: []launchRuntimeProd{
			{ProdHostname: "weather", RepoURL: "https://github.com/krls2020/xy3", SetupName: "weather"},
		},
	}

	block := prodCDActionsBlock(stateDir, state)
	if block == nil {
		t.Fatal("actions source must get the prodCD block")
	}
	wf, _ := block["workflowFile"].(map[string]any)
	content, _ := wf["content"].(string)
	for _, want := range []string{
		"tags: ['v*.*.*']",
		`zcli push --service-id "prod-svc-9" --setup "weather"`,
		"ZEROPS_TOKEN_PROD",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("prod workflow missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, " -g") {
		t.Errorf("prod job must NOT ship .git (prod is never git-read):\n%s", content)
	}
	secret, _ := block["secret"].(map[string]any)
	cmd, _ := secret["command"].(string)
	if !strings.Contains(cmd, "GH_TOKEN=") || !strings.Contains(cmd, "-R krls2020/xy3") {
		t.Errorf("secret command must convey GH_TOKEN per invocation against the right repo: %s", cmd)
	}
	src, _ := secret["source"].(string)
	if !strings.Contains(src, "NEVER reuse the launch key") {
		t.Errorf("secret source must forbid launch-key reuse: %s", src)
	}

	// Webhook source → nil (dashboard story owns it).
	if err := workflow.UpdateServiceMeta(stateDir, "weather", func(m *workflow.ServiceMeta) error {
		m.BuildIntegration = topology.BuildIntegrationWebhook
		return nil
	}); err != nil {
		t.Fatalf("UpdateServiceMeta: %v", err)
	}
	if b := prodCDActionsBlock(stateDir, state); b != nil {
		t.Errorf("webhook source must not get the actions track; got %v", b)
	}
}

// TestLaunchGate_RepoNotPublic_WarnsWithOptions pins the FP-3 read-side
// half: a private remote surfaces the warn blocker naming all three
// options BEFORE any key is minted; a public remote stays clean.
func TestLaunchGate_RepoNotPublic_WarnsWithOptions(t *testing.T) {
	// non-parallel: stubs package-level readers.
	stateDir := t.TempDir()
	seedL1Meta(t, stateDir, "weather", topology.PlanModeSimple, "")

	prevVis := launchRepoVisibilityReader
	launchRepoVisibilityReader = func(_ context.Context, _ ops.SSHDeployer, _, _ string) (bool, error) {
		return false, nil
	}
	t.Cleanup(func() { launchRepoVisibilityReader = prevVis })
	cleanupRemote := setLaunchLiveRemoteReader(func(_ context.Context, _ ops.SSHDeployer, _ runtime.Info, _ string) (string, error) {
		return "https://github.com/example/app.git", nil
	})
	t.Cleanup(cleanupRemote)
	prevProof := launchPushProofReader
	launchPushProofReader = func(_ context.Context, _ ops.SSHDeployer, _ runtime.Info, _, _ string) (LaunchPushProofResult, error) {
		return LaunchPushProofResult{LocalHead: "abc", RemoteHead: "abc"}, nil
	}
	t.Cleanup(func() { launchPushProofReader = prevProof })

	ssh := &containerSSHStub{} // presence check returns "ok" → present
	_, blockers, err := validateLaunchSourceControl(
		context.Background(), nil, ssh, runtime.Info{InContainer: true},
		stateDir, "proj", "weather", []string{"weather"},
	)
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	var visBlocker *topology.Blocker
	for i := range blockers {
		if blockers[i].ID == "repo-not-public-weather" {
			visBlocker = &blockers[i]
		}
	}
	if visBlocker == nil {
		t.Fatalf("expected repo-not-public blocker; got %+v", blockers)
	}
	if visBlocker.Severity != topology.BlockerSeverityWarn {
		t.Errorf("read-side visibility is warn (existing-project path may carry OAuth); got %s", visBlocker.Severity)
	}
	for _, want := range []string{"make the repo public", "existingProjectId", "abort"} {
		if !strings.Contains(visBlocker.Message, want) {
			t.Errorf("blocker must name option %q; got: %s", want, visBlocker.Message)
		}
	}
}
