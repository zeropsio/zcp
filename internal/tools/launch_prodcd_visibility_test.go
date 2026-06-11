package tools

import (
	"strings"
	"testing"

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
