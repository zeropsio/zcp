package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// non-parallel: the status-file environment is shared by the process.
// witness: internal/tools/workflow_bootstrap.go | !mateEnabled && role == workflow.RuntimeURLRoleStage → role == workflow.RuntimeURLRoleStage
func TestBootstrapResponse_BRUrls_MateCompletionAndStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(mate.EnvStatusFile, filepath.Join(dir, "status.json"))
	eng := workflow.NewEngine(dir, workflow.EnvContainer, nil)
	seedBootstrapPlanAtClose(t, eng, []workflow.BootstrapTarget{{Runtime: workflow.RuntimeTarget{
		DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "standard", ExplicitStage: "appstage",
	}}})
	client := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", SubdomainHost: "24cb.prg1.zerops.app"}).
		WithServices([]platform.ServiceStack{{ID: "svc-stage", Name: "appstage", SubdomainAccess: true, Ports: []platform.Port{{Port: 3000}}}})
	rt := runtime.Info{MateEnabled: true}
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		call func() (*mcp.CallToolResult, any, error)
	}{
		{"status", func() (*mcp.CallToolResult, any, error) {
			return handleBootstrapStatus(ctx, eng, client, "proj-1", rt, nil)
		}},
		{"completion", func() (*mcp.CallToolResult, any, error) {
			return handleBootstrapComplete(ctx, eng, client, nil, nil, WorkflowInput{Step: workflow.StepClose, Attestation: "bootstrap completed"}, nil, "proj-1", dir, nil, nil, rt, nil)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, _, err := tc.call()
			if err != nil || result == nil {
				t.Fatalf("bootstrap %s: result=%+v error=%v", tc.name, result, err)
			}
			if result.IsError {
				t.Fatalf("bootstrap %s: %s", tc.name, getTextContent(t, result))
			}
			var resp workflow.BootstrapResponse
			if err := json.Unmarshal([]byte(getTextContent(t, result)), &resp); err != nil {
				t.Fatalf("decode bootstrap %s: %v", tc.name, err)
			}
			if len(resp.RuntimeURLs) != 1 || resp.RuntimeURLs[0].Role != "stage" || resp.RuntimeURLs[0].Handoff {
				t.Fatalf("runtime URLs = %+v, want a stage without a handoff", resp.RuntimeURLs)
			}
			text := resp.Message
			if resp.Current != nil {
				text = resp.Current.DetailedGuide
			}
			_, guide, ok := strings.Cut(text, "## Runtime URLs\n\n")
			if !ok {
				t.Fatalf("bootstrap %s lacks runtime URL guidance: %s", tc.name, text)
			}
			for _, want := range []string{
				"Once the draft is ready, tell the person they can open it in Mate.",
				"stage: runs what was added to main; the Review build builds a change on it",
				"https://appstage-24cb-3000.prg1.zerops.app",
			} {
				if !strings.Contains(guide, want) {
					t.Errorf("bootstrap %s guide lacks %q: %s", tc.name, want, guide)
				}
			}
			for _, forbidden := range []string{"Hand this URL back", "preview", "handoff"} {
				if strings.Contains(strings.ToLower(guide), strings.ToLower(forbidden)) {
					t.Errorf("bootstrap %s guide contains %q: %s", tc.name, forbidden, guide)
				}
			}
		})
	}
}
