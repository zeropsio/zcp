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

	self := actionsWorkflowYAML("weather", true, "main")
	if !strings.Contains(self, "-g") || !strings.Contains(self, `--setup "weather" -g`) {
		t.Errorf("self-target workflow must push with -g:\n%s", self)
	}
	if !strings.Contains(self, "persist-credentials: false") {
		t.Errorf("self-target workflow must disable checkout credential persistence:\n%s", self)
	}

	pair := actionsWorkflowYAML("prod", false, "main")
	if strings.Contains(pair, " -g") {
		t.Errorf("stage-targeting workflow must NOT ship .git (cross-build):\n%s", pair)
	}
	if strings.Contains(pair, "persist-credentials") {
		t.Errorf("stage-targeting workflow needs no checkout override:\n%s", pair)
	}
}

// TestBuildIntegration_ActionsTemplateUsesTrackedRef pins GF-7 (docs/spec-
// workflows.md §12.6): the emitted Actions workflow triggers on the
// SERVICE's recorded tracked ref, not a hardcoded "main" — every template
// variant (self-target, pair, and the compact wrapper-action variant).
func TestBuildIntegration_ActionsTemplateUsesTrackedRef(t *testing.T) {
	t.Parallel()

	self := actionsWorkflowYAML("weather", true, "release")
	if !strings.Contains(self, "branches: [release]") {
		t.Errorf("self-target workflow must trigger on the tracked ref 'release':\n%s", self)
	}

	pair := actionsWorkflowYAML("prod", false, "release")
	if !strings.Contains(pair, "branches: [release]") {
		t.Errorf("pair workflow must trigger on the tracked ref 'release':\n%s", pair)
	}

	wrapper := actionsSingleSetupWorkflowYAML("release")
	if !strings.Contains(wrapper, "branches: [release]") {
		t.Errorf("wrapper-action workflow must trigger on the tracked ref 'release':\n%s", wrapper)
	}
}

// TestActionsTemplate_VersionNameSHA pins GF-10 (docs/spec-workflows.md
// §12.6): the setup-aware zcli push line records the built commit via
// --version-name "$GITHUB_SHA" so SearchAppVersions.name is a platform-
// side breadcrumb independent of local session history. The compact
// wrapper-action variant cannot express it (zeropsio/actions exposes no
// version-name input) and must not claim to.
func TestActionsTemplate_VersionNameSHA(t *testing.T) {
	t.Parallel()

	self := actionsWorkflowYAML("weather", true, "main")
	if !strings.Contains(self, `zcli push --service-id "${{ secrets.ZEROPS_SERVICE_ID }}" --setup "weather" -g --version-name "$GITHUB_SHA"`) {
		t.Errorf("self-target zcli push must carry --version-name \"$GITHUB_SHA\":\n%s", self)
	}

	pair := actionsWorkflowYAML("prod", false, "main")
	if !strings.Contains(pair, `zcli push --service-id "${{ secrets.ZEROPS_SERVICE_ID }}" --setup "prod" --version-name "$GITHUB_SHA"`) {
		t.Errorf("pair zcli push must carry --version-name \"$GITHUB_SHA\":\n%s", pair)
	}

	wrapper := actionsSingleSetupWorkflowYAML("main")
	if strings.Contains(wrapper, "version-name") {
		t.Errorf("wrapper-action variant cannot express --version-name (no such zeropsio/actions input) and must not claim to:\n%s", wrapper)
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
