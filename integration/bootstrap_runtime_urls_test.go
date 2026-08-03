// Tests for: integration — RCO-7 structured runtime-URL collection carried
// through the real MCP zerops_workflow tool (not a bypassed engine call).

package integration_test

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// runtimeURLsMock seeds a classic-route standard-mode pair (tmponb3adev /
// tmponb3astage) plus a managed postgresql dependency, matching the plan
// this file's test submits. Pre-seeding services this way (rather than
// driving a real zerops_import) is the established integration-test
// shortcut for exercising checkProvision without dynamic mock service
// creation — see bootstrap_realistic_test.go's bootstrapMock().
func runtimeURLsMock() *platform.Mock {
	return platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "myapp", Status: "ACTIVE", SubdomainHost: "24cb.prg1.zerops.app"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-dev", Name: "tmponb3adev", Status: "RUNNING",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
			{ID: "svc-stage", Name: "tmponb3astage", Status: "RUNNING", SubdomainAccess: true,
				Ports:                []platform.Port{{Port: 3000}},
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
			{ID: "svc-db", Name: "db", Status: "RUNNING",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
		}).
		WithServiceEnv("svc-db", []platform.ServiceEnvVar{
			{ID: "env-1", Key: "connectionString", Content: "postgresql://zerops:secret@db:5432/db"},
		})
}

// TestIntegration_BootstrapCloseActive_StatusCarriesRuntimeURLs pins RCO-7's
// "action=status while close remains active" contract: once provision
// completes and the session lands on the close step, the response that
// advanced it there AND a follow-up action="status" call must both carry
// the same structured runtime-URL collection — the stage entry marked as
// the handoff, resolved from the project-level subdomainHost — through the
// real MCP zerops_workflow tool end-to-end (not a bypassed engine call).
//
// Independent-oracle literal: hostname "tmponb3astage", project subdomain
// host "24cb", port 3000 → "https://tmponb3astage-24cb-3000.prg1.zerops.app"
// (live-verified observation cited in the slice brief) — hand-written here,
// not recomputed via the implementation's own BuildSubdomainURL formula.
func TestIntegration_BootstrapCloseActive_StatusCarriesRuntimeURLs(t *testing.T) {
	t.Parallel()

	const wantURL = "https://tmponb3astage-24cb-3000.prg1.zerops.app"

	session, cleanup := setupWorkflowServer(t, runtimeURLsMock())
	defer cleanup()

	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic",
		"intent": "nodejs app with postgres",
	})

	callAndGetText(t, session, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover",
		"plan": []map[string]any{
			{
				"runtime": map[string]any{
					"devHostname":   "tmponb3adev",
					"type":          "nodejs@22",
					"bootstrapMode": "standard",
					"stageHostname": "tmponb3astage",
				},
				"dependencies": []map[string]any{
					{"hostname": "db", "type": "postgresql@16", "mode": "NON_HA", "resolution": "CREATE"},
				},
			},
		},
	})

	provisionText := completeStep(t, session, "provision", "Completed provision")
	var provisionResp workflow.BootstrapResponse
	mustUnmarshal(t, provisionText, &provisionResp)
	assertStep(t, &provisionResp, "close", 2)
	assertRuntimeURLs(t, &provisionResp, wantURL)

	statusText := callAndGetText(t, session, "zerops_workflow", map[string]any{"action": "status"})
	var statusResp workflow.BootstrapResponse
	mustUnmarshal(t, statusText, &statusResp)
	assertStep(t, &statusResp, "close", 2)
	assertRuntimeURLs(t, &statusResp, wantURL)
}

func assertRuntimeURLs(t *testing.T, resp *workflow.BootstrapResponse, wantURL string) {
	t.Helper()
	var stage *workflow.RuntimeURL
	for i := range resp.RuntimeURLs {
		if resp.RuntimeURLs[i].Role == workflow.RuntimeURLRoleStage {
			stage = &resp.RuntimeURLs[i]
		}
	}
	if stage == nil {
		t.Fatalf("no stage entry in runtime URLs: %+v", resp.RuntimeURLs)
	}
	if stage.URL != wantURL {
		t.Errorf("stage URL = %q, want %q", stage.URL, wantURL)
	}
	if !stage.Handoff {
		t.Errorf("stage entry must be marked handoff: %+v", stage)
	}
	if resp.Current == nil || !strings.Contains(resp.Current.DetailedGuide, wantURL) {
		t.Errorf("close guide should carry the resolved stage URL, got current: %+v", resp.Current)
	}
}
