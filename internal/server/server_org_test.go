// Tests for: server.NewOrg — org mode's tool surface (docs/spec-mate.md §2.11).
package server

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// emptyOrg is an organization with nothing in it.
type emptyOrg struct{}

func (emptyOrg) OrgID() string                                            { return "org1" }
func (emptyOrg) ListProjects(context.Context) ([]platform.Project, error) { return nil, nil }
func (emptyOrg) ListServices(context.Context, string) ([]platform.ServiceStack, error) {
	return nil, nil
}
func (emptyOrg) GetProjectEnv(context.Context, string) ([]platform.ProjectEnvVar, error) {
	return nil, nil
}
func (emptyOrg) GetServiceEnv(context.Context, string) ([]platform.ServiceEnvVar, error) {
	return nil, nil
}
func (emptyOrg) GetProjectLog(context.Context, string) (*platform.LogAccess, error) {
	return &platform.LogAccess{}, nil
}
func (emptyOrg) ListPublicHTTPRoutings(context.Context, string) ([]platform.PublicHTTPRouting, error) {
	return nil, nil
}
func (emptyOrg) SearchProcesses(context.Context, string, int) ([]platform.ProcessEvent, error) {
	return nil, nil
}
func (emptyOrg) SearchAppVersions(context.Context, string, int) ([]platform.AppVersionEvent, error) {
	return nil, nil
}
func (emptyOrg) ListActiveServiceTypeVersions(context.Context) ([]string, error) { return nil, nil }
func (emptyOrg) GetPerson(context.Context) (platform.OrgPerson, error) {
	return platform.OrgPerson{}, nil
}
func (emptyOrg) ListPortRoutings(context.Context, string) ([]platform.OrgPortRouting, error) {
	return nil, nil
}
func (emptyOrg) SearchCosts(context.Context) (*platform.OrgCosts, error) {
	return &platform.OrgCosts{}, nil
}
func (emptyOrg) SearchCostBreakdown(context.Context, platform.OrgCostQuery) ([]platform.OrgCostLine, error) {
	return nil, nil
}
func (emptyOrg) SearchResourceUse(context.Context, string, time.Time, time.Time, string) ([]platform.OrgResourceUse, error) {
	return nil, nil
}

// orgTools is org mode's own surface.
var orgTools = []string{
	"zerops_org_overview", "zerops_org_services", "zerops_org_logs", "zerops_org_failures",
	"zerops_org_env", "zerops_org_processes", "zerops_org_costs", "zerops_org_usage",
	"zerops_org_propose_service", "zerops_org_propose_env", "zerops_org_propose_message",
	"zerops_org_propose_domain", "zerops_org_propose_access",
}

func listOrgServerTools(t *testing.T, rt runtime.Info) []string {
	t.Helper()
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	srv := NewOrg(context.Background(), emptyOrg{}, "localhost", store, platform.NewMockLogFetcher(), rt, nil)
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.MCPServer().Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0.1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

// TestNewOrg_RegistersTheOrgSurfaceOnly: in org mode the Chief gets the
// org tools and platform knowledge. The single-project surface — workflows,
// deploys, discover, env, manage, import, delete… — acts on its own empty
// project and is not registered.
func TestNewOrg_RegistersTheOrgSurfaceOnly(t *testing.T) {
	// Non-parallel: t.Chdir rebases the state dir NewOrg derives.
	t.Chdir(t.TempDir())
	tests := []struct {
		name string
		rt   runtime.Info
		want []string
	}{
		{
			name: "local",
			rt:   runtime.Info{Chief: true},
			want: append(slices.Clone(orgTools), "zerops_knowledge"),
		},
		{
			name: "a Mate's container draws pages",
			rt:   runtime.Info{Chief: true, InContainer: true, ServiceID: "s1", MateEnabled: true},
			want: append(slices.Clone(orgTools), "zerops_knowledge", "zerops_publish_page"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := listOrgServerTools(t, tt.rt)
			slices.Sort(tt.want)
			if !slices.Equal(got, tt.want) {
				t.Errorf("tools = %v\nwant    %v", got, tt.want)
			}
			for _, name := range defaultExpectedTools {
				if name != "zerops_knowledge" && slices.Contains(got, name) {
					t.Errorf("single-project tool %s registered in org mode", name)
				}
			}
		})
	}
}

// TestNewOrg_RefreshesTheChiefSection: org mode keeps AGENTS.md's
// managed section on the Chief's guidance.
func TestNewOrg_RefreshesTheChiefSection(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	agents := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(agents, []byte("<!-- ZCP:BEGIN -->\nold\n<!-- ZCP:END -->\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	listOrgServerTools(t, runtime.Info{Chief: true})
	body, err := os.ReadFile(agents)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "zerops_org_overview") || strings.Contains(string(body), "\nold\n") {
		t.Errorf("AGENTS.md not refreshed to the Chief section:\n%s", body)
	}
}
