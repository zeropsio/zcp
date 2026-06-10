package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestActionsWorkflowYAML_SelfTargetParity pins the single .git-in-artifact
// predicate (ops.SelfBuildTarget, spec-git-delivery-target §5) on the
// emitted CI template: a self-targeting service's workflow must ship .git
// (`zcli push … -g`) and must not persist the checkout token into the
// .git/config the artifact carries; a pair's stage-targeting workflow must
// stay git-less (cross-build semantics — ZCP never reads git from stage).
// Without -g every CI build of a self-target wiped /var/www/.git — the
// prod.txt T2 degradation spiral.
func TestActionsWorkflowYAML_SelfTargetParity(t *testing.T) {
	t.Parallel()

	self := actionsWorkflowYAML("weather", true)
	if !strings.Contains(self, "-g") || !strings.Contains(self, `--setup "weather" -g`) {
		t.Errorf("self-target workflow must push with -g:\n%s", self)
	}
	if !strings.Contains(self, "persist-credentials: false") {
		t.Errorf("self-target workflow must disable checkout credential persistence:\n%s", self)
	}

	pair := actionsWorkflowYAML("prod", false)
	if strings.Contains(pair, " -g") {
		t.Errorf("stage-targeting workflow must NOT ship .git (cross-build):\n%s", pair)
	}
	if strings.Contains(pair, "persist-credentials") {
		t.Errorf("stage-targeting workflow needs no checkout override:\n%s", pair)
	}
}

// TestHandleBuildIntegration_SelfTarget_NoSingleSetupVariant pins that the
// zeropsio/actions wrapper variant (which cannot express -g) is not offered
// for self-targeting services, and that the emitted default carries -g.
func TestHandleBuildIntegration_SelfTarget_NoSingleSetupVariant(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "weather",
		Mode:             topology.PlanModeSimple,
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/example/weather.git",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-06-10",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, _ := handleBuildIntegration(context.Background(), nil, nil, "", WorkflowInput{
		Service:     "weather",
		Integration: string(topology.BuildIntegrationActions),
	}, stateDir, runtime.Info{InContainer: true})
	body := getTextContent(t, result)

	if strings.Contains(body, "single-setup-action") || strings.Contains(body, "zeropsio/actions@") {
		t.Errorf("self-target confirm must not offer the wrapper variant (cannot express -g): %s", body)
	}
	if !strings.Contains(body, `-g`) || !strings.Contains(body, "persist-credentials: false") {
		t.Errorf("self-target confirm must emit the -g + persist-credentials:false workflow: %s", body)
	}
}
