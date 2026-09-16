package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestActionsConfirm_NeverHandsOutTheContainerKey pins guide 0.9 / A6: the
// GitHub build-integration output must never name ZCP_API_KEY as the value of
// ZEROPS_TOKEN, and must never suggest reusing the container's key as a CI
// credential. The container's key is scoped to the Mate's own project and
// reaching CI with it hands every job that project. The replacement guidance:
// the person mints a token of their own — NO_ACCESS at the organization,
// BASIC_USER on the target project only — and `gh secret set` takes the value
// they paste, never a substitution that reads the container's environment.
func TestActionsConfirm_NeverHandsOutTheContainerKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rt   runtime.Info
	}{
		{name: "container", rt: runtime.Info{InContainer: true}},
		{name: "local", rt: runtime.Info{InContainer: false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
				Hostname:         "appdev",
				Mode:             topology.PlanModeStandard,
				StageHostname:    "appstage",
				GitPushState:     topology.GitPushConfigured,
				RemoteURL:        "https://github.com/example/demo.git",
				BootstrapSession: "test",
				BootstrappedAt:   "2026-09-16",
			}); err != nil {
				t.Fatalf("WriteServiceMeta: %v", err)
			}

			result, _, _ := handleBuildIntegration(context.Background(), nil, nil, "", WorkflowInput{
				Service:     "appdev",
				Integration: string(topology.BuildIntegrationActions),
			}, stateDir, tt.rt)
			if result.IsError {
				t.Fatalf("expected declared, got error: %s", getTextContent(t, result))
			}
			body := getTextContent(t, result)

			for _, forbidden := range []string{"ZCP_API_KEY", "reuse", "Reuse", "REUSE"} {
				if strings.Contains(body, forbidden) {
					t.Errorf("build-integration output must not carry %q: %s", forbidden, body)
				}
			}
			for _, want := range []string{
				"NO_ACCESS",
				"BASIC_USER",
				"gh secret set ZEROPS_TOKEN",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("build-integration output missing %q: %s", want, body)
				}
			}
		})
	}
}

// TestActionsConfirm_GiteaRemote_EmitsBrokerWorkflow pins guide 2.3 + 5.4: on
// the account's own Gitea the emitted workflow lives at
// `.gitea/workflows/zerops.yml` and deploys by ASKING the broker, with the
// job's own token. There is nothing to wire: no repository secret, no Zerops
// token, no zcli. Default permissions already grant the `actions: read` the
// broker's proof needs (it reads the job to learn which repository really
// called it), so the workflow declares none.
func TestActionsConfirm_GiteaRemote_EmitsBrokerWorkflow(t *testing.T) {
	t.Parallel()
	const giteaURL = "https://web-2ff4-3000.prg1.zerops.app"

	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "api",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "apistage",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        giteaURL + "/acme/api",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-09-16",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, _ := handleBuildIntegration(context.Background(), nil, nil, "", WorkflowInput{
		Service:     "api",
		Integration: string(topology.BuildIntegrationActions),
	}, stateDir, runtime.Info{InContainer: true, GitHostKnown: true, GiteaURL: giteaURL})
	if result.IsError {
		t.Fatalf("expected declared, got error: %s", getTextContent(t, result))
	}
	body := getTextContent(t, result)

	for _, want := range []string{
		".gitea/workflows/zerops.yml",
		"zeropsio/gitea-mate/actions/deploy@v1",
		"environment: stage",
		"service: api",
		"actions/checkout@v4",
		"branches: [main]",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("gitea confirm missing %q: %s", want, body)
		}
	}
	for _, forbidden := range []string{
		"secrets.",
		"ZEROPS_TOKEN",
		"zcli",
		".github/workflows",
		"gh secret set",
		"zeropsio/actions@",
		"permissions:",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("gitea confirm must not carry %q: %s", forbidden, body)
		}
	}
}

// The same remote host with no GITEA_URL in the environment is an
// unidentified forge, not the account's Gitea: the GitHub-shaped emission
// stands, because nothing has told ZCP a broker exists.
func TestActionsConfirm_UnknownHost_KeepsGitHubEmission(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "api",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "apistage",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://web-2ff4-3000.prg1.zerops.app/acme/api",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-09-16",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, _ := handleBuildIntegration(context.Background(), nil, nil, "", WorkflowInput{
		Service:     "api",
		Integration: string(topology.BuildIntegrationActions),
	}, stateDir, runtime.Info{InContainer: true})
	body := getTextContent(t, result)

	if !strings.Contains(body, ".github/workflows/zerops.yml") {
		t.Errorf("an unidentified host keeps the GitHub emission: %s", body)
	}
	if strings.Contains(body, ".gitea/workflows") {
		t.Errorf("an unidentified host must not be handed a broker workflow: %s", body)
	}
}
