package tools

import (
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestRepoConfigFile_AnAgentIsOnlyEverToldTheReposOwnConfigFileName: once
// zcp has read a repo that keeps its config as zerops.yml, neither the
// develop guidance nor any knowledge it points to calls that file
// zerops.yaml — the agent has no name to rename it to — and a repo with
// zerops.yaml never hears zerops.yml. A GitHub Actions workflow file
// (.github/workflows/zerops.yml) is another file and keeps its name.
func TestRepoConfigFile_AnAgentIsOnlyEverToldTheReposOwnConfigFileName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		file, never string
	}{
		{file: "zerops.yml", never: "zerops.yaml"},
		{file: "zerops.yaml", never: "zerops.yml"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)
			if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
				Hostname: "appdev", Mode: topology.PlanModeDev,
				BootstrapSession: "sess1", BootstrappedAt: "2026-10-15",
			}); err != nil {
				t.Fatalf("WriteServiceMeta: %v", err)
			}
			mock := platform.NewMock().
				WithProject(&platform.Project{ID: "proj1", Name: "weather"}).
				WithServices([]platform.ServiceStack{{
					ID: "svc-appdev", Name: "appdev", Status: "ACTIVE",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"},
				}})
			repos := &partsRepos{out: map[string]string{
				"appdev": "@@file\n" + tc.file + "\n@@yaml\nzerops:\n  - setup: dev\n    run:\n      start: zsc noop\n@@declaration\n@@markers\n@@packages\n",
			}}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			rt := runtime.Info{InContainer: true, ServiceName: "zcp"}
			RegisterWorkflow(srv, mock, nil, "proj1", nil, engine, nil, dir, "zcp", nil, repos, rt, "")
			store, err := knowledge.GetEmbeddedStore()
			if err != nil {
				t.Fatalf("GetEmbeddedStore: %v", err)
			}
			RegisterKnowledge(srv, store, nil, nil, nil, engine)

			develop := callTool(t, srv, "zerops_workflow", map[string]any{
				"action": "start", "workflow": "develop", "intent": "get the weather app running", "scope": []string{"appdev"},
			})
			t.Cleanup(func() { _ = workflow.DeleteWorkSession(dir, os.Getpid()) })
			if develop.IsError {
				t.Fatalf("develop start failed: %s", getTextContent(t, develop))
			}
			told := map[string]string{"develop start": getTextContent(t, develop)}
			for name, args := range map[string]map[string]any{
				"core reference":    {"uri": "zerops://themes/core"},
				"monorepo playbook": {"uri": "zerops://playbooks/monorepo"},
				"scaffold atom":     {"uri": "zerops://atoms/develop-first-deploy-scaffold-yaml"},
				"search":            {"query": "zerops.yaml filename yml vs yaml"},
			} {
				told[name] = getTextContent(t, callTool(t, srv, "zerops_knowledge", args))
			}
			if !strings.Contains(told["develop start"], tc.file) {
				t.Errorf("develop start never names %s:\n%s", tc.file, told["develop start"])
			}
			for name, text := range told {
				for at := 0; ; {
					i := strings.Index(text[at:], tc.never)
					if i < 0 {
						break
					}
					i += at
					if !strings.HasSuffix(text[:i], "workflows/") {
						t.Errorf("%s tells a %s repo %q: …%s…", name, tc.file, tc.never, text[max(0, i-80):min(len(text), i+80)])
						break
					}
					at = i + len(tc.never)
				}
			}
		})
	}
}
